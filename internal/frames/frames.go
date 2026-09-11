// Package frames 把一份錄影重播進終端模擬器，在正確的時間點抓出影格。
//
// 這一層吃掉所有跟「時間」有關的處理——區間裁切、靜止壓縮、播放倍率、事件合併——
// 讓 render 那層只需要面對「一連串帶時間戳的畫面」。
//
// ⭐ 串流，不是一次回傳整個 slice：影格是這個管線裡最大的東西
// （每張是 cols×rows 個 Cell），全部留在記憶體就會變成 svg-term-cli 那個
// heap out of memory 的老問題。Capture 產一張就交出去一張，
// 呼叫端用完即丟，記憶體用量與影片長度無關。
package frames

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/term"
)

// Options 是時間軸的處理參數，對應 CLI 旗標。
type Options struct {
	// From / To 以**原始錄影時間**（秒）裁切區間。To 為 0 表示到結尾。
	// From 之前的事件仍會餵進模擬器建立畫面狀態，只是不產生影格——
	// 直接跳過會讓畫面內容是錯的。
	From, To float64

	// Speed 是播放倍率。輸出時間軸整個除以它。
	Speed float64

	// IdleTimeLimit 把超過這個秒數的靜止壓成這個長度。0 表示不壓。
	// 對應 svg-term-cli 那個沒人做的 issue「Support for asciicast idle_time_limit」。
	IdleTimeLimit float64

	// MergeWindow 內的連續事件併成同一張影格。
	// asciinema 常把一次 write 拆成好幾個事件，不合併會產生大量無意義影格。
	MergeWindow float64

	// MaxEvents 是安全閥。超過就報錯，而不是吃光記憶體。
	MaxEvents int
}

// DefaultOptions 是不給任何旗標時的行為。
func DefaultOptions() Options {
	return Options{
		Speed:       1.0,
		MergeWindow: 0.001, // 1 ms
		MaxEvents:   200_000,
	}
}

func (o Options) normalized() Options {
	if o.Speed <= 0 {
		o.Speed = 1.0
	}
	if o.MergeWindow < 0 {
		o.MergeWindow = 0
	}
	if o.MaxEvents <= 0 {
		o.MaxEvents = 200_000
	}
	return o
}

// Emit 是每產生一張影格就被呼叫一次的回呼。回傳 error 會中止擷取。
type Emit func(term.Frame) error

// Capture 重播 c，把影格交給 emit。
//
// 影格的 Time 是**輸出時間軸**上的秒數：從 0 起算，已套用 From 位移、
// 靜止壓縮與播放倍率。render 那層可以直接拿來排 keyframes，不必再換算。
func Capture(c *cast.Cast, opts Options, emit Emit) error {
	opts = opts.normalized()

	if opts.To > 0 && opts.To <= opts.From {
		return fmt.Errorf("--to (%.3fs) must be later than --from (%.3fs)", opts.To, opts.From)
	}
	if n := len(c.Events); n > opts.MaxEvents {
		return fmt.Errorf("%d events exceeds the limit of %d; trim with --from/--to",
			n, opts.MaxEvents)
	}

	em := term.NewEmulator(c.Header.Width, c.Header.Height)

	// idleLimit 旗標優先於 header 的 idle_time_limit。
	idleLimit := opts.IdleTimeLimit
	if idleLimit == 0 {
		idleLimit = c.Header.IdleTimeLimit
	}

	var (
		outT     float64 // 輸出時間軸上的目前位置（尚未除以 speed）
		prevOrig float64 // 上一個被計入輸出時間軸的事件，其原始時間
		started  bool    // 是否已經越過 From
		emitted  bool
	)

	// flush 把目前的畫面狀態當成一張影格交出去。
	flush := func(t float64) error {
		f := em.Snapshot()
		f.Time = t / opts.Speed
		emitted = true
		return emit(f)
	}

	for i := 0; i < len(c.Events); i++ {
		ev := c.Events[i]

		switch ev.Type {
		case cast.Output, cast.Resize:
		default:
			continue // 輸入事件與標記在 v1 一律忽略
		}

		if opts.To > 0 && ev.Time > opts.To {
			break
		}

		// 還沒到 From：餵進模擬器建立狀態，但不產生影格。
		if ev.Time < opts.From {
			if err := apply(em, ev); err != nil {
				return err
			}
			continue
		}

		// 推進輸出時間軸。
		if !started {
			started = true
			outT = 0
		} else {
			gap := ev.Time - prevOrig
			if gap < 0 {
				gap = 0 // 時間戳倒退的壞檔案：當成同時發生，不要讓時間軸倒退
			}
			if idleLimit > 0 && gap > idleLimit {
				gap = idleLimit
			}
			outT += gap
		}
		prevOrig = ev.Time

		if err := apply(em, ev); err != nil {
			return err
		}

		// 合併：把後面所有落在 MergeWindow 內的事件一起吃掉，只出一張影格。
		for i+1 < len(c.Events) {
			next := c.Events[i+1]
			if next.Type != cast.Output && next.Type != cast.Resize {
				i++
				continue
			}
			if opts.To > 0 && next.Time > opts.To {
				break
			}
			if next.Time-prevOrig > opts.MergeWindow {
				break
			}
			if err := apply(em, next); err != nil {
				return err
			}
			prevOrig = next.Time
			i++
		}

		if err := flush(outT); err != nil {
			return err
		}
	}

	// From 落在錄影結束之後，或整份錄影都被裁掉：至少給一張終態影格，
	// 這樣呼叫端不必特別處理「零影格」。
	if !emitted {
		return flush(0)
	}
	return nil
}

func apply(em term.Emulator, ev cast.Event) error {
	switch ev.Type {
	case cast.Output:
		if _, err := em.Write([]byte(ev.Data)); err != nil {
			return fmt.Errorf("writing to emulator at t=%.3f: %w", ev.Time, err)
		}
	case cast.Resize:
		cols, rows, err := parseResize(ev.Data)
		if err != nil {
			return fmt.Errorf("resize event at t=%.3f: %w", ev.Time, err)
		}
		em.Resize(cols, rows)
	}
	return nil
}

// parseResize 解析 "80x24"。
func parseResize(s string) (cols, rows int, err error) {
	x := strings.IndexByte(s, 'x')
	if x < 0 {
		return 0, 0, fmt.Errorf("expected COLSxROWS, got %q", s)
	}
	if cols, err = strconv.Atoi(s[:x]); err != nil {
		return 0, 0, fmt.Errorf("columns %q: %w", s[:x], err)
	}
	if rows, err = strconv.Atoi(s[x+1:]); err != nil {
		return 0, 0, fmt.Errorf("rows %q: %w", s[x+1:], err)
	}
	if cols <= 0 || rows <= 0 {
		return 0, 0, fmt.Errorf("size must be positive, got %dx%d", cols, rows)
	}
	return cols, rows, nil
}

package frames

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/term"
)

// collect 跑一次 Capture，把影格全收進 slice（測試用；正式路徑是串流的）。
func collect(t *testing.T, c *cast.Cast, opts Options) []term.Frame {
	t.Helper()
	var out []term.Frame
	if err := Capture(c, opts, func(f term.Frame) error {
		out = append(out, f)
		return nil
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return out
}

// build 從逐字的事件描述組出一份 cast，測試時間軸邏輯用。
func build(events ...cast.Event) *cast.Cast {
	return &cast.Cast{
		Header: cast.Header{Version: 2, Width: 20, Height: 3},
		Events: events,
	}
}

func out(t float64, data string) cast.Event {
	return cast.Event{Time: t, Type: cast.Output, Data: data}
}

func rowText(f term.Frame, y int) string {
	var b strings.Builder
	for _, c := range f.Rows[y] {
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestCapture_Hello(t *testing.T) {
	f, err := os.Open("../../testdata/hello.cast")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := cast.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	got := collect(t, c, DefaultOptions())
	if len(got) == 0 {
		t.Fatal("沒有產生任何影格")
	}
	// 輸出時間軸從 0 起算
	if !nearly(got[0].Time, 0) {
		t.Errorf("第一張影格 Time=%v，預期 0", got[0].Time)
	}
	// 總長 = 最後事件 - 第一個事件 = 2.4 - 0.1
	if want := 2.3; !nearly(got[len(got)-1].Time, want) {
		t.Errorf("最後一張影格 Time=%v，預期 %v", got[len(got)-1].Time, want)
	}
	// 終態畫面應該看得到 hello
	last := got[len(got)-1]
	var found bool
	for y := range last.Rows {
		if strings.Contains(rowText(last, y), "hello") {
			found = true
		}
	}
	if !found {
		t.Error("終態畫面裡找不到 \"hello\"")
	}
	t.Logf("影格數 %d，總長 %.3fs", len(got), last.Time)
}

// 事件合併：MergeWindow 內的連續事件只出一張影格。
func TestCapture_MergesCloseEvents(t *testing.T) {
	c := build(
		out(0.000, "a"),
		out(0.0002, "b"), // 0.2ms —— 應被併入上一張
		out(0.0004, "c"), // 同上
		out(1.000, "d"),  // 遠遠之後，另一張
	)
	got := collect(t, c, DefaultOptions())
	if len(got) != 2 {
		t.Fatalf("影格數 = %d，預期 2（abc 合併成一張、d 一張）", len(got))
	}
	if s := rowText(got[0], 0); s != "abc" {
		t.Errorf("第一張影格內容 = %q，預期 \"abc\"", s)
	}
	if s := rowText(got[1], 0); s != "abcd" {
		t.Errorf("第二張影格內容 = %q，預期 \"abcd\"", s)
	}
}

// 靜止壓縮：超過 limit 的空檔被壓成 limit，後面的時間整體前移。
func TestCapture_IdleCompression(t *testing.T) {
	c := build(
		out(0, "a"),
		out(10, "b"), // 10 秒空檔
		out(11, "c"), // 1 秒空檔，不受影響
	)
	opts := DefaultOptions()
	opts.IdleTimeLimit = 2

	got := collect(t, c, opts)
	if len(got) != 3 {
		t.Fatalf("影格數 = %d，預期 3", len(got))
	}
	want := []float64{0, 2, 3} // 10s 被壓成 2s；之後的 1s 保持
	for i, w := range want {
		if !nearly(got[i].Time, w) {
			t.Errorf("影格 %d 的 Time=%v，預期 %v", i, got[i].Time, w)
		}
	}
}

// header 裡的 idle_time_limit 要生效，且旗標優先。
func TestCapture_IdleFromHeader(t *testing.T) {
	c := build(out(0, "a"), out(10, "b"))
	c.Header.IdleTimeLimit = 3

	got := collect(t, c, DefaultOptions())
	if !nearly(got[1].Time, 3) {
		t.Errorf("header 的 idle_time_limit 沒生效：Time=%v，預期 3", got[1].Time)
	}

	opts := DefaultOptions()
	opts.IdleTimeLimit = 1 // 旗標應覆寫 header
	got = collect(t, c, opts)
	if !nearly(got[1].Time, 1) {
		t.Errorf("旗標沒有覆寫 header：Time=%v，預期 1", got[1].Time)
	}
}

func TestCapture_Speed(t *testing.T) {
	c := build(out(0, "a"), out(2, "b"), out(4, "c"))
	opts := DefaultOptions()
	opts.Speed = 2

	got := collect(t, c, opts)
	want := []float64{0, 1, 2}
	for i, w := range want {
		if !nearly(got[i].Time, w) {
			t.Errorf("影格 %d 的 Time=%v，預期 %v（2 倍速）", i, got[i].Time, w)
		}
	}
}

// ⭐ From 之前的事件必須餵進模擬器建立畫面狀態，只是不產生影格。
// 直接跳過的話畫面內容會是錯的——這是這一層最容易寫錯的地方。
func TestCapture_FromKeepsPriorState(t *testing.T) {
	c := build(
		out(0, "prefix-"), // 在 From 之前，但畫面上必須留著
		out(5, "after"),
	)
	opts := DefaultOptions()
	opts.From = 1

	got := collect(t, c, opts)
	if len(got) != 1 {
		t.Fatalf("影格數 = %d，預期 1", len(got))
	}
	if s := rowText(got[0], 0); s != "prefix-after" {
		t.Errorf("內容 = %q，預期 \"prefix-after\"——From 之前的輸出被丟掉了", s)
	}
	if !nearly(got[0].Time, 0) {
		t.Errorf("Time=%v，預期 0（輸出時間軸應從 From 之後的第一個事件起算）", got[0].Time)
	}
}

func TestCapture_To(t *testing.T) {
	c := build(out(0, "a"), out(1, "b"), out(9, "c"))
	opts := DefaultOptions()
	opts.To = 2

	got := collect(t, c, opts)
	if len(got) != 2 {
		t.Fatalf("影格數 = %d，預期 2（t=9 應被裁掉）", len(got))
	}
	if s := rowText(got[len(got)-1], 0); s != "ab" {
		t.Errorf("內容 = %q，預期 \"ab\"", s)
	}
}

// From 超過錄影總長：仍應給一張終態影格，呼叫端不必特別處理零影格。
func TestCapture_FromBeyondEnd(t *testing.T) {
	c := build(out(0, "a"), out(1, "b"))
	opts := DefaultOptions()
	opts.From = 100

	got := collect(t, c, opts)
	if len(got) != 1 {
		t.Fatalf("影格數 = %d，預期 1 張終態影格", len(got))
	}
	if s := rowText(got[0], 0); s != "ab" {
		t.Errorf("內容 = %q，預期終態 \"ab\"", s)
	}
}

func TestCapture_Resize(t *testing.T) {
	c := build(
		out(0, "a"),
		cast.Event{Time: 1, Type: cast.Resize, Data: "40x5"},
		out(2, "b"),
	)
	got := collect(t, c, DefaultOptions())
	last := got[len(got)-1]
	if len(last.Rows) != 5 || len(last.Rows[0]) != 40 {
		t.Errorf("resize 之後的尺寸 = %dx%d，預期 40x5", len(last.Rows[0]), len(last.Rows))
	}
}

func TestCapture_Errors(t *testing.T) {
	cases := []struct {
		name string
		c    *cast.Cast
		opts func(Options) Options
		want string
	}{
		{
			"to 早於 from",
			build(out(0, "a")),
			func(o Options) Options { o.From, o.To = 5, 1; return o },
			"must be later than",
		},
		{
			"事件數超過上限",
			build(out(0, "a"), out(1, "b"), out(2, "c")),
			func(o Options) Options { o.MaxEvents = 2; return o },
			"exceeds the limit",
		},
		{
			"resize 格式錯誤",
			build(cast.Event{Time: 0, Type: cast.Resize, Data: "garbage"}),
			func(o Options) Options { return o },
			"expected COLSxROWS",
		},
		{
			"resize 尺寸為零",
			build(cast.Event{Time: 0, Type: cast.Resize, Data: "0x24"}),
			func(o Options) Options { return o },
			"must be positive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Capture(tc.c, tc.opts(DefaultOptions()), func(term.Frame) error { return nil })
			if err == nil {
				t.Fatal("預期出錯，卻成功了")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("錯誤訊息 = %q，預期含有 %q", err, tc.want)
			}
		})
	}
}

// emit 回傳 error 必須中止擷取，而不是被忽略。
func TestCapture_EmitErrorAborts(t *testing.T) {
	c := build(out(0, "a"), out(1, "b"), out(2, "c"))
	sentinel := fmt.Errorf("停")
	n := 0
	err := Capture(c, DefaultOptions(), func(term.Frame) error {
		n++
		return sentinel
	})
	if err == nil || !strings.Contains(err.Error(), "停") {
		t.Errorf("err = %v，預期把 emit 的錯誤傳出來", err)
	}
	if n != 1 {
		t.Errorf("emit 被呼叫 %d 次，預期第一次出錯就停", n)
	}
}

// 時間戳倒退的壞檔案不能讓輸出時間軸倒退。
func TestCapture_NonMonotonicTimestamps(t *testing.T) {
	c := build(out(0, "a"), out(5, "b"), out(3, "c"))
	got := collect(t, c, DefaultOptions())
	for i := 1; i < len(got); i++ {
		if got[i].Time < got[i-1].Time {
			t.Errorf("影格 %d 的時間 %v 早於前一張 %v", i, got[i].Time, got[i-1].Time)
		}
	}
}

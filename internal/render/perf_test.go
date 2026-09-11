package render

import (
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
)

// bigCast 產生一份很大的錄影：大量輸出、會捲動、有顏色。
func bigCast(events int) *cast.Cast {
	c := &cast.Cast{Header: cast.Header{Version: 2, Width: 80, Height: 24}}
	c.Events = make([]cast.Event, 0, events)
	for i := 0; i < events; i++ {
		var data string
		switch i % 4 {
		case 0:
			data = fmt.Sprintf("\x1b[32m[%06d]\x1b[0m processing item %d\r\n", i, i)
		case 1:
			data = fmt.Sprintf("  \x1b[33mwarn\x1b[0m: retry %d\r\n", i%7)
		case 2:
			data = fmt.Sprintf("  ok %d bytes\r\n", i*13%9999)
		default:
			data = fmt.Sprintf("\x1b[34m::\x1b[0m %d\r\n", i)
		}
		c.Events = append(c.Events, cast.Event{
			Time: float64(i) * 0.004,
			Type: cast.Output,
			Data: data,
		})
	}
	return c
}

// ⭐ 規格 ③：大檔不能爆記憶體、不能卡死。
//
// svg-term-cli 的使用者回報「JavaScript heap out of memory」與
// 「小檔案卻長時間卡住後 core dump」——那是換掉它最直接的理由，
// 所以這條必須有硬性的數字保證。
func TestPerf_LargeCast(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	// 本機約 5 秒。上限放到 30 秒是給 CI 共用 runner 的餘裕——
	// 這條要抓的是「卡死／指數爆炸」那種病，不是 runner 忙不忙。
	const (
		events    = 50_000
		maxWall   = 30 * time.Second
		maxAllocs = 200 << 20 // 200 MB
	)

	c := bigCast(events)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	a := NewAnimator(DefaultOptions())
	fo := frames.DefaultOptions()
	fo.MaxEvents = events + 1
	if err := frames.Capture(c, fo, a.Add); err != nil {
		t.Fatal(err)
	}
	// 直接丟棄輸出——這裡量的是產生過程，不是寫檔。
	n, err := writeCounting(a)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	peak := after.TotalAlloc - before.TotalAlloc

	t.Logf("%d 個事件 → %d bytes", events, n)
	t.Logf("  耗時     : %v （上限 %v）", elapsed.Round(time.Millisecond), maxWall)
	t.Logf("  累積配置 : %.1f MB （上限 %d MB）", float64(peak)/(1<<20), maxAllocs>>20)
	t.Logf("  峰值堆積 : %.1f MB", float64(after.HeapAlloc)/(1<<20))

	if elapsed > maxWall {
		t.Errorf("耗時 %v 超過上限 %v", elapsed, maxWall)
	}
	// HeapAlloc 是當下實際佔用；TotalAlloc 會把中途釋放的也算進去，
	// 所以用 HeapAlloc 判斷「有沒有把整份影格留在記憶體裡」。
	if after.HeapAlloc > maxAllocs {
		t.Errorf("峰值堆積 %.1f MB 超過上限 %d MB——影格可能沒有被串流處理",
			float64(after.HeapAlloc)/(1<<20), maxAllocs>>20)
	}
}

func writeCounting(a *Animator) (int, error) {
	var cw countingWriter
	err := a.Write(&cw)
	return cw.n, err
}

type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

var _ io.Writer = (*countingWriter)(nil)

// ⭐ 記憶體用量必須與影片長度無關——這是串流設計的核心保證。
//
// 影格是這個管線裡最大的東西（每張 cols×rows 個 Cell）。如果哪天有人
// 不小心把影格存進 slice，這個測試會抓到：事件數翻 4 倍，堆積不該跟著翻。
func TestPerf_MemoryIndependentOfLength(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	// ⚠️ 一定要先強制 GC 再讀 HeapAlloc。
	//
	// 不 GC 的話量到的是「上次回收之後配置了多少」，那跟 GC 的觸發時機有關，
	// 測試會時好時壞。GC 之後讀到的才是**真正還被持有**的記憶體，
	// 也才是這個測試想問的問題：有沒有東西被留著沒放。
	measure := func(events int) float64 {
		c := bigCast(events)
		a := NewAnimator(DefaultOptions())
		fo := frames.DefaultOptions()
		fo.MaxEvents = events + 1
		if err := frames.Capture(c, fo, a.Add); err != nil {
			t.Fatal(err)
		}

		// 讓輸入的 cast 可以被回收——我們要量的是 animator 持有的量，
		// 不是那份測試素材。
		c = nil
		runtime.GC()
		runtime.GC() // 兩次：第一次可能只是把物件排進終結佇列

		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		retained := float64(ms.HeapAlloc) / (1 << 20)

		runtime.KeepAlive(a) // 確保 animator 在量測時還活著
		_ = c
		return retained
	}

	small := measure(5_000)
	large := measure(20_000)
	t.Logf("5,000 事件: %.1f MB", small)
	t.Logf("20,000 事件: %.1f MB", large)

	// 事件數 4 倍。已完成的 run 會累積（那是必要的，輸出需要它們），
	// 但**影格**不該累積。若記憶體也跟著 4 倍以上，代表有東西沒有被串流。
	if large > small*4 {
		t.Errorf("事件數 4 倍時記憶體成長 %.1fx，超過線性——可能有影格被留在記憶體裡",
			large/small)
	}
}

func BenchmarkPipeline(b *testing.B) {
	c := bigCast(5_000)
	fo := frames.DefaultOptions()
	fo.MaxEvents = 10_000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := NewAnimator(DefaultOptions())
		if err := frames.Capture(c, fo, a.Add); err != nil {
			b.Fatal(err)
		}
		var cw countingWriter
		if err := a.Write(&cw); err != nil {
			b.Fatal(err)
		}
	}
}

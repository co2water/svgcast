package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
	"github.com/co2water/svgcast/internal/term"
)

// scrollCast 產生一份必定會捲動的錄影：往 5 列高的終端印 12 行。
func scrollCast(rows, lines int) *cast.Cast {
	c := &cast.Cast{Header: cast.Header{Version: 2, Width: 20, Height: rows}}
	for i := 0; i < lines; i++ {
		c.Events = append(c.Events, cast.Event{
			Time: float64(i) * 0.1,
			Type: cast.Output,
			Data: fmt.Sprintf("line%02d\r\n", i),
		})
	}
	return c
}

// 先確認終端真的有捲動——不然後面的測試都是空的。
func TestScroll_TerminalActuallyScrolls(t *testing.T) {
	c := scrollCast(5, 12)
	var last term.Frame
	if err := frames.Capture(c, frames.DefaultOptions(), func(f term.Frame) error {
		last = f
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for y := range last.Rows {
		s := strings.TrimRight(rowString(last, y), " ")
		got = append(got, s)
	}
	t.Logf("終態畫面 %d 列: %q", len(got), got)

	// 印了 12 行到 5 列高的終端，最後應該只看得到最後幾行
	if strings.Contains(strings.Join(got, "|"), "line00") {
		t.Error("line00 還在畫面上——終端沒有捲動，這份測試素材無效")
	}
}

func rowString(f term.Frame, y int) string {
	var b strings.Builder
	for _, c := range f.Rows[y] {
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}

// ⭐ 捲動必須被偵測到，而且要轉成 viewport 平移而不是整螢幕重開。
func TestScroll_Detected(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	if err := frames.Capture(scrollCast(5, 12), frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}

	if len(a.scrollTrack) == 0 {
		t.Fatal("完全沒有偵測到捲動——detectScroll 沒有生效")
	}
	t.Logf("偵測到 %d 次捲動，累積 %d 列", len(a.scrollTrack), a.scrollOff)

	// 12 行印進 5 列的終端，應該捲掉約 7 列
	if a.scrollOff < 5 {
		t.Errorf("累積捲動 %d 列，預期至少 5 列", a.scrollOff)
	}
}

// 捲動之後，每一行內容應該只出現一次——不是每捲一次就重畫一遍。
func TestScroll_ContentNotDuplicated(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	if err := frames.Capture(scrollCast(5, 12), frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()

	for i := 0; i < 12; i++ {
		want := fmt.Sprintf("line%02d", i)
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("%q 在輸出裡出現 %d 次，預期剛好 1 次（捲動時不該重畫）", want, n)
		}
	}
	if !strings.Contains(out, "@keyframes vp") {
		t.Error("缺少 viewport 平移動畫")
	}
	t.Logf("輸出 %d bytes", len(out))
}

// viewport 的平移量必須跟累積捲動列數對得上。
func TestScroll_ViewportOffsetMatchesLines(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	if err := frames.Capture(scrollCast(5, 12), frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}
	g := opts.geom(20, 5)
	want := fnum(-float64(a.scrollOff) * g.lineH)

	if !strings.Contains(b.String(), "translateY("+want+"px)") {
		t.Errorf("找不到最終平移量 translateY(%spx)（累積捲動 %d 列）", want, a.scrollOff)
	}
}

// 清空畫面不可以被誤判成捲動。
func TestScroll_ClearIsNotScroll(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	c := &cast.Cast{Header: cast.Header{Version: 2, Width: 20, Height: 5}}
	c.Events = []cast.Event{
		{Time: 0, Type: cast.Output, Data: "hello"},
		{Time: 1, Type: cast.Output, Data: "\x1b[2J\x1b[H"}, // 清空 + 游標歸位
	}

	a := NewAnimator(opts)
	if err := frames.Capture(c, frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}
	if len(a.scrollTrack) != 0 {
		t.Errorf("清空畫面被誤判成捲動 %d 次", len(a.scrollTrack))
	}
}

package render

import (
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
	"github.com/co2water/svgcast/internal/term"
)

// frameWith 造一張只有第 0 列有內容的影格。
func frameWith(t float64, rows int, cols int, text string) term.Frame {
	f := blankFrame(rows, cols)
	f.Time = t
	setText(&f, 0, 0, text)
	return f
}

func animate(t *testing.T, fs []term.Frame, opts Options) string {
	t.Helper()
	var b strings.Builder
	if err := Render(&b, fs, opts); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

// ── ⭐ 差分：前綴保留 ──────────────────────────────────────

// 逐字打字時，已經畫好的部分不可以被關掉重開。
// 沒有這個行為的話，一行 40 個字會產生 40×40 個元素——
// 那正是 svg-term-cli 檔案爆掉（heap out of memory）的原因。
func TestDiff_TypingKeepsPrefix(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	for i, s := range []string{"a", "ab", "abc", "abcd"} {
		if err := a.Add(frameWith(float64(i), 1, 20, s)); err != nil {
			t.Fatal(err)
		}
	}
	a.finish()

	// 每個按鍵應該只新增一小段，而不是重畫整列。
	if len(a.done) != 4 {
		var got []string
		for _, r := range a.done {
			got = append(got, r.Text)
		}
		t.Fatalf("run 數 = %d %v，預期 4（每個字元一段，前面的都保留）", len(a.done), got)
	}

	sort.Slice(a.done, func(i, j int) bool { return a.done[i].Col < a.done[j].Col })
	want := []struct {
		text  string
		col   int
		start float64
	}{
		{"a", 0, 0}, {"b", 1, 1}, {"c", 2, 2}, {"d", 3, 3},
	}
	for i, w := range want {
		r := a.done[i]
		if r.Text != w.text || r.Col != w.col || math.Abs(r.Start-w.start) > 1e-9 {
			t.Errorf("run %d = {文字 %q, 欄 %d, 起始 %v}，預期 {%q, %d, %v}",
				i, r.Text, r.Col, r.Start, w.text, w.col, w.start)
		}
	}
	// 第一段從頭活到尾
	if a.done[0].End < 3 {
		t.Errorf("第一段的 End = %v，預期一直活到最後（3）", a.done[0].End)
	}
}

// ⭐ 最後一張影格才出現的內容，必須有夠長的可見時間。
//
// 沒有結尾停留的話，它的起訖時間相同，只會在動畫的最後一瞬間閃一下，
// 讀者永遠看不到指令的結果——README 上那張 demo 就白做了。
func TestAnim_EndHoldMakesFinalStateVisible(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false
	opts.EndHold = 1.0

	a := NewAnimator(opts)
	_ = a.Add(frameWith(0, 1, 20, "before"))
	// 用單一詞：沒有背景色的空白會被跳過（見 visible()），
	// 「final result」會被切成兩段，不適合拿來當這個測試的斷言目標。
	_ = a.Add(frameWith(2, 1, 20, "done!"))
	a.finish()

	total := a.totalDuration()
	if math.Abs(total-3.0) > 1e-9 {
		t.Fatalf("總長 = %v，預期 3（2 秒錄影 + 1 秒停留）", total)
	}

	var final run
	for _, r := range a.done {
		if r.Text == "done!" {
			final = r
		}
	}
	if final.Text == "" {
		t.Fatal("找不到最後那段內容")
	}
	visible := final.End - final.Start
	if visible < 0.9 {
		t.Errorf("終態只可見 %v 秒，預期至少 0.9（結尾停留沒有生效）", visible)
	}

	// 換算成百分比也要佔得夠久
	lo, hi := pctRange(final.Start, final.End, total)
	if hi-lo < 25 {
		t.Errorf("終態只佔動畫的 %.2f%%，太短了", hi-lo)
	}
}

// 整列被換掉時就該全部關閉重開。
func TestDiff_FullReplace(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	_ = a.Add(frameWith(0, 1, 20, "hello"))
	_ = a.Add(frameWith(1, 1, 20, "world"))
	a.finish()

	if len(a.done) != 2 {
		t.Fatalf("run 數 = %d，預期 2", len(a.done))
	}
	var hello, world run
	for _, r := range a.done {
		switch r.Text {
		case "hello":
			hello = r
		case "world":
			world = r
		}
	}
	if hello.End != 1 {
		t.Errorf("hello 的 End = %v，預期 1（被取代時關閉）", hello.End)
	}
	if world.Start != 1 {
		t.Errorf("world 的 Start = %v，預期 1", world.Start)
	}
}

// 樣式改變不能被當成單純的文字延長。
func TestDiff_StyleChangeBreaksPrefix(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	f1 := frameWith(0, 1, 20, "ab")
	f2 := blankFrame(1, 20)
	f2.Time = 1
	setText(&f2, 0, 0, "abc")
	for x := 0; x < 3; x++ {
		f2.Rows[0][x].Style.Attr |= term.AttrBold
	}

	a := NewAnimator(opts)
	_ = a.Add(f1)
	_ = a.Add(f2)
	a.finish()

	for _, r := range a.done {
		if r.Text == "ab" && r.End != 1 {
			t.Errorf("樣式改變時舊的 \"ab\" 應該關閉，End = %v", r.End)
		}
	}
}

// ── ⭐ keyframes 百分比 ────────────────────────────────────

var keyframeRE = regexp.MustCompile(`@keyframes (k\d+)\{([^}]*(?:\}[^@]*?)*?)\}(?:\.|@|$)`)
var stopRE = regexp.MustCompile(`([\d.]+)%\{`)

// 抓出每一組 keyframes 的停格百分比。
func keyframeStops(css string) map[string][]float64 {
	out := map[string][]float64{}
	// 用 @keyframes kN{...} 之間的區段切
	idx := strings.Split(css, "@keyframes ")
	for _, seg := range idx[1:] {
		sp := strings.IndexByte(seg, '{')
		if sp < 0 {
			continue
		}
		name := strings.TrimSpace(seg[:sp])
		// 找到對應的收尾大括號（keyframes 內部有巢狀大括號）
		depth := 0
		end := -1
		for i := sp; i < len(seg); i++ {
			switch seg[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			continue
		}
		body := seg[sp : end+1]
		var stops []float64
		for _, m := range stopRE.FindAllStringSubmatch(body, -1) {
			v, err := strconv.ParseFloat(m[1], 64)
			if err == nil {
				stops = append(stops, v)
			}
		}
		out[name] = stops
	}
	return out
}

// ⚠️ VHS 的 SVG fork 踩過這個坑（PR #646「Fix SVG keyframe collisions with
// dynamic precision」）：兩個時間點被四捨五入到同一個百分比，
// keyframes 就會出現重複的停格點，動畫變得無法預測。
func TestKeyframes_StrictlyIncreasing(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	// 刻意用非常接近的時間戳去逼出碰撞路徑
	a := NewAnimator(opts)
	times := []float64{0, 0.0011, 0.0012, 0.0013, 5.0}
	for i, tt := range times {
		_ = a.Add(frameWith(tt, 1, 40, strings.Repeat("x", i+1)))
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}

	stops := keyframeStops(b.String())
	if len(stops) == 0 {
		t.Fatal("沒有產生任何 keyframes")
	}
	for name, s := range stops {
		if len(s) < 2 {
			continue
		}
		for i := 1; i < len(s); i++ {
			if s[i] <= s[i-1] {
				t.Errorf("%s 的停格點沒有嚴格遞增: %v", name, s)
				break
			}
		}
	}
	t.Logf("檢查了 %d 組 keyframes", len(stops))
}

// 從頭到尾都在的內容不該有任何動畫——這是體積上最大的一筆節省。
func TestKeyframes_AlwaysVisibleHasNoAnimation(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false

	a := NewAnimator(opts)
	// 第 0 列從頭到尾不變；第 1 列後來才出現。
	for i, extra := range []string{"", "", "later"} {
		f := blankFrame(2, 20)
		f.Time = float64(i)
		setText(&f, 0, 0, "constant")
		if extra != "" {
			setText(&f, 1, 0, extra)
		}
		_ = a.Add(f)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()

	doc := parseSVG(t, out)
	for _, txt := range doc.Texts {
		if strings.Contains(txt.Content, "constant") && strings.Contains(txt.Class, "k") {
			t.Errorf("從頭到尾都在的內容不該有動畫 class，得到 class=%q", txt.Class)
		}
	}
	if !strings.Contains(out, "@keyframes") {
		t.Error("後來才出現的內容應該要有 keyframes")
	}
}

// 用 opacity + steps(1,end)，不用 visibility。
// visibility 在 CSS 動畫裡有特殊插值規則，元素該消失時不會消失。
func TestKeyframes_UsesOpacitySteps(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false
	out := animate(t, []term.Frame{
		frameWith(0, 1, 20, "a"),
		frameWith(1, 1, 20, "b"),
	}, opts)

	if !strings.Contains(out, "opacity") {
		t.Error("keyframes 應該用 opacity")
	}
	if strings.Contains(out, "visibility") {
		t.Error("不該使用 visibility（CSS 動畫的插值規則會讓它不消失）")
	}
	if !strings.Contains(out, "steps(1,end)") {
		t.Error("應該用 steps(1,end) 讓每段區間維持定值，不要插值")
	}
}

func TestKeyframes_NoLoop(t *testing.T) {
	opts := DefaultOptions()
	opts.Cursor = false
	opts.Loop = false
	out := animate(t, []term.Frame{
		frameWith(0, 1, 20, "a"),
		frameWith(1, 1, 20, "b"),
	}, opts)

	if !strings.Contains(out, "animation-fill-mode:forwards") {
		t.Error("--no-loop 時應該有 animation-fill-mode:forwards，否則動畫結束會跳回初始狀態")
	}
	if strings.Contains(out, "infinite") {
		t.Error("--no-loop 時不該是 infinite")
	}
}

func TestPctRange_Guarantees(t *testing.T) {
	cases := []struct{ start, end, total float64 }{
		{0, 1, 10},
		{0, 0, 10},        // 零長度
		{9.99999, 10, 10}, // 幾乎在結尾
		{5, 5.0000001, 10},
		{0, 10, 10}, // 全長
	}
	for _, c := range cases {
		lo, hi := pctRange(c.start, c.end, c.total)
		if lo >= hi {
			t.Errorf("pctRange(%v,%v,%v) = (%v,%v)，lo 必須嚴格小於 hi",
				c.start, c.end, c.total, lo, hi)
		}
		if lo < 0 || hi > 100 {
			t.Errorf("pctRange(%v,%v,%v) = (%v,%v)，必須落在 [0,100]",
				c.start, c.end, c.total, lo, hi)
		}
	}
}

// ── 端到端 ─────────────────────────────────────────────

func TestAnim_EndToEnd(t *testing.T) {
	for _, name := range []string{"hello", "powerline"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open("../../testdata/" + name + ".cast")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			c, err := cast.Parse(f)
			if err != nil {
				t.Fatal(err)
			}

			a := NewAnimator(DefaultOptions())
			if err := frames.Capture(c, frames.DefaultOptions(), a.Add); err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := a.Write(&b); err != nil {
				t.Fatal(err)
			}
			out := b.String()

			doc := parseSVG(t, out) // 同時驗證是合法 XML
			if len(doc.Texts) == 0 {
				t.Error("沒有任何 <text>")
			}
			if !strings.Contains(out, "@keyframes") {
				t.Error("沒有任何 keyframes——動畫沒有產生")
			}
			checkGolden(t, "anim-"+name+".svg", out)
			t.Logf("%s: %d bytes, %d 個 <text>", name, len(out), len(doc.Texts))
		})
	}
}

// 動畫輸出裡的 PUA 座標必須跟靜態一樣精確——差分不能破壞護城河。
func TestAnim_PowerlineStillNoDrift(t *testing.T) {
	f, err := os.Open("../../testdata/powerline.cast")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := cast.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	a := NewAnimator(opts)
	if err := frames.Capture(c, frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}
	doc := parseSVG(t, b.String())
	g := opts.geom(80, 24)

	var n int
	for _, txt := range doc.Texts {
		for _, r := range txt.Content {
			if r < 0xE000 || r > 0xF8FF {
				continue
			}
			x := atof(t, txt.X)
			col := (x - g.pad) / g.cellW
			if math.Abs(col-math.Round(col)) > 1e-6 {
				t.Errorf("U+%04X 的 x=%s 不落在整數欄位上（算出來是第 %v 欄）", r, txt.X, col)
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("動畫輸出裡找不到 PUA 字元")
	}
	t.Logf("動畫輸出裡的 %d 個 PUA 字元全部落在整數欄位上", n)
}

func TestAnim_TimeGoesBackwards(t *testing.T) {
	a := NewAnimator(DefaultOptions())
	if err := a.Add(frameWith(5, 1, 10, "a")); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(frameWith(1, 1, 10, "b")); err == nil {
		t.Error("影格時間倒退時應該報錯")
	}
}

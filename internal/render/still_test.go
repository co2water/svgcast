package render

import (
	"encoding/xml"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
	"github.com/co2water/svgcast/internal/term"
)

var update = flag.Bool("update", false, "重新產生 golden 檔")

// ── 解析輸出的 SVG，讓測試能對結構下斷言 ──────────────────────

type svgDoc struct {
	XMLName xml.Name  `xml:"svg"`
	Width   string    `xml:"width,attr"`
	Height  string    `xml:"height,attr"`
	Style   string    `xml:"style"`
	Texts   []svgText `xml:"text"`
	Rects   []svgRect `xml:"rect"`
}

type svgText struct {
	X            string `xml:"x,attr"`
	Y            string `xml:"y,attr"`
	TextLength   string `xml:"textLength,attr"`
	LengthAdjust string `xml:"lengthAdjust,attr"`
	Class        string `xml:"class,attr"`
	Fill         string `xml:"fill,attr"`
	Content      string `xml:",chardata"`
}

type svgRect struct {
	X     string `xml:"x,attr"`
	Y     string `xml:"y,attr"`
	W     string `xml:"width,attr"`
	H     string `xml:"height,attr"`
	Class string `xml:"class,attr"`
	Fill  string `xml:"fill,attr"`
}

// parseSVG 同時驗證輸出是合法的 XML——這本身就是一項驗收。
func parseSVG(t *testing.T, s string) svgDoc {
	t.Helper()
	var doc svgDoc
	if err := xml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("輸出不是合法的 XML: %v", err)
	}
	return doc
}

func atof(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("數值 %q 解析失敗: %v", s, err)
	}
	return v
}

// ── 測試素材 ────────────────────────────────────────────

// lastFrame 把一份 cast 跑完，回傳終態影格。
func lastFrame(t *testing.T, path string) term.Frame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := cast.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	var last term.Frame
	if err := frames.Capture(c, frames.DefaultOptions(), func(fr term.Frame) error {
		last = fr
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return last
}

func renderStill(t *testing.T, fr term.Frame, opts Options) string {
	t.Helper()
	var b strings.Builder
	if err := RenderStill(&b, fr, opts); err != nil {
		t.Fatalf("RenderStill: %v", err)
	}
	return b.String()
}

// ── ⭐ 護城河 ① 的驗收 ────────────────────────────────────

// TestStill_PowerlineNoDrift 是這個專案最重要的一個測試。
//
// termtosvg #14「Powerline fonts have mangled output」2018 年開，至今 OPEN。
// 症狀是私用區字元的前進量對不上，整行愈往右偏得愈多。
//
// 我們的解法是每個 <text> 都給明確的 x = padding + 欄位 × 格寬。
// 這個測試直接驗那件事：先從影格裡找出每個 PUA 字元在第幾欄，
// 再到輸出的 SVG 裡確認真的有一個 <text> 落在對應的座標上。
func TestStill_PowerlineNoDrift(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	opts := DefaultOptions()
	doc := parseSVG(t, renderStill(t, fr, opts))
	g := opts.geom(len(fr.Rows[0]), len(fr.Rows))

	// 從影格裡蒐集所有 PUA 字元的實際位置
	type want struct {
		row, col int
		r        rune
	}
	var wants []want
	for y, row := range fr.Rows {
		for x, c := range row {
			if c.Rune >= 0xE000 && c.Rune <= 0xF8FF {
				wants = append(wants, want{y, x, c.Rune})
			}
		}
	}
	if len(wants) == 0 {
		t.Fatal("影格裡沒有 PUA 字元，測試素材壞了")
	}

	for _, w := range wants {
		wantX := g.x(w.col)
		wantY := g.baseline(w.row)

		var found bool
		for _, txt := range doc.Texts {
			if !strings.ContainsRune(txt.Content, w.r) {
				continue
			}
			if math.Abs(atof(t, txt.X)-wantX) < 1e-6 &&
				math.Abs(atof(t, txt.Y)-wantY) < 1e-6 {
				found = true
				break
			}
		}
		if !found {
			var got []string
			for _, txt := range doc.Texts {
				if strings.ContainsRune(txt.Content, w.r) {
					got = append(got, "x="+txt.X+" y="+txt.Y)
				}
			}
			t.Errorf("U+%04X 應在第 %d 列第 %d 欄 → x=%s y=%s，"+
				"但 SVG 裡含該字元的 <text> 在 %v（偏移沒有被消掉）",
				w.r, w.row, w.col, fnum(wantX), fnum(wantY), got)
		}
	}
	t.Logf("驗證了 %d 個 PUA 字元的座標，全部精確落在欄位上", len(wants))
}

// PUA 字元必須自成一段，不能跟旁邊的文字合併——
// 合併就等於讓瀏覽器用字型的前進量去排它，偏移就會回來。
func TestStill_PUAIsolated(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	doc := parseSVG(t, renderStill(t, fr, DefaultOptions()))

	for _, txt := range doc.Texts {
		var hasPUA, hasOther bool
		for _, r := range txt.Content {
			if r >= 0xE000 && r <= 0xF8FF {
				hasPUA = true
			} else {
				hasOther = true
			}
		}
		if hasPUA && hasOther {
			t.Errorf("PUA 字元跟一般文字被合併在同一個 <text> 裡: %q", txt.Content)
		}
	}
}

// ── 一般結構驗收 ─────────────────────────────────────────

func TestStill_WellFormed(t *testing.T) {
	for _, name := range []string{"hello.cast", "powerline.cast"} {
		t.Run(name, func(t *testing.T) {
			fr := lastFrame(t, filepath.Join("../../testdata", name))
			out := renderStill(t, fr, DefaultOptions())
			doc := parseSVG(t, out)

			if doc.XMLName.Local != "svg" {
				t.Errorf("根元素 = %q，預期 svg", doc.XMLName.Local)
			}
			if doc.Width == "" || doc.Height == "" {
				t.Error("缺少 width/height")
			}
			if !strings.Contains(doc.Style, "--fg") {
				t.Error("<style> 裡沒有主題變數")
			}
			if len(doc.Texts) == 0 {
				t.Error("沒有任何 <text>")
			}
			if !strings.Contains(out, `xml:space="preserve"`) {
				t.Error("缺少 xml:space=preserve，連續空白會被吃掉")
			}
		})
	}
}

// 多格的 run 要有 textLength（第二道防偏移保險）；單格的不該有。
func TestStill_TextLengthOnlyWhenUseful(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	opts := DefaultOptions()
	g := opts.geom(len(fr.Rows[0]), len(fr.Rows))
	doc := parseSVG(t, renderStill(t, fr, opts))

	var multi, single int
	for _, txt := range doc.Texts {
		n := len([]rune(txt.Content))
		if n > 1 {
			multi++
			if txt.TextLength == "" {
				t.Errorf("多格 run %q 沒有 textLength", txt.Content)
				continue
			}
			// lengthAdjust 刻意不輸出——SVG 規格的預設值就是 spacing。
			// 每次省 25 個位元組，在長錄影裡累積起來很可觀。
			if txt.LengthAdjust != "" {
				t.Errorf("run %q 輸出了多餘的 lengthAdjust=%q（預設值就是 spacing）",
					txt.Content, txt.LengthAdjust)
			}
			// textLength 必須等於格子數 × 格寬
			wantLen := float64(n) * g.cellW
			if math.Abs(atof(t, txt.TextLength)-wantLen) > 1e-6 {
				t.Errorf("run %q 的 textLength = %s，預期 %s",
					txt.Content, txt.TextLength, fnum(wantLen))
			}
		} else {
			single++
			if txt.TextLength != "" {
				t.Errorf("單格 run %q 不該有 textLength", txt.Content)
			}
		}
	}
	t.Logf("多格 run %d 個，單格 run %d 個", multi, single)
}

func TestStill_Escaping(t *testing.T) {
	fr := blankFrame(2, 20)
	setText(&fr, 0, 0, `a<b&c>d`)
	out := renderStill(t, fr, DefaultOptions())

	doc := parseSVG(t, out) // 這行本身就驗證了逸出是對的
	var joined string
	for _, txt := range doc.Texts {
		joined += txt.Content
	}
	if !strings.Contains(joined, "a<b&c>d") {
		t.Errorf("逸出後再解析回來 = %q，預期含有原文", joined)
	}
	if strings.Contains(out, "<b&c>") {
		t.Error("原始字元沒有被逸出，輸出會是壞掉的 XML")
	}
}

// 空白格子不該產出任何元素——終端畫面大部分是空白，這是體積的關鍵。
func TestStill_SkipsBlankCells(t *testing.T) {
	fr := blankFrame(24, 80)
	setText(&fr, 0, 0, "hi")
	doc := parseSVG(t, renderStill(t, fr, DefaultOptions()))

	if len(doc.Texts) != 1 {
		t.Errorf("<text> 數量 = %d，預期 1（只有 \"hi\"，其餘空白全跳過）", len(doc.Texts))
	}
	// 只有整張畫布的背景那一個 rect
	if len(doc.Rects) != 1 {
		t.Errorf("<rect> 數量 = %d，預期 1（只有畫布背景）", len(doc.Rects))
	}
}

// ── run 切分的單元測試 ────────────────────────────────────

func TestRunsFromFrame_Splitting(t *testing.T) {
	fr := blankFrame(1, 20)
	setText(&fr, 0, 0, "abc")
	// 中間插一個 PUA
	fr.Rows[0][3] = term.Cell{Rune: 0xE0B0, Width: 1}
	setText(&fr, 0, 4, "def")

	runs := runsFromFrame(fr)
	if len(runs) != 3 {
		var got []string
		for _, r := range runs {
			got = append(got, r.Text)
		}
		t.Fatalf("run 數 = %d %v，預期 3（abc / PUA / def）", len(runs), got)
	}
	if runs[0].Text != "abc" || runs[1].Text != "" || runs[2].Text != "def" {
		t.Errorf("切分結果 = %q / %q / %q", runs[0].Text, runs[1].Text, runs[2].Text)
	}
	if runs[2].Col != 4 {
		t.Errorf("第三段的 Col = %d，預期 4", runs[2].Col)
	}
}

func TestRunsFromFrame_StyleBreak(t *testing.T) {
	fr := blankFrame(1, 20)
	setText(&fr, 0, 0, "ab")
	setText(&fr, 0, 2, "cd")
	for x := 2; x < 4; x++ {
		fr.Rows[0][x].Style.Attr |= term.AttrBold
	}
	runs := runsFromFrame(fr)
	if len(runs) != 2 {
		t.Fatalf("run 數 = %d，預期 2（樣式改變要切開）", len(runs))
	}
	if runs[1].Style.Attr&term.AttrBold == 0 {
		t.Error("第二段應該是粗體")
	}
}

// reverse 應該在切 run 之前就被化成前景/背景交換。
func TestRunsFromFrame_ReverseSwapped(t *testing.T) {
	fr := blankFrame(1, 10)
	setText(&fr, 0, 0, "x")
	fr.Rows[0][0].Style.FG = term.Color{Kind: term.ColorIndexed, Index: 1}
	fr.Rows[0][0].Style.BG = term.Color{Kind: term.ColorIndexed, Index: 2}
	fr.Rows[0][0].Style.Attr |= term.AttrReverse

	runs := runsFromFrame(fr)
	if len(runs) != 1 {
		t.Fatalf("run 數 = %d，預期 1", len(runs))
	}
	st := runs[0].Style
	if st.Attr&term.AttrReverse != 0 {
		t.Error("reverse 屬性應該已經被化掉")
	}
	if st.FG.Index != 2 || st.BG.Index != 1 {
		t.Errorf("前景/背景 = %d/%d，預期交換成 2/1", st.FG.Index, st.BG.Index)
	}
}

// ── golden ──────────────────────────────────────────────

func TestStill_Golden(t *testing.T) {
	for _, name := range []string{"hello", "powerline"} {
		t.Run(name, func(t *testing.T) {
			fr := lastFrame(t, "../../testdata/"+name+".cast")
			got := renderStill(t, fr, DefaultOptions())
			checkGolden(t, "still-"+name+".svg", got)
		})
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("../../testdata/golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("已更新 golden：%s（%d bytes）", path, len(got))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("讀取 golden 失敗（第一次請用 -update 產生）: %v", err)
	}
	if string(want) != got {
		t.Errorf("輸出與 golden 不同（%d vs %d bytes）。"+
			"確認差異是預期的之後，用 go test ./internal/render -update 重生",
			len(got), len(want))
	}
}

// ── 建構測試用影格的小工具 ──────────────────────────────

func blankFrame(rows, cols int) term.Frame {
	f := term.Frame{Rows: make([][]term.Cell, rows)}
	for y := range f.Rows {
		f.Rows[y] = make([]term.Cell, cols)
		for x := range f.Rows[y] {
			f.Rows[y][x] = term.Cell{Rune: ' ', Width: 1}
		}
	}
	return f
}

func setText(f *term.Frame, row, col int, s string) {
	for i, r := range s {
		f.Rows[row][col+i] = term.Cell{Rune: r, Width: 1}
	}
}

package render

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/co2water/svgcast/internal/term"
)

// geom 是版面計算。所有座標都從這裡出來，不散在各處。
type geom struct {
	cellW, lineH, fontSize, pad float64
	cols, rows                  int
	width, height               float64
}

func (o Options) geom(cols, rows int) geom {
	g := geom{
		fontSize: o.FontSize,
		cellW:    o.CellWidth,
		lineH:    o.LineHeight,
		pad:      o.Padding,
		cols:     cols,
		rows:     rows,
	}
	if g.fontSize <= 0 {
		g.fontSize = 14
	}
	if g.cellW <= 0 {
		g.cellW = g.fontSize * 0.6 // 等寬字型的標準比例
	}
	if g.lineH <= 0 {
		g.lineH = g.fontSize * 1.2
	}
	if g.pad < 0 {
		g.pad = 0
	}
	g.width = g.pad*2 + float64(cols)*g.cellW
	g.height = g.pad*2 + float64(rows)*g.lineH
	return g
}

// ⭐ x 是防偏移的核心：欄位直接乘上格寬，不依賴前一段文字畫了多寬。
// termtosvg #14 的「愈往右偏得愈多」就是因為靠前進量累加。
func (g geom) x(col int) float64      { return g.pad + float64(col)*g.cellW }
func (g geom) rowTop(row int) float64 { return g.pad + float64(row)*g.lineH }
func (g geom) baseline(row int) float64 {
	return g.rowTop(row) + g.lineH*0.5 + g.fontSize*0.35
}

// fnum 把浮點數格式化成最短的樣子：去掉尾端的零與小數點。
// 座標在輸出裡出現上千次，這個函式直接影響檔案大小。
func fnum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "" || s == "-" || s == "-0" {
		return "0"
	}
	return s
}

var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// escapeText 處理 XML 特殊字元，並丟掉不能出現在 XML 1.0 裡的控制字元。
func escapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// XML 1.0 只允許 tab / LF / CR 這三個控制字元；其餘會讓文件變成不合法。
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			b.WriteRune(0xFFFD)
			continue
		}
		b.WriteRune(r)
	}
	return xmlEscaper.Replace(b.String())
}

// classSet 記錄實際用到的顏色 class，只輸出用得到的 CSS 規則。
type classSet map[string]bool

func (cs classSet) add(c string) {
	if c != "" {
		cs[c] = true
	}
}

func (cs classSet) sorted() []string {
	out := make([]string, 0, len(cs))
	for k := range cs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// styleClasses 回傳一段文字要套的 class 與（必要時的）直接 fill 值。
func styleClasses(st term.Style, cs classSet) (classAttr, fillAttr string) {
	var classes []string

	fc, ff := colorRef(st.FG, true)
	if fc != "" {
		cs.add(fc)
		classes = append(classes, fc)
	}

	if st.Attr&term.AttrBold != 0 {
		classes = append(classes, "b")
	}
	if st.Attr&term.AttrItalic != 0 {
		classes = append(classes, "i")
	}
	if st.Attr&term.AttrUnderline != 0 {
		classes = append(classes, "u")
	}
	if st.Attr&term.AttrFaint != 0 {
		classes = append(classes, "d")
	}
	// AttrBlink 刻意不實作：termtosvg 有一個「blink 不會閃」的 issue，
	// 我們選擇明確不做並寫進 README，而不是做一個會壞的版本。

	return strings.Join(classes, " "), ff
}

// RenderStill 把單一影格畫成一張靜態 SVG。
//
// 這也是 --still 那張預覽圖的產生方式：規格表格 #2 裡
// termtosvg #50「不想讓瀏覽器在使用者點擊前就載入巨大的 svg 動畫」要的東西。
func RenderStill(w io.Writer, f term.Frame, opts Options) error {
	if len(f.Rows) == 0 {
		return fmt.Errorf("影格是空的")
	}
	g := opts.geom(len(f.Rows[0]), len(f.Rows))
	runs := runsFromFrame(f)

	lib, err := buildGlyphLib(opts, runs)
	if err != nil {
		return err
	}

	var body strings.Builder
	cs := classSet{}

	// 背景色塊要先畫，才不會蓋住文字。
	writeBGRects(&body, g, runs, cs, "")

	if opts.Cursor && f.Cursor.Visible &&
		f.Cursor.Row < g.rows && f.Cursor.Col < g.cols {
		writeCursor(&body, g, run{Row: f.Cursor.Row, Col: f.Cursor.Col}, "")
	}

	for _, r := range runs {
		writeTextRun(&body, g, r, cs, "", lib)
	}

	return writeDocument(w, g, opts, cs, body.String(), "", lib)
}

// joinClass 把顏色/樣式的 class 跟動畫的 class 併起來。
func joinClass(base, anim string) string {
	switch {
	case base == "":
		return anim
	case anim == "":
		return base
	}
	return base + " " + anim
}

// writeBGRects 輸出一組 run 底下的背景色塊，相鄰同色的會先併起來。
func writeBGRects(b *strings.Builder, g geom, runs []run, cs classSet, animClass string) {
	for _, br := range mergeBGRects(runs) {
		bc, bf := colorRef(br.BG, false)
		if bc == "" && bf == "" {
			continue
		}
		cs.add(bc)

		b.WriteString("<rect")
		attr(b, "x", fnum(g.x(br.Col)))
		attr(b, "y", fnum(g.rowTop(br.Row)))
		attr(b, "width", fnum(float64(br.Cells)*g.cellW))
		attr(b, "height", fnum(g.lineH))
		if cls := joinClass(bc, animClass); cls != "" {
			attr(b, "class", cls)
		}
		if bf != "" {
			attr(b, "fill", bf)
		}
		b.WriteString("/>")
	}
}

// singleRune 回傳只有一個字元時的那個字元，否則回 0。
func singleRune(s string) rune {
	rs := []rune(s)
	if len(rs) != 1 {
		return 0
	}
	return rs[0]
}

// writeGlyphUse 用 <use> 引用 <defs> 裡預先定義好的字符輪廓。
//
// 路徑定義在原點（基線、左緣），所以 x/y 直接給格子的座標就對齊了。
func writeGlyphUse(b *strings.Builder, g geom, r run, cs classSet, animClass string, lib *glyphLib) {
	ch := singleRune(r.Text)
	cls, fill := styleClasses(r.Style, cs)
	cls = joinClass(cls, animClass)

	b.WriteString("<use")
	attr(b, "href", "#"+lib.ids[ch])
	attr(b, "x", fnum(g.x(r.Col)))
	attr(b, "y", fnum(g.baseline(r.Row)))
	if cls != "" {
		attr(b, "class", cls)
	}
	if fill != "" {
		attr(b, "fill", fill)
	}
	b.WriteString("/>")
}

// writeCursor 輸出游標方塊。
func writeCursor(b *strings.Builder, g geom, r run, animClass string) {
	b.WriteString("<rect")
	attr(b, "class", joinClass("cur", animClass))
	attr(b, "x", fnum(g.x(r.Col)))
	attr(b, "y", fnum(g.rowTop(r.Row)))
	attr(b, "width", fnum(g.cellW))
	attr(b, "height", fnum(g.lineH))
	b.WriteString("/>")
}

// attr 附加一個 XML 屬性。屬性值都是我們自己產生的（數字、hex、class 名稱），
// 不含使用者資料，所以不需要再逸出；文字內容才需要，那走 escapeText。
func attr(b *strings.Builder, name, value string) {
	b.WriteString(` ` + name + `="` + value + `"`)
}

// writeTextRun 輸出一段文字。animClass 非空時會一併掛上動畫的 class。
//
// lib 不為 nil 且這一段剛好是一個「已嵌入輪廓的私用區字符」時，改成引用
// <defs> 裡的路徑——那樣讀者沒裝 Nerd Font 也看得到正確的符號。
// 一般文字一律留在 <text>，保持可選取。
func writeTextRun(b *strings.Builder, g geom, r run, cs classSet, animClass string, lib *glyphLib) {
	if lib.has(singleRune(r.Text)) {
		writeGlyphUse(b, g, r, cs, animClass, lib)
		return
	}

	cls, fill := styleClasses(r.Style, cs)
	cls = joinClass(cls, animClass)

	b.WriteString("<text")
	attr(b, "x", fnum(g.x(r.Col)))
	attr(b, "y", fnum(g.baseline(r.Row)))

	// ⭐ 第二道防偏移保險：把這一段強制拉成「格子數 × 格寬」。
	// 就算讀者的字型 advance 不是標準等寬，瀏覽器也會把它調到剛好。
	//
	// 只在多於一格時才輸出——單一字元沒有字間距可以調整，textLength 沒有作用，
	// 而它的位置已經由上面那個明確的 x 決定了。順便省位元組。
	if n := r.cells(); n > 1 {
		attr(b, "textLength", fnum(float64(n)*g.cellW))
		// lengthAdjust 不輸出：SVG 規格的預設值就是 "spacing"。
		// 它每次要 25 個位元組，在長錄影裡是好幾 KB 的純浪費。
	}
	if cls != "" {
		attr(b, "class", cls)
	}
	if fill != "" {
		attr(b, "fill", fill)
	}
	b.WriteString(">" + escapeText(r.Text) + "</text>")
}

// writeDocument 組出完整的 SVG，包含 <style> 與內容。
// extraCSS 給 S3 的 keyframes 用。
func writeDocument(w io.Writer, g geom, opts Options, cs classSet, body, extraCSS string, lib *glyphLib) error {
	var b strings.Builder

	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="` + fnum(g.width) +
		`" height="` + fnum(g.height) +
		`" viewBox="0 0 ` + fnum(g.width) + ` ` + fnum(g.height) +
		`" xml:space="preserve">`)

	b.WriteString("<style>")
	writeThemeCSS(&b, opts)
	b.WriteString(`text{font-family:` + opts.FontFamily +
		`;font-size:` + fnum(g.fontSize) + `px;white-space:pre;fill:var(--fg)}`)
	b.WriteString(`.cur{fill:var(--cur);opacity:.65}`)
	b.WriteString(`.b{font-weight:700}.i{font-style:italic}.u{text-decoration:underline}.d{opacity:.6}`)

	for _, c := range cs.sorted() {
		// f0..f15 是前景，b0..b15 是背景；兩者都用 fill。
		idx := c[1:]
		b.WriteString(`.` + c + `{fill:var(--c` + idx + `)}`)
	}
	b.WriteString(extraCSS)
	b.WriteString("</style>")

	lib.writeDefs(&b)
	b.WriteString(`<rect width="` + fnum(g.width) + `" height="` + fnum(g.height) + `" fill="var(--bg)"/>`)
	b.WriteString(body)
	b.WriteString("</svg>")

	_, err := io.WriteString(w, b.String())
	return err
}

// writeThemeCSS 輸出配色變數（規格 v1.1 的 ④）。
//
// ⭐ 這是唯一「GIF 結構上辦不到」的功能：點陣圖一個檔案只有一套顏色。
// SVG 內部可以放 media query，同一個檔案就能跟著讀者的系統主題切換。
//
// 而 GitHub 官方推薦的做法需要**兩個檔案**——
// <picture> + <source media="(prefers-color-scheme: dark)"> + 兩張圖。
// 我們一個就夠。
//
// spike 已驗證（2026-09-08，Chromium）：<img> / inline / <object> 三種載入
// 情境下 media query 都生效。素材見 docs/spike-theme/。
func writeThemeCSS(b *strings.Builder, opts Options) {
	switch opts.Theme {
	case "light":
		b.WriteString(":root{")
		writePaletteVars(b, lightPalette)
		b.WriteString("}")
	case "dark":
		b.WriteString(":root{")
		writePaletteVars(b, darkPalette)
		b.WriteString("}")
	default: // "auto" 與任何未知值
		b.WriteString(":root{")
		writePaletteVars(b, lightPalette)
		b.WriteString("}")
		b.WriteString("@media(prefers-color-scheme:dark){:root{")
		writePaletteVars(b, darkPalette)
		b.WriteString("}}")
	}
}

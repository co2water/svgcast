package render

import (
	"sort"
	"strings"

	"github.com/co2water/svgcast/internal/glyph"
	"github.com/co2water/svgcast/internal/term"
)

// effectiveStyle 把 reverse 屬性化掉——直接交換前景與背景，
// 之後所有環節就不必再判斷 reverse。
//
// 交換時要處理「預設色」：反白的預設前景，其背景要變成預設前景色，
// 所以用一個標記讓輸出層知道要填 var(--fg)/var(--bg)。
func effectiveStyle(s term.Style) term.Style {
	if s.Attr&term.AttrReverse == 0 {
		return s
	}
	s.FG, s.BG = s.BG, s.FG
	s.Attr &^= term.AttrReverse
	return s
}

// visible 判斷這一格有沒有東西需要畫。
//
// 空白 + 沒有背景色 + 沒有底線 = 什麼都不用輸出。
// 終端畫面絕大部分是這種格子，跳過它們是體積上最大的一筆節省。
func visible(c term.Cell, st term.Style) bool {
	if st.BG.Kind != term.ColorDefault {
		return true
	}
	if st.Attr&(term.AttrUnderline|term.AttrStrike) != 0 {
		return true
	}
	return c.Rune != 0 && c.Rune != ' '
}

// runsFromFrame 把一張影格切成一段段可以用單一 <text> 畫出來的連續文字。
//
// ⭐ 切分規則同時決定正確性與體積（見 glyph.MustBreakRun 的說明）：
//   - 切太細 → 每個字一個 <text>，檔案爆掉
//   - 切太粗 → 私用區與寬字元的前進量誤差會累積，就是 termtosvg #14
func runsFromFrame(f term.Frame) []run {
	var flat []run
	for _, row := range runsByRow(f) {
		flat = append(flat, row...)
	}
	return flat
}

// runsByRow 跟 runsFromFrame 一樣，但保留每一列的分組——
// 動畫的差分是逐列做的（見 Animator.diffRow）。
func runsByRow(f term.Frame) [][]run {
	out := make([][]run, len(f.Rows))
	var b strings.Builder

	for y, row := range f.Rows {
		var runs []run
		var (
			cur      run
			open     bool
			prevCol  int
			prevBrk  bool
			curStyle term.Style
		)

		flush := func() {
			if !open {
				return
			}
			cur.Text = b.String()
			b.Reset()
			if cur.Text != "" {
				runs = append(runs, cur)
			}
			open = false
		}

		for x, c := range row {
			st := effectiveStyle(c.Style)

			if !visible(c, st) {
				flush()
				continue
			}

			ch := c.Rune
			if ch == 0 {
				ch = ' '
			}
			brk := glyph.MustBreakRun(ch)

			// 是否能接在目前這一段後面
			cont := open &&
				curStyle == st &&
				x == prevCol+1 &&
				!brk && !prevBrk

			if !cont {
				flush()
				cur = run{Row: y, Col: x, Style: st}
				curStyle = st
				open = true
			}

			b.WriteRune(ch)
			prevCol = x
			prevBrk = brk
		}
		flush()
		out[y] = runs
	}
	return out
}

// cells 回傳這一段佔了幾個終端格子。
//
// 用 glyph.Width 累加而不是 len([]rune)——寬字元佔兩格，
// textLength 必須照格子數算，否則瀏覽器會把它壓成一格寬。
func (r run) cells() int {
	return glyph.StringWidth(r.Text)
}

// bgRect 是一塊背景色。
type bgRect struct {
	Row, Col, Cells int
	BG              term.Color
}

// mergeBGRects 把相鄰、同底色的背景色塊併成一塊。
//
// 為什麼需要：run 會因為 PUA 字元、樣式邊界而被切開，但它們的**背景色**
// 常常是連續的一整條（powerline 的提示列就是典型）。不合併的話，
// 一條色帶會變成七八個 <rect>，每個都要寫 x/y/width/height 四個座標。
//
// ⚠️ 只能在「同時出現、同時消失」的一組 run 之間合併——
// 跨動畫群組合併會讓色塊的存活區間錯亂，所以呼叫端要按群組分別呼叫。
func mergeBGRects(runs []run) []bgRect {
	ordered := make([]run, 0, len(runs))
	for _, r := range runs {
		if r.Text == cursorMarker || r.Style.BG.Kind == term.ColorDefault {
			continue
		}
		ordered = append(ordered, r)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Row != ordered[j].Row {
			return ordered[i].Row < ordered[j].Row
		}
		return ordered[i].Col < ordered[j].Col
	})

	var out []bgRect
	for _, r := range ordered {
		n := r.cells()
		if n == 0 {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Row == r.Row && last.BG == r.Style.BG && last.Col+last.Cells == r.Col {
				last.Cells += n
				continue
			}
		}
		out = append(out, bgRect{Row: r.Row, Col: r.Col, Cells: n, BG: r.Style.BG})
	}
	return out
}

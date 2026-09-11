package term

import (
	"github.com/hinshun/vt10x"
)

// vt10xEmulator 用 github.com/hinshun/vt10x 實作 Emulator。
//
// 為什麼選它（W1 spike #1，2026-09-08）：
//
//	pkg.go.dev 的 imported-by：vt10x 82 vs charmbracelet/x/vt 6。
//	被 openshift/odo、tektoncd/cli、jenkins-x/jx、podman-tui 拿去跑測試，身經百戰。
//	而且 MrMarble/termsvg（唯一活著的競品）用的就是它——它已經證明能撐起
//	asciicast → SVG 這條管線。
//	charm 的 vt 才兩個月大、6 個 importer；用新生的模擬器等於繼承它的 bug，
//	而那正是我們承諾要修的東西。
//
//	額外好處：**零相依**（它的 go.mod 沒有任何 require）。
//
// 代價：停在 2022 年的版本，撞到 bug 沒人修，得自己 fork。已接受。
type vt10xEmulator struct {
	term vt10x.Terminal
}

// NewEmulator 建立預設的實作。
func NewEmulator(cols, rows int) Emulator {
	return &vt10xEmulator{
		term: vt10x.New(vt10x.WithSize(cols, rows)),
	}
}

func (e *vt10xEmulator) Write(p []byte) (int, error) {
	return e.term.Write(p)
}

func (e *vt10xEmulator) Resize(cols, rows int) {
	e.term.Resize(cols, rows)
}

// vt10x 的屬性位元是**未匯出**的（state.go 裡的 attrReverse、attrBold… 全小寫），
// 所以只能照抄它的 `1 << iota` 順序。
//
// ⚠️ 這是脆弱的耦合：上游若調換順序，我們會安靜地讀錯屬性。
// 防線有兩道——go.mod 把版本釘死，以及 vt10x_test.go 裡的 TestAttrBitsStillMatch。
const (
	vtAttrReverse   int16 = 1 << 0
	vtAttrUnderline int16 = 1 << 1
	vtAttrBold      int16 = 1 << 2
	vtAttrGfx       int16 = 1 << 3 // 繪圖字元集，我們不需要
	vtAttrItalic    int16 = 1 << 4
	vtAttrBlink     int16 = 1 << 5
	vtAttrWrap      int16 = 1 << 6 // 標記這格是換行點，不是樣式
)

// Snapshot 抓出目前的螢幕狀態。
func (e *vt10xEmulator) Snapshot() Frame {
	e.term.Lock()
	defer e.term.Unlock()

	cols, rows := e.term.Size()
	f := Frame{Rows: make([][]Cell, rows)}

	for y := 0; y < rows; y++ {
		row := make([]Cell, cols)
		for x := 0; x < cols; x++ {
			g := e.term.Cell(x, y)
			row[x] = Cell{
				Rune:  g.Char,
				Style: convertStyle(g),
				// ⭐ 寬度不採信 vt10x —— 它根本不算寬度（見下方 NormalizeWidths 的說明）。
				Width: 0,
			}
		}
		f.Rows[y] = row
	}

	cur := e.term.Cursor()
	f.Cursor = Cursor{Row: cur.Y, Col: cur.X, Visible: e.term.CursorVisible()}

	NormalizeWidths(&f)
	return f
}

func convertStyle(g vt10x.Glyph) Style {
	var a Attr
	if g.Mode&vtAttrBold != 0 {
		a |= AttrBold
	}
	if g.Mode&vtAttrItalic != 0 {
		a |= AttrItalic
	}
	if g.Mode&vtAttrUnderline != 0 {
		a |= AttrUnderline
	}
	if g.Mode&vtAttrBlink != 0 {
		a |= AttrBlink
	}
	if g.Mode&vtAttrReverse != 0 {
		a |= AttrReverse
	}
	// ⚠️ vt10x 不追蹤 faint 與 strikethrough——它的屬性位元裡沒有這兩個。
	// AttrFaint / AttrStrike 永遠不會被設起來。若之後真的需要，得 fork 上游。
	return Style{
		FG:   convertColor(g.FG),
		BG:   convertColor(g.BG),
		Attr: a,
	}
}

func convertColor(c vt10x.Color) Color {
	switch c {
	case vt10x.DefaultFG, vt10x.DefaultBG, vt10x.DefaultCursor:
		// 交給渲染層用主題的預設色填（規格 v1.1 的 ④ 深色模式要靠這個）。
		return Color{Kind: ColorDefault}
	}
	// ⚠️ 已知限制：vt10x 的顏色編碼本身有歧義，這裡無法完全還原。
	//
	// 它把真彩色存成 Color(r<<16 | g<<8 | b)，而 0–255 同時是調色盤索引：
	//
	//	SGR 38;5;N       → Color(N)              N ∈ [0,256)
	//	SGR 38;2;r;g;b   → Color(r<<16|g<<8|b)
	//
	// 當 r==0 且 g==0 時，真彩色算出來的值落在 [0,256)，跟調色盤索引撞在一起。
	// 資訊在 vt10x 內部就已經丟了，我們拿不回來。
	//
	// 碰撞範圍很窄——只有「純藍色通道」的深色（含純黑）會中。其餘只要 r 或 g
	// 不為零，值就 ≥ 256，會被正確判成 RGB。
	//
	// 選擇把 <256 一律當索引色：終端輸出絕大多數用的是索引色，而且純黑 RGB(0,0,0)
	// 對到調色盤第 0 色（黑）語意上剛好是對的。要真正修好得 fork 上游。
	if c < 256 {
		return Color{Kind: ColorIndexed, Index: uint8(c)}
	}
	return Color{
		Kind: ColorRGB,
		R:    uint8(c >> 16),
		G:    uint8(c >> 8),
		B:    uint8(c),
	}
}

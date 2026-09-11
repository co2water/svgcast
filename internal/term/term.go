// Package term 把 asciicast 的輸出位元組餵進一個虛擬終端，抽出每一格的螢幕狀態。
//
// ⭐ 硬規則（規格第 3 段）：**不要自己寫終端模擬器。**
// 自己寫 ANSI 狀態機會直接吃掉六週。這一層只是把現成函式庫包成我們要的形狀。
//
// W1 任務 #1（spike，最多一天）：拿 testdata 裡的錄影分別餵給候選函式庫，
// 看哪個能正確處理：SGR 顏色、粗體/底線、游標移動、清畫面、換行捲動、寬字元。
// 選完就把版本釘死在 go.mod，不要再換。
package term

import "github.com/co2water/svgcast/internal/glyph"

// ColorKind 區分顏色的三種來源。
//
// 一開始把「預設色」跟「RGB」都用 Index==-1 表示，結果純黑（RGB 0,0,0）
// 會被誤判成預設色。改成顯式的 Kind，零值剛好是 ColorDefault。
type ColorKind uint8

const (
	// ColorDefault 表示終端沒有指定顏色，交給渲染層用主題的預設前景／背景填。
	// ⭐ 深色模式（規格 ④）就是靠這個：這些格子會拿到 var(--fg)/var(--bg)。
	ColorDefault ColorKind = iota
	// ColorIndexed 是 0–255 的調色盤索引。0–15 會對應到主題的 CSS 變數。
	ColorIndexed
	// ColorRGB 是 24 位元真彩色。
	ColorRGB
)

// Color 是一個 cell 的顏色。
type Color struct {
	Kind    ColorKind
	Index   uint8 // Kind == ColorIndexed 時有效
	R, G, B uint8 // Kind == ColorRGB 時有效
}

// Attr 是文字樣式旗標。
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrFaint
	AttrItalic
	AttrUnderline
	AttrBlink // termtosvg「blink 不會閃」的 issue：v1 要決定是實作還是明確不做
	AttrReverse
	AttrStrike
)

// Style 是一個 cell 的完整外觀。
type Style struct {
	FG   Color
	BG   Color
	Attr Attr
}

// Cell 是螢幕上的一格。
type Cell struct {
	Rune  rune
	Style Style
	// Width 是這個字元佔幾格（1 或 2）。0 表示這是寬字元的第二格佔位。
	Width int
}

// Frame 是某個時間點的整個螢幕。
type Frame struct {
	Time   float64
	Rows   [][]Cell
	Cursor Cursor
}

// Cursor 是游標位置與可見性。
type Cursor struct {
	Row, Col int
	Visible  bool
}

// Emulator 是我們對 VT 函式庫的唯一依賴面。
//
// 把介面切在這裡，是為了讓 W1 的 spike 能換函式庫而不動到 render 那層。
type Emulator interface {
	// Resize 改變螢幕大小（對應 asciicast 的 "r" 事件）。
	Resize(cols, rows int)
	// Write 餵入終端輸出的位元組。
	Write(p []byte) (int, error)
	// Snapshot 回傳目前的螢幕狀態。實作必須回傳可安全保留的複本。
	Snapshot() Frame
}

// NewEmulator 的實作在 vt10x.go（W1 spike #1 已完成，選定 hinshun/vt10x）。

// NormalizeWidths 用我們自己的寬度規則填每一格的寬度。
//
// ⭐ 這是護城河接進管線的地方。
//
// spike #1 的發現：**vt10x 完全不算字元寬度**——它的原始碼裡連 "width" 這個字都沒有，
// 一個 rune 就佔一格，永遠前進 1。這件事有兩個後果：
//
//	✅ PUA（Powerline／Nerd Font）：vt10x 的天真模型**剛好就是我們要的**（寬度 1）。
//	   所以 termtosvg #14 那個 bug 不在模擬器，在**渲染層**——這也印證了我們把
//	   護城河放在 glyph + render 而不是 VT 層是對的。
//
//	⚠️ CJK 等雙寬字元：vt10x 會把它們一格一個塞，版面會錯。
//	   glyph.Width 在這裡標出 Width==2，但**格子本身的位置已經被 vt10x 排錯了**。
//	   v1 已知限制，寫進 README；要真的修得 fork 上游或加一層重排。
func NormalizeWidths(f *Frame) {
	for r := range f.Rows {
		for c := range f.Rows[r] {
			cell := &f.Rows[r][c]
			if cell.Rune == 0 {
				continue
			}
			cell.Width = glyph.Width(cell.Rune)
		}
	}
}

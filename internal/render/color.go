package render

import (
	"fmt"
	"strings"

	"github.com/co2water/svgcast/internal/term"
)

// Palette 是一套完整的配色。
//
// 只有 ANSI 的前 16 色與 FG/BG/Cursor 會隨主題改變——它們輸出成 CSS 變數，
// 深色模式（規格 ④）就是在 @media 區塊裡重新定義同一批變數。
// 256 色調色盤的第 16–255 色是規格固定的立方體與灰階，不隨主題變，直接寫死 hex。
type Palette struct {
	FG, BG, Cursor string
	ANSI           [16]string
}

// lightPalette 底色取 GitHub README 的白，前景取它的正文色。
var lightPalette = Palette{
	FG:     "#24292f",
	BG:     "#ffffff",
	Cursor: "#24292f",
	ANSI: [16]string{
		"#24292f", "#cf222e", "#116329", "#4d2d00",
		"#0969da", "#8250df", "#1b7c83", "#6e7781",
		"#57606a", "#a40e26", "#1a7f37", "#633c01",
		"#218bff", "#a475f9", "#3192aa", "#8c959f",
	},
}

// darkPalette 底色取 GitHub 深色模式的 #0d1117。
var darkPalette = Palette{
	FG:     "#e6edf3",
	BG:     "#0d1117",
	Cursor: "#e6edf3",
	ANSI: [16]string{
		"#484f58", "#ff7b72", "#3fb950", "#d29922",
		"#58a6ff", "#bc8cff", "#39c5cf", "#b1bac4",
		"#6e7681", "#ffa198", "#56d364", "#e3b341",
		"#79c0ff", "#d2a8ff", "#56d4dd", "#f0f6fc",
	},
}

// xterm256 回傳 xterm 256 色調色盤第 16–255 色的 hex。
//
// 16–231 是 6×6×6 的色彩立方體，232–255 是 24 階灰。
// 這段是規格定死的，不隨主題變。
func xterm256(i uint8) string {
	switch {
	case i < 16:
		// 不該走到這裡——0–15 由主題的 CSS 變數負責。
		return lightPalette.ANSI[i]
	case i < 232:
		levels := [6]int{0, 95, 135, 175, 215, 255}
		n := int(i) - 16
		r := levels[n/36]
		g := levels[(n/6)%6]
		b := levels[n%6]
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	default:
		v := 8 + 10*(int(i)-232)
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
}

// colorRef 把一個顏色轉成「CSS class 名稱」或「直接的 fill 值」。
//
// 回傳 class 非空時用 class（省位元組，且 0–15 能跟著主題變）；
// 否則用 fill 的字面值。fg 決定用前景還是背景的 class 前綴。
func colorRef(c term.Color, fg bool) (class, fill string) {
	prefix := "b"
	if fg {
		prefix = "f"
	}
	switch c.Kind {
	case term.ColorDefault:
		// 預設色由 <text>/<rect> 的繼承或畫布底色處理，不需要任何標記。
		return "", ""
	case term.ColorIndexed:
		if c.Index < 16 {
			return fmt.Sprintf("%s%d", prefix, c.Index), ""
		}
		return "", xterm256(c.Index)
	case term.ColorRGB:
		return "", fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return "", ""
}

// writePaletteVars 輸出一組 CSS 變數定義。
func writePaletteVars(b *strings.Builder, p Palette) {
	fmt.Fprintf(b, "--bg:%s;--fg:%s;--cur:%s;", p.BG, p.FG, p.Cursor)
	for i, c := range p.ANSI {
		fmt.Fprintf(b, "--c%d:%s;", i, c)
	}
}

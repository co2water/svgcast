// Package glyph 決定每個字元佔幾個終端 cell，以及在 SVG 裡要怎麼擺。
//
// ⭐ 這是整個專案的護城河（規格第 2 段 ①）。
//
// 背景：termtosvg #14「Powerline fonts have mangled output」2018 年開，至今 OPEN；
// svg-term-cli 也有人要 Nerd Font 支援，同樣沒人做。兩個專案、9,756★ 與 4,245★、
// 八年，沒有人修。修好它就贏了。
//
// 病根不是「不認得那些字」，是**寬度前進量（advance）對不上**：
//
//	終端把每個 cell 當固定寬度；但 Powerline／Nerd Font 的符號放在 Private Use Area，
//	字型給的 advance 不保證等於一個 cell。文字一路排下去就會逐格偏移，
//	愈往右偏得愈多——那就是使用者看到的 "mangled"。
//
// ⭐ 我們的解法（SVG 有終端沒有的優勢）：
//
//	終端只能一路往右推進，我們可以**明確指定每個位置的 x 座標**。
//	x = col * cellWidth，偏移就不可能累積。
//
//	代價是不能把整行合成一個 <text>，檔案會變大——所以只在**必要時**斷開：
//	一般 ASCII 連續排（省空間），碰到 PUA／寬字元就斷開並指定座標（保正確）。
//	MustBreakRun 就是這個判斷。這條規則同時服務 ①（正確）與 ②（體積）。
package glyph

import "github.com/mattn/go-runewidth"

// Range 是一段有名字的碼位區間，方便除錯與測試時看懂。
type Range struct {
	Lo, Hi rune
	Name   string
}

// nerdFontRanges 是 Nerd Fonts v3 的符號區間。
//
// ⚠️ W3 待辦：對照 Nerd Fonts 官方的 cheat sheet 逐一核對並補齊。
// 這裡先放最常出現在 shell prompt 裡的那幾組——它們就是 #14 的肇事者。
var nerdFontRanges = []Range{
	{0x23FB, 0x23FE, "IEC Power Symbols"},
	{0x2B58, 0x2B58, "IEC Power Symbols"},
	{0xE000, 0xE00A, "Pomicons"},
	{0xE0A0, 0xE0A2, "Powerline Symbols"},
	{0xE0A3, 0xE0A3, "Powerline Extra"},
	{0xE0B0, 0xE0B3, "Powerline Symbols"},
	{0xE0B4, 0xE0C8, "Powerline Extra"},
	{0xE0CA, 0xE0CA, "Powerline Extra"},
	{0xE0CC, 0xE0D7, "Powerline Extra"},
	{0xE200, 0xE2A9, "Font Awesome Extension"},
	{0xE300, 0xE3E3, "Weather Icons"},
	{0xE5FA, 0xE6B7, "Seti-UI + Custom"},
	{0xE700, 0xE8EF, "Devicons"},
	{0xEA60, 0xEC1E, "Codicons"},
	{0xED00, 0xF2FF, "Font Awesome"},
	{0xF300, 0xF375, "Font Logos"},
	{0xF400, 0xF533, "Octicons"},
	{0xF0001, 0xF1AF0, "Material Design Icons"}, // 注意：在第 15 平面，不是 BMP
}

// puaBMP 是 BMP 的私用區。go-runewidth 把這段當作「寬度不定（ambiguous）」，
// 會依 East Asian Width 設定回傳 1 或 2——這正是偏移的來源。
const (
	puaBMPLo = 0xE000
	puaBMPHi = 0xF8FF
)

// IsNerdFont 回報 r 是否落在已知的 Nerd Font 區間，並回傳區間名稱。
func IsNerdFont(r rune) (string, bool) {
	for _, rg := range nerdFontRanges {
		if r >= rg.Lo && r <= rg.Hi {
			return rg.Name, true
		}
	}
	return "", false
}

// IsPUA 回報 r 是否在私用區（含第 15、16 平面的補充私用區）。
func IsPUA(r rune) bool {
	switch {
	case r >= puaBMPLo && r <= puaBMPHi:
		return true
	case r >= 0xF0000 && r <= 0xFFFFD: // Supplementary PUA-A
		return true
	case r >= 0x100000 && r <= 0x10FFFD: // Supplementary PUA-B
		return true
	}
	return false
}

// Width 回傳 r 佔用幾個終端 cell。
//
// ⭐ 關鍵決定：**私用區一律當成 1 格。**
//
// 這是刻意偏離 go-runewidth 的行為。理由：實務上 Powerline 與 Nerd Font 的符號
// 就是設計成佔一格（它們是 prompt 的分隔符與圖示），而 go-runewidth 因為
// 「ambiguous」有時給 2，於是整行往右滑掉一格——就是 #14 的症狀。
//
// 這個決定必須寫進 README 的「已知行為」，因為它是有意的取捨。
func Width(r rune) int {
	if IsPUA(r) {
		return 1
	}
	switch {
	case r == 0:
		return 0
	case r < 0x20: // 控制字元不佔格
		return 0
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	if w > 2 {
		return 2
	}
	return w
}

// MustBreakRun 回報是否必須在此字元切斷 <text> 連續段、改用明確 x 座標。
//
// ⭐ 這個函式同時決定了正確性（①）與檔案體積（②）：
//   - 回 true 太少 → 偏移累積 → 就是那個八年沒修的 bug
//   - 回 true 太多 → 每個字一個 <text> → 檔案爆掉，變成 svg-term-cli 的老問題
//
// 目前規則：私用區與雙寬字元斷開；純 ASCII／一般文字連續排。
func MustBreakRun(r rune) bool {
	if IsPUA(r) {
		return true
	}
	if Width(r) == 2 {
		return true // CJK、emoji：字型的 advance 常與兩格不完全相等
	}
	if runewidth.IsAmbiguousWidth(r) {
		return true
	}
	return false
}

// StringWidth 是整串字的 cell 寬度。
func StringWidth(s string) int {
	n := 0
	for _, r := range s {
		n += Width(r)
	}
	return n
}

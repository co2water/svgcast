package render

import (
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/term"
)

// ⭐ 規格 v1.1 的 ④：同一個檔案跟著讀者的主題走。
//
// 這是唯一「GIF 結構上辦不到」的功能——點陣圖一個檔案只有一套顏色。
// GitHub 官方推薦的做法要兩個檔案（<picture> + 兩張圖），我們一個。
func TestTheme_AutoHasMediaQuery(t *testing.T) {
	opts := DefaultOptions()
	opts.Theme = "auto"
	out := renderStill(t, oneLineFrame("hi"), opts)

	if !strings.Contains(out, "prefers-color-scheme:dark") {
		t.Fatal("auto 模式應該輸出 @media (prefers-color-scheme: dark)")
	}
	// 淺色與深色的底色都要在，而且不一樣
	if !strings.Contains(out, lightPalette.BG) {
		t.Errorf("缺少淺色底色 %s", lightPalette.BG)
	}
	if !strings.Contains(out, darkPalette.BG) {
		t.Errorf("缺少深色底色 %s", darkPalette.BG)
	}
	if lightPalette.BG == darkPalette.BG {
		t.Fatal("兩套配色的底色相同，切換會看不出差別")
	}
}

func TestTheme_ForcedHasNoMediaQuery(t *testing.T) {
	for _, tc := range []struct {
		theme   string
		wantBG  string
		otherBG string
	}{
		{"light", lightPalette.BG, darkPalette.BG},
		{"dark", darkPalette.BG, lightPalette.BG},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			opts := DefaultOptions()
			opts.Theme = tc.theme
			out := renderStill(t, oneLineFrame("hi"), opts)

			if strings.Contains(out, "prefers-color-scheme") {
				t.Error("強制單一配色時不該有 media query")
			}
			if !strings.Contains(out, tc.wantBG) {
				t.Errorf("缺少 %s 的底色 %s", tc.theme, tc.wantBG)
			}
			if strings.Contains(out, tc.otherBG) {
				t.Errorf("不該包含另一套配色的底色 %s", tc.otherBG)
			}
		})
	}
}

// 未指定顏色的文字必須用主題變數，不能被寫死成固定的 hex——
// 寫死的話深色模式就不會生效。
func TestTheme_DefaultColorsUseVariables(t *testing.T) {
	opts := DefaultOptions()
	out := renderStill(t, oneLineFrame("hi"), opts)

	if !strings.Contains(out, "fill:var(--fg)") {
		t.Error("預設前景色應該是 var(--fg)")
	}
	if !strings.Contains(out, `fill="var(--bg)"`) {
		t.Error("畫布底色應該是 var(--bg)")
	}
}

// 0–15 的索引色要走 CSS 變數（會隨主題變）；16–255 直接寫 hex（不隨主題變）。
func TestTheme_PaletteSplit(t *testing.T) {
	f := oneLineFrame("ab")
	f.Rows[0][0].Style.FG = term.Color{Kind: term.ColorIndexed, Index: 3}   // 主題色
	f.Rows[0][1].Style.FG = term.Color{Kind: term.ColorIndexed, Index: 240} // 固定灰

	out := renderStill(t, f, DefaultOptions())

	if !strings.Contains(out, ".f3{fill:var(--c3)}") {
		t.Error("索引色 3 應該對應到 CSS 變數 --c3")
	}
	if !strings.Contains(out, xterm256(240)) {
		t.Errorf("索引色 240 應該直接寫成 %s", xterm256(240))
	}
}

func TestXterm256(t *testing.T) {
	cases := []struct {
		i    uint8
		want string
	}{
		{16, "#000000"},  // 色彩立方體的起點
		{231, "#ffffff"}, // 色彩立方體的終點
		{232, "#080808"}, // 灰階起點
		{255, "#eeeeee"}, // 灰階終點
		{240, "#585858"}, // powerline.cast 用到的底色
		{236, "#303030"},
	}
	for _, c := range cases {
		if got := xterm256(c.i); got != c.want {
			t.Errorf("xterm256(%d) = %s，預期 %s", c.i, got, c.want)
		}
	}
}

func oneLineFrame(s string) term.Frame {
	f := blankFrame(1, 20)
	setText(&f, 0, 0, s)
	return f
}

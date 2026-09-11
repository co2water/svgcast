package glyph

import "testing"

// 這張表就是 v1 的驗收標準。每一列都對應一個真實 issue。
func TestWidth(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want int
	}{
		// 一般情況
		{"ascii", 'a', 1},
		{"space", ' ', 1},
		{"控制字元不佔格", '\x1b', 0},

		// 雙寬
		{"CJK", '中', 2},
		{"全形括號", '（', 2},

		// ⭐ Powerline —— termtosvg #14 的肇事者
		{"powerline 右尖角 U+E0B0", 0xE0B0, 1},
		{"powerline 右細線 U+E0B1", 0xE0B1, 1},
		{"powerline 左尖角 U+E0B2", 0xE0B2, 1},
		{"powerline branch U+E0A0", 0xE0A0, 1},
		{"powerline line-number U+E0A1", 0xE0A1, 1},
		{"powerline 鎖 U+E0A2", 0xE0A2, 1},

		// ⭐ Nerd Font —— svg-term-cli 的請求
		{"devicons 區間", 0xE700, 1},
		{"codicons 區間", 0xEA60, 1},
		{"font awesome 區間", 0xF0DA, 1},
		{"material design（第 15 平面）", 0xF0001, 1},
	}
	for _, c := range cases {
		if got := Width(c.r); got != c.want {
			t.Errorf("%s: Width(%U) = %d，預期 %d", c.name, c.r, got, c.want)
		}
	}
}

func TestIsNerdFont(t *testing.T) {
	if name, ok := IsNerdFont(0xE0B0); !ok || name != "Powerline Symbols" {
		t.Errorf("U+E0B0 應為 Powerline Symbols，得到 %q ok=%v", name, ok)
	}
	if _, ok := IsNerdFont('a'); ok {
		t.Error("'a' 不該被判為 Nerd Font")
	}
}

func TestMustBreakRun(t *testing.T) {
	// ASCII 要能連續排，否則檔案會爆
	for _, r := range []rune("hello world 0123456789") {
		if MustBreakRun(r) {
			t.Errorf("ASCII %q 不該斷開連續段——會害檔案變大", r)
		}
	}
	// PUA 與雙寬一定要斷開，否則偏移會累積
	for _, r := range []rune{0xE0B0, 0xF0001, '中'} {
		if !MustBreakRun(r) {
			t.Errorf("%U 必須斷開連續段——不斷會累積偏移（termtosvg #14）", r)
		}
	}
}

// ⭐ 這是最重要的一個測試：整行 prompt 的累積寬度。
// #14 的症狀就是「愈往右偏得愈多」，所以要驗的是總和，不是單字。
func TestStringWidth_PowerlinePrompt(t *testing.T) {
	// 典型的 powerline prompt：文字 + 分隔符 + 文字 + 分隔符
	const sep = 0xE0B0 // powerline 右尖角分隔符
	// 用碼位建構，不貼字面 PUA 字元——字面 PUA 經過編輯器/git 常會壞掉，
	// 壞掉時會讓人誤以為是程式錯了。
	s := string(rune(sep)) + " ~/src " + string(rune(sep)) + " main " + string(rune(sep))
	want := 1 + 1 + 5 + 1 + 1 + 1 + 4 + 1 + 1 // 逐格數出來的期望值
	if got := StringWidth(s); got != want {
		t.Errorf("powerline prompt 寬度 = %d，預期 %d（偏移會在這裡現形）", got, want)
	}
}

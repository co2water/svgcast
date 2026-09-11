package glyph

import (
	"testing"

	"github.com/mattn/go-runewidth"
)

// TestWidth_DivergesFromRunewidth 把我們「刻意偏離 go-runewidth」這件事釘死。
//
// 這不只是文件，是保險：如果哪天 go-runewidth 改了行為，或有人「順手把它改回標準」，
// 這個測試會擋下來——因為那正是 termtosvg #14 的成因。
//
// 用 -v 跑可以看到對照表。
func TestWidth_DivergesFromRunewidth(t *testing.T) {
	// 模擬「使用者的終端設定了 East Asian Ambiguous = 寬」的情況。
	// 這是 CJK 使用者非常常見的設定，也是 bug 最容易現形的環境。
	eaw := runewidth.NewCondition()
	eaw.EastAsianWidth = true

	cases := []struct {
		name string
		r    rune
	}{
		{"powerline 右尖角", 0xE0B0},
		{"powerline 左尖角", 0xE0B2},
		{"powerline branch", 0xE0A0},
		{"devicons", 0xE700},
		{"codicons", 0xEA60},
	}

	t.Logf("%-22s %-10s %-14s %-10s", "字元", "碼位", "go-runewidth", "svgcast")
	diverged := 0
	for _, c := range cases {
		theirs := eaw.RuneWidth(c.r)
		ours := Width(c.r)
		t.Logf("%-22s U+%04X %-14d %-10d", c.name, c.r, theirs, ours)

		if ours != 1 {
			t.Errorf("%s (U+%04X): svgcast 回 %d，必須是 1", c.name, c.r, ours)
		}
		if theirs != ours {
			diverged++
		}
	}

	if diverged == 0 {
		t.Log("注意：這次沒有任何偏離。可能是 go-runewidth 換了行為——" +
			"若真是如此，重新確認 glyph.Width 的特例是否還有必要。")
	} else {
		t.Logf("共 %d/%d 個字元上刻意偏離 go-runewidth。"+
			"每一個偏離都是在防止一次累積偏移（termtosvg #14）。", diverged, len(cases))
	}
}

// TestPrompt_NoDrift 直接示範 bug 本身：整行 prompt 的累積偏移。
//
// #14 的症狀不是「某個字寬度錯了」，是「愈往右偏得愈多」。
// 所以要驗的是**最右邊那一格的欄位座標**。
func TestPrompt_NoDrift(t *testing.T) {
	const (
		branch = 0xE0A0
		sepR   = 0xE0B0
	)
	prompt := []rune(string(rune(branch)) + " main " + string(rune(sepR)) + " $ ")

	eaw := runewidth.NewCondition()
	eaw.EastAsianWidth = true

	var ours, theirs int
	for _, r := range prompt {
		ours += Width(r)
		theirs += eaw.RuneWidth(r)
	}

	t.Logf("prompt 共 %d 個字元", len(prompt))
	t.Logf("  svgcast 算出的結束欄位 : %d", ours)
	t.Logf("  go-runewidth 算出的     : %d", theirs)
	t.Logf("  偏移                    : %d 格", theirs-ours)

	// prompt 是：branch(1) + " main "(6) + sep(1) + " $ "(3) = 11
	const want = 11
	if ours != want {
		t.Errorf("結束欄位 = %d，預期 %d", ours, want)
	}
}

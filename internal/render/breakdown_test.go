package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
)

// TestBreakdown 不是斷言，是量測工具：把輸出的位元組拆給人看。
// 優化之前先知道錢花在哪，不要靠猜。
//
//	go test ./internal/render -run TestBreakdown -v
func TestBreakdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    func() *cast.Cast
	}{
		{"7s", synthSession},
		{"20s", synthSession20s},
	} {
		t.Run(tc.name, func(t *testing.T) { breakdown(t, tc.c()) })
	}
}

func breakdown(t *testing.T, c *cast.Cast) {
	out := renderCast(t, c, DefaultOptions())

	styleRE := regexp.MustCompile(`(?s)<style>(.*?)</style>`)
	m := styleRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("找不到 <style>")
	}
	style := m[1]
	body := out[len(m[0]):]

	kfRE := regexp.MustCompile(`@keyframes k\d+\{`)
	nameRE := regexp.MustCompile(`\.k\d+\{animation-name:k\d+\}`)
	gRE := regexp.MustCompile(`<g class="a k\d+">`)
	textRE := regexp.MustCompile(`<text[^>]*>`)
	rectRE := regexp.MustCompile(`<rect[^>]*>`)

	kfCount := len(kfRE.FindAllString(style, -1))
	nameDecls := nameRE.FindAllString(style, -1)
	groups := gRE.FindAllString(body, -1)
	texts := textRE.FindAllString(body, -1)
	rects := rectRE.FindAllString(body, -1)

	sum := func(ss []string) int {
		n := 0
		for _, s := range ss {
			n += len(s)
		}
		return n
	}

	// keyframes 區塊的總長 = style 扣掉主題變數與固定規則
	kfStart := strings.Index(style, "@keyframes")
	kfBytes := 0
	if kfStart >= 0 {
		kfBytes = len(style) - kfStart
	}
	themeBytes := len(style) - kfBytes

	t.Logf("總長                    %6d bytes", len(out))
	t.Logf("├─ <style>              %6d  (%.0f%%)", len(style), pct(len(style), len(out)))
	t.Logf("│   ├─ 主題與固定規則    %6d", themeBytes)
	t.Logf("│   └─ keyframes×%-3d    %6d  (每組約 %d)", kfCount, kfBytes, safeDiv(kfBytes, kfCount))
	t.Logf("│       └─ name 宣告     %6d  (每組約 %d)", sum(nameDecls), safeDiv(sum(nameDecls), len(nameDecls)))
	t.Logf("└─ body                 %6d  (%.0f%%)", len(body), pct(len(body), len(out)))
	t.Logf("    ├─ <g> 包裝×%-3d      %6d", len(groups), sum(groups)+len(groups)*len("</g>"))
	t.Logf("    ├─ <text>×%-4d       %6d  (每個約 %d，不含內容)", len(texts), sum(texts), safeDiv(sum(texts), len(texts)))
	t.Logf("    └─ <rect>×%-4d       %6d", len(rects), sum(rects))
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func safeDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

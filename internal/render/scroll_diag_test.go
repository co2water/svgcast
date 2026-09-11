package render

import (
	"regexp"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
)

// TestDiag_ScrollCost 驗證「捲動是體積爆炸的主因」這個假設。
//
// 做法：同樣的事件數、同樣的內容，只改終端高度。
// 矮的終端會捲動，高的不會。若捲動真是主因，兩者的差距會非常大。
func TestDiag_ScrollCost(t *testing.T) {
	build := func(rows int) *cast.Cast {
		c := synthSession20s()
		c.Header.Height = rows
		return c
	}

	textRE := regexp.MustCompile(`<text[^>]*>`)
	gRE := regexp.MustCompile(`<g class="a k\d+">`)

	for _, rows := range []int{24, 60, 200} {
		c := build(rows)
		out := renderCast(t, c, DefaultOptions())
		nText := len(textRE.FindAllString(out, -1))
		nGroup := len(gRE.FindAllString(out, -1))

		scrolls := "會捲動"
		if rows >= 120 {
			scrolls = "不捲動"
		}
		t.Logf("終端高度 %3d 列（%s）: %7d bytes, %5d 個 <text>, %4d 個動畫群組",
			rows, scrolls, len(out), nText, nGroup)
	}
}

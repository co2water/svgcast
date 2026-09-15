package render

import (
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/term"
)

// ── 進度條與控制列（svg-term-cli #96 的 time scrubber 需求） ───────

// 三張影格：0s "a"、1s "ab"、2s "abc"。加上 1.2s 結尾停留，總長 3.2s。
func progressFrames() []term.Frame {
	var fs []term.Frame
	for i, s := range []string{"a", "ab", "abc"} {
		fs = append(fs, frameWith(float64(i), 2, 10, s))
	}
	return fs
}

// renderCastFile 把一份 cast 檔走正式的串流路徑（Animator）渲染出來。
func renderCastFile(t *testing.T, path string, opts Options) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := cast.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return renderCast(t, c, opts)
}

// 預設不畫進度條：它會多幾百個位元組、也是一個看得見的元素，必須是使用者自己開的。
func TestProgress_OffByDefault(t *testing.T) {
	svg := animate(t, progressFrames(), DefaultOptions())
	for _, bad := range []string{`class="pg"`, `@keyframes pg`, `<script>`, `class="pgh"`} {
		if strings.Contains(svg, bad) {
			t.Errorf("預設輸出不該有 %s", bad)
		}
	}
}

func TestProgress_Bar(t *testing.T) {
	opts := DefaultOptions()
	opts.Progress = true
	svg := animate(t, progressFrames(), opts)

	for _, want := range []string{
		`<g class="pgw"`,
		`<rect class="pgt"`,
		`<rect class="pg"`,
		`@keyframes pg{from{transform:scaleX(0)}to{transform:scaleX(1)}}`,
		`animation:pg 3.2s linear infinite`, // 2s 內容 + 1.2s 結尾停留
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("缺少 %s", want)
		}
	}
	// 只開 Progress 不該有 script，也不該有點擊區。
	for _, bad := range []string{`<script>`, `class="pgh"`} {
		if strings.Contains(svg, bad) {
			t.Errorf("只開 --progress 不該有 %s", bad)
		}
	}
	parseSVG(t, svg) // 仍是合法 XML
}

// 不循環時進度條要停在 100%，跟其他動畫一樣用 forwards。
func TestProgress_NoLoopHolds(t *testing.T) {
	opts := DefaultOptions()
	opts.Progress = true
	opts.Loop = false
	svg := animate(t, progressFrames(), opts)
	if !strings.Contains(svg, `animation:pg 3.2s linear 1 forwards`) {
		t.Errorf("no-loop 的進度條應該用 1 forwards，實際：%s", cssRule(svg, ".pg{"))
	}
}

// 靜態輸出（總長 0）沒有進度可言，不該畫。
func TestProgress_SkippedWhenStatic(t *testing.T) {
	opts := DefaultOptions()
	opts.Controls = true
	opts.EndHold = 0
	svg := animate(t, []term.Frame{frameWith(0, 2, 10, "hi")}, opts)
	for _, bad := range []string{`class="pg"`, `<script>`} {
		if strings.Contains(svg, bad) {
			t.Errorf("靜態輸出不該有 %s", bad)
		}
	}
}

func TestControls_Script(t *testing.T) {
	opts := DefaultOptions()
	opts.Controls = true
	svg := animate(t, progressFrames(), opts)

	for _, want := range []string{
		`<rect class="pg"`,  // Controls 隱含進度條
		`<rect class="pgh"`, // 點擊區
		`.pgh{fill:none;pointer-events:all;cursor:pointer}`,
		`<script>//<![CDATA[`,
		`document.getAnimations()`,
		`var T=3200,`, // 總長度（毫秒）
		`//]]></script>`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("缺少 %s", want)
		}
	}
	if !strings.HasSuffix(svg, `</script></svg>`) {
		t.Errorf("script 應該緊接在 </svg> 之前，結尾是：%q", svg[len(svg)-40:])
	}
	// 有 script 仍然必須是合法 XML——CDATA 沒包好的話 <img> 會直接拒繪。
	parseSVG(t, svg)
}

// 進度條不能放在會捲動的 viewport 裡，否則它會跟著內容往上飛。
func TestProgress_OutsideViewport(t *testing.T) {
	opts := DefaultOptions()
	opts.Progress = true
	svg := renderCastFile(t, "../../testdata/scroll.cast", opts)
	if !strings.Contains(svg, `<g class="vp">`) {
		t.Fatal("scroll.cast 應該觸發捲動（有 .vp），否則這個測試沒有意義")
	}

	// 用 XML 解析，找 .pgw 的父元素。
	dec := xml.NewDecoder(strings.NewReader(svg))
	var stack []string
	parent := "(沒找到 .pgw)"
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch e := tok.(type) {
		case xml.StartElement:
			cls := ""
			for _, a := range e.Attr {
				if a.Name.Local == "class" {
					cls = a.Value
				}
			}
			if cls == "pgw" {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, e.Name.Local+"."+cls)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	if parent != "svg." {
		t.Errorf("進度條的父元素是 %q，應該直接掛在 <svg> 下", parent)
	}
}

func TestControls_Golden(t *testing.T) {
	opts := DefaultOptions()
	opts.Controls = true
	checkGolden(t, "anim-controls-hello.svg", renderCastFile(t, "../../testdata/hello.cast", opts))
}

// cssRule 取出第一個含有 needle 的 CSS 規則，給錯誤訊息用。
func cssRule(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "(沒找到)"
	}
	j := strings.Index(s[i:], "}")
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j+1]
}

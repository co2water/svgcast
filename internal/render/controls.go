package render

import (
	"fmt"
	"strings"
)

// ═══════════════════════════════════════════════════════════════════
// 進度條與播放控制（svg-term-cli #96 的 time scrubber 需求）
// ═══════════════════════════════════════════════════════════════════
//
// 長錄影的讀者最常抱怨的是「不知道播到哪、不能拖」。這裡分成兩層，
// 因為兩層能跑的環境不一樣：
//
//	--progress   純 CSS 的進度條。跟其他動畫一樣走 CSS keyframes，
//	             所以 <img> 情境（GitHub README）也會動。
//	--controls   點進度條跳到該時間、點畫面暫停／繼續、空白鍵與方向鍵。
//	             需要 <script>，只有把 SVG 當「文件」開的時候才會執行：
//	             直接開檔、<object>、<iframe>、自己網站上的連結。
//
// ⚠️ 兩個誠實的限制，README 裡也要寫：
//
//	1. <img> 永遠不執行 script（約束一，見 svg.go），所以 README 裡只有進度條。
//	2. raw.githubusercontent.com 的回應帶
//	   Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox
//	   ——連「點圖片開原檔」都不會跑 script。控制列在 GitHub Pages、docs 站、
//	   本機檔案上才有作用。
//
// 控制列的實作走 Web Animations API：document.getAnimations() 拿到文件裡
// 每一個 CSS 動畫，統一設 currentTime。所有動畫的 duration 都等於總長度、
// 都從文件載入時同時開始，所以設同一個 currentTime 就會同步。
//
// 不能用「改 animation-delay 來跳時間」的偷懶做法：改 delay 不會重啟動畫，
// 有效時間 = 已播時間 − delay，跳到的位置會跟已經播了多久有關，
// 拖第二次就不準了。

const (
	progressH = 3 // 進度條高度（px）
	hitH      = 16
)

// writeProgress 在底部留白裡畫一條進度條，寬度用 scaleX 從 0 線性長到 1。
//
// 放在 viewport <g> 之外——它不能跟著內容捲動。
func writeProgress(body, css *strings.Builder, g geom, opts Options, total float64) {
	if total <= 0 {
		return
	}
	w := g.width - 2*g.pad
	if w <= 0 {
		return
	}
	// 條放在下方留白的正中間；留白太窄就貼著底邊。
	gap := g.pad
	if gap > 8 {
		gap = 8
	}
	y := g.height - progressH - (gap-progressH)/2
	if y < 0 {
		y = 0
	}

	fmt.Fprintf(body, `<g class="pgw" transform="translate(%s,%s)">`, fnum(g.pad), fnum(y))
	// 軌道
	fmt.Fprintf(body, `<rect class="pgt" width="%s" height="%d" rx="1.5"/>`, fnum(w), progressH)
	// 進度
	fmt.Fprintf(body, `<rect class="pg" width="%s" height="%d" rx="1.5"/>`, fnum(w), progressH)
	if opts.Controls {
		// 透明的點擊區，比條本身高，好抓。
		fmt.Fprintf(body, `<rect class="pgh" y="%s" width="%s" height="%d"/>`,
			fnum(-float64(hitH-progressH)/2), fnum(w), hitH)
	}
	body.WriteString(`</g>`)

	iter := "infinite"
	if !opts.Loop {
		iter = "1 forwards"
	}
	css.WriteString(`.pgt{fill:var(--fg);opacity:.15}`)
	fmt.Fprintf(css, `.pg{fill:var(--fg);opacity:.45;transform-origin:0 0;animation:pg %ss linear %s}`,
		fnum(total), iter)
	css.WriteString(`@keyframes pg{from{transform:scaleX(0)}to{transform:scaleX(1)}}`)
	if opts.Controls {
		css.WriteString(`.pgh{fill:none;pointer-events:all;cursor:pointer}`)
	}
}

// writeControls 附上控制用的 script。
//
// 內容刻意寫成不依賴任何函式庫、不用新語法的樣子，並包在 CDATA 裡，
// 讓 SVG 仍然是合法的 XML（測試會用 encoding/xml 整份解析）。
func writeControls(body *strings.Builder, total float64) {
	if total <= 0 {
		return
	}
	body.WriteString("<script>//<![CDATA[\n")
	fmt.Fprintf(body, controlsJS, fnum(total*1000))
	body.WriteString("\n//]]></script>")
}

// controlsJS 是控制列的程式。%s 是總長度（毫秒）。
//
//	點進度條／拖曳  跳到該時間
//	點畫面          暫停／繼續
//	空白鍵          暫停／繼續
//	← →            前後跳總長的 1/20（至少 1 秒）
//
// 暫停狀態從動畫本身讀（playState），不另外記旗標——
// 旗標會跟外部呼叫（例如宿主頁面自己 pause）失去同步。
const controlsJS = `(function(){var T=%s,s=document.documentElement,h=s.querySelector('.pgh');if(!h||!document.getAnimations)return;
var A=function(){return document.getAnimations()},drag=false;
function at(t){A().forEach(function(a){a.currentTime=t})}
function seek(x){var r=h.getBoundingClientRect();at(Math.min(1,Math.max(0,(x-r.left)/r.width))*T)}
function toggle(){var a=A();if(!a.length)return;var p=a[0].playState==='paused';a.forEach(function(x){p?x.play():x.pause()})}
h.addEventListener('pointerdown',function(e){drag=true;h.setPointerCapture(e.pointerId);seek(e.clientX);e.preventDefault()});
h.addEventListener('pointermove',function(e){if(drag)seek(e.clientX)});
h.addEventListener('pointerup',function(){drag=false});
h.addEventListener('pointercancel',function(){drag=false});
s.addEventListener('click',function(e){if(e.target!==h)toggle()});
s.addEventListener('keydown',function(e){
if(e.key===' '){toggle();e.preventDefault();return}
if(e.key!=='ArrowRight'&&e.key!=='ArrowLeft')return;
var a=A();if(!a.length)return;var step=Math.max(1000,T/20),t=(a[0].currentTime||0)+(e.key==='ArrowRight'?step:-step);
at(((t%%T)+T)%%T);e.preventDefault()});
s.setAttribute('tabindex','0');s.style.outline='none';
})();`

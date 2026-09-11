package render

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/co2water/svgcast/internal/term"
)

// timeQuantum 是時間量化的粒度。
//
// 把時間戳對齊到 1 ms，讓「幾乎同時」出現的內容真的落進同一組 keyframes。
// 終端一次印一整列，同一瞬間會產生很多段文字——分組成功與否直接決定 CSS 的大小。
//
// ⚠️ 放寬它「看起來」像是省體積的好槓桿，但實測過了，不要再試一次：
//
//	1 ms   74,931 bytes  252 組
//	20 ms  74,949        252     ← 完全沒用
//	50 ms  74,439        249     ← 幾乎沒用
//	100 ms 61,528        164     ← 省 18%，但打字會變成兩個字一跳
//
// 打字速度落在 50–80 ms/字，量化粒度一跨過去就吃掉按鍵之間的間隔，
// 畫面品質的損失肉眼可見。省下的 18% 不值得。
const timeQuantum = 0.001

// pctPrecision 是百分比輸出的小數位數。
// 過少會讓相鄰的時間點撞在一起（VHS 那個 SVG fork 的 PR #646 就是這個 bug）；
// 過多只是浪費位元組。
const pctPrecision = 4

// minPctStep 是兩個百分比之間至少要差多少，才不會在 CSS 裡變成同一個停格點。
var minPctStep = math.Pow(10, -pctPrecision)

// Animator 逐張吃進影格，算出每一段內容的存活區間。
//
// ⭐ 串流：只保留「目前開著的 run」與「已完成的 run」，不保留任何歷史影格。
// 記憶體用量是 O(畫面大小 + 內容變化次數)，與影片長度無關——
// 這是 svg-term-cli 那個 heap out of memory 的正解。
type Animator struct {
	opts Options

	open [][]run // 每一個**螢幕**列目前顯示中的 run（Start 已填）
	done []run   // 已經關閉的 run（Start/End 都已填）

	// ⭐ 捲動處理。
	//
	// 終端每捲一列，畫面上所有內容的螢幕列索引都會變。若照著關閉再重開，
	// 一次捲動就是整螢幕的 run 重來一遍——實測 22 秒的錄影會從 62 KB 爆到 386 KB。
	//
	// 改成：run 存**絕對列座標**（螢幕列 + 累積捲動量），捲出畫面的內容
	// 不關閉、留在文件裡；整份內容包一層 viewport <g>，用 translateY 動畫。
	// 一次捲動只花一個 keyframe 停格。
	//
	// 捲出視野的內容會被根 <svg> 的 viewBox 自動裁掉，不需要額外的 clipPath。
	scrollOff   int          // 累積捲掉幾列
	scrollTrack []scrollStop // 捲動量的變化點
	scrolled    []run        // 已捲出畫面、但仍然開著的 run

	// ⭐ 游標不走一般的 run 差分。
	//
	// 逐字打字時游標每一次按鍵都移動，若當成 run 處理，每次移動就是
	// 「關掉舊的、開一個新的」——一段 50 個字的指令會產生 50 個獨立的
	// 動畫群組，每組都要一份 keyframes、一份 animation-name、一個 <g> 包裝。
	// 實測那樣光游標就佔掉整份檔案的一半。
	//
	// 改成一個元素 + 一條位置軌道：只有一份 keyframes，每個時間點只花一個停格。
	cursorTrack []cursorStop

	cols, rows int     // 取所有影格的最大值
	last       float64 // 最後一張影格的時間
	started    bool
}

// NewAnimator 建立一個累積器。
func NewAnimator(opts Options) *Animator {
	return &Animator{opts: opts}
}

// quantize 把時間對齊到 timeQuantum。
func quantize(t float64) float64 {
	return math.Round(t/timeQuantum) * timeQuantum
}

// Add 吃進一張影格。影格必須按時間遞增送入。
func (a *Animator) Add(f term.Frame) error {
	if len(f.Rows) == 0 {
		return fmt.Errorf("影格是空的")
	}
	t := quantize(f.Time)
	if a.started && t < a.last {
		return fmt.Errorf("影格時間倒退：%.3f 在 %.3f 之後送入", t, a.last)
	}
	a.started = true
	a.last = t

	if c := len(f.Rows[0]); c > a.cols {
		a.cols = c
	}
	if r := len(f.Rows); r > a.rows {
		a.rows = r
	}

	// 列數變多時補上空的槽位
	for len(a.open) < len(f.Rows) {
		a.open = append(a.open, nil)
	}

	rows := runsByRow(f)

	// 先看整片畫面是不是往上捲了——是的話只要平移 viewport，
	// 接下來的差分就會發現大部分列都沒變。
	if k := a.detectScroll(rows); k > 0 {
		a.applyScroll(k, t)
	}

	for y := range rows {
		a.diffRow(y, rows[y], t)
	}
	// 影格變矮時，超出範圍的列要整列關掉
	for y := len(rows); y < len(a.open); y++ {
		a.diffRow(y, nil, t)
	}

	a.updateCursor(f, t)
	return nil
}

// scrollStop 是 viewport 軌道上的一個時間點。
type scrollStop struct {
	t   float64
	off int
}

// rowsEqual 比較兩列的內容是否相同（只看欄位、文字、樣式，不看列號——
// 列號正是捲動時會變的東西）。
func rowsEqual(as, bs []run) bool {
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !sameRun(as[i], bs[i]) {
			return false
		}
	}
	return true
}

// detectScroll 判斷新影格是不是舊畫面往上捲了 k 列。
//
// ⚠️ 不能要求「整段都對得上」。捲動與新內容通常發生在**同一個事件**裡——
// 往最後一列寫字、再換行才觸發捲動——所以新畫面的底部必然是新內容。
// 第一版就是栽在這裡：比到最後一列發現不同，整個 k 被否決，一次都沒偵測到。
//
// 改成找「最佳對齊」：對每個候選 k 算出從頂端開始能連續對上幾列，
// 取對得最長的那個。沒有捲動時 k=0 自然勝出。
func (a *Animator) detectScroll(next [][]run) int {
	n := len(next)
	if n > len(a.open) {
		n = len(a.open)
	}
	if n < 2 {
		return 0
	}

	// 從頂端算起連續對上的列數，以及其中有幾列是非空的。
	align := func(k int) (matched, nonEmpty int) {
		for y := 0; y+k < n; y++ {
			if !rowsEqual(next[y], a.open[y+k]) {
				return
			}
			matched++
			if len(next[y]) > 0 {
				nonEmpty++
			}
		}
		return
	}

	best, _ := align(0) // 不捲動時的對齊長度，當作基準
	bestK := 0

	for k := 1; k < n; k++ {
		matched, nonEmpty := align(k)
		// 至少要對上一列**有內容**的，否則整片空白對任何 k 都成立，
		// 清空畫面就會被誤判成捲動。
		if nonEmpty > 0 && matched > best {
			best, bestK = matched, k
		}
	}
	return bestK
}

// applyScroll 把畫面往上移 k 列。
//
// 捲出去的那幾列**不關閉**——它們還在文件裡，只是被 viewport 裁掉了。
// 這正是省下大量元素的地方。
func (a *Animator) applyScroll(k int, t float64) {
	if k > len(a.open) {
		k = len(a.open)
	}
	for y := 0; y < k; y++ {
		a.scrolled = append(a.scrolled, a.open[y]...)
		a.open[y] = nil
	}
	copy(a.open, a.open[k:])
	for y := len(a.open) - k; y < len(a.open); y++ {
		a.open[y] = nil
	}

	a.scrollOff += k
	a.scrollTrack = append(a.scrollTrack, scrollStop{t: t, off: a.scrollOff})
}

// ⭐ diffRow 是體積的關鍵：保留共同前綴的列級差分。
//
// 純粹的「整列重畫」在逐字打指令時，每按一個鍵就會關掉整列再開一次，
// 一行 40 個字就產生 40×40 個元素。
//
// 保留前綴之後，打字只會在列尾新增一段——前面已經畫好的部分完全不動。
// 更細的 cell 級差分留給 v2；列級加前綴在真實的終端錄影裡已經吃掉絕大部分冗餘。
func (a *Animator) diffRow(y int, next []run, t float64) {
	cur := a.open[y]

	var kept []run
	i, j := 0, 0

	// pend 是「正在跟 cur 比對的那一段新內容」。
	// 發生部分匹配（打字）時它會被削掉前面已經匹配的部分，剩下的繼續往下比。
	var pend run
	havePend := false

	for i < len(cur) {
		if !havePend {
			if j >= len(next) {
				break
			}
			pend = next[j]
			havePend = true
		}
		c := cur[i]

		// 完全相同：這一段不動，繼續往下。
		if sameRun(c, pend) {
			kept = append(kept, c)
			i++
			j++
			havePend = false
			continue
		}

		// ⭐ 打字的情況：舊的那段是新那段的前綴（同欄、同樣式）。
		// 保留舊 run 不動，只把多出來的字當成一段新 run——
		// 這就是「一行 40 個字不會產生 40×40 個元素」的關鍵。
		if c.Col == pend.Col && c.Style == pend.Style &&
			len(pend.Text) > len(c.Text) && strings.HasPrefix(pend.Text, c.Text) {
			kept = append(kept, c)
			pend.Col = c.Col + c.cells()
			pend.Text = pend.Text[len(c.Text):]
			i++
			continue // havePend 維持 true，pend 現在是剩餘的部分
		}

		break
	}

	// 沒能匹配到的舊 run 結束
	for _, r := range cur[i:] {
		r.End = t
		a.done = append(a.done, r)
	}

	// 還沒被消化的新內容開始。
	// ⭐ 開啟時把螢幕列換成絕對列座標——之後就算畫面捲動，
	// 這段內容的座標也不會變，不必關閉重開。
	if havePend {
		pend.Start = t
		pend.Row = y + a.scrollOff
		kept = append(kept, pend)
		j++
	}
	for _, r := range next[j:] {
		r.Start = t
		r.Row = y + a.scrollOff
		kept = append(kept, r)
	}

	a.open[y] = kept
}

func sameRun(a, b run) bool {
	return a.Col == b.Col && a.Text == b.Text && a.Style == b.Style
}

// cursorStop 是游標軌道上的一個時間點。
type cursorStop struct {
	t        float64
	row, col int
	visible  bool
}

func (a *Animator) updateCursor(f term.Frame, t float64) {
	// 游標也用絕對列座標——它跟著內容一起放在 viewport 裡。
	s := cursorStop{
		t:       t,
		row:     f.Cursor.Row + a.scrollOff,
		col:     f.Cursor.Col,
		visible: a.opts.Cursor && f.Cursor.Visible && f.Cursor.Row < a.rows && f.Cursor.Col < a.cols,
	}
	// 位置與可見性都沒變就不用記一個停格。
	if n := len(a.cursorTrack); n > 0 {
		p := a.cursorTrack[n-1]
		if p.row == s.row && p.col == s.col && p.visible == s.visible {
			return
		}
	}
	a.cursorTrack = append(a.cursorTrack, s)
}

// cursorMarker 保留給 run 的識別（目前只有測試用得到）。
const cursorMarker = "\x00cursor"

// totalDuration 是動畫一圈的長度：最後一張影格的時間，加上結尾停留。
func (a *Animator) totalDuration() float64 {
	hold := a.opts.EndHold
	if hold < 0 {
		hold = 0
	}
	return a.last + hold
}

// finish 關閉所有還開著的 run，讓它們一直活到結尾停留結束。
func (a *Animator) finish() {
	end := a.totalDuration()
	for y := range a.open {
		for _, r := range a.open[y] {
			r.End = end
			a.done = append(a.done, r)
		}
		a.open[y] = nil
	}
	// 已經捲出畫面但從未關閉的內容，一樣活到最後——
	// 它們被 viewport 裁掉，不會畫在畫面上。
	for _, r := range a.scrolled {
		r.End = end
		a.done = append(a.done, r)
	}
	a.scrolled = nil
}

// writeScrollTrack 輸出 viewport 的平移動畫。
//
// 一次捲動只花一個停格，而不是整螢幕的 run 重開一遍。
func (a *Animator) writeScrollTrack(css *strings.Builder, g geom, total float64) {
	if len(a.scrollTrack) == 0 || total <= 0 {
		return
	}
	css.WriteString("@keyframes vp{0%{transform:translateY(0)}")
	last := ""
	for _, s := range a.scrollTrack {
		p := fmtPct(roundPct(s.t/total*100), 2)
		if p == last || p == "0" {
			continue
		}
		last = p
		fmt.Fprintf(css, "%s%%{transform:translateY(%spx)}", p, fnum(-float64(s.off)*g.lineH))
	}
	css.WriteString("}")

	iter := "infinite"
	if !a.opts.Loop {
		iter = "1 forwards"
	}
	fmt.Fprintf(css, ".vp{animation:vp %ss steps(1,end) %s}", fnum(total), iter)
}

// writeCursorTrack 輸出游標：一個元素 + 一條 transform 軌道。
//
// 每個停格只花「百分比 + translate」，而不是一整組 keyframes + animation-name
// + <g> 包裝 + 一個新的 <rect>。
func (a *Animator) writeCursorTrack(body, css *strings.Builder, g geom, total float64) {
	track := a.cursorTrack
	// 去掉開頭的不可見停格
	for len(track) > 0 && !track[0].visible {
		track = track[1:]
	}
	if len(track) == 0 {
		return
	}

	first := track[0]
	// CSS 的長度值只有 0 可以省略單位，其餘一律要 px。
	unit := func(v float64) string {
		s := fnum(v)
		if s == "0" {
			return "0"
		}
		return s + "px"
	}
	pos := func(s cursorStop) string {
		return "translate(" +
			unit(g.x(s.col)-g.x(first.col)) + "," +
			unit(g.rowTop(s.row)-g.rowTop(first.row)) + ")"
	}

	body.WriteString("<rect")
	attr(body, "class", "cur")
	attr(body, "x", fnum(g.x(first.col)))
	attr(body, "y", fnum(g.rowTop(first.row)))
	attr(body, "width", fnum(g.cellW))
	attr(body, "height", fnum(g.lineH))
	body.WriteString("/>")

	// 只有一個停格就不需要動畫。
	if len(track) == 1 && first.visible {
		return
	}
	if total <= 0 {
		return
	}

	// 只有在可見性真的變過的時候才需要輸出 opacity——
	// 絕大多數錄影裡游標一直是可見的，那 90 個停格就能各省 10 個位元組。
	needOpacity := false
	for _, s := range track {
		if !s.visible {
			needOpacity = true
			break
		}
	}

	prec := 2
	css.WriteString("@keyframes cur{")
	last := ""
	for _, s := range track {
		p := fmtPct(roundPct(s.t/total*100), prec)
		if p == last {
			continue // 同一個停格點，後面的會覆蓋前面的，直接跳過
		}
		last = p
		fmt.Fprintf(css, "%s%%{transform:%s", p, pos(s))
		if needOpacity {
			op := "1"
			if !s.visible {
				op = "0"
			}
			fmt.Fprintf(css, ";opacity:%s", op)
		}
		css.WriteString("}")
	}
	css.WriteString("}")

	iter := "infinite"
	if !a.opts.Loop {
		iter = "1 forwards"
	}
	fmt.Fprintf(css, ".cur{animation:cur %ss steps(1,end) %s}", fnum(total), iter)
}

// span 是一段存活區間。相同 span 的 run 共用同一組 keyframes。
type span struct{ start, end float64 }

// Write 產出完整的動畫 SVG。
func (a *Animator) Write(w io.Writer) error {
	a.finish()

	if a.cols == 0 || a.rows == 0 {
		return fmt.Errorf("沒有任何影格")
	}
	g := a.opts.geom(a.cols, a.rows)
	total := a.totalDuration()

	// 只有一張影格（或全部同時發生）就沒有動畫可言，退化成靜態輸出。
	static := total <= 0

	// 依 span 分組。always 是「從頭到尾都在」的內容——它們完全不需要動畫，
	// 這在終端錄影裡佔比很高（提示字元、標題列、不再變動的輸出）。
	groups := map[span][]run{}
	var always []run
	for _, r := range a.done {
		if static || (r.Start <= 0 && r.End >= total) {
			always = append(always, r)
			continue
		}
		groups[span{r.Start, r.End}] = append(groups[span{r.Start, r.End}], r)
	}

	// 排序讓輸出穩定（golden 測試需要）
	keys := make([]span, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].start != keys[j].start {
			return keys[i].start < keys[j].start
		}
		return keys[i].end < keys[j].end
	})

	lib, err := buildGlyphLib(a.opts, a.done)
	if err != nil {
		return err
	}

	cs := classSet{}
	var body, css strings.Builder

	// 先畫沒有動畫的內容（不需要包 <g>，也不需要任何 CSS）
	writeGroup(&body, g, always, cs, "", lib)

	if len(keys) > 0 {
		iter := "infinite"
		fill := ""
		if !a.opts.Loop {
			iter = "1"
			fill = ";animation-fill-mode:forwards"
		}

		// ⭐ 體積優化 1：共用的動畫屬性只寫一次。
		// 每一組原本都要重複 "opacity:0;animation:kN Ts steps(1,end) infinite"，
		// 抽出來之後每組只剩 animation-name。
		fmt.Fprintf(&css, ".a{opacity:0;animation-duration:%ss;"+
			"animation-timing-function:steps(1,end);animation-iteration-count:%s%s}",
			fnum(total), iter, fill)

		prec := pctPrecisionFor(keys, total)

		for i, k := range keys {
			cls := fmt.Sprintf("k%d", i)
			lo, hi := pctRange(k.start, k.end, total)

			// steps(1,end)：每一段區間都維持起始值，到區間結束才跳變。
			// 用 opacity 而不是 visibility——visibility 在 CSS 動畫裡有特殊的插值
			// 規則（只要端點之一是 visible，整段就會被當成可見），元素該消失時不會消失。
			loS, hiS := fmtPct(lo, prec), fmtPct(hi, prec)

			fmt.Fprintf(&css, "@keyframes %s{", cls)
			// 只有在 lo 真的不是 0% 時才需要前面那段隱藏；
			// 兩者相同會產生重複的停格點（見 pctPrecisionFor 的說明）。
			if loS != "0" {
				css.WriteString("0%{opacity:0}")
			}
			fmt.Fprintf(&css, "%s%%{opacity:1}", loS)
			if hiS != "100" && hiS != loS {
				fmt.Fprintf(&css, "%s%%{opacity:0}", hiS)
			}
			css.WriteString("}")

			fmt.Fprintf(&css, ".%s{animation-name:%s}", cls, cls)

			// ⭐ 體積優化 2：整組包在一個 <g> 裡，class 只寫一次。
			// 終端一次印一整列，一組裡常有十幾段文字——
			// 逐個掛 class 是十幾份重複。
			fmt.Fprintf(&body, `<g class="a %s">`, cls)
			writeGroup(&body, g, groups[k], cs, "", lib)
			body.WriteString("</g>")
		}
	}

	a.writeCursorTrack(&body, &css, g, total)
	a.writeScrollTrack(&css, g, total)

	// 有捲動時，整份內容包一層會平移的 viewport。
	// 沒捲動就不包，省下一層元素。
	content := body.String()
	if len(a.scrollTrack) > 0 {
		content = `<g class="vp">` + content + `</g>`
	}

	return writeDocument(w, g, a.opts, cs, content, css.String(), lib)
}

// ⭐ 體積優化 3：百分比只用剛好夠用的小數位數。
//
// 固定 4 位在短錄影裡是浪費——3.5 秒的錄影用 2 位就足以區分所有停格點，
// 而每個群組有兩個百分比，省下來的量隨群組數線性成長。
//
// 但精度不能砍到讓兩個不同的時間點撞在一起（VHS 的 SVG fork 就是栽在這裡，
// PR #646「Fix SVG keyframe collisions with dynamic precision」），
// 所以這裡從低到高試，取第一個不會產生碰撞的位數。
func pctPrecisionFor(keys []span, total float64) int {
	if total <= 0 {
		return 0
	}
	// 蒐集所有會被寫進 CSS 的百分比。
	//
	// ⚠️ 必須把隱含的 0% 與 100% 也算進去：keyframes 裡會補上
	// "0%{opacity:0}" 與 "…%{opacity:0}" 兩個邊界停格，
	// 如果 lo 被四捨五入成 "0"，就會跟那個隱含的 0% 撞在一起。
	// （第一版漏掉這件事，TestKeyframes_StrictlyIncreasing 抓到了。）
	vals := make([]float64, 0, len(keys)*2+2)
	vals = append(vals, 0, 100)
	for _, k := range keys {
		lo, hi := pctRange(k.start, k.end, total)
		vals = append(vals, lo, hi)
	}

	for p := 0; p < pctPrecision; p++ {
		seen := map[string]float64{}
		ok := true
		for _, v := range vals {
			s := fmtPct(v, p)
			if prev, dup := seen[s]; dup && prev != v {
				ok = false // 兩個不同的值被四捨五入到同一個字串
				break
			}
			seen[s] = v
		}
		if ok {
			return p
		}
	}
	return pctPrecision
}

// fmtPct 以指定小數位數格式化百分比，並去掉尾端的零。
func fmtPct(v float64, prec int) string {
	s := fmt.Sprintf("%.*f", prec, v)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "" {
		return "0"
	}
	return s
}

// writeGroup 輸出一組 run（背景色塊先於文字，游標另外處理）。
//
// 同一組的 run 存活區間相同，所以背景色塊可以安全地合併——
// 它們會同時出現、同時消失。
func writeGroup(b *strings.Builder, g geom, runs []run, cs classSet, animClass string, lib *glyphLib) {
	writeBGRects(b, g, runs, cs, animClass)
	for _, r := range runs {
		if r.Text == cursorMarker {
			writeCursor(b, g, r, animClass)
			continue
		}
		writeTextRun(b, g, r, cs, animClass, lib)
	}
}

// pctRange 把一段時間換算成百分比，並保證 lo 嚴格小於 hi。
//
// ⚠️ 這裡是 VHS 的 SVG fork 踩過的坑（PR #646「Fix SVG keyframe collisions
// with dynamic precision」）：兩個時間點四捨五入到同一個百分比之後，
// keyframes 會出現重複的停格點，動畫行為就變得無法預測。
func pctRange(start, end, total float64) (lo, hi float64) {
	if total <= 0 {
		return 0, 100
	}
	lo = roundPct(start / total * 100)
	hi = roundPct(end / total * 100)

	if lo < 0 {
		lo = 0
	}
	if hi > 100 {
		hi = 100
	}
	// 保證嚴格遞增：至少差一個最小可表示的步進。
	if hi <= lo {
		hi = lo + minPctStep
		if hi > 100 {
			hi = 100
			lo = 100 - minPctStep
		}
	}
	return lo, hi
}

func roundPct(v float64) float64 {
	p := math.Pow(10, pctPrecision)
	return math.Round(v*p) / p
}

// fnumPct 格式化百分比，去掉尾端的零。
func fnumPct(v float64) string {
	s := strings.TrimRight(fmt.Sprintf("%.*f", pctPrecision, v), "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

// Render 是給測試與簡單呼叫用的便利函式。
// 正式路徑請用 NewAnimator + Add + Write，那條路是串流的。
func Render(w io.Writer, fs []term.Frame, opts Options) error {
	a := NewAnimator(opts)
	for _, f := range fs {
		if err := a.Add(f); err != nil {
			return err
		}
	}
	return a.Write(w)
}

// Package render 把螢幕影格轉成動畫 SVG。
//
// ═══════════════════════════════════════════════════════════════════
// 兩個硬約束，先讀懂再動手
// ═══════════════════════════════════════════════════════════════════
//
// 【約束一】SVG 會被當成 <img> 載入 —— 必須完全自給自足
//
//	README 裡是 ![demo](demo.svg)，瀏覽器以「圖片」模式載入 SVG。
//	這個模式下：外部 CSS 不會套用、<script> 不會執行、**外部字型不會下載**。
//	→ 樣式一律 inline <style>，不得有任何外部資源參照。
//	→ 這也是為什麼不能 @import 一個 Nerd Font。
//
// 【約束二】字型與體積是同一個問題的兩面
//
//	Powerline／Nerd Font 的符號在私用區。使用者的機器不一定裝了那些字型，
//	而我們又不能從外部載入 → 畫出來就是空白方框（豆腐）。
//	唯一的解是**把字型嵌進 SVG**，但整套 Nerd Font 是好幾 MB，
//	直接撞上 GitHub 的 10MB 上限（規格表格 #2）。
//
// ⭐ 這裡就是這個產品真正的差異化：
//
//	**只把實際用到的 glyph 子集嵌進去。**
//	一段終端錄影通常只用到幾十個 PUA 符號，子集化之後是幾 KB，不是幾 MB。
//	→ 同時解掉 ①（字型正確）與 ②（檔案要小）。
//	→ 現存四個專案沒有一個做這件事。
//
// ═══════════════════════════════════════════════════════════════════
// 體積策略：差分影格，不是完整影格
// ═══════════════════════════════════════════════════════════════════
//
//	svg-term-cli 的做法是每一格都完整輸出再用 CSS 切換顯示，
//	大小是 O(影格數 × 格子數)——所以才會 heap out of memory（規格表格 #6）。
//
//	我們只輸出**變動的部分**：每個 text run 有一段存活區間 [start, end)，
//	輸出一次，用 CSS 在那段時間顯示。終端錄影絕大多數格子是靜止的，
//	所以差分後的元素數量是「內容變化次數」而不是「影格 × 格子」。
//
//	再加上 run 合併：同樣式且不需斷開的連續格子合成一個 <text>（見 glyph.MustBreakRun）。
package render

import (
	"github.com/co2water/svgcast/internal/term"
)

// Options 是輸出的可調參數。對應 CLI 旗標。
type Options struct {
	// FontFamily 是 font-family 的 fallback 鏈。
	// 預設用等寬系統字型；使用者可用 --font 覆寫。
	FontFamily string

	// EmbedFont 指向一個字型檔。有給的話，只把用到的 glyph 子集
	// 以 base64 內嵌進 SVG（見上方說明）。這是預設關閉的重量級選項。
	EmbedFont string

	// CellWidth / LineHeight 是版面的基本單位（單位：px）。
	// ⭐ 每個 cell 的 x 座標一律是 col * CellWidth，明確指定，
	// 不依賴字型的 advance——這樣偏移就不可能累積（termtosvg #14）。
	CellWidth  float64
	LineHeight float64
	FontSize   float64

	// ⭐ Theme 決定配色策略（規格 v1.1 的 ④）。
	//
	//	"auto"（預設）—— 在 SVG 裡輸出 @media (prefers-color-scheme: dark)，
	//	                 **同一個檔案**跟著讀者的主題切換配色。
	//	"light" / "dark" —— 強制單一配色（給非 Web 的使用場景）。
	//
	// 這是唯一「GIF 結構上辦不到」的功能：點陣圖一個檔案只有一套顏色。
	// 而 GitHub 官方推薦的做法要兩個檔案（<picture> + <source media=...>），我們一個。
	//
	// ✅ spike 已通過（2026-09-08，Chromium）：<img src> / inline <svg> / <object>
	// 三種載入情境下 media query 都生效。素材在 docs/spike-theme/。
	// ⚠️ 它跟的是作業系統的 prefers-color-scheme，不是 GitHub 的主題設定——
	//    但 GitHub 官方的 <picture> 做法用同一個 query，限制一樣，我們不比它差。
	Theme string

	// Speed 是播放倍率，1.0 為原速。
	Speed float64
	// Loop 決定動畫是否循環（termtosvg #75 有人要求可關閉）。
	Loop bool

	// ⭐ EndHold 是結尾停留的秒數。
	//
	// 沒有它的話，最後一張影格才出現的內容起訖時間相同，只在動畫的最後一瞬間
	// 可見——終態畫面一閃就跳回開頭，讀者根本看不到指令的結果。
	// 留一段停留讓終態被看見，這也是 README 裡那張 demo 的可讀性關鍵。
	EndHold float64
	// From / To 只輸出這段區間（秒）。
	// ⚠️ svg-term-cli 用毫秒當單位被抱怨了 11 則——CLI 那層要收時間字串。
	From, To float64

	// IdleTimeLimit 把超過這個秒數的靜止壓縮掉。0 表示不壓縮。
	IdleTimeLimit float64

	// Padding 是四周留白（px）。
	Padding float64

	// Cursor 決定要不要畫游標方塊。
	Cursor bool
}

// DefaultOptions 是不給任何旗標時的行為。
func DefaultOptions() Options {
	return Options{
		// 刻意不含任何需要下載的字型（約束一）。
		FontFamily: `"Cascadia Code","JetBrains Mono","Fira Code",` +
			`"SF Mono",Menlo,Consolas,"DejaVu Sans Mono",monospace`,
		CellWidth:  8.4,
		LineHeight: 17,
		FontSize:   14,
		Theme:      "auto", // 預設就跟著讀者的主題走
		Speed:      1.0,
		Loop:       true,
		EndHold:    1.2,
		Padding:    12,
		Cursor:     true,
	}
}

// run 是一段連續、同樣式、可以合成一個 <text> 的格子。
type run struct {
	Row   int
	Col   int // 起始欄；x 座標 = Col * CellWidth
	Text  string
	Style term.Style

	// Start / End 是這段內容在畫面上存在的時間區間（秒）。
	Start float64
	End   float64
}

// 實作分工：
//
//	run.go    把影格切成一段段可以用單一 <text> 畫出來的連續文字
//	still.go  單張影格 → SVG（版面數學、逸出、CSS class）
//	anim.go   影格序列 → 存活區間 → keyframes（Animator）
//	color.go  調色盤與顏色轉換
//
// 字型嵌入：見 font.go——改用「抽出用到的字符輪廓轉成 <path>」，不做 TTF 子集化。

// ✅ W1 任務 #2 已完成（2026-09-02）：**GitHub 相容性 spike —— 通過**
//
// 這是整個專案唯一一個「錯了就全盤皆輸」的假設：如果 GitHub 把 README 裡
// SVG 的 CSS 動畫消毒掉，產品就不存在。結論是**沒有被消毒**。
//
// 證據鏈（用 termtosvg 自己 README 上那張 SVG 驗的）：
//
//  1. GitHub 原封不動地送出檔案。
//     GET raw.githubusercontent.com/.../awesome_window_frame_powershell.svg
//     → HTTP 200 · content-type: image/svg+xml · 50,250 bytes
//     內含 <style> / @keyframes / animation-name / animation-duration /
//     animation-timing-function / animation-iteration-count / animation-fill-mode
//     沒有 <script>（也不需要），沒有 SMIL <animate>——termtosvg 用的是純 CSS。
//
//  2. README 頁面確實以 <img> 載入它，naturalWidth=703 naturalHeight=404、
//     complete=true——GitHub 的 CSP 沒有擋。
//
//  3. <img> 情境下 CSS 動畫真的會播。把同一份 SVG 以 data: URL 放進本地頁面，
//     間隔截圖：先是 3 行，數秒後變成 5 行（多出 for 迴圈與 echo）。內容有推進。
//
// ⚠️ 量測上的坑，記下來免得以後有人重踩：
//
//	**不要用 canvas.drawImage() + getImageData 去偵測動畫。**
//	drawImage 對 SVG 只會光柵化靜態的初始幀，取樣 10 次會拿到 10 個一樣的雜湊，
//	看起來像「動畫被剝掉了」，其實是量錯了。要用真的截圖比對。
//
// 待驗：Safari 與 Firefox 尚未實測（termtosvg 在兩者都有 OPEN 的 bug）。
// 上面驗的是 Chromium。W5 找朋友裝的時候一併驗。
//
// 順帶量到的基準線：termtosvg 那張 703×404、約 20 秒的錄影是 **50 KB**。
// 這是我們要打敗的數字（規格第 2 段 ②）。

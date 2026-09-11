package render

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/co2water/svgcast/internal/glyph"
)

// ⭐ 護城河 ②：讓 Powerline / Nerd Font 的符號在沒裝那些字型的機器上也畫得出來。
//
// 問題：README 裡的 SVG 是用 <img> 載入的，那是沙箱模式——外部字型不會下載
// （spike #2 已驗證）。使用者機器上沒有 Nerd Font，那些私用區字符就是空白方框。
//
// 原本規劃的解法是「子集化 TTF 再 base64 嵌入」。改掉了，理由：
//
//	子集化要重寫 glyf/loca/cmap/hmtx/maxp 並重算 checksum，
//	是好幾百行高風險的二進位處理，而且嵌進去的還是一整個字型檔的骨架。
//
// 現在的做法：**只把實際用到的私用區字符，抽出輪廓轉成 SVG <path>**。
//
//	<defs><path id="g0" d="…"/></defs>   ← 每個符號定義一次
//	<use href="#g0" x="87.6" y="25.4"/>  ← 用到幾次就引用幾次
//
// 好處：
//   - 不碰字型二進位，用經過驗證的 x/image/font/sfnt 解析
//   - 只帶走真正用到的那幾個符號，是 KB 不是 MB
//   - **一般文字仍然是 <text>**，保持可選取、可被螢幕閱讀器讀出來
//     （這是相對於 GIF 的另一個賣點，不能為了嵌字型而放棄）

// glyphLib 是一份「私用區字符 → SVG 路徑」的對照表。
type glyphLib struct {
	// paths 依字元排序，輸出順序才會穩定（golden 測試需要）。
	order []rune
	paths map[rune]string
	ids   map[rune]string
}

// loadGlyphLib 從字型檔抽出指定字元的輪廓。
//
// 抽不到的字元會被跳過而不是報錯——使用者給的字型不一定涵蓋所有符號，
// 那種情況下退回原本的 <text>，畫出方框總比整個失敗好。
func loadGlyphLib(path string, runes map[rune]bool, fontSize float64) (*glyphLib, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("讀取字型 %s: %w", path, err)
	}
	f, err := parseFontOrCollection(data)
	if err != nil {
		return nil, fmt.Errorf("parsing font %s: %w (TrueType/OpenType .ttf/.otf/.ttc)", path, err)
	}

	lib := &glyphLib{
		paths: map[rune]string{},
		ids:   map[rune]string{},
	}
	var buf sfnt.Buffer
	ppem := fixed.Int26_6(fontSize * 64)

	var sorted []rune
	for r := range runes {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	for _, r := range sorted {
		idx, err := f.GlyphIndex(&buf, r)
		if err != nil || idx == 0 {
			continue // 這個字型沒有這個字符
		}
		segs, err := f.LoadGlyph(&buf, idx, ppem, nil)
		if err != nil {
			continue
		}
		d := segmentsToPath(segs)
		if d == "" {
			continue // 空字符（例如空白）
		}
		lib.order = append(lib.order, r)
		lib.paths[r] = d
		lib.ids[r] = fmt.Sprintf("g%d", len(lib.order)-1)
	}
	return lib, nil
}

// parseFontOrCollection 同時接受單一字型與字型集合（.ttc）。
//
// macOS 上很多字型是 .ttc（Menlo、Nerd Font 的某些打包），sfnt.Parse 會回
// 「invalid single font (data is a font collection)」——CI 的 macOS runner 第一次就撞到。
// 集合取第一個字型；要選其他索引是 v2 的事。
func parseFontOrCollection(data []byte) (*sfnt.Font, error) {
	if f, err := sfnt.Parse(data); err == nil {
		return f, nil
	}
	c, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	if c.NumFonts() == 0 {
		return nil, fmt.Errorf("font collection is empty")
	}
	return c.Font(0)
}

func (l *glyphLib) has(r rune) bool {
	if l == nil {
		return false
	}
	_, ok := l.paths[r]
	return ok
}

func (l *glyphLib) empty() bool { return l == nil || len(l.order) == 0 }

// writeDefs 輸出 <defs> 區塊。
func (l *glyphLib) writeDefs(b *strings.Builder) {
	if l.empty() {
		return
	}
	b.WriteString("<defs>")
	for _, r := range l.order {
		b.WriteString(`<path id="` + l.ids[r] + `" d="` + l.paths[r] + `"/>`)
	}
	b.WriteString("</defs>")
}

// f26 把 26.6 定點數轉成最短的十進位字串。
func f26(v fixed.Int26_6) string {
	return fnum(float64(v) / 64)
}

// segmentsToPath 把字型輪廓轉成 SVG 的 path 資料。
//
// sfnt 給的座標是 y 軸向下（螢幕座標系），原點在字符的原點
// （基線、左緣），跟 SVG 的慣例一致，所以不需要翻轉。
func segmentsToPath(segs []sfnt.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			b.WriteString("M" + f26(s.Args[0].X) + " " + f26(s.Args[0].Y))
		case sfnt.SegmentOpLineTo:
			b.WriteString("L" + f26(s.Args[0].X) + " " + f26(s.Args[0].Y))
		case sfnt.SegmentOpQuadTo:
			b.WriteString("Q" + f26(s.Args[0].X) + " " + f26(s.Args[0].Y) +
				" " + f26(s.Args[1].X) + " " + f26(s.Args[1].Y))
		case sfnt.SegmentOpCubeTo:
			b.WriteString("C" + f26(s.Args[0].X) + " " + f26(s.Args[0].Y) +
				" " + f26(s.Args[1].X) + " " + f26(s.Args[1].Y) +
				" " + f26(s.Args[2].X) + " " + f26(s.Args[2].Y))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("Z")
	return b.String()
}

// collectEmbedRunes 掃出所有需要嵌入輪廓的字元。
//
// 只收私用區——一般文字留在 <text> 裡，保持可選取。
func collectEmbedRunes(runs []run) map[rune]bool {
	out := map[rune]bool{}
	for _, r := range runs {
		for _, ch := range r.Text {
			if glyph.IsPUA(ch) {
				out[ch] = true
			}
		}
	}
	return out
}

// buildGlyphLib 是給渲染路徑用的入口：沒指定字型就回傳 nil。
func buildGlyphLib(opts Options, runs []run) (*glyphLib, error) {
	if opts.EmbedFont == "" {
		return nil, nil
	}
	need := collectEmbedRunes(runs)
	if len(need) == 0 {
		return nil, nil
	}
	fontSize := opts.FontSize
	if fontSize <= 0 {
		fontSize = 14
	}
	return loadGlyphLib(opts.EmbedFont, need, fontSize)
}

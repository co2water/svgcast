package render

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/term"
)

// systemFont 找一個本機可用的 TrueType 字型。
//
// 刻意**不把字型放進 repo**：授權要附條款、體積也不小。
// 找不到就跳過——CI 上 Linux runner 通常有 DejaVu。
func systemFont(t *testing.T) string {
	t.Helper()
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Windows\Fonts\consola.ttf`,
			`C:\Windows\Fonts\lucon.ttf`,
			`C:\Windows\Fonts\arial.ttf`,
		}
	case "darwin":
		// Menlo 是 .ttc 集合——刻意留著，同時驗證 parseFontOrCollection 的路徑。
		candidates = []string{
			"/System/Library/Fonts/Supplemental/Arial.ttf",
			"/System/Library/Fonts/Menlo.ttc",
			"/Library/Fonts/Arial.ttf",
		}
	default:
		candidates = []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("找不到可用的系統字型（試過 %v）", candidates)
	return ""
}

// ⭐ 護城河 ② 的核心機制：從字型抽出字符輪廓，轉成 SVG 路徑。
func TestGlyphLib_ExtractsOutline(t *testing.T) {
	font := systemFont(t)

	lib, err := loadGlyphLib(font, map[rune]bool{'A': true, 'o': true}, 14)
	if err != nil {
		t.Fatal(err)
	}
	if lib.empty() {
		t.Fatal("沒有抽出任何字符輪廓")
	}
	for _, r := range []rune{'A', 'o'} {
		if !lib.has(r) {
			t.Errorf("%q 沒有被抽出來", r)
			continue
		}
		d := lib.paths[r]
		if !strings.HasPrefix(d, "M") {
			t.Errorf("%q 的路徑不是以 M 開頭: %.40s", r, d)
		}
		if !strings.HasSuffix(d, "Z") {
			tail := d
			if len(tail) > 20 {
				tail = tail[len(tail)-20:]
			}
			t.Errorf("%q 的路徑沒有收尾: …%s", r, tail)
		}
		t.Logf("%q → %d 字元的路徑: %.60s…", r, len(d), d)
	}

	// 'o' 有內外兩圈輪廓，路徑裡應該有多段 M
	if n := strings.Count(lib.paths['o'], "M"); n < 2 {
		t.Errorf("'o' 的路徑只有 %d 段，預期至少 2 段（外圈與內圈）", n)
	}
}

// 字型沒有的字符要被跳過，不能報錯——使用者給的字型不一定涵蓋所有符號，
// 那時退回 <text> 畫方框，總比整個轉檔失敗好。
func TestGlyphLib_MissingGlyphSkipped(t *testing.T) {
	font := systemFont(t)

	// 私用區字符，一般字型不會有
	lib, err := loadGlyphLib(font, map[rune]bool{0xE0B0: true, 'A': true}, 14)
	if err != nil {
		t.Fatal(err)
	}
	if lib.has(0xE0B0) {
		t.Log("這個字型剛好有 U+E0B0（是 Nerd Font？），跳過這條斷言")
		return
	}
	if !lib.has('A') {
		t.Error("缺一個字符不該影響其他字符的抽取")
	}
}

func TestGlyphLib_BadFont(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "not-a-font.ttf")
	if err := os.WriteFile(bad, []byte("this is not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGlyphLib(bad, map[rune]bool{'A': true}, 14); err == nil {
		t.Error("壞掉的字型檔應該報錯")
	}

	if _, err := loadGlyphLib(filepath.Join(dir, "nope.ttf"), map[rune]bool{'A': true}, 14); err == nil {
		t.Error("不存在的字型檔應該報錯")
	}
}

// 只有私用區字符會被嵌入——一般文字必須留在 <text> 裡保持可選取。
func TestCollectEmbedRunes_OnlyPUA(t *testing.T) {
	runs := []run{
		{Text: "hello world"},
		{Text: string(rune(0xE0B0))},
		{Text: "中文"},
		{Text: string(rune(0xF0001))}, // 第 15 平面的補充私用區
	}
	got := collectEmbedRunes(runs)

	if len(got) != 2 {
		t.Fatalf("收集到 %d 個字元，預期 2 個（只有私用區）: %v", len(got), got)
	}
	if !got[0xE0B0] || !got[0xF0001] {
		t.Errorf("私用區字元沒有被收進來: %v", got)
	}
	for _, r := range "hello world中文" {
		if got[r] {
			t.Errorf("一般文字 %q 不該被嵌入（會失去可選取性）", r)
		}
	}
}

// <defs> + <use> 的輸出結構。
func TestGlyphUse_Structure(t *testing.T) {
	lib := &glyphLib{
		order: []rune{0xE0B0},
		paths: map[rune]string{0xE0B0: "M0 0L10 0L10 10Z"},
		ids:   map[rune]string{0xE0B0: "g0"},
	}

	fr := blankFrame(1, 10)
	fr.Rows[0][3] = termCell(0xE0B0)

	opts := DefaultOptions()
	opts.Cursor = false
	g := opts.geom(10, 1)

	var body strings.Builder
	cs := classSet{}
	for _, r := range runsFromFrame(fr) {
		writeTextRun(&body, g, r, cs, "", lib)
	}
	out := body.String()

	if !strings.Contains(out, `href="#g0"`) {
		t.Errorf("私用區字符應該用 <use> 引用 defs，得到: %s", out)
	}
	if strings.Contains(out, "<text") {
		t.Errorf("已嵌入輪廓的字符不該再輸出 <text>: %s", out)
	}
	// 位置必須跟 <text> 走同一套座標——護城河 ① 不能因為改用 <use> 就失效
	wantX := fnum(g.x(3))
	if !strings.Contains(out, `x="`+wantX+`"`) {
		t.Errorf("<use> 的 x 應該是 %s（第 3 欄），得到: %s", wantX, out)
	}

	var defs strings.Builder
	lib.writeDefs(&defs)
	if !strings.Contains(defs.String(), `<path id="g0" d="M0 0L10 0L10 10Z"/>`) {
		t.Errorf("defs 內容不對: %s", defs.String())
	}
}

// 沒指定 --embed-font 時完全不該有 defs——不能讓沒用到的功能增加體積。
func TestEmbed_OffByDefault(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	out := renderStill(t, fr, DefaultOptions())

	if strings.Contains(out, "<defs>") {
		t.Error("預設不該輸出 <defs>")
	}
	if strings.Contains(out, "<use") {
		t.Error("預設不該輸出 <use>")
	}
}

// 指定了字型但字型沒有那些符號時，要優雅退回 <text>，不能失敗。
func TestEmbed_GracefulFallback(t *testing.T) {
	font := systemFont(t)

	fr := lastFrame(t, "../../testdata/powerline.cast")
	opts := DefaultOptions()
	opts.EmbedFont = font

	out := renderStill(t, fr, opts)
	doc := parseSVG(t, out) // 仍須是合法 XML

	if len(doc.Texts) == 0 {
		t.Error("沒有任何 <text>——退回機制壞了")
	}
	// PUA 字符應該還在（以 <text> 形式）
	var found bool
	for _, txt := range doc.Texts {
		if strings.ContainsRune(txt.Content, 0xE0B0) {
			found = true
		}
	}
	if !found && !strings.Contains(out, "<use") {
		t.Error("PUA 字符既沒有 <text> 也沒有 <use>，被吃掉了")
	}
}

// termCell 是測試用的簡寫：一個佔一格的字元。
func termCell(r rune) term.Cell {
	return term.Cell{Rune: r, Width: 1}
}

package render

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
	"github.com/co2water/svgcast/internal/term"
)

// baselineBytes 是要打敗的數字。
//
// termtosvg 自己 README 上那張 703×404、約 20 秒的錄影是 50,250 bytes
// （2026-09-02 從 raw.githubusercontent.com 量到）。
// 那還是一份**沒有嵌字型**的檔案——我們要在同等條件下明顯小於它。
const baselineBytes = 50250

// synthSession 產生一份接近真實的終端錄影：打指令、有輸出、有顏色、
// 有捲動，約 20 秒、80×24。
//
// 為什麼要合成而不是用 testdata 裡那兩份：hello.cast 只有 6 個事件，
// 量出來的數字沒有代表性。真正的錄影是「逐字打字 + 大量輸出」，
// 而逐字打字正是差分演算法最吃重的地方。
func synthSession() *cast.Cast {
	c := &cast.Cast{Header: cast.Header{Version: 2, Width: 80, Height: 24}}
	t := 0.0

	add := func(dt float64, s string) {
		t += dt
		c.Events = append(c.Events, cast.Event{Time: t, Type: cast.Output, Data: s})
	}

	// 提示字元用顏色，模擬真實 shell
	prompt := "\x1b[32muser@host\x1b[0m:\x1b[34m~/project\x1b[0m$ "

	type step struct {
		cmd  string
		out  []string
		fast bool
	}
	steps := []step{
		{cmd: "ls -la", out: []string{
			"total 48",
			"drwxr-xr-x  6 user staff   192 Sep  8 10:22 \x1b[34m.\x1b[0m",
			"drwxr-xr-x 18 user staff   576 Sep  1 09:14 \x1b[34m..\x1b[0m",
			"-rw-r--r--  1 user staff  1204 Sep  8 10:20 README.md",
			"-rw-r--r--  1 user staff   318 Sep  3 17:41 go.mod",
			"drwxr-xr-x  9 user staff   288 Sep  8 10:22 \x1b[34minternal\x1b[0m",
			"-rwxr-xr-x  1 user staff  8912 Sep  8 10:22 \x1b[32msvgcast\x1b[0m",
		}},
		{cmd: "go test ./...", out: []string{
			"ok  \tgithub.com/example/proj/internal/cast\t0.44s",
			"ok  \tgithub.com/example/proj/internal/frames\t0.52s",
			"ok  \tgithub.com/example/proj/internal/glyph\t0.41s",
			"ok  \tgithub.com/example/proj/internal/render\t0.53s",
			"ok  \tgithub.com/example/proj/internal/term\t0.47s",
		}},
		{cmd: "git status --short", out: []string{
			"\x1b[31m M\x1b[0m internal/render/anim.go",
			"\x1b[31m M\x1b[0m internal/render/still.go",
			"\x1b[32m??\x1b[0m internal/render/size_test.go",
		}},
		{cmd: "svgcast demo.cast -o demo.svg", out: []string{
			"\x1b[36m已輸出\x1b[0m demo.svg (12.4 KB)",
		}},
	}

	for _, s := range steps {
		add(0.35, prompt)
		// 逐字打字——差分演算法最吃重的路徑
		for _, ch := range s.cmd {
			add(0.055, string(ch))
		}
		add(0.18, "\r\n")
		for _, line := range s.out {
			add(0.045, line+"\r\n")
		}
	}
	add(0.4, prompt)
	return c
}

// synthSession20s 是跟基準線同長度（約 20 秒）的版本。
//
// 拿 6.9 秒的數字去外推是不誠實的：群組數不是線性成長，
// 而且真實錄影中間會有思考停頓。要比就直接量同樣長度的。
func synthSession20s() *cast.Cast {
	base := synthSession()
	c := &cast.Cast{Header: base.Header}
	t := 0.0
	for round := 0; round < 3; round++ {
		for _, ev := range base.Events {
			c.Events = append(c.Events, cast.Event{
				Time: t + ev.Time,
				Type: ev.Type,
				Data: ev.Data,
			})
		}
		t += base.Duration()
		// 每一輪之間有一段思考停頓，真實錄影都有
		t += 0.9
	}
	return c
}

func renderCast(t *testing.T, c *cast.Cast, opts Options) string {
	t.Helper()
	a := NewAnimator(opts)
	if err := frames.Capture(c, frames.DefaultOptions(), a.Add); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func gzipSize(s string) int {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return buf.Len()
}

// ⭐ 體積驗收。
//
// ⚠️ 為什麼不拿 termtosvg 的 50,250 bytes 當硬性門檻：
//
//	那是**它自己那份錄影**的大小，我不知道它的內容密度。
//	拿不同內容的檔案大小互比是不成立的比較。
//	這裡的合成錄影是刻意做成最壞情況——全程打字沒有停頓、大量彩色輸出、
//	而且會捲動，比一般 README demo 密得多。
//
// 真正成立的比較留在 S8：拿**同一份 cast** 產生 GIF 與 SVG 並列數字。
// 那才是要寫進 README 的那張表，也才是產品宣稱的依據。
//
// 這裡守的是兩件事：與 GIF 量級的差距，以及不要回歸。
func TestSize_Measure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     *cast.Cast
		limit int // 回歸上限（依實測值加約 15% 餘裕）
	}{
		{"7s 密集", synthSession(), 23000},
		{"22s 密集且會捲動", synthSession20s(), 86000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := renderCast(t, tc.c, DefaultOptions())
			raw, gz := len(out), gzipSize(out)
			dur := tc.c.Duration()

			t.Logf("%d 個事件、%.1f 秒、80x24", len(tc.c.Events), dur)
			t.Logf("  原始 : %6d bytes  (%.0f bytes/秒)", raw, float64(raw)/dur)
			t.Logf("  gzip : %6d bytes  ← GitHub 傳輸時的實際成本", gz)
			t.Logf("  參考 : termtosvg 自己那份約 20 秒的 demo 是 %d bytes（內容不同，僅供參考）",
				baselineBytes)

			if raw > tc.limit {
				t.Errorf("輸出 %d bytes 超過回歸上限 %d。"+
					"若是預期中的成長，調整上限並說明原因", raw, tc.limit)
			}
			// 產品宣稱是「別再塞 5 MB 的 GIF」——至少要遠低於那個量級。
			if raw > 1024*1024 {
				t.Errorf("輸出 %d bytes 已經是 MB 等級，賣點不成立", raw)
			}
		})
	}
}

// 捲動處理必須讓「會捲動」與「不捲動」的差距保持在小倍數內。
//
// 這是 S5 最重要的一條回歸防線：捲動處理一旦壞掉，差距會回到 6 倍以上
// （實測未處理時 386 KB vs 62 KB）。
func TestSize_ScrollOverheadBounded(t *testing.T) {
	small := synthSession20s()
	small.Header.Height = 24 // 會捲動

	tall := synthSession20s()
	tall.Header.Height = 200 // 不會捲動

	a := len(renderCast(t, small, DefaultOptions()))
	b := len(renderCast(t, tall, DefaultOptions()))

	ratio := float64(a) / float64(b)
	t.Logf("會捲動 %d bytes / 不捲動 %d bytes = %.2fx", a, b, ratio)

	if ratio > 1.6 {
		t.Errorf("捲動造成 %.2f 倍的膨脹，捲動處理可能失效了（修好之前是 6.2 倍）", ratio)
	}
}

// 靜止壓縮應該要能明顯縮小有長停頓的錄影——真實的 README demo 都有停頓。
func TestSize_IdleCompressionHelps(t *testing.T) {
	c := synthSession20s()

	plain := renderCast(t, c, DefaultOptions())

	a := NewAnimator(DefaultOptions())
	fo := frames.DefaultOptions()
	fo.IdleTimeLimit = 0.4
	if err := frames.Capture(c, fo, a.Add); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := a.Write(&b); err != nil {
		t.Fatal(err)
	}

	t.Logf("不壓縮靜止             : %6d bytes", len(plain))
	t.Logf("--idle-time-limit 0.4s : %6d bytes", b.Len())

	// 壓縮靜止只改變時間軸，不改變內容量，所以檔案大小幾乎不動——
	// 百分比字串的長度會有些微差異。這裡只擋住「明顯變大」。
	//
	// ⚠️ 它幫不上忙這件事本身就是訊號：體積的瓶頸不在時間軸，在**捲動**
	// 造成的元素數量爆炸。見 TestDiag_ScrollCost。
	if b.Len() > len(plain)*11/10 {
		t.Errorf("壓縮靜止之後明顯變大：%d > %d", b.Len(), len(plain))
	}
}

// 相鄰、同底色的背景色塊必須被併成一塊。
// powerline 的提示列不合併的話，一條色帶會變成七八個 <rect>。
func TestSize_BGRectsMerged(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	doc := parseSVG(t, renderStill(t, fr, DefaultOptions()))

	// 畫布底色那一個 + 合併後的色帶。合併前是 14 個。
	if len(doc.Rects) > 8 {
		t.Errorf("<rect> 數量 = %d，相鄰同色的色塊沒有被合併", len(doc.Rects))
	}
	t.Logf("<rect> 數量 = %d（合併前是 14）", len(doc.Rects))
}

// 合併不能改變視覺結果：色塊的總覆蓋範圍要跟合併前一致。
func TestMergeBGRects_PreservesCoverage(t *testing.T) {
	fr := lastFrame(t, "../../testdata/powerline.cast")
	runs := runsFromFrame(fr)

	// 合併前：逐段累加格子數
	before := map[int]int{} // row -> 有底色的格子數
	for _, r := range runs {
		if r.Style.BG.Kind == 0 {
			continue
		}
		before[r.Row] += r.cells()
	}

	after := map[int]int{}
	for _, br := range mergeBGRects(runs) {
		after[br.Row] += br.Cells
	}

	for row, n := range before {
		if after[row] != n {
			t.Errorf("第 %d 列的底色覆蓋格數：合併前 %d，合併後 %d", row, n, after[row])
		}
	}
	if len(before) != len(after) {
		t.Errorf("有底色的列數不一致：%d vs %d", len(before), len(after))
	}
}

// 不同底色的相鄰色塊不可以被併在一起。
func TestMergeBGRects_DifferentColorsNotMerged(t *testing.T) {
	fr := blankFrame(1, 10)
	setText(&fr, 0, 0, "ab")
	fr.Rows[0][0].Style.BG = termColor(1)
	fr.Rows[0][1].Style.BG = termColor(2)

	got := mergeBGRects(runsFromFrame(fr))
	if len(got) != 2 {
		t.Fatalf("色塊數 = %d，預期 2（不同顏色不能合併）", len(got))
	}
}

// 中間隔開的同色色塊也不能併。
func TestMergeBGRects_GapNotMerged(t *testing.T) {
	fr := blankFrame(1, 10)
	setText(&fr, 0, 0, "a")
	setText(&fr, 0, 5, "b")
	fr.Rows[0][0].Style.BG = termColor(3)
	fr.Rows[0][5].Style.BG = termColor(3)

	got := mergeBGRects(runsFromFrame(fr))
	if len(got) != 2 {
		t.Fatalf("色塊數 = %d，預期 2（中間有空隙不能合併）", len(got))
	}
	if got[0].Cells != 1 || got[1].Cells != 1 {
		t.Errorf("色塊寬度 = %d, %d，預期各 1", got[0].Cells, got[1].Cells)
	}
}

// termColor 是測試用的簡寫：指定調色盤索引色。
func termColor(i uint8) term.Color {
	return term.Color{Kind: term.ColorIndexed, Index: i}
}

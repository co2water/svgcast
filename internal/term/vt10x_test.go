package term

import (
	"os"
	"testing"

	"github.com/hinshun/vt10x"

	"github.com/co2water/svgcast/internal/cast"
)

// feed 把一份 asciicast 的輸出事件全部餵進模擬器，回傳最後的畫面。
func feed(t *testing.T, path string) Frame {
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

	em := NewEmulator(c.Header.Width, c.Header.Height)
	for _, ev := range c.Events {
		if ev.Type != cast.Output {
			continue
		}
		if _, err := em.Write([]byte(ev.Data)); err != nil {
			t.Fatalf("寫入模擬器: %v", err)
		}
	}
	return em.Snapshot()
}

// rowRunes 把某一列還原成字串，去掉尾端空白。
func rowRunes(f Frame, y int) []rune {
	var out []rune
	for _, c := range f.Rows[y] {
		if c.Rune == 0 {
			out = append(out, ' ')
			continue
		}
		out = append(out, c.Rune)
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}

// ⭐ W1 spike #1 的主要驗收：vt10x 怎麼處理 PUA（Powerline／Nerd Font）字元。
//
// 這是整個專案最關鍵的一個問題——termtosvg #14 的 bug 到底出在模擬器還是渲染層？
func TestVT10x_PowerlinePUA(t *testing.T) {
	f := feed(t, "../../testdata/powerline.cast")

	const (
		branch = 0xE0A0
		sepR   = 0xE0B0
	)

	// 掃全畫面，記下每個 PUA 字元出現在哪一格
	type pos struct{ row, col int }
	var found []pos
	for y := range f.Rows {
		for x, c := range f.Rows[y] {
			if c.Rune == branch || c.Rune == sepR {
				found = append(found, pos{y, x})
				if c.Width != 1 {
					t.Errorf("U+%04X 在 (%d,%d) 的 Width=%d，必須是 1", c.Rune, y, x, c.Width)
				}
			}
		}
	}

	if len(found) == 0 {
		t.Fatal("畫面上找不到任何 PUA 字元——vt10x 把它們吃掉了，這會直接否決這個函式庫")
	}
	t.Logf("PUA 字元存活 %d 個，位置：%v", len(found), found)

	// 第一列應該是 prompt。印出來人工核對。
	t.Logf("第 0 列還原：%q", string(rowRunes(f, 0)))
}

// vt10x 完全不處理字元寬度：一個 rune 一格，永遠前進 1。
// 對 PUA 來說這**剛好正確**，所以護城河確實在渲染層而不是模擬器。
// 這個測試把這個前提釘住。
func TestVT10x_IgnoresWidth(t *testing.T) {
	em := NewEmulator(20, 2)
	// 兩個雙寬 CJK 字元 + 一個 ASCII
	if _, err := em.Write([]byte("中文x")); err != nil {
		t.Fatal(err)
	}
	f := em.Snapshot()

	got := string(rowRunes(f, 0))
	t.Logf("寫入 %q，畫面第 0 列是 %q", "中文x", got)

	// 如果 vt10x 有處理雙寬，'x' 會落在第 4 欄；它不處理，所以落在第 2 欄。
	if f.Rows[0][2].Rune != 'x' {
		t.Errorf("預期 'x' 落在第 2 欄（vt10x 不算寬度），實際第 2 欄是 %q；"+
			"若上游改成會算寬度了，term.go 的說明要一起改", f.Rows[0][2].Rune)
	}
	// 但我們自己的寬度規則仍然把 CJK 標成 2
	if f.Rows[0][0].Width != 2 {
		t.Errorf("'中' 的 Width=%d，glyph 規則應該給 2", f.Rows[0][0].Width)
	}
}

// ⚠️ vt10x 的屬性位元是未匯出的，我們照抄了它的 1<<iota 順序。
// 這個測試用真的 SGR 逸出序列反推，確認我們抄對了——
// 上游若調換順序，這裡會炸，而不是安靜地讀錯樣式。
func TestAttrBitsStillMatch(t *testing.T) {
	cases := []struct {
		name string
		sgr  string
		want Attr
	}{
		{"bold", "\x1b[1m", AttrBold},
		{"italic", "\x1b[3m", AttrItalic},
		{"underline", "\x1b[4m", AttrUnderline},
		{"blink", "\x1b[5m", AttrBlink},
		{"reverse", "\x1b[7m", AttrReverse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			em := NewEmulator(10, 1)
			if _, err := em.Write([]byte(c.sgr + "A")); err != nil {
				t.Fatal(err)
			}
			f := em.Snapshot()
			got := f.Rows[0][0].Style.Attr
			if got&c.want == 0 {
				t.Errorf("送出 %q 之後，屬性是 %08b，預期含有 %08b。"+
					"vt10x 的 attr 位元順序可能變了——去看 state.go 的 const 區塊", c.sgr, got, c.want)
			}
		})
	}
}

// 確認 Default 顏色有被轉成 Index=-1，讓渲染層能用主題色填（深色模式要靠這個）。
func TestDefaultColorsMapToThemeSlot(t *testing.T) {
	em := NewEmulator(10, 1)
	if _, err := em.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	f := em.Snapshot()
	st := f.Rows[0][0].Style
	if st.FG.Kind != ColorDefault || st.BG.Kind != ColorDefault {
		t.Errorf("未指定顏色時 FG=%+v BG=%+v，預期都是 ColorDefault（交給主題填）", st.FG, st.BG)
	}
	_ = vt10x.DefaultFG // 確保常數名稱沒變
}

// 明確指定的顏色不能被誤判成「預設色」——這是改用 Kind 之前的真 bug：
// 當時預設色與 RGB 都用 Index==-1 表示，純黑會被當成「沒指定顏色」。
func TestExplicitColorIsNotDefault(t *testing.T) {
	cases := []struct {
		name string
		sgr  string
	}{
		{"真彩色純黑", "\x1b[38;2;0;0;0m"},
		{"真彩色紅", "\x1b[38;2;255;0;0m"},
		{"索引色 0", "\x1b[38;5;0m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			em := NewEmulator(10, 1)
			if _, err := em.Write([]byte(c.sgr + "A")); err != nil {
				t.Fatal(err)
			}
			fg := em.Snapshot().Rows[0][0].Style.FG
			if fg.Kind == ColorDefault {
				t.Errorf("%s 被判成 ColorDefault，會拿到主題色而不是指定的顏色", c.name)
			}
		})
	}
}

// 真彩色只要 r 或 g 不為零就能被正確辨識。
func TestTruecolorDetected(t *testing.T) {
	em := NewEmulator(10, 1)
	if _, err := em.Write([]byte("\x1b[38;2;255;128;64mA")); err != nil {
		t.Fatal(err)
	}
	fg := em.Snapshot().Rows[0][0].Style.FG
	if fg.Kind != ColorRGB {
		t.Fatalf("Kind=%v，預期 ColorRGB", fg.Kind)
	}
	if fg.R != 255 || fg.G != 128 || fg.B != 64 {
		t.Errorf("顏色 = (%d,%d,%d)，預期 (255,128,64)", fg.R, fg.G, fg.B)
	}
}

// ⚠️ 把 vt10x 的顏色編碼歧義釘成一個「已知行為」的測試。
//
// 上游把真彩色存成 r<<16|g<<8|b，跟 0–255 的調色盤索引重疊。
// 當 r==0 且 g==0 時無法區分——資訊在 vt10x 內部就丟了。
// 這個測試不是在慶祝這個行為，是確保它是**已知且穩定**的，
// 哪天換掉 VT 層或 fork 上游時，這裡會提醒我們這個限制沒了。
func TestKnownLimitation_TruecolorIndexCollision(t *testing.T) {
	em := NewEmulator(10, 1)
	if _, err := em.Write([]byte("\x1b[38;2;0;0;9mA")); err != nil { // RGB(0,0,9)
		t.Fatal(err)
	}
	fg := em.Snapshot().Rows[0][0].Style.FG
	if fg.Kind != ColorIndexed || fg.Index != 9 {
		t.Logf("行為改變了：RGB(0,0,9) 現在是 %+v。"+
			"若已改用能區分兩者的 VT 層，可以移除這個測試與 convertColor 的相關說明", fg)
	}
}

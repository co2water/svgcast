package cast

import (
	"os"
	"strings"
	"testing"
)

func TestParse_Hello(t *testing.T) {
	f, err := os.Open("../../testdata/hello.cast")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	c, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Header.Version != 2 {
		t.Errorf("version = %d，預期 2", c.Header.Version)
	}
	if c.Header.Width != 80 || c.Header.Height != 24 {
		t.Errorf("尺寸 = %dx%d，預期 80x24", c.Header.Width, c.Header.Height)
	}
	if len(c.Events) != 6 {
		t.Fatalf("事件數 = %d，預期 6", len(c.Events))
	}
	if c.Events[0].Type != Output || c.Events[0].Data != "$ " {
		t.Errorf("第一個事件 = %+v", c.Events[0])
	}
	if got := c.Duration(); got != 2.4 {
		t.Errorf("Duration = %v，預期 2.4", got)
	}
}

// ⭐ 這份 testdata 是 termtosvg #14 的回歸測試素材。
// 它刻意用 JSON 的 \u 轉義寫 ESC 與 PUA 字元——不放裸位元組，
// 因為裸的控制字元不是合法 JSON，而裸的 PUA 字元經過工具鏈容易壞掉。
func TestParse_Powerline(t *testing.T) {
	f, err := os.Open("../../testdata/powerline.cast")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	c, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Events) == 0 {
		t.Fatal("沒有解析到任何事件")
	}
	// 轉義應該已經被還原成真正的字元
	first := c.Events[0].Data
	if !strings.ContainsRune(first, 0x1b) {
		t.Error("預期含有 ESC(0x1b)，\\u001b 沒有被還原")
	}
	if !strings.ContainsRune(first, 0xE0B0) {
		t.Error("預期含有 powerline 分隔符 U+E0B0，\\ue0b0 沒有被還原")
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空檔案", "", "empty file"},
		{"版本不對", `{"version":1,"width":80,"height":24}`, "only asciicast v2"},
		{"尺寸無效", `{"version":2,"width":0,"height":24}`, "invalid terminal size"},
		{
			// termtosvg 的「stdin 關閉時產生空 SVG」——我們選擇報錯而不是默默輸出空檔
			"只有 header 沒有事件",
			`{"version":2,"width":80,"height":24}`,
			"no events",
		},
		{
			"事件元素數不對",
			"{\"version\":2,\"width\":80,\"height\":24}\n[0.1,\"o\"]",
			"needs 3 elements",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(c.in))
			if err == nil {
				t.Fatalf("預期出錯，卻成功了")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("錯誤訊息 = %q，預期含有 %q", err.Error(), c.want)
			}
		})
	}
}

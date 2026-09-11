package cast

import (
	"strings"
	"testing"
)

// FuzzParse 確保任何輸入都不會 panic——只能回傳錯誤。
//
// 使用者會餵進截斷的檔案、別的工具產生的 JSON、甚至隨手指到的二進位檔。
// 那些情況都該是清楚的錯誤訊息，不是 stack trace。
//
//	go test ./internal/cast -fuzz FuzzParse -fuzztime 30s
func FuzzParse(f *testing.F) {
	f.Add(`{"version":2,"width":80,"height":24}` + "\n" + `[0.1,"o","hi"]`)
	f.Add(`{"version":2,"width":80,"height":24}`)
	f.Add(`{"version":2,"width":80,"height":24}` + "\n" + `[0.1,"r","80x24"]`)
	f.Add(`{"version":2,"width":80,"height":24}` + "\n" + `[1e308,"o","x"]`)
	f.Add(`{"version":2,"width":0,"height":0}`)
	f.Add("")
	f.Add("not json at all")
	f.Add("\x00\x01\x02")

	f.Fuzz(func(t *testing.T, in string) {
		// 只要不 panic 就算通過；回傳 error 是正常結果。
		c, err := Parse(strings.NewReader(in))
		if err != nil {
			return
		}
		// 解析成功的話，回傳值必須是自洽的——後面的管線會信任這些不變式。
		if c.Header.Width <= 0 || c.Header.Height <= 0 {
			t.Fatalf("解析成功卻有無效尺寸 %dx%d", c.Header.Width, c.Header.Height)
		}
		if c.Header.Width > maxCols || c.Header.Height > maxRows {
			t.Fatalf("解析成功卻超過尺寸上限 %dx%d", c.Header.Width, c.Header.Height)
		}
		if len(c.Events) == 0 {
			t.Fatal("解析成功卻沒有任何事件")
		}
		_ = c.Duration()
	})
}

// 各種壞輸入都要給出清楚的錯誤，而不是 panic 或默默通過。
func TestParse_HostileInputs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"尺寸大到會吃光記憶體",
			`{"version":2,"width":999999,"height":999999}` + "\n" + `[0,"o","x"]`,
			"exceeds the limit",
		},
		{
			"負的尺寸",
			`{"version":2,"width":-5,"height":24}` + "\n" + `[0,"o","x"]`,
			"invalid terminal size",
		},
		{
			"header 不是 JSON",
			"garbage\n[0,\"o\",\"x\"]",
			"parsing header",
		},
		{
			"事件不是陣列",
			`{"version":2,"width":80,"height":24}` + "\n" + `{"not":"array"}`,
			"not a valid event array",
		},
		{
			"時間欄位是字串",
			`{"version":2,"width":80,"height":24}` + "\n" + `["zero","o","x"]`,
			"time field",
		},
		{
			"資料欄位是數字",
			`{"version":2,"width":80,"height":24}` + "\n" + `[0,"o",123]`,
			"data field",
		},
		{
			"截斷的檔案",
			`{"version":2,"width":80,"height":24}` + "\n" + `[0.1,"o","unterm`,
			"not a valid event array",
		},
		{
			"二進位垃圾",
			"\x00\x01\x02\x03",
			"parsing header",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(c.in))
			if err == nil {
				t.Fatal("預期出錯，卻成功了")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("錯誤訊息 = %q，預期含有 %q", err, c.want)
			}
		})
	}
}

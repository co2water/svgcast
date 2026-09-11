package main

import (
	"flag"
	"reflect"
	"testing"
)

func newFS() *flag.FlagSet {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("o", "", "")
	fs.String("from", "", "")
	fs.Float64("speed", 1, "")
	fs.Bool("no-loop", false, "")
	fs.Bool("version", false, "")
	return fs
}

// ⭐ 使用者一定會打 `svgcast demo.cast -o out.svg`。
// Go 的 flag 套件預設會在第一個位置參數就停止解析，那樣旗標會被當成檔名。
// 這是「30 秒能跑起來」的第一道關卡，不能壞。
func TestReorderArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			"旗標在後（使用者最常打的形式）",
			[]string{"demo.cast", "-o", "out.svg"},
			[]string{"-o", "out.svg", "demo.cast"},
		},
		{
			"旗標在前",
			[]string{"-o", "out.svg", "demo.cast"},
			[]string{"-o", "out.svg", "demo.cast"},
		},
		{
			"混合",
			[]string{"-speed", "2", "demo.cast", "--from", "1s"},
			[]string{"-speed", "2", "--from", "1s", "demo.cast"},
		},
		{
			"布林旗標不吃下一個參數",
			[]string{"demo.cast", "--no-loop"},
			[]string{"--no-loop", "demo.cast"},
		},
		{
			"布林旗標後面接檔名",
			[]string{"--no-loop", "demo.cast"},
			[]string{"--no-loop", "demo.cast"},
		},
		{
			"等號形式",
			[]string{"demo.cast", "--o=out.svg"},
			[]string{"--o=out.svg", "demo.cast"},
		},
		{
			"-- 之後全部是位置參數",
			[]string{"--", "-weird-name.cast"},
			[]string{"-weird-name.cast"},
		},
		{
			"只有檔名",
			[]string{"demo.cast"},
			[]string{"demo.cast"},
		},
		{
			"空的",
			[]string{},
			nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reorderArgs(newFS(), c.in)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("reorderArgs(%v)\n  得到 %v\n  預期 %v", c.in, got, c.want)
			}
		})
	}
}

// 重排之後真的能解析出正確的值。
func TestReorderThenParse(t *testing.T) {
	fs := newFS()
	if err := fs.Parse(reorderArgs(fs, []string{"demo.cast", "-o", "out.svg", "--speed", "2"})); err != nil {
		t.Fatal(err)
	}
	if fs.NArg() != 1 || fs.Arg(0) != "demo.cast" {
		t.Errorf("位置參數 = %v，預期 [demo.cast]", fs.Args())
	}
	if got := fs.Lookup("o").Value.String(); got != "out.svg" {
		t.Errorf("-o = %q，預期 out.svg", got)
	}
	if got := fs.Lookup("speed").Value.String(); got != "2" {
		t.Errorf("--speed = %q，預期 2", got)
	}
}

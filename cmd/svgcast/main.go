// svgcast 把 asciinema 的錄影轉成又小又清晰的動畫 SVG。
//
//	asciinema rec demo.cast
//	svgcast demo.cast -o demo.svg
//
// 錄製交給 asciinema，我們只做轉換（見規格第 2 段）。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/co2water/svgcast/internal/cast"
	"github.com/co2water/svgcast/internal/frames"
	"github.com/co2water/svgcast/internal/render"
	"github.com/co2water/svgcast/internal/term"
)

// version 由 goreleaser 的 -ldflags 注入；`go install …@vX.Y.Z` 不會經過 ldflags，
// 那條路改讀模組的 build info，使用者回報問題時才有真正的版本號而不是 "dev"。
var version = "dev"

func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "svgcast:", err)
		os.Exit(1)
	}
}

func run() error {
	opts := render.DefaultOptions()

	fs := flag.NewFlagSet("svgcast", flag.ContinueOnError)
	fs.Usage = func() { usage(fs) }

	var (
		out       = fs.String("o", "", "output file (default: input with .svg extension)")
		still     = fs.String("still", "", "also write a static preview SVG to this path")
		fontFam   = fs.String("font", opts.FontFamily, "font-family fallback chain")
		embedFont = fs.String("embed-font", "", "embed outlines of used private-use glyphs from this font file")
		theme     = fs.String("theme", "auto", "color scheme: auto (follows the reader's light/dark mode) | light | dark")
		speed     = fs.Float64("speed", 1.0, "playback speed multiplier")
		noLoop    = fs.Bool("no-loop", false, "play once instead of looping")
		noCursor  = fs.Bool("no-cursor", false, "do not draw the cursor block")
		from      = fs.String("from", "", "start at this time, e.g. 5s or 1m30s")
		to        = fs.String("to", "", "stop at this time, e.g. 5s or 1m30s")
		idle      = fs.String("idle-time-limit", "", "cap idle gaps at this duration, e.g. 2s")
		showVer   = fs.Bool("version", false, "print version")
	)

	// ⭐ 先把位置參數移到最後再解析。
	// Go 的 flag 套件遇到第一個非旗標參數就停止解析，所以
	// `svgcast demo.cast -o out.svg` 會整個失效——而那正是 README 裡寫的用法。
	// 使用者一定會這樣打，不能讓他在第一分鐘就撞牆。
	if err := fs.Parse(reorderArgs(fs, os.Args[1:])); err != nil {
		return err
	}

	if *showVer {
		fmt.Println("svgcast", versionString())
		return nil
	}
	if fs.NArg() != 1 {
		fs.Usage()
		if fs.NArg() == 0 {
			return fmt.Errorf("need an input .cast file")
		}
		return fmt.Errorf("expected exactly one input file, got %d: %v", fs.NArg(), fs.Args())
	}

	in := fs.Arg(0)
	outPath := *out
	if outPath == "" {
		outPath = strings.TrimSuffix(in, ".cast") + ".svg"
	}

	// ⚠️ 時間一律收「時間字串」不收毫秒數字。
	// svg-term-cli 用毫秒被抱怨了 11 則，是該專案討論度最高的 issue（規格表格 #3）。
	var err error
	if opts.From, err = parseDuration(*from); err != nil {
		return fmt.Errorf("--from: %w", err)
	}
	if opts.To, err = parseDuration(*to); err != nil {
		return fmt.Errorf("--to: %w", err)
	}
	if opts.IdleTimeLimit, err = parseDuration(*idle); err != nil {
		return fmt.Errorf("--idle-time-limit: %w", err)
	}

	opts.FontFamily = *fontFam
	opts.EmbedFont = *embedFont
	opts.Theme = *theme
	opts.Speed = *speed
	opts.Loop = !*noLoop
	opts.Cursor = !*noCursor

	switch opts.Theme {
	case "auto", "light", "dark":
	default:
		return fmt.Errorf("--theme must be auto, light or dark (got %q)", opts.Theme)
	}
	if opts.Speed <= 0 {
		return fmt.Errorf("--speed must be greater than 0")
	}
	if opts.To > 0 && opts.To <= opts.From {
		return fmt.Errorf("--to must be later than --from")
	}

	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	c, err := cast.Parse(f)
	if err != nil {
		return err
	}

	fopts := frames.DefaultOptions()
	fopts.From = opts.From
	fopts.To = opts.To
	fopts.Speed = opts.Speed
	fopts.IdleTimeLimit = opts.IdleTimeLimit

	// ⭐ 一次擷取，同時餵給動畫累積器與（需要時）記下終態影格。
	// 影格是串流的，不會全部留在記憶體裡。
	anim := render.NewAnimator(opts)
	var last term.Frame
	if err := frames.Capture(c, fopts, func(fr term.Frame) error {
		if *still != "" {
			last = fr
		}
		return anim.Add(fr)
	}); err != nil {
		return err
	}

	if err := writeSVG(outPath, anim.Write); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%s)\n", outPath, fileSize(outPath))
	warnIfLarge(outPath)

	if *still != "" {
		if err := writeSVG(*still, func(w io.Writer) error {
			return render.RenderStill(w, last, opts)
		}); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote still preview %s (%s)\n", *still, fileSize(*still))
	}
	return nil
}

// GitHub README 的圖片上限是 10 MB，實務上建議壓在 5 MB 以下才不會拖慢載入。
const (
	githubHardLimit = 10 << 20
	githubAdvised   = 5 << 20
)

// warnIfLarge 在輸出大到會影響 README 時提醒使用者，並給出可行的做法。
//
// 很長的錄影本來就會產生很大的 SVG——這不是 bug，但使用者應該在
// 推上 GitHub 之前就知道，而不是等到 README 載入很慢才發現。
func warnIfLarge(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	switch {
	case fi.Size() > githubHardLimit:
		fmt.Fprint(os.Stderr,
			"warning: over GitHub's 10 MB limit for README images; it will not render.\n"+
				"  try --idle-time-limit 2s, --from/--to to trim, or --speed 2\n")
	case fi.Size() > githubAdvised:
		fmt.Fprint(os.Stderr,
			"note: over 5 MB; READMEs load slowly at this size.\n"+
				"  try --idle-time-limit 2s to collapse pauses, or --from/--to to trim\n")
	}
}

// fileSize 回傳人類看得懂的檔案大小。
// 體積是這個工具的賣點之一，每次輸出都印出來提醒自己。
func fileSize(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	n := fi.Size()
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// writeSVG 把內容寫進檔案。先寫暫存檔再改名，避免中途失敗留下半截檔案。
func writeSVG(path string, render func(io.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := render(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// parseDuration 接受 "5s"、"1m30s"、"500ms"；空字串回 0。
func parseDuration(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("cannot parse duration %q (try 5s or 1m30s)", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("duration cannot be negative")
	}
	return d.Seconds(), nil
}

// reorderArgs 把位置參數搬到最後，讓旗標可以寫在檔名之後。
//
// Go 的 flag 套件一遇到非旗標參數就停止解析，但 CLI 的慣例是
// `tool input.cast -o out.svg` 兩種順序都要能用。
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]

		// "--" 之後全部當成位置參數
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)

			// --name=value 形式已經自帶值；--name value 形式要把下一個參數一起帶走，
			// 否則它會被誤判成位置參數。布林旗標不吃值。
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if f := fs.Lookup(name); f != nil && !isBoolFlag(f) && i+1 < len(args) {
					i++
					flags = append(flags, args[i])
				}
			}
			continue
		}

		positional = append(positional, a)
	}
	return append(flags, positional...)
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

func usage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `svgcast — turn asciinema recordings into small, crisp animated SVGs

Usage:
  svgcast [flags] <input.cast>
  svgcast <input.cast> [flags]

Examples:
  asciinema rec demo.cast
  svgcast demo.cast -o demo.svg
  svgcast demo.cast -o demo.svg --still preview.svg --speed 1.5
  svgcast demo.cast -o demo.svg --embed-font ~/.fonts/FiraCodeNerdFont-Regular.ttf

Flags:
`)
	fs.PrintDefaults()
}

module github.com/co2water/svgcast

go 1.23.0

require (
	github.com/hinshun/vt10x v0.0.0-20220301184237-5011da428d02
	github.com/mattn/go-runewidth v0.0.16
	golang.org/x/image v0.23.0
)

require (
	github.com/rivo/uniseg v0.2.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

// ✅ W1 spike #1 完成（2026-09-08）：VT 層選定 hinshun/vt10x，版本已釘死。
//
// 為什麼是它（落選的是 charmbracelet/x/vt 與 rcarmo/go-te）：
//   - pkg.go.dev imported-by：vt10x 82 vs charmbracelet/x/vt 6
//   - 被 openshift/odo、tektoncd/cli、jenkins-x/jx、podman-tui 拿去跑測試
//   - MrMarble/termsvg（唯一活著的競品）用的就是它，已證明能撐 asciicast → SVG
//   - 零相依（它自己的 go.mod 沒有任何 require）
//
// ⚠️ 版本不要動。理由有二：
//   1. 上游停在 2022，動了也沒有新東西，只有風險
//   2. 它的屬性位元是未匯出的，我們在 internal/term/vt10x.go 照抄了 1<<iota 順序；
//      TestAttrBitsStillMatch 會擋住抄錯，但升版前務必先跑那個測試
//
// 硬規則（規格第 3 段）：不要自己寫終端模擬器。破了這條，六週做不完。

// ⚠️ x/image 與 x/text 刻意釘在舊版，不要隨手 `go get -u`。
//
// 新版把 go 指令拉到 1.26，等於要求使用者裝很新的 Go 才能 `go install`。
// 這個工具的第一賣點是「一行安裝」，最低版本越低越好。
// 實測：x/image v0.23.0 需要 go 1.18、x/text v0.21.0 同理；
// 升到 x/image v0.46.0 + x/text v0.42.0 會強制 go 1.26。
//
// 我們只用 x/image/font/sfnt 讀字型輪廓，那部分的 API 多年沒變，舊版完全夠用。

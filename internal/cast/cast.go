// Package cast 解析 asciinema 的 asciicast v2 格式。
//
// 格式：第一行是 header JSON，之後每行一個事件陣列 [time, type, data]。
// 規格：https://docs.asciinema.org/manual/asciicast/v2/
//
// 我們只讀不寫——錄製交給 asciinema（見規格第 2 段「不做錄製器」）。
package cast

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// 終端尺寸的上限。
//
// 模擬器會配置 width×height 個 cell，所以壞掉或惡意的 header 可以靠一行 JSON
// 讓程式吃光記憶體。這裡給的餘裕遠超任何真實終端（一般是 80×24 到 300×80），
// 但足以擋掉 999999×999999 那種輸入。
const (
	maxCols = 2000
	maxRows = 1000
)

// Theme 是 header 裡選擇性的配色。沒有的話用內建預設。
type Theme struct {
	Foreground string `json:"fg,omitempty"`
	Background string `json:"bg,omitempty"`
	// Palette 是 8 或 16 色，以冒號分隔的 hex，例如 "#000000:#ff0000:..."
	Palette string `json:"palette,omitempty"`
}

// Header 是 asciicast v2 的第一行。
type Header struct {
	Version int    `json:"version"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Title   string `json:"title,omitempty"`

	Timestamp int64             `json:"timestamp,omitempty"`
	Duration  float64           `json:"duration,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Theme     *Theme            `json:"theme,omitempty"`

	// IdleTimeLimit：超過這個秒數的靜止會被壓縮掉。
	// svg-term-cli 有人開 issue 要這個功能且沒人做（規格表格 #3）。W4 要支援。
	IdleTimeLimit float64 `json:"idle_time_limit,omitempty"`
}

// EventType 是事件的種類。v1 只在意 Output 與 Resize。
type EventType string

const (
	Output EventType = "o" // 終端輸出
	Input  EventType = "i" // 使用者輸入（v1 忽略）
	Resize EventType = "r" // 尺寸改變，data 形如 "80x24"
	Marker EventType = "m" // 章節標記（v2 再說）
)

// Event 是錄影中的一個事件。
type Event struct {
	Time float64
	Type EventType
	Data string
}

// Cast 是一份完整的錄影。
type Cast struct {
	Header Header
	Events []Event
}

// Duration 回傳最後一個事件的時間；header 有 duration 就用它。
func (c *Cast) Duration() float64 {
	if c.Header.Duration > 0 {
		return c.Header.Duration
	}
	if len(c.Events) == 0 {
		return 0
	}
	return c.Events[len(c.Events)-1].Time
}

// Parse 讀入一份 asciicast v2。
func Parse(r io.Reader) (*Cast, error) {
	sc := bufio.NewScanner(r)
	// 單一事件可能很長（大量輸出擠在一行），預設 64KB 不夠。
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		return nil, fmt.Errorf("empty file: an asciicast needs at least a header line")
	}

	var c Cast
	if err := json.Unmarshal(sc.Bytes(), &c.Header); err != nil {
		return nil, fmt.Errorf("parsing header: %w", err)
	}
	if c.Header.Version != 2 {
		return nil, fmt.Errorf("only asciicast v2 is supported, this file is v%d", c.Header.Version)
	}
	if c.Header.Width <= 0 || c.Header.Height <= 0 {
		return nil, fmt.Errorf("invalid terminal size in header: %dx%d", c.Header.Width, c.Header.Height)
	}
	// 尺寸上限：模擬器會配置 width×height 個 cell，壞掉或惡意的 header
	// （例如 999999x999999）會直接吃光記憶體。與其 OOM，不如給明確的錯誤。
	if c.Header.Width > maxCols || c.Header.Height > maxRows {
		return nil, fmt.Errorf("terminal size %dx%d exceeds the limit of %dx%d",
			c.Header.Width, c.Header.Height, maxCols, maxRows)
	}

	for lineNo := 2; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ev, err := parseEvent(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		c.Events = append(c.Events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading events: %w", err)
	}

	// termtosvg 的 bug：stdin 關閉時產生空 SVG（規格表格 #5）。
	// 我們在這裡就報錯，而不是產出一個空檔案讓使用者困惑。
	if len(c.Events) == 0 {
		return nil, fmt.Errorf("this recording has no events")
	}
	return &c, nil
}

func parseEvent(line string) (Event, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Event{}, fmt.Errorf("not a valid event array: %w", err)
	}
	if len(raw) != 3 {
		return Event{}, fmt.Errorf("an event needs 3 elements, this line has %d", len(raw))
	}

	var ev Event
	if err := json.Unmarshal(raw[0], &ev.Time); err != nil {
		return Event{}, fmt.Errorf("time field: %w", err)
	}
	var typ string
	if err := json.Unmarshal(raw[1], &typ); err != nil {
		return Event{}, fmt.Errorf("type field: %w", err)
	}
	ev.Type = EventType(typ)
	if err := json.Unmarshal(raw[2], &ev.Data); err != nil {
		return Event{}, fmt.Errorf("data field: %w", err)
	}
	return ev, nil
}

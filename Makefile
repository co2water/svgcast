BINARY  := svgcast
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test lint fmt demo clean spike snapshot check

all: check build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/svgcast

# check 是提交前該跑的那一組——跟 CI 的內容一致。
check: fmt-check vet test

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "not gofmt'd:"; echo "$$out"; exit 1; fi

fuzz:
	go test ./internal/cast -fuzz FuzzParse -fuzztime 30s

# ⭐ dogfood：README 的 demo 就是用它自己產生的。
#
# gen-demo.py 是兩段式的：先產一次量出真實大小，再把真實數字寫回 demo 內容。
# demo 裡出現的每個數字都必須是真的。
demo: build
	python docs/gen-demo.py
	./bin/$(BINARY) docs/demo.cast -o docs/demo.svg --still docs/demo-still.svg
	@echo "--- 大小 ---"
	@ls -lh docs/demo.svg docs/demo-still.svg

# 跟 GIF 比大小——README 那張表就是這樣量的。
# 需要 agg（asciinema 官方的 GIF 產生器）：brew install agg，或
# https://github.com/asciinema/agg/releases
sizes: build
	@for c in docs/demo.cast testdata/scroll.cast testdata/typical.cast; do \
	  agg $$c /tmp/svgcast-x.gif >/dev/null 2>&1; \
	  ./bin/$(BINARY) $$c -o /tmp/svgcast-x.svg >/dev/null 2>&1; \
	  g=$$(wc -c < /tmp/svgcast-x.gif); s=$$(wc -c < /tmp/svgcast-x.svg); \
	  awk -v c="$$c" -v g="$$g" -v s="$$s" \
	    'BEGIN{printf "%-26s gif %8d  svg %7d  %4.1fx\n", c, g, s, g/s}'; \
	done

# 本機試跑 goreleaser（不發布任何東西）
snapshot:
	goreleaser release --snapshot --clean

spike:
	@echo "W1 的三個 spike 全部通過："
	@echo "  #2 GitHub 相容性  CSS 動畫沒有被消毒，<img> 情境下確實會播"
	@echo "  #3 深色模式      @media (prefers-color-scheme) 在三種載入情境全生效"
	@echo "  #1 VT 函式庫     hinshun/vt10x，PUA 字元零偏移"
	@echo "細節見 internal/render/svg.go 與 internal/term/vt10x.go 的註解"

clean:
	rm -rf bin dist docs/demo.svg docs/demo-still.svg docs/s[2-6]-*.svg

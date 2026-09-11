#!/usr/bin/env bash
# 抓下對應平台的 svgcast 並放進 PATH。
#
# 拆成獨立的 shell script 而不是內嵌在 action.yml 裡，
# 是為了能在本機直接跑起來驗證——action.yml 沒辦法單獨測試。
set -euo pipefail

VERSION="${1:-latest}"
REPO="co2water/svgcast"

case "$(uname -s)" in
  Linux*)  GOOS=linux ;;
  Darwin*) GOOS=darwin ;;
  *)       echo "svgcast: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) echo "svgcast: unsupported arch $(uname -m)" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | cut -d'"' -f4)
  [ -n "$VERSION" ] || { echo "svgcast: cannot resolve latest release" >&2; exit 1; }
fi

NUM="${VERSION#v}"
ASSET="svgcast_${NUM}_${GOOS}_${GOARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "svgcast: downloading ${VERSION} (${GOOS}/${GOARCH})"
curl -fsSL "$URL" -o "$TMP/a.tar.gz"
tar -xzf "$TMP/a.tar.gz" -C "$TMP"

DEST="${RUNNER_TEMP:-/usr/local}/bin"
mkdir -p "$DEST"
install -m 0755 "$TMP/svgcast" "$DEST/svgcast"
echo "$DEST" >> "${GITHUB_PATH:-/dev/null}"
"$DEST/svgcast" --version

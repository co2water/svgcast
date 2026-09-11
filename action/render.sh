#!/usr/bin/env bash
# 把符合條件的每個 .cast 轉成 .svg。
set -euo pipefail

INPUT="$1"; shift
OUTDIR="${1:-}"; shift || true

shopt -s nullglob
FILES=( $INPUT )
if [ ${#FILES[@]} -eq 0 ]; then
  echo "svgcast: no files matched '$INPUT'" >&2
  exit 1
fi

for f in "${FILES[@]}"; do
  base="$(basename "${f%.cast}")"
  if [ -n "$OUTDIR" ]; then
    mkdir -p "$OUTDIR"
    out="$OUTDIR/$base.svg"
  else
    out="${f%.cast}.svg"
  fi
  echo "svgcast: $f -> $out"
  svgcast "$f" -o "$out" "$@"
done

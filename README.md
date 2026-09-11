<div align="center">

# svgcast

**Stop putting blurry GIFs in your README.**

Turn [asciinema](https://asciinema.org) recordings into animated SVGs that are 3–8× smaller than
the same recording as a GIF, stay crisp at any zoom, and **follow your reader's light/dark mode —
from a single file**.

[![CI](https://github.com/co2water/svgcast/actions/workflows/ci.yml/badge.svg)](https://github.com/co2water/svgcast/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/co2water/svgcast?include_prereleases)](https://github.com/co2water/svgcast/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/co2water/svgcast)](https://goreportcard.com/report/github.com/co2water/svgcast)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

English | [繁體中文](README.zh-TW.md)

</div>

![demo](docs/demo.svg)

<p align="center"><i>That demo is 17.5 KB. The same recording as a GIF is 64 KB. It was produced by the command it shows.</i></p>

## Install

```bash
go install github.com/co2water/svgcast/cmd/svgcast@latest
```

Or grab a binary from [Releases](https://github.com/co2water/svgcast/releases) — a single static
file for macOS, Linux and Windows, no dependencies.

`npx svgcast` and `brew install co2water/tap/svgcast` ship with v0.1.0.

## Usage

```bash
asciinema rec demo.cast
svgcast demo.cast -o demo.svg
```

That's it. svgcast doesn't record — [asciinema](https://asciinema.org) already does that well.
It only converts.

```bash
svgcast demo.cast -o demo.svg --still preview.svg   # also write a static preview
svgcast demo.cast -o demo.svg --theme dark          # force one palette
svgcast demo.cast -o demo.svg --idle-time-limit 2s  # collapse long pauses
svgcast demo.cast -o demo.svg --from 5s --to 1m     # trim to a range
```

## Why not a GIF

|                     | GIF            | svgcast SVG          |
| ------------------- | -------------- | -------------------- |
| Zoom / HiDPI        | blurry         | crisp at any size    |
| Dark mode           | ❌ impossible   | ✅ same file adapts   |
| Selectable text     | ❌              | ✅                    |
| Screen readers      | ❌              | ✅                    |

The last three aren't "svgcast is better at this" — they are things a raster image
**cannot do**. A GIF is a grid of pixels; it has exactly one palette, no text, and nothing
for a screen reader to read.

Size, measured on the same recordings — GIF via [agg](https://github.com/asciinema/agg)
(asciinema's own generator, defaults) vs svgcast (defaults):

| Recording                              | GIF (agg) | SVG (svgcast) | Ratio |
| -------------------------------------- | --------: | ------------: | ----: |
| The demo above (68×12, 11 s)           |   64.4 KB |       17.5 KB |  3.7× |
| 30 lines scrolling by (60×12, 4 s)     |   44.2 KB |        8.3 KB |  5.3× |
| Typical session (80×24, 15 s, 5 cmds)  |  197.1 KB |       23.7 KB |  8.3× |

The gap widens with terminal size and recording length: a GIF pays for every pixel of every
frame; an SVG pays only for what changed. Reproduce with `make sizes` (needs `agg` on PATH).

## Dark mode

One file. It follows the reader's system theme, because the SVG carries both palettes and a
`prefers-color-scheme` media query.

GitHub's own recommended approach needs **two files** — `<picture>` with a
`<source media="(prefers-color-scheme: dark)">` and two separate images. svgcast needs one.

## Nerd Font / Powerline glyphs

Those symbols live in the Unicode private use area. A README image is loaded as `<img>`,
which is a sandbox: **external fonts are never downloaded**. On a reader's machine without
your font, they render as empty boxes.

`--embed-font` fixes that by embedding **only the outlines of the glyphs your recording
actually uses** — a handful of `<path>` elements, not a multi-megabyte font file:

```bash
svgcast demo.cast -o demo.svg --embed-font ~/.fonts/FiraCodeNerdFont-Regular.ttf
```

Regular text stays as `<text>`, so it remains selectable.

## Keep your demo fresh in CI

A demo that drifts from the tool is worse than no demo. This action re-renders your `.cast`
files on every push:

```yaml
- uses: co2water/svgcast/action@v1
  with:
    input: docs/*.cast
    args: --idle-time-limit 2s
```

## Flags

```
-o <file>                output file (default: input with .svg extension)
--still <file>           also write a static preview SVG
--theme auto|light|dark  color scheme (default: auto)
--embed-font <ttf>       embed outlines of used private-use glyphs
--speed <n>              playback speed multiplier
--from / --to <dur>      trim to a range, e.g. --from 5s --to 1m30s
--idle-time-limit <dur>  collapse long pauses, e.g. 2s
--no-loop                play once instead of looping
--no-cursor              don't draw the cursor block
--font <family>          font-family fallback chain
```

Durations are written the way you'd say them (`5s`, `1m30s`), not milliseconds.

## Why another one

The need is proven. Nobody is maintaining it:

| Project                                                    |     ★ | Last update           |
| ---------------------------------------------------------- | ----: | --------------------- |
| [termtosvg](https://github.com/nbedos/termtosvg)           | 9,756 | 2020 · archived       |
| [svg-term-cli](https://github.com/marionebl/svg-term-cli)  | 4,245 | 2024 · 48 open issues |

svgcast targets the three things **both** were asked to fix and neither did:

1. **Powerline / Nerd Font glyphs don't drift.** termtosvg
   [#14](https://github.com/nbedos/termtosvg/issues/14) has been open since 2018. The cause is
   glyph advance widths not matching the cell grid; svgcast gives every run an explicit `x`, so
   error cannot accumulate.
2. **Small files.** Differential frames, run merging, scrolling handled as a viewport transform
   instead of redrawing the screen, and glyph outline embedding instead of font subsetting.
3. **Large recordings don't blow up.** 50,000 events render in ~5 s with a ~43 MB peak heap.
   Frames are streamed, never accumulated.

## Known limitations

- **CJK and other double-width characters mis-align.** The underlying VT emulator
  ([vt10x](https://github.com/hinshun/vt10x)) doesn't track character width, so it advances
  one cell per character. ASCII, Powerline and Nerd Font glyphs are unaffected.
- **Private-use characters are always one cell wide.** Deliberate: Powerline and Nerd Font
  symbols are designed to occupy one cell, while standard width tables classify the private
  use area as "ambiguous". Following the tables makes whole lines slide right — that is
  exactly termtosvg #14.
- **Blink is not implemented.** termtosvg had a "blink does not blink" issue; svgcast chooses
  not to implement it rather than ship a broken version.
- **Faint and strikethrough are dropped.** vt10x doesn't track them.
- **Themes follow the OS `prefers-color-scheme`, not your GitHub theme setting.** GitHub's own
  `<picture>` approach has the identical limitation.

## Contributing

Issues and pull requests are welcome. Run `make check` (gofmt, vet, tests) before submitting.

## License

[Apache-2.0](LICENSE)

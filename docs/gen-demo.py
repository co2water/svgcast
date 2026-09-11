#!/usr/bin/env python3
"""產生 README 用的 demo.cast。

兩段式：先用佔位大小產生一次、量出真實大小，再用真實數字重產一次。
demo 裡出現的每個數字都必須是真的——README 的可信度從這裡開始。

用法：  python docs/gen-demo.py   （之後 make demo 會渲染它）
"""
import io, json, subprocess, sys, os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CAST = os.path.join(ROOT, "docs", "demo.cast")
SVG  = os.path.join(ROOT, "docs", "demo.svg")
BIN  = os.path.join(ROOT, "bin", "svgcast.exe" if os.name == "nt" else "svgcast")

G="\033[32m"; B="\033[34m"; C="\033[36m"; Y="\033[33m"; D="\033[90m"; R="\033[0m"

def build(size_label):
    ev, t = [], 0.0
    def out(dt, s):
        nonlocal t
        t += dt; ev.append([round(t, 3), "o", s])
    def typing(s, dt=0.055):
        for ch in s: out(dt, ch)

    prompt = G + "~/svgcast" + R + " " + B + "$" + R + " "

    out(0.4, prompt)
    typing("asciinema rec demo.cast")
    out(0.35, "\r\n")
    out(0.15, D + "asciinema: recording asciicast to demo.cast" + R + "\r\n")
    out(0.15, D + "asciinema: press <ctrl-d> to finish" + R + "\r\n")
    out(0.9, prompt)
    typing("svgcast demo.cast -o demo.svg")
    out(0.35, "\r\n")
    out(0.25, C + "wrote" + R + " demo.svg (" + Y + size_label + R + ")\r\n")
    out(0.9, prompt)
    typing("# this image was produced by the command above")
    out(1.4, "\r\n")
    out(0.3, prompt)

    hdr = {"version": 2, "width": 68, "height": 12, "title": "svgcast demo"}
    with io.open(CAST, "w", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(hdr, ensure_ascii=False) + "\n")
        for e in ev:
            f.write(json.dumps(e, ensure_ascii=False) + "\n")

def render():
    subprocess.run([BIN, CAST, "-o", SVG], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return os.path.getsize(SVG)

def label(n):
    return "%.1f KB" % (n / 1024)

build("?? KB")
first = render()
# 用真實大小重產。長度變化可能讓大小再變一點點，但差異在 0.1 KB 內，收斂夠快。
build(label(first))
second = render()
build(label(second))
final = render()
print("demo.cast 已更新；demo.svg = %s (%d bytes)" % (label(final), final))

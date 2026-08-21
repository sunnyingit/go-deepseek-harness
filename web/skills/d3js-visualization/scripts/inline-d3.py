#!/usr/bin/env python3
"""Inline vendored D3 v7 into an HTML file so the page works offline.

Replaces CDN <script src="...d3..."> with the contents of vendor/d3.v7.min.js.
Uses a lambda replacement so D3's backslashes are not interpreted by re.sub.

Usage:
  python3 inline-d3.py chart.html
  python3 inline-d3.py in.html -o out.html
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
VENDOR_D3 = SCRIPT_DIR.parent / "vendor" / "d3.v7.min.js"

CDN_SCRIPT = re.compile(
    r"""<script\b[^>]*\bsrc=["'][^"']*(?:d3js\.org/d3|cdn\.jsdelivr\.net/npm/d3@|unpkg\.com/d3)[^"']*["'][^>]*>\s*</script>""",
    re.I,
)


def inline_block(d3_src: str) -> str:
    return "<script>\n" + d3_src.rstrip() + "\n</script>"


def inline_html(html: str, d3_src: str) -> str:
    block = inline_block(d3_src)
    updated, n = CDN_SCRIPT.subn(lambda _m: block, html, count=1)
    if n:
        return updated
    if "d3js.org v7" in html or "Copyright 2010-2023 Mike Bostock" in html:
        return html
    if re.search(r"</body>", html, re.I):
        return re.sub(r"</body>", block + "\n</body>", html, count=1, flags=re.I)
    return html + "\n" + block + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description="Inline vendored D3 v7 into HTML")
    parser.add_argument("html", type=Path, help="HTML file with a D3 CDN script tag")
    parser.add_argument("-o", "--output", type=Path, default=None)
    parser.add_argument("--d3", type=Path, default=VENDOR_D3)
    args = parser.parse_args()
    if not args.html.is_file():
        print(f"missing html: {args.html}", file=sys.stderr)
        return 1
    if not args.d3.is_file():
        print(f"missing d3 vendor file: {args.d3}", file=sys.stderr)
        return 1
    d3_src = args.d3.read_text(encoding="utf-8")
    html = args.html.read_text(encoding="utf-8")
    out = inline_html(html, d3_src)
    dest = args.output or args.html
    dest.write_text(out, encoding="utf-8")
    print(f"inlined D3 v7 -> {dest} ({dest.stat().st_size} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

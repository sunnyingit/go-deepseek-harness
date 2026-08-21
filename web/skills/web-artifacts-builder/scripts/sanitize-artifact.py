#!/usr/bin/env python3
"""Make a web-artifacts project Parcel-safe and self-contained.

Parcel cannot resolve /favicon.svg from public/, and it fails the build when
index.html (or CSS) still points at Google Fonts. This script:

- inlines a red five-point star SVG as a data-URI favicon
- strips Google Fonts <link> / preconnect / @import
- deletes Vite's leftover public icons (vite.svg, favicon.*)

Run from the project root (the directory that contains index.html).
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

STAR_HREF = (
    "data:image/svg+xml,"
    "%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E"
    "%3Cpolygon fill='%23DE2910' "
    "points='16,2 20.12,12.47 31.51,12.47 22.2,18.94 25.64,29.53 "
    "16,22.9 6.36,29.53 9.8,18.94 0.49,12.47 11.88,12.47'/%3E"
    "%3C/svg%3E"
)
STAR_LINK = f'<link rel="icon" type="image/svg+xml" href="{STAR_HREF}" />'

GOOGLE_HOST = re.compile(r"fonts\.googleapis\.com|fonts\.gstatic\.com", re.I)
LINK_TAG = re.compile(r"<link\b[^>]*>\s*", re.I)
IMPORT_FONT = re.compile(
    r"""@import\s+(?:url\()?['"]https?://fonts\.(?:googleapis|gstatic)\.com[^;]+;\s*""",
    re.I,
)
REL_ICON = re.compile(r"""\brel=["'](?:shortcut\s+)?icon["']""", re.I)
HREF_ATTR = re.compile(r"""\bhref=["']([^"']+)["']""", re.I)
SKIP_DIRS = {".git", "node_modules", "dist", ".parcel-cache"}
TEXT_SUFFIXES = {".html", ".css", ".scss", ".tsx", ".ts", ".jsx", ".js"}
PUBLIC_JUNK = ("vite.svg", "favicon.svg", "favicon.ico", "favicon.png", "apple-touch-icon.png")


def fix_favicon(html: str) -> str:
    found = False

    def repl_link(match: re.Match[str]) -> str:
        nonlocal found
        tag = match.group(0)
        if GOOGLE_HOST.search(tag):
            return ""
        if not REL_ICON.search(tag):
            return tag
        found = True
        href_m = HREF_ATTR.search(tag)
        if not href_m:
            return STAR_LINK + "\n    "
        href = href_m.group(1)
        if href.startswith("data:"):
            return tag
        return tag.replace(href, STAR_HREF, 1)

    html = LINK_TAG.sub(repl_link, html)
    html = IMPORT_FONT.sub("", html)
    if found:
        return html
    if re.search(r"<meta\s+charset=", html, re.I):
        return re.sub(
            r"(<meta\s+charset=[^>]*>)",
            r"\1\n    " + STAR_LINK,
            html,
            count=1,
            flags=re.I,
        )
    return re.sub(
        r"(<head[^>]*>)",
        r"\1\n    " + STAR_LINK,
        html,
        count=1,
        flags=re.I,
    )


def sanitize_html(html: str) -> str:
    return fix_favicon(html)


def iter_text_files(root: Path):
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        if path.suffix.lower() in TEXT_SUFFIXES:
            yield path


def delete_public_icons(root: Path) -> None:
    public = root / "public"
    if not public.is_dir():
        return
    for name in PUBLIC_JUNK:
        junk = public / name
        if junk.is_file():
            junk.unlink()
            print(f"  removed {junk}")


def sanitize_project(root: Path) -> None:
    delete_public_icons(root)
    for path in iter_text_files(root):
        original = path.read_text(encoding="utf-8")
        if path.suffix.lower() == ".html":
            updated = sanitize_html(original)
        else:
            updated = IMPORT_FONT.sub("", original)
            def drop_link(match: re.Match[str]) -> str:
                tag = match.group(0)
                return "" if GOOGLE_HOST.search(tag) else tag
            updated = LINK_TAG.sub(drop_link, updated)
        if updated != original:
            path.write_text(updated, encoding="utf-8")
            print(f"  sanitized {path}")


def _selftest() -> None:
    sample = """<!doctype html>
<html>
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;700&display=swap" rel="stylesheet">
    <title>demo</title>
  </head>
</html>
"""
    out = sanitize_html(sample)
    assert "fonts.googleapis" not in out, out
    assert "fonts.gstatic" not in out, out
    assert "favicon.svg" not in out, out
    assert STAR_HREF in out, out
    css = "@import url('https://fonts.googleapis.com/css2?family=Inter&display=swap');\nbody{font-family:serif}"
    assert "fonts.googleapis" not in IMPORT_FONT.sub("", css)


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
        print("sanitize-artifact selftest ok")
        raise SystemExit(0)
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
    if not (root / "index.html").is_file():
        print(f"no index.html under {root}", file=sys.stderr)
        raise SystemExit(1)
    print(f"🔧 Sanitizing artifact at {root}")
    sanitize_project(root)
    print("✅ Favicon inlined, Google Fonts stripped, public icons cleared")

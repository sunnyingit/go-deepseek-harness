#!/usr/bin/env python3
"""Assemble a self-contained fortune HTML from the skill template.

Replaces placeholders with file contents / divine.py JSON. Do not do this by
hand: data.js is long, and wrapping __TODAY__ in extra quotes breaks JS
(const TODAY = ""2026-08-22"";).

Usage:
  python3 build_fortune.py -o 今日运势.html
  python3 build_fortune.py --cheer 1 --salt witch-1 -o 今日运势.html
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from datetime import date
from pathlib import Path

SKILL_DIR = Path(__file__).resolve().parent.parent
TEMPLATE = SKILL_DIR / "assets" / "fortune_template.html"
DATA_JS = SKILL_DIR / "assets" / "data.js"
DIVINE = SKILL_DIR / "scripts" / "divine.py"

PLACEHOLDERS = ("__DATA_JS__", "__PRESET_JSON__", "__TODAY__", "__SALT__", "__CHEER__")


def clamp_cheer(n: int) -> int:
    return max(0, min(int(n), 6))


def divine_json(date_str: str, salt: str, cheer: int) -> dict:
    raw = subprocess.check_output(
        [
            sys.executable,
            str(DIVINE),
            "--date",
            date_str,
            "--salt",
            salt,
            "--cheer",
            str(cheer),
        ],
        text=True,
    )
    return json.loads(raw)


def assemble(date_str: str, salt: str, cheer: int) -> tuple[str, dict]:
    html = TEMPLATE.read_text(encoding="utf-8")
    data_js = DATA_JS.read_text(encoding="utf-8")
    missing = [p for p in PLACEHOLDERS if p not in html]
    if missing:
        raise SystemExit("template missing placeholders: " + ", ".join(missing))

    preset = divine_json(date_str, salt, cheer)
    # TODAY already sits inside quotes in the template: const TODAY = "__TODAY__";
    html = html.replace("__DATA_JS__", data_js)
    html = html.replace("__PRESET_JSON__", json.dumps(preset, ensure_ascii=False))
    html = html.replace("__TODAY__", date_str)
    html = html.replace("__SALT__", salt.replace("\\", "\\\\").replace('"', '\\"'))
    html = html.replace("__CHEER__", str(cheer))
    leftover = [p for p in PLACEHOLDERS if p in html]
    if leftover:
        raise SystemExit("unreplaced placeholders: " + ", ".join(leftover))
    if f'""{date_str}""' in html or f'const TODAY = ""{date_str}""' in html:
        raise SystemExit("TODAY was double-quoted; template quotes must stay as-is")
    return html, preset


def main() -> None:
    parser = argparse.ArgumentParser(description="生成自包含周易运势 HTML")
    parser.add_argument("--date", default=None, help="YYYY-MM-DD，默认今天")
    parser.add_argument("--salt", default="", help="姓名/问题/小巫女 salt")
    parser.add_argument("--cheer", type=int, default=0, help="好运加成 0-6")
    parser.add_argument("-o", "--output", required=True, help="写入会话工作区的 .html 路径")
    args = parser.parse_args()

    date_str = args.date or date.today().isoformat()
    cheer = clamp_cheer(args.cheer)
    html, preset = assemble(date_str, args.salt or "", cheer)
    out = Path(args.output)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(html, encoding="utf-8")
    print(
        json.dumps(
            {
                "ok": True,
                "path": str(out.resolve()),
                "date": date_str,
                "salt": args.salt or "",
                "cheer": cheer,
                "hexagram": preset["original_hexagram"]["name"],
                "level_word": preset["original_hexagram"]["level_word"],
            },
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    main()

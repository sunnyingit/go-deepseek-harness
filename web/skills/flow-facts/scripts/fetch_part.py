#!/usr/bin/env python3
"""Read one volume-flow reply slice. Does not call Tushare.

Usage: fetch_part.py <ts_code> <dc|factor|chip|bar|premarket|limit|toplist|margin> [days]
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

PARTS = ("dc", "factor", "chip", "bar", "premarket", "limit", "toplist", "margin")


def main(argv: list[str]) -> int:
    if len(argv) not in (3, 4) or argv[2] not in PARTS or not argv[1].strip():
        sys.stderr.write("usage: fetch_part.py <ts_code> <dc|factor|chip|bar|premarket|limit|toplist|margin> [days]\n")
        return 2
    days = argv[3].strip() if len(argv) == 4 else ""
    if days and not days.isdigit():
        sys.stderr.write("days must be a positive integer\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    code = urllib.parse.quote(argv[1].strip(), safe="")
    url = f"{base}/api/v1/stocks/{code}/dimensions/volume_flow/reply?part={argv[2]}" + (f"&days={days}" if days else "")
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=80) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"flow {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"flow request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("flow response is not an object\n")
        return 1
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

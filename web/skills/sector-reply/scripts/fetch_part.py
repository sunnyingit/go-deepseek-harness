#!/usr/bin/env python3
"""Read one sector reply slice from the stock service. Does not call Tushare.

Usage: fetch_part.py <ts_code> <membership|tape|position|rank|heat|dc|sw>
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

PARTS = ("membership", "tape", "position", "rank", "heat", "dc", "sw")


def main(argv: list[str]) -> int:
    if len(argv) != 3 or argv[2] not in PARTS or not argv[1].strip():
        sys.stderr.write("usage: fetch_part.py <ts_code> <membership|tape|position|rank|heat|dc|sw>\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    code = urllib.parse.quote(argv[1].strip(), safe="")
    url = f"{base}/api/v1/stocks/{code}/dimensions/sector/reply?part={argv[2]}"
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=110) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"sector {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"sector request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("sector response is not an object\n")
        return 1
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

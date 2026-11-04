#!/usr/bin/env python3
"""Read the stock service basic card. Does not call Tushare.

Usage: fetch_basic.py <ts_code>
STOCK_API_BASE defaults to http://127.0.0.1:8090.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request


def main(argv: list[str]) -> int:
    if len(argv) != 2 or not argv[1].strip():
        sys.stderr.write("usage: fetch_basic.py <ts_code>\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    query = urllib.parse.urlencode({"q": argv[1].strip()})
    url = f"{base}/api/stocks/basic?{query}"
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"basic {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"basic request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("basic response is not an object\n")
        return 1
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

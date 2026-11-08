#!/usr/bin/env python3
"""Read the Baidu hot-search board (finance by default) from the stock service.

The service does the fetching, parsing and caching; this script only asks it and
shapes the answer into facts/rows for the analyst. Usage:

    fetch_baidu_board.py [board] [limit]

board: finance (default) | realtime | civil | car | game | movie | teleplay | novel
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request


def main(argv: list[str]) -> int:
    if len(argv) > 3:
        sys.stderr.write("usage: fetch_baidu_board.py [board] [limit]\n")
        return 2
    board = argv[1].strip() if len(argv) > 1 and argv[1].strip() else "finance"
    limit = argv[2].strip() if len(argv) > 2 and argv[2].strip() else "15"
    if not limit.isdigit() or not 1 <= int(limit) <= 50:
        sys.stderr.write("limit must be a number from 1 to 50\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    url = f"{base}/api/v1/shared/baidu-board?" + urllib.parse.urlencode({"board": board, "limit": limit})
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        try:
            detail = json.loads(detail).get("error") or detail
        except ValueError:
            pass
        sys.stderr.write(f"baidu board {exc.code}: {str(detail)[:300]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"baidu board request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict) or payload.get("status") != "ok":
        sys.stderr.write("baidu board response is not ok\n")
        return 1
    json.dump(document(payload), sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


def document(payload: dict) -> dict:
    items = [item for item in (payload.get("items") or []) if isinstance(item, dict)]
    facts = []
    rows = []
    for item in items:
        rank = item.get("rank")
        fact_id = f"bd.{rank}"
        title = str(item.get("title") or "").strip()
        hot = str(item.get("hot_text") or "")
        tag = str(item.get("tag") or "")
        desc = str(item.get("desc") or "").strip()
        text = " ".join(bit for bit in (f"第 {rank} 名", title, f"热度 {hot}" if hot else "", f"[{tag}]" if tag else "", desc) if bit)
        facts.append({"id": fact_id, "label": title[:40] or fact_id, "text": text})
        rows.append({"id": fact_id, "名次": rank, "标题": title, "热度": hot, "标签": tag, "简介": desc[:120]})
    return {
        "source": "baidu_board",
        "board": payload.get("board") or "",
        "board_name": payload.get("board_name") or "",
        "updated_at": payload.get("updated_at") or "",
        "fetched_at": payload.get("fetched_at") or "",
        "stale": bool(payload.get("stale")),
        "stale_reason": payload.get("stale_reason") or "",
        "count": len(items),
        "facts": facts,
        "rows": rows,
    }


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

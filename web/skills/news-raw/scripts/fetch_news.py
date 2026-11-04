#!/usr/bin/env python3
"""Read raw stock news from the stock service and optionally keep a topic.

Does not call Tushare. Keyword matching is local, including 储存/存储.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

_PAIRS = (("储存", "存储"), ("存储", "储存"))


def main(argv: list[str]) -> int:
    if len(argv) < 2 or len(argv) > 3 or not argv[1].strip():
        sys.stderr.write("usage: fetch_news.py <ts_code> [keyword]\n")
        return 2
    code = argv[1].strip()
    keyword = argv[2].strip() if len(argv) == 3 else ""
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    params = {"q": code, "limit": "50"}
    if keyword:
        params["keyword"] = keyword
    query = urllib.parse.urlencode(params)
    url = f"{base}/api/stocks/news?{query}"
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=40) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"news {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"news request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("news response is not an object\n")
        return 1
    data = payload.get("data") if isinstance(payload.get("data"), dict) else {}
    items = [item for item in (data.get("items") or []) if isinstance(item, dict)]
    json.dump(document(payload, keyword, len(items), items), sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


def variants(keyword: str) -> list[str]:
    text = keyword.strip()
    if not text:
        return []
    found = {text}
    for left, right in _PAIRS:
        if left in text:
            found.add(text.replace(left, right))
    return [item for item in found if item]


def hit(item: dict, keyword: str) -> bool:
    words = variants(keyword)
    if not words:
        return True
    blob = " ".join(str(item.get(key) or "") for key in ("title", "summary"))
    return any(word in blob for word in words)


def document(payload: dict, keyword: str, scanned: int, items: list[dict]) -> dict:
    facts = []
    rows = []
    for index, item in enumerate(items, start=1):
        title = str(item.get("title") or "").strip()
        summary = str(item.get("summary") or "").strip()
        when = str(item.get("time") or "")
        source = str(item.get("source_name") or item.get("source") or "")
        fact_id = f"nw.{index}"
        text = " ".join(bit for bit in (when, source, title, summary) if bit)
        facts.append({"id": fact_id, "label": title[:40] or fact_id, "text": text})
        rows.append({"id": fact_id, "时间": when[:16], "标题": title, "来源": source, "摘要": summary[:120]})
    return {
        "ts_code": payload.get("ts_code") or "",
        "stock_name": payload.get("stock_name") or "",
        "source": "news",
        "keyword": keyword,
        "query": str((payload.get("data") or {}).get("query") or ""),
        "unavailable": (payload.get("data") or {}).get("unavailable") or [],
        "scanned": scanned,
        "matched": len(items),
        "facts": facts,
        "rows": rows,
    }


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

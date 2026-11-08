#!/usr/bin/env python3
"""Read one slice of the fundamental reply. Does not call Tushare.

Usage: fetch_part.py <ts_code> <company|business|valuation|quality|holders|forecast|express|share_float|survey|mainbz|dividend|chain>
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

DETAIL_PARTS = ("company", "business", "valuation", "quality", "holders")
REPLY_PARTS = ("forecast", "express", "share_float", "survey", "mainbz", "dividend", "chain")
PARTS = DETAIL_PARTS + REPLY_PARTS


def main(argv: list[str]) -> int:
    if len(argv) != 3 or argv[2] not in PARTS or not argv[1].strip():
        sys.stderr.write("usage: fetch_part.py <ts_code> <company|business|valuation|quality|holders|forecast|express|share_float|survey|mainbz|dividend|chain>\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    code = urllib.parse.quote(argv[1].strip(), safe="")
    if argv[2] in REPLY_PARTS:
        url = f"{base}/api/v1/stocks/{code}/dimensions/fundamental/reply?part={argv[2]}"
    else:
        url = f"{base}/api/v1/stocks/{code}/dimensions/fundamental/detail"
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=80) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"fundamental {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"fundamental request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("fundamental response is not an object\n")
        return 1
    body = payload if argv[2] in REPLY_PARTS else pack(argv[2], payload)
    json.dump(body, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


def pack(part: str, payload: dict) -> dict:
    meta = payload.get("meta") if isinstance(payload.get("meta"), dict) else {}
    industry = meta.get("industry") if isinstance(meta.get("industry"), dict) else {}
    business = payload.get("business") if isinstance(payload.get("business"), dict) else {}
    base = {
        "ts_code": payload.get("ts_code") or "",
        "stock_name": payload.get("stock_name") or "",
        "part": part,
        "as_of": meta.get("as_of") or "",
    }
    if part == "company":
        base.update({
            "industry": industry.get("name") or "",
            "peer_count": industry.get("peer_count"),
            "main_business": business.get("main_business") or "",
            "period": business.get("period") or "",
        })
        return base
    if part == "business":
        lines = []
        series = []
        for item in business.get("items") or []:
            if not isinstance(item, dict):
                continue
            name = str(item.get("item") or "")
            share = item.get("share")
            lines.append({
                "名称": name,
                "收入亿元": item.get("sales_yi"),
                "占比": None if share is None else round(float(share) * 100, 1),
                "同比": None if item.get("yoy") is None else round(float(item["yoy"]) * 100, 1),
                "毛利率": None if item.get("gross_margin") is None else round(float(item["gross_margin"]) * 100, 1),
            })
            if name and share is not None:
                series.append({"label": name, "value": round(float(share) * 100, 1)})
        base.update({"period": business.get("period") or "", "lines": lines, "series": series})
        return base
    if part == "valuation":
        block = payload.get("valuation") if isinstance(payload.get("valuation"), dict) else {}
        history = block.get("history") if isinstance(block.get("history"), dict) else {}
        dates = history.get("dates") or []
        values = history.get("values") or []
        points = [{"label": str(day)[:10], "value": value} for day, value in zip(dates, values) if value is not None]
        peers = []
        for item in (block.get("peers") or [])[:8]:
            if isinstance(item, dict):
                peers.append({"name": item.get("name") or item.get("ts_code") or "", "value": item.get("value")})
        base.update({
            "metric": block.get("metric_text") or "",
            "value": block.get("value"),
            "pctile_5y": None if block.get("pctile_5y") is None else round(float(block["pctile_5y"]) * 100),
            "peer_median": block.get("peer_median"),
            "peers": peers,
            "bands": {"p20": history.get("p20"), "p50": history.get("p50"), "p80": history.get("p80")},
            "series": _sample(points, 40),
        })
        return base
    if part == "quality":
        block = payload.get("quality") if isinstance(payload.get("quality"), dict) else {}
        rows = []
        for item in block.get("items") or []:
            if not isinstance(item, dict):
                continue
            pct = item.get("peer_pctile")
            rows.append({
                "项目": item.get("metric_text") or item.get("key") or "",
                "数值": item.get("value_text") or "",
                "同行分位": None if pct is None else round(float(pct) * 100),
            })
        base.update({"rows": rows, "weakest": (block.get("weakest") or {}).get("text") or ""})
        return base
    events = []
    for item in payload.get("holder_events") or []:
        if not isinstance(item, dict):
            continue
        events.append({
            "日期": item.get("date") or "",
            "标题": item.get("title") or "",
            "说明": item.get("detail_text") or "",
            "影响": item.get("impact") or "",
        })
    base.update({"events": events})
    return base


def _sample(points: list[dict], limit: int) -> list[dict]:
    if len(points) <= limit:
        return points
    step = len(points) / limit
    picked = [points[min(len(points) - 1, int(i * step))] for i in range(limit)]
    if picked[-1] is not points[-1]:
        picked[-1] = points[-1]
    return picked


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

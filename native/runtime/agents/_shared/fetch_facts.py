#!/usr/bin/env python3
"""Read one dimension's already-built detail from the stock service.

The dimension and query string come from the agent config via the environment.
This script does not accept a dimension argument and does not call Tushare.
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def main(argv: list[str]) -> int:
    if len(argv) != 2 or not argv[1].strip():
        sys.stderr.write("usage: fetch_facts.py <ts_code>\n")
        return 2
    dimension = os.environ.get("STOCK_FACTS_DIMENSION", "").strip()
    if not dimension:
        sys.stderr.write("STOCK_FACTS_DIMENSION is not set\n")
        return 2
    base = os.environ.get("STOCK_API_BASE", "http://127.0.0.1:8090").rstrip("/")
    code = urllib.parse.quote(argv[1].strip(), safe="")
    dim = urllib.parse.quote(dimension, safe="")
    url = f"{base}/api/v1/stocks/{code}/dimensions/{dim}/detail"
    query = os.environ.get("STOCK_FACTS_QUERY", "").strip()
    if query:
        url += "?" + query
    try:
        payload = load_ready(url)
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        sys.stderr.write(f"detail {exc.code}: {detail[:500]}\n")
        return 1
    except Exception as exc:
        sys.stderr.write(f"detail request failed: {exc}\n")
        return 1
    if not isinstance(payload, dict):
        sys.stderr.write("detail response is not an object\n")
        return 1
    json.dump(slim(payload, dimension), sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


_NEWS_IMPACT = {"positive": "利好", "negative": "利空", "neutral": "中性"}
_NEWS_SCOPE = {"company": "公司", "industry": "行业", "market": "市场"}
_NEWS_GRADE = {"A": "A 公告", "B": "B 官方", "C": "C 媒体", "D": "D 研报", "E": "E 热榜"}
_NEWS_CATEGORY = {
    "earnings": "业绩", "operations": "经营", "capital": "资本", "governance": "风险与治理",
    "industry": "政策与行业", "macro": "宏观与市场", "opinion": "观点", "rumor": "传闻与澄清",
}
_NEWS_BASIS = {"fact": "事实", "plan": "计划", "forecast": "预测", "rumor": "传闻"}
_NEWS_MAGNITUDE = {"large": "大", "medium": "中", "small": "小"}
_NEWS_PRICED = {"not_yet": "尚未反映", "partly": "部分反映", "mostly": "大多已反映", "unknown": "对不上"}


def _news_row(item: dict) -> dict:
    grade = str(item.get("source_grade") or "")
    published = str(item.get("published_at") or item.get("time") or "")
    if len(published) >= 16:
        published = published[:16].replace("T", " ")
    reaction = item.get("reaction") if isinstance(item.get("reaction"), dict) else {}
    priced = _NEWS_PRICED.get(str(reaction.get("priced_in") or ""), str(reaction.get("priced_in") or ""))
    magnitude = str(item.get("magnitude_text") or "") or _NEWS_MAGNITUDE.get(str(item.get("magnitude") or ""), "")
    return {
        "id": item.get("event_id"),
        "时间": published,
        "标题": str(item.get("title") or ""),
        "来源": _NEWS_GRADE.get(grade, grade),
        "方向": _NEWS_IMPACT.get(str(item.get("impact") or ""), str(item.get("impact") or "")),
        "范围": _NEWS_SCOPE.get(str(item.get("scope") or ""), str(item.get("scope") or "")),
        "类别": _NEWS_CATEGORY.get(str(item.get("category") or ""), str(item.get("category") or "")),
        "量级": magnitude,
        "依据": _NEWS_BASIS.get(str(item.get("basis") or ""), str(item.get("basis") or "")),
        "当日反应": str(reaction.get("text") or ""),
        "是否反映": priced,
        "说明": str(item.get("summary") or ""),
    }


def _news_pack(payload: dict) -> tuple[list[dict], list[dict]]:
    rows = []
    facts = []
    for item in payload.get("key_events") or []:
        if not isinstance(item, dict) or not item.get("event_id"):
            continue
        row = _news_row(item)
        rows.append(row)
        text = "，".join(
            bit for bit in (
                row["时间"], row["标题"], row["来源"], row["方向"], row["类别"],
                row["当日反应"], row["是否反映"], row["说明"],
            ) if bit
        )
        facts.append({
            "id": item["event_id"],
            "label": row["标题"][:40] or item["event_id"],
            "group": "新闻",
            "text": text,
            "value": None,
            "unit": "",
        })
    return rows, facts


def load_ready(url: str) -> dict:
    """详情正在计算时先返回空壳。等到算完再交给模型，避免把「还在算」说成没有数据。"""
    pause = float(os.environ.get("STOCK_FACTS_RETRY_PAUSE", "3"))
    deadline = time.monotonic() + float(os.environ.get("STOCK_FACTS_RETRY_DEADLINE", "75"))
    payload = None
    while True:
        remaining = max(5.0, deadline - time.monotonic())
        request = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(request, timeout=remaining) as response:
            payload = json.loads(response.read().decode("utf-8"))
        if not isinstance(payload, dict) or not _waiting(payload) or time.monotonic() >= deadline:
            return payload
        time.sleep(min(pause, max(0.0, deadline - time.monotonic())))


def _waiting(payload: dict) -> bool:
    review = payload.get("review") if isinstance(payload.get("review"), dict) else {}
    facts = review.get("facts") if isinstance(review.get("facts"), list) else []
    if facts:
        return False
    meta = payload.get("meta") if isinstance(payload.get("meta"), dict) else {}
    state = str(payload.get("review_state") or "")
    meta_state = str(meta.get("state") or "")
    return state == "computing" or meta_state == "computing"


def slim(payload: dict, dimension: str) -> dict:
    """Keep the fact sheet. Drop charts and raw series so the model cannot invent from them."""
    review = payload.get("review") if isinstance(payload.get("review"), dict) else {}
    meta = payload.get("meta") if isinstance(payload.get("meta"), dict) else {}
    facts = []
    for row in review.get("facts") or []:
        if isinstance(row, dict) and row.get("id"):
            facts.append({
                "id": row.get("id"),
                "label": row.get("label") or "",
                "group": row.get("group") or "",
                "text": row.get("text") or "",
                "value": row.get("value"),
                "unit": row.get("unit") or "",
            })
    rows = []
    if dimension == "news":
        rows, event_facts = _news_pack(payload)
        seen = {row["id"] for row in facts}
        facts.extend(fact for fact in event_facts if fact["id"] not in seen)
    items = []
    for row in review.get("items") or []:
        if isinstance(row, dict):
            items.append({
                "key": row.get("key"),
                "title": row.get("title") or "",
                "status": row.get("status") or "",
                "answer": row.get("answer") or "",
            })
    series = _flow_daily(payload) if dimension == "volume_flow" else []
    return {
        "ts_code": payload.get("ts_code") or "",
        "stock_name": payload.get("stock_name") or "",
        "dimension": dimension,
        "source": "detail",
        "as_of": review.get("as_of") or meta.get("as_of") or "",
        "facts": facts,
        "rows": rows,
        "series": series,
        "items": items,
        "missing": list(review.get("missing") or []),
        "review_state": payload.get("review_state") or "",
        "review_error": payload.get("review_error") or "",
    }


def _flow_daily(payload: dict) -> list[dict]:
    """Daily close and big-order net flow. Window sums live in facts, not here."""
    prices = {}
    volume = payload.get("price_volume") if isinstance(payload.get("price_volume"), dict) else {}
    bars = ((volume.get("chart") or {}).get("bars") or []) + (volume.get("bars") or [])
    for bar in bars:
        if isinstance(bar, dict) and bar.get("date"):
            prices[bar["date"]] = bar
    block = payload.get("flow_daily") if isinstance(payload.get("flow_daily"), dict) else {}
    days = block.get("history") or block.get("days") or []
    dates = []
    seen = set()
    for day in list(days) + [{"date": day} for day in prices]:
        date = day.get("date") if isinstance(day, dict) else None
        if date and date not in seen:
            seen.add(date)
            dates.append(date)
    dates.sort()
    by_date = {}
    for day in days:
        if isinstance(day, dict) and day.get("date"):
            by_date[day["date"]] = day
    out = []
    for date in dates:
        day = by_date.get(date) or {}
        if day.get("close") is not None:
            close = day.get("close")
            raw = day.get("chg")
            chg_pct = None if raw is None else round(float(raw), 2)
        else:
            price = prices.get(date) or {}
            close = price.get("close")
            raw = price.get("chg")
            chg_pct = None if raw is None else round(float(raw) * 100, 2)
        net = day.get("net") if "net" in day else None
        out.append({
            "date": date,
            "close": close,
            "chg_pct": chg_pct,
            "net_yi": None if net is None else round(float(net) / 1e8, 4),
        })
    return out


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

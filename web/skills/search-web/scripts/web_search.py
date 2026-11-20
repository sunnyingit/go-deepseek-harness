#!/usr/bin/env python3
"""web_search.py —— 问问/分析师的通用网络搜索（必应国内版，无 key）。

用法: python3 web_search.py <query> [count]
输出: JSON {results: [{title, snippet, date, url}], error: ""}
解析失败时 results 为空并带 error，调用方（Go 工具）把错误透传给模型。
"""
import html
import json
import re
import sys
import urllib.parse
import urllib.request

UA = ("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
      "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
TIMEOUT = 6
MAX_COUNT = 8


def strip_tags(s: str) -> str:
    return html.unescape(re.sub(r"<[^>]+>", "", s)).strip()


def fetch(query: str, count: int) -> dict:
    url = "https://cn.bing.com/search?q=" + urllib.parse.quote(query) + f"&count={max(count, 6)}"
    req = urllib.request.Request(url, headers={
        "User-Agent": UA,
        "Accept-Language": "zh-CN,zh;q=0.9",
    })
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        raw = resp.read().decode("utf-8", "ignore")

    results = []
    # b_algo 每条: <h2><a href="url">title</a></h2> ... <p>snippet</p>
    for block in re.findall(r'<li class="b_algo".*?</li>', raw, re.S):
        title_m = re.search(r"<h2[^>]*>\s*<a[^>]+href=\"([^\"]+)\"[^>]*>(.*?)</a>", block, re.S)
        if not title_m:
            continue
        url_out, title = title_m.group(1), strip_tags(title_m.group(2))
        snip_m = re.search(r"<p[^>]*>(.*?)</p>", block, re.S)
        snippet = strip_tags(snip_m.group(1)) if snip_m else ""
        date_m = re.search(r"(\d{4}年\d{1,2}月\d{1,2}日|\d+ 小时之前|\d+ 小时前|昨天|前天)", block)
        date = date_m.group(1) if date_m else ""
        if title:
            results.append({"title": title, "snippet": snippet, "date": date, "url": url_out})
        if len(results) >= count:
            break
    return {"results": results, "error": ""}


def main(argv: list[str]) -> int:
    if len(argv) < 2 or not argv[1].strip():
        print(json.dumps({"results": [], "error": "缺少搜索词"}, ensure_ascii=False))
        return 1
    query = argv[1].strip()[:120]
    try:
        count = min(int(argv[2]) if len(argv) > 2 else 6, MAX_COUNT)
    except ValueError:
        count = 6
    try:
        payload = fetch(query, count)
    except Exception as exc:  # 网络/解析任何异常都软失败，别把 agent 回合炸掉
        payload = {"results": [], "error": f"搜索失败: {exc}"}
    json.dump(payload, sys.stdout, ensure_ascii=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))

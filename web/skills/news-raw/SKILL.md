---
name: news-raw
description: >-
  新闻分析师追问时使用。调用 fetch_news，按用户原话搜索新闻，不限于当前股票。
  不替代开场用的 news-facts。不要调用 Tushare。
---

# 新闻原文

每次追问都用这个技能，并调用 `fetch_news`。开场那句结论仍用 `news-facts`。

调用 `fetch_news`。参数：

- `ts_code`：股票代码，接口需要，但不加入搜索词
- `keyword`：用户要搜的原话，必填。例如「最新的财经资讯」「物理AI」「国家政策」。不要在前面加股票名称

工具请求 `GET /api/stocks/news?q={代码}&keyword={原话}`。keyword 就是搜索词。不要改地址。

返回：

- `query`：实际搜索词，和 keyword 相同。
- `facts[]`：这次搜索到的新闻，`id` 形如 `nw.1`。来源是东方财富搜索，不限于这只股票，标题里没有股票名也要列出。
- `rows[]`：时间、标题、来源、摘要。追问只按这些字段写，不必用表格。
- `matched` 为 0：写「按这个搜索没有搜到新闻」。
- `unavailable` 里有 google：只说明 Google 这次没返回。`facts` 里已经有新闻就按那些写。

回答只依据这次 `fetch_news` 的 `rows`/`facts`。不要把开场近 3 日 detail 里的公告写成这次搜索结果。
无关结果照实列出并写明与本公司无关。传闻不坐实。数字和标题只抄返回 text。重复标题不叠加。
不编涨跌幅，不给买卖建议。禁止开场 JSON。写法见 reply.md；末尾可写「可以接着问」。

---
name: market-reply
description: >-
  大盘分析师追问时使用。按问题调用市场资金、国家队、市场板块、好不好做、日历、宏观分片。
  不替代开场用的 market-facts。不要调用 Tushare。
---

# 大盘追问

开场结论仍用 `market-facts`。用户追问时用 markdown 展开，不要输出开场 JSON；按问题选一个工具：

- 市场资金（成交/宽基/两融）：`fetch_market_fund`
- 国家队：`fetch_market_team`
- 全市场板块轮动：`fetch_market_sector`（不是个股归属）
- 好不好做 / 宽度情绪：`fetch_market_mood`
- 日历：`fetch_market_cal`
- 宏观：`fetch_market_macro`

参数只有 `ts_code`。数字只用返回的 text / facts。对用户可见正文不要写 `mk.` 编号。
末尾可写「可以接着问：」列表。

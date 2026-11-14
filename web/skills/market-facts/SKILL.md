---
name: market-facts
description: >-
  大盘分析师开场必用。调用 fetch_facts 读取市场结论、资金/国家队/情绪信号与市场板块一句话。
  数字只用 facts[].text。不要谈个股资金路径、个股板块归属、单公司财报估值。
---

# 大盘事实

只调用 `fetch_facts`。市场级数据与个股无关；若会话未绑定股票，不要自己编个股代码。维度已绑定为 market，不要改地址，不要调用 Tushare。

返回 JSON：

- `facts[]`：`id` 以 `mk.` 开头。结论看 `mk.conclusion.*`，三信号看 `mk.signal.*`。
- 开场输出 stance/strength/text（≤100 字）/fact_ids/suggested_questions；text 禁用英文双引号 `"`。
- 开场禁止再调 `market-reply` 分片工具。

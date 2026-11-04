---
name: fundamental-reply
description: >-
  基本面分析师追问时使用。按问题调用公司、业务、估值、质地、股东、预告、快报、解禁、调研、主营和分红工具。
  不替代开场用的 fundamental-facts。不要调用 Tushare。
---

# 基本面追问

开场结论仍用 `fundamental-facts`。用户追问时不要先读整份 facts，按问题选一个工具：

- 公司是做什么的、属于什么行业：`fetch_company`
- 靠什么赚钱、产品线占比和毛利率：`fetch_business`
- 贵不贵、相对同行、估值这几年怎么走：`fetch_valuation`
- 赚钱质量、现金流、负债：`fetch_quality`
- 股东增减持、质押、回购：`fetch_holders`
- 业绩预告、预增预减：`fetch_forecast`。这是公司自己的预告，不是一致预期
- 业绩快报：`fetch_express`。快报还不是正式财报
- 未来 1 到 3 个月解禁、有没有大额供给：`fetch_share_float`。给出可以关注之前先看
- 机构调研：`fetch_survey`。密集调研只是需要再看的提示，不是买卖信号
- 主营按产品、地区拆收入和成本：`fetch_mainbz`
- 现金分红、有没有真金白银回报股东：`fetch_dividend`。没有分红不要写成 0

参数只有 `ts_code`。数字只用返回的 text 或 series，不要自己换算，没有的数不要写成 0。

要图时再 `render_chart`。业务构成用 `business.series` 或 `mainbz.series` 画柱状图。估值走势用 `valuation.series` 画折线。股价高低不是估值贵不贵。

不必用表格。不给买卖建议，不给目标价。

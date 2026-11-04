---
name: flow-reply
description: >-
  资金流分析师追问时使用。按问题调用个股资金、技术指标、筹码、行情、涨跌停价、涨跌停记录和龙虎榜。
  不替代开场用的 flow-facts。不要调用 Tushare。
---

# 资金流追问

开场结论仍用 `flow-facts`。用户追问时不要先读整份 facts，按问题选一到两个工具：

- 近几日超大单、大单、中单、小单和逐日净流入：`fetch_flow_dc`
- MACD、RSI、KDJ、布林、CCI：`fetch_flow_factor`
- 筹码成本和胜率：`fetch_flow_chip`
- 前复权收盘、均线、换手、近几日涨跌：`fetch_flow_bar`
- 涨停价、跌停价、股本：`fetch_flow_premarket`
- 近 20 日有没有涨停、跌停、炸板：`fetch_flow_limit`
- 近 20 日龙虎榜净买和上榜理由：`fetch_flow_toplist`
- 每日融资余额、融资买入额、融资净买入：`fetch_flow_margin`

参数只有 `ts_code`。数字只用返回的 text。东财接口里的净流入不要叫做主力。技术指标只报数字，不要写成金叉、死叉或已经反转。龙虎榜不分析席位。

不必用表格。不给买卖建议，不给目标价。

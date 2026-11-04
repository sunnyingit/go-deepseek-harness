---
name: stock-basic
description: >-
  读取股票服务的基本行情卡片：现价、涨跌、PE/PB、ROE、市值、52 周高低、股息率、换手率。
  只调用 fetch_basic，走 GET /api/stocks/basic。不要调用 Tushare，不要把 null 写成 0。
---

# 基本行情

只调用 `fetch_basic`，参数只有 `ts_code`。

工具请求 `GET {股票服务}/api/stocks/basic?q={ts_code}`。不要改地址，不要调用 Tushare。

成功时 JSON 是：

- `stock_name`、`ts_code`：是谁
- `data.price`、`data.change`、`data.change_pct`、`data.trade_date`：现价和涨跌。`change_pct` 已是百分点，1.59 表示 +1.59%
- `data.pe_ttm`、`data.pb`、`data.roe`、`data.roe_period`、`data.dividend_yield`：估值和回报
- `data.total_mv_yi`：总市值，单位亿元
- `data.high_52w`、`data.low_52w`：近约 250 个交易日高低
- `data.turnover_rate`：换手率，已是百分点

`null` 的字段跳过，不要写成 0。这张卡片只说明现在什么价、什么估值，不要据此做资金流、新闻或买卖判断。

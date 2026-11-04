# 基本行情字段

数据来自股票服务 `GET /api/stocks/basic`。不直连 Tushare。

## 成功 JSON

```json
{
  "status": "ok",
  "ts_code": "600519.SH",
  "stock_name": "贵州茅台",
  "data": {}
}
```

## `data` 字段

| 字段 | 类型 | 单位 / 含义 |
|------|------|-------------|
| `trade_date` | string \| null | 行情日期 `YYYYMMDD` |
| `price` | number | 最新收盘价 |
| `change` | number \| null | 涨跌额 |
| `change_pct` | number \| null | 涨跌幅（已是百分点，如 `1.59` = +1.59%） |
| `pe_ttm` | number \| null | 市盈率 TTM |
| `pb` | number \| null | 市净率 |
| `roe` | number \| null | 净资产收益率（百分比口径） |
| `roe_period` | string \| null | ROE 报告期 `YYYYMMDD` |
| `total_mv_yi` | number \| null | 总市值，**亿元**（由 Tushare 万元市值 ÷ 10000） |
| `high_52w` | number \| null | 约 250 个交易日最高价 |
| `low_52w` | number \| null | 约 250 个交易日最低价 |
| `dividend_yield` | number \| null | 股息率（优先 `dv_ttm`，否则 `dv_ratio`） |
| `turnover_rate` | number \| null | 换手率（百分比口径） |

## 失败常见原因

| 情况 | stderr |
|------|--------|
| 没装包 | `缺少依赖，请先安装: pip install tushare pandas` |
| 没 token | `未配置 TUSHARE_TOKEN` |
| 名称对不上 | `未找到股票: …` |
| 港股 | `本脚本暂只支持 A 股` |
| 无日线 | `未获取到 … 的最新价格` |

## 展示注意

- `change_pct` / `roe` / `turnover_rate` / `dividend_yield`：**不要再 ×100**，直接加 `%`。
- `total_mv_yi` 已是亿元；过千亿可说「约 x.xx 万亿元」。
- `null` 省略，不要写成 0。

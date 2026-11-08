# 调用与展示样例

分析师会话里调用工具 `fetch_basic`，参数只有股票代码。脚本本身是：

```bash
STOCK_API_BASE=http://127.0.0.1:8090 python3 scripts/fetch_basic.py 600519.SH
```

不要设置 `TUSHARE_TOKEN`，也不要直连 Tushare。

## 成功输出（结构示意）

```json
{
  "status": "ok",
  "ts_code": "601689.SH",
  "stock_name": "拓普集团",
  "data": {
    "trade_date": "20261031",
    "price": 51.1,
    "change": 0.8,
    "change_pct": 1.59,
    "pe_ttm": 28.5,
    "pb": 4.2,
    "roe": 15.3,
    "roe_period": "20250630",
    "total_mv_yi": 888.03,
    "high_52w": 72.5,
    "low_52w": 41.2,
    "dividend_yield": 0.96,
    "turnover_rate": 2.1
  },
  "source": "tushare"
}
```

## 推荐口述

> 拓普集团（601689.SH），截至 20261031 收盘 51.10，涨 0.80（+1.59%）。  
> PE(TTM) 约 28.5，PB 约 4.2；最新报告期（20250630）ROE 约 15.3%。  
> 总市值约 888 亿元；近一年大约在 41.20～72.50。

## 失败时怎么说

- `未配置 TUSHARE_TOKEN` → 请配置环境变量后再查
- `未找到股票` → 换完整名称或 `600519.SH` 这类代码
- `未获取到 … 最新价格` → Tushare 当日还没有这只票的日线，稍后再试

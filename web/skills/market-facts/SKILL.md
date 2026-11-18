---
name: market-facts
description: >-
  大盘分析师开场必用。调用 fetch_facts 读取市场结论与六维硬事实（趋势、位置/反弹、宽基资金、情绪、宽度、风格）。
  数字只用 facts[].text。不要谈个股资金路径、个股板块归属、单公司财报估值。
---

# 大盘事实

只调用 `fetch_facts`。市场级数据与个股无关；若会话未绑定股票，不要自己编个股代码。维度已绑定为 market，不要改地址，不要调用 Tushare。

返回 JSON：

- `facts[]`：`id` 以 `mk.` 开头。结论看 `mk.conclusion.*`，三信号看 `mk.signal.*`。
- 开场输出 stance/strength/text（≤280 字）/fact_ids/suggested_questions；text 禁用英文双引号 `"`。
- 开场禁止再调 `market-reply` 分片工具。

## 开场结构：固定六维（有数才写，不编造）

`text` 先用一句点明总档（顺风/逆风/中性），再按下面六维压缩；**对应 id 有值时必引，缺维跳过**，不要用空话凑维。

1. **指数趋势** — `mk.index.trend`（优先）或 `mk.index.streak` / `mk.index.tape`  
   短中长、涨跌或震荡、均线结构。60 日线只作背景，不能当唯一主句。
2. **超跌反弹 / 冲高回落** — `mk.index.rebound`  
   近 20 日回撤、破位、连跌/连涨下的反弹性质。
3. **宽基资金** — `mk.money.with_tape`（优先，对齐指数窗口）+ 可辅 `mk.money.summary` / `mk.team.summary`  
   不要只说「资金观望」而不写申赎/份额窗口。
4. **市场情绪** — `mk.mood.stage`（优先）+ 可辅 `mk.mood.summary`  
   阶段、上涨占比、涨跌停/炸板；不能只剩「偏逆/偏顺」。
5. **宽度与内部结构** — `mk.breadth.structure`  
   涨跌家数、全 A 站上均线占比、大小盘相对强弱（文案若写「非等权」须照抄）。
6. **风格与板块轮动** — `mk.style.rotation` 或 `mk.sector.headline`  
   在攻/在出或偏强偏弱方向。

总档怎么做（`mk.conclusion.action`）可一句收尾。数字只抄 `facts[].text`。

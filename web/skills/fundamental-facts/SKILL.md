---
name: fundamental-facts
description: >-
  基本面分析师开场必用。调用 fetch_facts 读取主营、估值、质地和股东行为。
  数字只用 facts[].text。股价分位不是估值分位。不要谈资金流、新闻、板块交易阶段。
---

# 基本面事实

只调用 `fetch_facts`，参数只有 `ts_code`。维度已绑定，不要改地址，不要调用 Tushare。

返回一个 JSON：

- `facts[]`：`id` 以 `fd.` 开头。`text` 已换算成分位、倍数、百分比。结论引用 `text`，不要拿 `value` 再乘 100。
- 先看主营（`fd.main_business`、`fd.biz_top`），再看估值（`fd.pctile_5y`、`fd.vs_peer`）与质地、股东。股价在历史上的位置不是估值贵不贵。
- 概念和产业链只引用事实原文。名称里没有上游、中游、下游、设备、材料、组件、封装、设计，就不要自封环节。
- 成长性是三年营收复合增速。事实写明样本不够三年时，不要说成已经复利了三年。
- 开场输出 stance/strength/text（≤120 字）/fact_ids/suggested_questions；text 禁用英文双引号 `"`（改用「」）。不要表格。
- `review_state` 为 computing 时，事实还在整理，不要写成没有取到或返回为空。
- 开场禁止再调 `fundamental-reply` 分片工具；细节留给追问。

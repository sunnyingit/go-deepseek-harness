---
name: finance-facts
description: >-
  财报分析师开场必用。调用 fetch_facts 读取最新财报 detail。
  数字只用 facts[].text。单季同比不是三年复合增速。不要谈资金流、新闻、估值分位、板块。
---

# 财报事实

只调用 `fetch_facts`，参数只有 `ts_code`。维度已绑定，不要改地址，不要调用 Tushare。

返回一个 JSON：

- `facts[]`：`id` 以 `ff.` 开头，形如 `ff.growth.q_rev_yoy`。`text` 里的金额已是亿元，同比已是百分数。不要拿 `value` 再换算。
- 单季同比不要说成三年复合增速。
- 扭亏为盈不要说成仍在亏损。由盈转亏要说明现在是亏损。
- 披露后的涨跌按事实里的交易日说，不要改成自然日。
- 开场只写 100 字以内的一段结论，不要表格。追问也不必用表格。数字只抄 `text`。
- `review_state` 为 computing 时，事实还在整理，不要写成没有取到或返回为空。

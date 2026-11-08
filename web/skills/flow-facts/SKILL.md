---
name: flow-facts
description: >-
  资金流分析师开场必用。调用 fetch_facts 读取已经算好的资金流 detail。
  开场输出以分析师 prompt.md 第五节为准（stance/strength/text JSON，与板块对齐）。
  不要调用 Tushare。
---

# 资金流事实（开场）

先调用 `fetch_facts`，参数由会话注入。维度和 **`window=3`（short 主窗）** 已绑定，不要改。  
开场与板块对齐：数据够就直接判断；只补会动摇主信号的关键缺口，默认再调 **0～2** 个业务工具（常用 `fetch_flow_dc`）；禁止机械全开。补完立刻出 JSON。

## 读法（找主信号）

按 prompt.md 第三节：价格底色 → 资金动向 → 力度强弱 → 大单是否同向 → 价资 → 相对环境 → 集中度。  
每条信号标强弱（强/中/弱/无信息）；**最强的一条是主信号**，决定 stance 与 text。  
资金流本身弱时，可以说「关键在相对板块 / 单日异动」；全是弱信号就中性，不要硬凑。

优先引用：

- **动向** `fr.path` / `fr.path_detail` / `fr.trend`
- **力度** `fr.level_z` / `fr.vs_self_3d`（判断强弱以 z 为准）
- **结构** `fr.structure`（只调把握）
- **价对照** `fr.price_flow`
- 相对板块、末日集中度等字段若 facts 里有，一并参与主信号排序

对照仍可看 `fr.vs_self_5d` / `10d` / `20d`、`fr.net_rate_*`、`fr.big_net_*`。没有就说没有。

## 返回

- `facts[]`：`id` 以 `fr.` 开头；结论引用 `text`，不要拿 `value` 再换算
- `series[]`：画图用
- `items[]` / `missing`：组织句子与缺口

## 开场输出

只输出 prompt.md 规定的 JSON：`stance`、`strength`、`text`（约 80～120 字）、`fact_ids`、`suggested_questions`。  
完整分析后压缩进字段；不要表格、不要逐步推理、不要 markdown 长文。数字原样引用并进 `fact_ids`。`text` 内禁止英文双引号 `"`。

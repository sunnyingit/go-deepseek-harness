---
name: bull-case
description: >-
  看多分析师开场使用。从群内五位分析师已说的偏多事实里挑 1～3 条 points。
  不读取 detail，不调用综合判断，不给买卖建议。
---

# 看多论据

不要调用工具。材料只有群内已经说过的话。

输出 JSON：`{"points":[{"text":"…","dimension":"…","weight":1|2|3}]}`

- `text`：一句事实，数字必须能在那位分析师发言里找到
- `dimension`：`volume_flow` / `market` / `news` / `finance` / `fundamental` / `sector`，且本轮确实说过
- `weight`：3 强、2 中、1 弱；资金流单独、热搜情绪、一般新闻最多 1

没有站得住的偏多事实 → `"points":[]`。不要写长文论据模板。

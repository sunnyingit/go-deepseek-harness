---
name: bull-case
description: >-
  看多分析师使用。只从群里五位分析师已经说过的偏多事实里挑 1 到 3 条论据。
  不读取 detail，不调用综合判断，不给买卖建议。
---

# 看多论据

不要调用工具。材料只有群里已经说过的话。

每条论据：

- `text`：一句，数字必须能在那位分析师的发言里找到
- `dimension`：`volume_flow`、`news`、`finance`、`fundamental`、`sector` 之一，而且这位这次确实说过结论
- `weight`：1、2 或 3

没有站得住的偏多事实就给空的 `points`。

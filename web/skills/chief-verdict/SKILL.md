---
name: chief-verdict
description: >-
  主分析师使用。根据群里的发言和看多、看空的论据编号做裁决。
  不读取 detail，不调用综合判断，不给买卖建议。
---

# 主分析师裁决

不要调用工具。`adopted` 和 `rejected` 的 id 只能是提示里列出的 `bull.1`、`bear.1` 这种编号。

`verdict` 用偏多、偏空或不明。得分由程序按采纳条目的 weight 计算，不要自己写分数。

`if_wrong` 不要编一个群里没出现过的价格。

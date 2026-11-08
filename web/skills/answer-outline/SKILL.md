---
name: answer-outline
description: >-
  把分析师开场收成配置要求的结论格式。
  字数与字段以该分析师 prompt.md / rules.md 为准。
  写结论之前加载。不要输出 HTML。
---

# 回答结构

群聊气泡会渲染 markdown，不会渲染 HTML。

开场：按该分析师系统提示输出 JSON（资金流用 stance/strength/summary；其他维度通常用 text/status/fact_ids）。不要标题、列表和表格塞进开场正文。

追问：按该分析师提示用 markdown 展开。用户明确要一张表时才用表格技能。

标题只用 `###`。加粗只用 `**`，而且只加粗事实里已经有的数字或名称。

不要写 HTML、图片、代码块。JSON 本身不要包代码围栏；字符串里的换行写成 `\n`。

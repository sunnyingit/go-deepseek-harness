---
name: sector-facts
description: >-
  板块分析师开场必用。调用 fetch_facts 读取所属板块、阶段、成分角色与行情镜头。
  数字只用 facts[].text。关键缺口可再调 sector-reply 工具，默认 0～2 次。
---

# 板块事实

先调用 `fetch_facts`，参数只有 `ts_code`。维度已绑定，不要改地址，不要调用 Tushare。

返回一个 JSON：

- `facts[]`：`id` 以 `sp.` 开头。`text` 已写成百分数或阶段名。结论引用 `text`，不要拿 `value` 再乘 100。
- 是否属于、成分角色、阶段只引用这些事实。申万行业和同花顺概念分开写。没有明确阶段就说没有明确阶段。
- **归属 ≠ 方向**：`sp.p` / `sp.s` 说算谁家的；`sp.tape.lens` / `sp.tape.vs_belong` 说今天谁在解释股价、是否与归属同向。开场 `stance`/`strength` 跟镜头 / 服务端 tape_verdict 走，不另起判决；背离/对立/独立或弱板强股 → stance=中性、strength=弱。
- 阶段只用启动、主升、高潮、分歧、退潮、弱势；不是导入期/成长期，也不是加速/分化。
- 角色是领涨、抗跌、中军、跟风、独立；不是龙头或腰部。市值排名只抄 `text`。
- 开场是操作基准，输出 JSON：`stance`、`strength`、`text`（**不超过 150 字**）、`fact_ids`、`suggested_questions`。先人话结论（方向 + 板块对个股是支撑/拖累/不构成支撑），再一句关键事实。少写「行情镜头、双驱动、解释占比」等行话。不要表格，细节留给追问。不要写 `status`/`status_text`。
- 必答清单过不去时，才按缺口调用追问工具（membership/tape/position 优先），默认合计 0～2 次；非缺口不问不调 rank/sw/dc/heat。补证写入论证，`fact_ids` 仍只引用本次 `sp.*`。
- `review_state` 为 computing 时，事实还在整理，不要写成没有取到或返回为空。

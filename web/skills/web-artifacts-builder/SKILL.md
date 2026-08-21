---
name: web-artifacts-builder
description: 做能点开的精美单页 HTML。介绍页、生平、时间线、关系图默认直接写一个自包含 HTML，禁止为单页跑 Vite/Parcel。只有多组件状态或客户端路由才用 init-artifact.sh。
license: Complete terms in LICENSE.txt
---

# Web Artifacts Builder

对话页会 iframe 会话工作区里的 `.html`。目标是尽快交出一页能点的成品。

## 先选路径（必读）

| 任务 | 做法 |
|---|---|
| 介绍某人/某事、时间线、章节导航、可点击卡片、一页展览 | **路径 A（默认，快）**：直接写一个自包含 HTML。**不要**跑 `init-artifact.sh` / `bundle-artifact.sh`。 |
| 真正需要 React 组件树、客户端路由、大量 shadcn 控件 | **路径 B（慢）**：才初始化 Vite 项目再打包。 |

生平介绍、纪念页、关系图、太阳系点选、毛主席/历史人物页 → **一律路径 A**。初始化 + Parcel 对单页是浪费，而且容易被 favicon / Google Fonts 卡死。

---

## 路径 A — 单文件 HTML（默认）

1. 在会话工作区（bash 默认 cwd）写 `mao.html` 这类短文件名，一次写完。
2. 一个文件里放好 `<style>` 和少量 `<script>`。不要 `src="/src/main.tsx"`，不要外链 CSS/JS/字体。
3. 写完即止。对话里会自动出现可点的预览。用简体中文告诉用户可以点进去看。

**自包含硬性要求：**

Favicon 用这颗红五星 data URI，不要 `/favicon.svg`、不要 `public/`：

```html
<link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Cpolygon fill='%23DE2910' points='16,2 20.12,12.47 31.51,12.47 22.2,18.94 25.64,29.53 16,22.9 6.36,29.53 9.8,18.94 0.49,12.47 11.88,12.47'/%3E%3C/svg%3E" />
```

字体用系统宋体/楷体，禁止 Google Fonts、`fonts.googleapis.com`、`fonts.gstatic.com`、preconnect、`@import` 外链：

```css
font-family: "Songti SC", "STSong", "SimSun", "Kaiti SC", "STKaiti", "KaiTi", Georgia, serif;
```

**骨架（按主题改，不要原样交作业）：**

```html
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>页面标题</title>
  <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Cpolygon fill='%23DE2910' points='16,2 20.12,12.47 31.51,12.47 22.2,18.94 25.64,29.53 16,22.9 6.36,29.53 9.8,18.94 0.49,12.47 11.88,12.47'/%3E%3C/svg%3E" />
  <style>
    :root { --ink: #1b140c; --paper: #f6efe3; --accent: #9b1b1b; }
    html, body { margin: 0; background: var(--paper); color: var(--ink);
      font-family: "Songti SC", "STSong", "SimSun", "Kaiti SC", "KaiTi", Georgia, serif; }
  </style>
</head>
<body>
  <!-- 按主题写内容：侧栏目录 / 时间线 / 点击展开均可 -->
</body>
</html>
```

设计上避免千篇一律的居中紫渐变、一律圆角、Inter。人物页要有该主题自己的材料感（纸、墨、金石、档案，而不是仪表盘）。

不要：
- 为单页执行 `init-artifact.sh`
- 把成品只写在技能目录
- 用截图代替可点页面
- 说「无法在对话框展示」

---

## 路径 B — React + shadcn（仅复杂应用）

只有用户明确要多页路由、复杂表单状态、大量组件时才用。步骤：

1. `bash <skill-base>/scripts/init-artifact.sh <project-name>`（在会话工作区，workdir 留空）
2. 改生成的代码
3. `bash <skill-base>/scripts/bundle-artifact.sh`（或把目标路径当第一个参数）
4. `cp "<project-name>/bundle.html" "<project-name>.html"` 到工作区根目录

打包脚本会处理 favicon data URI、剥掉 Google Fonts。仍不要在打包后再加外链字体或 `/favicon.svg`。

**Stack**：React 18 + TypeScript + Vite + Parcel + Tailwind + shadcn/ui

shadcn 参考：https://ui.shadcn.com/docs/components

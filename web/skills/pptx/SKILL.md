---
name: pptx
description: 用 pptxgenjs 做 16:9 研报/路演。封面深色+数据条，目录浅底+圆形图标，内页浅纸。禁止白底黑字备忘录。
---

# PowerPoint skill

质量对标：深墨绿/琥珀金封面、衬线标题、封面三条数据、浅色目录（大号 01 + 圆标 + 标题/副标题）。不要做成白底黑字备忘录。

`pptxgenjs` / `react-icons` / `sharp` 已在 `NODE_PATH`，不要 `npm install`。

## 必须按这个结构（研报 / 路演）

1. 先查公开数据，禁止编数字。
2. `require('<skill-base>/scripts/deck')`，用 helper，不要从空白页堆文字。
3. **封面深色 + 内页浅色**。不要每一页都刷成深绿。
4. 图标：`await deck.icon('sun', 'FaSun', '#ffffff')` 渲成白 PNG，贴在深绿圆上。禁止 emoji。
5. 图表用 `deck.chart`（PPT 原生图）。
6. `node <skill-base>/scripts/validate.js out.pptx`，失败就改。

## 脚本骨架

```js
const { newDeck } = require('<skill-base>/scripts/deck');

(async () => {
  const deck = newDeck({
    subject: '新能源产业研究',
    title: '新能源产业研究报告',
    footer: '新能源产业研究报告',
    pages: 12,
    palette: 'moss-amber', // 路演用 pitch
  });
  await deck.icon('mark', 'GiSolarPower', '#C9A227');
  await deck.icon('i1', 'FaIndustry', '#ffffff');
  await deck.icon('i2', 'FaLandmark', '#ffffff');
  await deck.icon('i3', 'FaCar', '#ffffff');
  await deck.icon('i4', 'FaProjectDiagram', '#ffffff');
  await deck.icon('i5', 'FaChartLine', '#ffffff');

  deck.cover({
    kicker: '行业研究报告 · 2026 年 8 月',
    title: '新能源产业研究报告',
    subtitle: '政策 · 市场 · 产业链投资机会全景扫描',
    icon: 'mark',
    metrics: [
      { value: '23.37 亿千瓦', label: '可再生能源累计装机' },
      { value: '744.6 万辆', label: '2026H1 新能源车销量' },
      { value: '12.4 亿千瓦', label: '光伏累计装机容量' },
    ],
  });
  deck.toc({
    items: [
      { icon: 'i1', title: '行业全景速览', desc: '可再生能源与核心市场数据' },
      { icon: 'i2', title: '政策驱动', desc: '十五五碳目标解读' },
      { icon: 'i3', title: '三大赛道透视', desc: '新能源车、光伏、风电' },
      { icon: 'i4', title: '储能、电网与产业链', desc: '环节映射与格局' },
      { icon: 'i5', title: '投资主线与风险', desc: '结构性机会与约束' },
    ],
  });
  deck.stats({ title: '核心数据', items: [
    { value: '23.37 亿千瓦', label: '可再生能源累计装机', icon: 'i1' },
  ]});
  deck.chart({ title: '装机趋势', kind: 'column', categories: ['2023','2024','2025'], series: { 装机: [1, 2, 3] } });
  deck.close({ title: '投资建议', subtitle: '一句话结论' });
  await deck.save('brief.pptx');
})();
```

页型：`cover`（深色，可带 `metrics`）`toc`（浅色目录）`section`（深色章节页）`stats` `bullets` `split` `chart` `table` `quote` `close`。

## 视觉

- 一套色：封面 `bg` + 强调 `accent`，内页 `paper`。标题用宋体（helper 已设 Songti SC / 宋体），正文苹方/雅黑。
- 封面：左侧琥珀金竖条、大标题、三条金色数据、右侧主题图标。
- 目录：CONTENTS + 「目录」、大号浅灰序号、深绿圆 + 白图标、标题+一句说明、细线分隔。
- 页脚 `Page n / N`。左右边距 ≥ 0.7"。正文 ≥ 15pt。

## 不要

- 白底黑标题 + 项目符号（备忘录）
- 每一页都深色
- python-pptx / Title 占位符
- 图表截图、`npm install`、`pip install`

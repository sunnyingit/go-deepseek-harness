'use strict';
const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

(async () => {
  const deck = newDeck({ subject: '种子轮融资路演' }); // -> pitch（深墨 + 琥珀金）

  // 图标（react-icons 渲 SVG → sharp 转 PNG）
  await deck.icon('bolt', 'FaBolt');
  await deck.icon('check', 'FaCheck');
  await deck.icon('trend', 'FaChartLine');
  await deck.icon('users', 'FaUsers');
  await deck.icon('shield', 'FaShieldAlt');
  await deck.icon('pie', 'FaChartPie');
  await deck.icon('flag', 'FaFlagCheckered');

  // ── P1 封面 ──────────────────────────────────────────
  deck.cover({
    kicker: 'SEED ROUND · 500 万元',
    title: '一笔 500 万，只投进一件已经验证过的事',
    subtitle: 'XX 科技 · 种子轮融资路演',
    icon: 'bolt',
    source: '本稿数据均为演示占位，正式路演请替换为经审计口径',
  });

  // ── 01 为什么是现在 ─────────────────────────────────
  deck.section({ label: '01', title: '客户已经在为结果付钱，这是最硬的证据' });

  deck.bullets({
    title: '一句话讲清楚我们在做什么',
    kicker: '问题与方案',
    items: [
      '我们把某关键环节的成本降低 40%、效率提升 2.5 倍',
      '客户今天靠人工和旧系统硬扛，付费意愿已经被真实成交确认',
      '不靠烧钱换增长：付费客户中 7 成是稳定复购',
    ],
  });

  deck.stats({
    title: '已经发生的结果，不是预测',
    kicker: '业务现状',
    items: [
      { value: '328 万', label: '年化收入 ARR', icon: 'trend' },
      { value: '41', label: '付费客户', icon: 'users' },
      { value: '68%', label: '毛利率', icon: 'check' },
      { value: '3.2%', label: '月度流失率', icon: 'shield' },
    ],
  });

  deck.split({
    title: '为什么是现在（窗口期）',
    kicker: '时机',
    left: { heading: '旧的世界', items: ['依赖人工，交付慢且易出错', '数据割裂，无法形成复利', '涨价靠堆人头，规模不经济'] },
    right: { heading: '我们的切入', items: ['产品化交付，边际成本趋零', '同一套数据越用越准', '订阅制，收入随客户自然放大'] },
  });

  // ── 02 市场与竞争 ───────────────────────────────────
  deck.section({ label: '02', title: '够大、在增长，且我们卡在清晰的位置' });

  deck.stats({
    title: '目标市场体量',
    kicker: '市场空间',
    items: [
      { value: '86 亿', label: '可服务市场 SAM（元）' },
      { value: '19%', label: '行业年复合增速' },
      { value: '0.4%', label: '当前渗透率' },
    ],
  });

  deck.table({
    title: '为什么客户选我们，而不是别人',
    kicker: '竞争格局',
    headers: ['维度', '旧方案 / 竞品', '我们'],
    rows: [
      ['交付速度', '以周计，需多轮人工', '以小时计，自动完成'],
      ['成本结构', '线性随人力上涨', '产品化，边际递减'],
      ['数据沉淀', '一次性的，不可复用', '越用越准，形成壁垒'],
      ['续费表现', '靠客情维系', '靠结果复购'],
    ],
  });

  // ── 03 钱花在哪、换回什么 ──────────────────────────
  deck.section({ label: '03', title: '这 500 万，每一分都能说出去向、换回结果' });

  deck.chart({
    title: '500 万元资金分配',
    kicker: '资金用途',
    kind: 'pie',
    categories: ['研发', '市场与获客', '销售团队', '运营储备'],
    series: { '金额（万元）': [220, 120, 100, 60] },
  });

  deck.table({
    title: '12 个月里程碑：钱投下去，换回这些',
    kicker: '里程碑',
    headers: ['时间', '关键结果（量化）'],
    rows: [
      ['第 1–3 个月', '产品 2.0 上线，交付时长再降 30%'],
      ['第 4–6 个月', '付费客户 41 → 70，ARR 突破 500 万'],
      ['第 7–9 个月', '切入第二个垂直行业，落地 5 家种子客户'],
      ['第 10–12 个月', 'ARR 冲击 1000 万，启动 A 轮'],
    ],
  });

  // ── 04 团队 + 条款 ─────────────────────────────────
  deck.section({ label: '04', title: '由能把这件事做出来的人掌舵' });

  deck.bullets({
    title: '核心团队',
    kicker: '团队',
    items: [
      '创始人 · 产品与战略：10 年行业经验，上一家做到年收入 4000 万',
      '技术合伙人 · 工程：主导过多套高并发系统从 0 到 1',
      '商务负责人 · 增长：手握行业头部客户资源，带来 6 成现有订单',
    ],
  });

  deck.stats({
    title: '本次融资诉求',
    kicker: '融资条款',
    items: [
      { value: '500 万', label: '融资金额（元）', icon: 'pie' },
      { value: '18 个月', label: '资金跑道', icon: 'flag' },
      { value: '15%', label: '出让股权' },
    ],
  });

  // ── 结尾 ────────────────────────────────────────────
  deck.quote({
    text: '我们不是在要一笔钱试试，而是在邀请你入股一个已被客户验证的生意。',
    attribution: '种子轮路演 · 结语',
  });

  deck.close({
    title: '500 万，换一个 12 个月内可量化的结果',
    subtitle: '约一次 30 分钟的深入沟通 —— 团队与数据都已就绪',
  });

  const out = await deck.save('种子轮融资路演-500万.pptx');
  console.log('saved:', out);
})();

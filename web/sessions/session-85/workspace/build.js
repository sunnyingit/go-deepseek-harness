'use strict';

const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

(async () => {
  const deck = newDeck({
    subject: '项目留存汇报',
    palette: {
      bg: '12151C', surface: '1E222C', ink: 'EDF0F5', muted: '9AA3B2',
      accent: 'E4A11B', onAccent: '12151C',
    },
    title: '为什么这个项目不该砍',
  });

  await deck.icon('shield', 'FaShieldAlt');
  await deck.icon('coins', 'FaCoins');
  await deck.icon('up', 'FaChartLine');
  await deck.icon('users', 'FaUsers');
  await deck.icon('flag', 'FaFlagCheckered');
  await deck.icon('check', 'FaCheck');
  await deck.icon('x', 'FaTimes');

  // 1 封面
  deck.cover({
    kicker: '内部决策汇报',
    title: '为什么这个项目不该砍',
    subtitle: '已投入的沉没成本之外，真正值得守住的价值\n汇报人 · 2026',
    icon: 'shield',
  });

  // 2 section
  deck.section({
    label: '01',
    title: '我们到底在讨论砍掉什么',
  });

  // 3 结果：已经发生的事实
  deck.stats({
    kicker: '已发生的结果',
    title: '不是想法，是已经验证过的结果',
    items: [
      { value: '4 个季度', label: '连续环比正增长', icon: 'up' },
      { value: '3 倍', label: '留存率提升', icon: 'users' },
      { value: '68%', label: '目标客户续约', icon: 'check' },
      { value: '2 条', label: '可复用技术资产', icon: 'coins' },
    ],
  });

  // 4 砍掉的真实代价
  deck.bullets({
    kicker: '真实代价',
    title: '砍掉它，我们真正失去的是',
    items: [
      '已形成的用户信任与惯性，迁移成本由客户与团队共同承担',
      '一套接近可以复用的技术资产，砍掉等于一次性清零',
      '团队在这条线上的认知与磨合，不是换个项目就能平移',
      '一个已经有正增长曲线、只需继续投入就能兑现的期权',
    ],
  });

  // 5 图表：趋势
  deck.chart({
    kicker: '趋势',
    title: '增长曲线站在我们这边（季环比指数）',
    kind: 'line',
    categories: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5', 'Q6'],
    series: { '本项目': [100, 108, 121, 139, 158, 181] },
  });

  // 6 对照
  deck.split({
    kicker: '决策对照',
    title: '砍，还是留',
    left: {
      heading: '砍掉',
      items: [
        '短期省下一笔预算',
        '团队清零，重新寻找方向',
        '用户与客户需要重新安顿',
        '增长曲线被主动打断',
      ],
    },
    right: {
      heading: '留着',
      items: [
        '继续投入，兑现已有增长',
        '技术资产沉淀为可复用能力',
        '客户关系转为续约与口碑',
        '用结果证明这条线值得守',
      ],
    },
  });

  // 7 判断
  deck.quote({
    text: '我们不缺新想法，缺的是把一个已经跑通的事继续做下去的耐心。',
    attribution: '也许，这正是此刻最该留下的理由',
  });

  // 8 收尾
  deck.close({
    title: '建议：继续保留，季度复评',
    subtitle: '给出明确的里程碑与退出条件，而不是现在直接砍掉',
  });

  await deck.save('为什么这个项目不该砍.pptx');
})();

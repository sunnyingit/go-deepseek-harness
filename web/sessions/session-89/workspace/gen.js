'use strict';

const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

(async () => {
  const deck = newDeck({
    subject: '内部路演 · 项目存续评估',
    title: '为什么这个项目不该砍',
    footer: '为什么这个项目不该砍',
    pages: 8,
    palette: 'pitch',
  });

  await deck.icon('mark', 'FaShieldAlt', '#C9A227');
  await deck.icon('i1', 'FaGem', '#ffffff');
  await deck.icon('i2', 'FaChartLine', '#ffffff');
  await deck.icon('i3', 'FaChessKnight', '#ffffff');
  await deck.icon('i4', 'FaLink', '#ffffff');
  await deck.icon('i5', 'FaFlagCheckered', '#ffffff');

  // 01 封面
  deck.cover({
    kicker: '内部决策汇报 · 项目存续评估',
    title: '为什么这个项目不该砍',
    subtitle: '一份关于继续投入、而非提前止损的论证',
    icon: 'mark',
    metrics: [
      { value: '【XX 万元】', label: '累计投入，砍掉即沉没' },
      { value: '【XX%】', label: '用户 90 日留存率' },
      { value: '【XX 个月】', label: '距离盈亏平衡点' },
    ],
    source: '数据来源：【内部经营数据，待填】',
  });

  // 02 目录
  deck.toc({
    items: [
      { icon: 'i1', title: '项目的核心价值', desc: '砍掉它，我们到底失去什么' },
      { icon: 'i2', title: '关键数据一览', desc: '投入、产出与增长曲线' },
      { icon: 'i3', title: '不可替代的战略定位', desc: '竞品与替代方案的缺口' },
      { icon: 'i4', title: '砍掉的真实代价', desc: '沉没、切换与机会成本' },
      { icon: 'i5', title: '结论与建议', desc: '继续投入的条件与边界' },
    ],
  });

  // 03 项目价值一句话
  deck.quote({
    text: '这个项目不是烧钱的试点，而是我们【某战略方向】上唯一跑通闭环、且正在形成数据资产的支点。',
    attribution: '—— 项目价值判断 · 【一句话结论，待填】',
  });

  // 04 关键数据
  deck.stats({
    title: '关键数据：投入 vs 产出',
    kicker: '核心指标',
    items: [
      { value: '【XX 万】', label: '累计投入（研发 + 运营 + 市场）', icon: 'i2' },
      { value: '【XX 万】', label: '当前月度收入 / 可验证 ROI', icon: 'i1' },
      { value: '【XX%】', label: '90 日留存 / 复购率', icon: 'i3' },
      { value: '【XX%】', label: '近三个季度增速', icon: 'i4' },
    ],
  });

  // 05 战略定位 split
  deck.split({
    title: '不可替代的战略定位',
    kicker: '为什么现在不能砍',
    left: {
      heading: '留着的理由',
      items: [
        '占据【某用户群 / 场景】的唯一入口',
        '沉淀【XX】条可复用的数据资产',
        '团队的领域 know-how 无法快速重建',
      ],
    },
    right: {
      heading: '砍掉后的缺口',
      items: [
        '竞品【某】将提前卡位，我们再进成本倍增',
        '替代方案【某】只覆盖 30% 场景，缺口在【X】',
        '已投入的【XX 万】与【XX 个月】全部沉没',
      ],
    },
  });

  // 06 趋势图（占位数据，说明为示意）
  deck.chart({
    title: '关键指标趋势（示意，数据待替换）',
    kicker: '增长曲线',
    kind: 'line',
    categories: ['Q1', 'Q2', 'Q3', 'Q4', 'Q5', 'Q6'],
    series: {
      收入: [3, 5, 8, 12, 18, 26],
      用户数: [10, 16, 24, 35, 48, 60],
    },
  });

  // 07 砍掉的真实代价
  deck.bullets({
    title: '砍掉的真实代价',
    kicker: '成本测算',
    items: [
      '沉没成本：【XX 万】研发与市场投入无法回收，团队认知资产一并流失',
      '切换成本：迁移至替代方案需额外【XX 万】与【XX 周】改造、培训',
      '机会成本：在【赛道/方向】上失去先发优势，追赶需要【XX 倍】投入',
      '组织成本：核心成员流失带来的隐性损失，招聘与磨合周期【XX 个月】',
    ],
  });

  // 08 结论
  deck.close({
    title: '建议：设定边界，继续持有',
    subtitle: '给出一个可验证的目标（【XX 个月】内达到【XX】），到点不达标再议；在此之前，砍掉它才是更贵的选项。',
  });

  const out = await deck.save('why-not-kill-this-project.pptx');
  console.log('SAVED', out);
})();

'use strict';

const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

const PH = '待填'; // 占位符统一标记

(async () => {
  const deck = newDeck({ subject: '种子轮融资路演' }); // -> pitch 深墨 + 琥珀金

  // 图标（react-icons 渲 SVG -> PNG）
  await deck.icon('mark', 'FaRocket');
  await deck.icon('check', 'FaCheck');
  await deck.icon('users', 'FaUsers');
  await deck.icon('coins', 'FaCoins');
  await deck.icon('chart', 'FaChartLine');
  await deck.icon('target', 'FaBullseye');

  // 1. 封面
  deck.cover({
    kicker: 'SEED ROUND · 通用框架',
    title: '向投资人要 500 万，讲清楚一件已经发生的事',
    subtitle: '公司名〔待填〕 · 行业〔待填〕 · 2026\n本稿为框架模板，所有数字均为占位符，替换后即为正式路演',
    icon: 'mark',
    source: '模板说明：▨ 待填 处需替换为真实可验证数据',
  });

  // 2. 一句话定位
  deck.section({ label: '01', title: '我们是谁，一句话' });

  // 3. 问题（split）
  deck.split({
    title: '痛点：今天的问题',
    kicker: 'PROBLEM',
    left: { heading: '用户的现状', items: ['现状一：〔待填〕', '现状二：〔待填〕', '现状三：〔待填〕'] },
    right: { heading: '由此带来的代价', items: ['代价一：〔待填〕', '代价二：〔待填〕', '代价三：〔待填〕'] },
  });

  // 4. 方案
  deck.split({
    title: '方案：我们怎么解决',
    kicker: 'SOLUTION',
    left: { heading: '产品/服务', items: ['做什么：〔待填〕', '怎么交付：〔待填〕', '收费方式：〔待填〕'] },
    right: { heading: '为什么是现在', items: ['时机一：〔待填〕', '时机二：〔待填〕', '壁垒一：〔待填〕'] },
  });

  // 5. 市场（stats 放数字占位）
  deck.stats({
    title: '市场：赛道有多大',
    kicker: 'MARKET',
    items: [
      { value: '〔待填〕', label: '市场规模（TAM）', icon: 'target' },
      { value: '〔待填〕', label: '可服务市场（SAM）', icon: 'target' },
      { value: '〔待填〕', label: '年增速 %', icon: 'chart' },
    ],
  });

  // 6. 已经发生的结果（traction，最重要的不尴尬页）
  deck.stats({
    title: '已验证：不是 PPT，是已经发生的结果',
    kicker: 'TRACTION',
    items: [
      { value: '〔待填〕', label: '付费用户数', icon: 'users' },
      { value: '〔待填〕', label: '月经常性收入 MRR', icon: 'coins' },
      { value: '〔待填〕', label: '留存/复购率', icon: 'check' },
      { value: '〔待填〕', label: '标杆客户数', icon: 'check' },
    ],
  });

  // 7. 趋势（原生图表，占位数据）
  deck.chart({
    title: '增长趋势（占位示例，替换为真实月度数据）',
    kicker: 'GROWTH',
    kind: 'column',
    categories: ['M1', 'M2', 'M3', 'M4', 'M5', 'M6'],
    series: { '营收（待填）': [0, 0, 0, 0, 0, 0] },
  });

  // 8. 单位经济模型（table）
  deck.table({
    title: '单位经济：每一单赚不赚钱',
    kicker: 'UNIT ECONOMICS',
    headers: ['指标', '数值', '备注'],
    rows: [
      ['客单价 / LTV', '〔待填〕', '〔待填〕'],
      ['获客成本 CAC', '〔待填〕', '〔待填〕'],
      ['LTV / CAC', '〔待填〕', '〔待填〕'],
      ['毛利率', '〔待填〕', '〔待填〕'],
    ],
  });

  // 9. 团队
  deck.stats({
    title: '团队：谁来做',
    kicker: 'TEAM',
    items: [
      { value: '〔待填〕', label: '创始人背景', icon: 'users' },
      { value: '〔待填〕', label: '核心团队人数', icon: 'users' },
      { value: '〔待填〕', label: '行业相关年限', icon: 'check' },
    ],
  });

  // 10. 资金用途
  deck.table({
    title: '500 万怎么花：18 个月的资金用途',
    kicker: 'USE OF FUNDS',
    headers: ['用途', '金额（万元）', '占比'],
    rows: [
      ['产品研发', '〔待填〕', '〔待填〕'],
      ['市场与获客', '〔待填〕', '〔待填〕'],
      ['团队扩充', '〔待填〕', '〔待填〕'],
      ['运营与储备', '〔待填〕', '〔待填〕'],
    ],
  });

  // 11. 里程碑
  deck.bullets({
    title: '18 个月里程碑：拿到钱后交付什么',
    kicker: 'MILESTONES',
    items: [
      '第 6 个月：〔待填〕',
      '第 12 个月：〔待填〕',
      '第 18 个月：〔待填〕',
    ],
  });

  // 12. 融资条款
  deck.bullets({
    title: '融资条款与估值口径',
    kicker: 'THE ASK',
    items: [
      '本轮融资金额：500 万元人民币',
      '出让股权 / 投前估值：〔待填〕',
      '资金到账后的现金跑道：〔待填〕个月',
      '本轮投资者将获得的回报路径：〔待填〕',
    ],
  });

  // 13. 结尾
  deck.close({
    title: '我们只做已经验证过的事',
    subtitle: '（公司名〔待填〕）· 期待与您进一步沟通 · 联系邮箱〔待填〕',
  });

  await deck.save('pitch-framework.pptx');
  console.log('saved pitch-framework.pptx');
})();

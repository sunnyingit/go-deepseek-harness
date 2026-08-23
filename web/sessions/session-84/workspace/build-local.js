'use strict';

const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

// 本地生活/消费品牌 —— 演示用 mock 数据
// 一律标注「演示用示例数据」，非真实经营数据
const DEMO = '（演示用示例数据）';

(async () => {
  const deck = newDeck({ subject: '种子轮融资路演' }); // pitch 深墨 + 琥珀金

  await deck.icon('mark', 'FaStore');
  await deck.icon('check', 'FaCheck');
  await deck.icon('users', 'FaUsers');
  await deck.icon('coins', 'FaCoins');
  await deck.icon('chart', 'FaChartLine');
  await deck.icon('target', 'FaBullseye');
  await deck.icon('store', 'FaStore');
  await deck.icon('dish', 'FaUtensils');
  await deck.icon('map', 'FaMapMarkerAlt');

  // 1. 封面
  deck.cover({
    kicker: 'SEED ROUND · 500 万元',
    title: '「早八点」——社区热食早餐，2 分钟带走',
    subtitle: '早八点餐饮管理（演示品牌）· 2026\n12 家直营店 · 月流水 138 万 · 已验证的坪效模型',
    icon: 'mark',
    source: '本稿全部数字为 ' + DEMO + '，非真实经营数据，仅作路演模板示例',
  });

  // 2. 一句话定位
  deck.section({ label: '01', title: '一句话：社区早餐的「即取即走」连锁' });

  // 3. 痛点
  deck.split({
    title: '痛点：早班族的早餐，慢、贵、丑',
    kicker: 'PROBLEM',
    left: { heading: '今天的现状', items: ['早高峰排队平均 8 分钟，来不及点', '外卖早餐均价 18 元，性价比差', '便利店预包装冷食，口味单一'] },
    right: { heading: '由此带来的代价', items: ['上班族长期不吃或乱吃早餐', '商家早高峰产能浪费、午后空转', '优质现制早餐供给严重不足'] },
  });

  // 4. 方案
  deck.split({
    title: '方案：中央厨房 + 15㎡ 社区档口',
    kicker: 'SOLUTION',
    left: { heading: '怎么交付', items: ['半成品冷链配送到店，现蒸现做', '档口 2.4 分钟出餐，扫码即取', '会员订阅制月卡，固定到店'] },
    right: { heading: '为什么是我们', items: ['单店盈利模型已跑通（见第 4 页）', '中央厨房摊薄成本，毛利 41%', '社区点位密度形成品牌壁垒'] },
  });

  // 5. 市场
  deck.stats({
    title: '市场：早餐/热食现制赛道',
    kicker: 'MARKET',
    items: [
      { value: '3000', label: '亿 · 全国早餐市场 TAM', icon: 'target' },
      { value: '800', label: '亿 · 社区热食 SAM', icon: 'target' },
      { value: '8.6%', label: '年复合增速', icon: 'chart' },
    ],
  });

  // 6. 已验证（traction）
  deck.stats({
    title: '已验证：不是想象，是 12 家店的真实结果',
    kicker: 'TRACTION',
    items: [
      { value: '12', label: '直营门店', icon: 'store' },
      { value: '138万', label: '月流水（合计）', icon: 'coins' },
      { value: '68%', label: '30 天复购率', icon: 'check' },
      { value: '41%', label: '综合毛利率', icon: 'check' },
    ],
  });

  // 7. 增长趋势（原生图表）
  deck.chart({
    title: '门店数与月流水增长 ' + DEMO,
    kicker: 'GROWTH',
    kind: 'column',
    categories: ['3月', '4月', '5月', '6月', '7月', '8月'],
    series: { '门店数(家)': [4, 5, 6, 8, 10, 12] },
  });

  // 8. 单位经济
  deck.table({
    title: '单位经济：单店能赚钱 ' + DEMO,
    kicker: 'UNIT ECONOMICS',
    headers: ['指标', '数值', '备注'],
    rows: [
      ['客单价', '12 元', '早餐+饮'],
      ['单店日订单', '320 单', '早高峰集中'],
      ['单店月流水', '11.5 万元', '× 12 家 = 138 万'],
      ['会员 LTV（12月）', '605 元', '月均到店 4.2 次'],
      ['获客 CAC', '82 元', '地推+私域'],
      ['LTV / CAC', '7.4x', '健康'],
    ],
  });

  // 9. 团队
  deck.stats({
    title: '团队：连锁餐饮老兵',
    kicker: 'TEAM',
    items: [
      { value: '12', label: '年 · 创始人连锁餐饮经验', icon: 'users' },
      { value: '8', label: '年 · 供应链负责人经历', icon: 'users' },
      { value: '26', label: '人 · 现有团队', icon: 'users' },
    ],
  });

  // 10. 资金用途
  deck.table({
    title: '500 万怎么花：18 个月 ' + DEMO,
    kicker: 'USE OF FUNDS',
    headers: ['用途', '金额（万元）', '占比'],
    rows: [
      ['中央厨房扩产', '180', '36%'],
      ['新开 12 家店', '220', '44%'],
      ['供应链与数字化', '60', '12%'],
      ['团队与储备', '40', '8%'],
    ],
  });

  // 11. 里程碑
  deck.bullets({
    title: '18 个月里程碑 ' + DEMO,
    kicker: 'MILESTONES',
    items: [
      '第 6 个月：门店 18 家，中央厨房产能翻倍，月流水破 200 万',
      '第 12 个月：门店 24 家，单店模型跑通复制 SOP，月流水破 280 万',
      '第 18 个月：区域密度成型，开放加盟/联营，实现正向现金流',
    ],
  });

  // 12. 融资条款
  deck.bullets({
    title: '融资条款与估值口径 ' + DEMO,
    kicker: 'THE ASK',
    items: [
      '本轮融资：500 万元人民币，出让 10% 股权（投后 5000 万估值，演示口径）',
      '现金跑道：到账后约 18 个月',
      '回报路径：门店密度 → 品牌议价 → 加盟/联营放量 → 并购或上市退出',
      '本轮资金 72% 投向可复制门店与产能，确定性高',
    ],
  });

  // 13. 结尾
  deck.close({
    title: '2 分钟，让早餐不再将就',
    subtitle: '「早八点」餐饮管理（演示品牌）· 联系邮箱 hello@zaobadian-demo.com · 本稿为 ' + DEMO,
  });

  await deck.save('pitch-local-life.pptx');
  console.log('saved pitch-local-life.pptx');
})();

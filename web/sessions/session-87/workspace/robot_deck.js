'use strict';

const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

(async () => {
  // moss-amber 配色，标题宋体 + 深底封面/浅底内页，全部由新版 deck.js 自动处理
  const deck = newDeck({
    subject: '机器人产业研究报告',
    palette: 'moss-amber',
    title: '机器人产业研究报告',
    footer: '机器人产业研究报告',
  });

  // ---- 图标（白色描边，Feather 线性风格，匹配原稿） ----
  await deck.icon('robot', 'FiCommand', 'FFFFFF');
  await deck.icon('factory', 'FiTool', 'FFFFFF');
  await deck.icon('chip', 'FiCpu', 'FFFFFF');
  await deck.icon('humanoid', 'FiUser', 'FFFFFF');
  await deck.icon('gear', 'FiSettings', 'FFFFFF');
  await deck.icon('money', 'FiTrendingUp', 'FFFFFF');
  await deck.icon('truck', 'FiTruck', 'FFFFFF');
  await deck.icon('globe', 'FiGlobe', 'FFFFFF');
  await deck.icon('chart', 'FiActivity', 'FFFFFF');
  await deck.icon('bolt', 'FiZap', 'FFFFFF');
  await deck.icon('shield', 'FiShield', 'FFFFFF');
  await deck.icon('layers', 'FiLayers', 'FFFFFF');
  await deck.icon('battery', 'FiBatteryCharging', 'FFFFFF');

  // ---- 1. 封面（新版 cover 自带底部 metrics 大数字栏） ----
  deck.cover({
    kicker: '行业研究报告 · 2026年8月',
    title: '机器人产业研究报告',
    subtitle: '政策 · 技术 · 产业链投资机会全景扫描',
    icon: 'robot',
    metrics: [
      { value: '40.9万台', label: '2025年工业机器人装机量' },
      { value: '约50%', label: '占全球工业机器人市场份额' },
      { value: '190亿元', label: '2026E人形机器人产值' },
    ],
    source: '数据口径见附录 · 基于公开信息整理',
  });

  // ---- 2. 目录（新版 toc） ----
  deck.toc({
    items: [
      { title: '行业全景速览', desc: '机器人装机与产业核心数据', icon: 'chart' },
      { title: '政策驱动', desc: '人形机器人·具身智能国家未来产业', icon: 'bolt' },
      { title: '三大赛道透视', desc: '工业·人形·服务机器人现状', icon: 'layers' },
      { title: '产业链图谱', desc: '上游零部件到下游应用全景', icon: 'chip' },
      { title: '投资主线与风险', desc: '结构性机会、潜在风险与展望', icon: 'shield' },
    ],
  });

  // ---- 3. 行业全景 ----
  deck.stats({
    kicker: '01 行业全景',
    title: '中国机器人行业核心数据速览',
    items: [
      { value: '40.9万台', label: '2025年工业机器人装机量，全球第一', icon: 'factory' },
      { value: '16.9%', label: '全球工业机器人装机量同比增速', icon: 'chart' },
      { value: '约50%', label: '中国占全球工业机器人市场份额', icon: 'globe' },
      { value: '190亿', label: '2026E人形机器人产业链产值(元)', icon: 'humanoid' },
    ],
  });

  // ---- 4. 政策（图表页） ----
  deck.chart({
    kicker: '02 政策驱动',
    title: '人形机器人被列为国家未来产业，规模快速爬坡',
    kind: 'column',
    categories: ['2023', '2024', '2025', '2026E'],
    series: { '人形机器人市场规模(亿元)': [13, 41, 90, 190] },
  });

  // ---- 5. 政策要点 ----
  deck.bullets({
    kicker: '02 政策驱动',
    title: '政策要点',
    items: [
      '2023年起人形机器人列入未来产业新赛道，北京、上海、深圳等地出台专项政策',
      '2025年全球首场人形机器人半程马拉松、多款量产下线，从实验室走向产业化',
      '减速器、伺服电机、控制器等核心零部件国产化成为扶持重点',
      '具身智能写入多个省市"十五五"规划，被定位为下一代智能终端',
    ],
  });

  // ---- 6. 工业机器人 ----
  deck.split({
    kicker: '03 赛道透视 · 工业机器人',
    title: '工业机器人：从"机器换人"到"灯塔工厂"',
    left: { heading: '装机与市场', items: ['中国连续12年蝉联全球最大市场', '汽车、3C、锂电、光伏为四大应用', '协作与焊接机器人增速领先'] },
    right: { heading: '结构变化', items: ['本土品牌份额持续提升', '减速器国产化约40%、伺服约50%', '海外龙头加速在华本土化'] },
  });

  // ---- 7. 人形机器人 ----
  deck.stats({
    kicker: '03 赛道透视 · 人形机器人',
    title: '人形机器人：2026 进入量产元年',
    items: [
      { value: '190亿', label: '2026E市场规模（亿元）', icon: 'money' },
      { value: '10万台', label: '特斯拉Optimus 2027年量产目标', icon: 'humanoid' },
      { value: '52个', label: '自由度（关节数量多、复杂度高）', icon: 'gear' },
      { value: '70%+', label: '核心零部件成本占比', icon: 'chip' },
    ],
  });

  // ---- 8. 服务机器人 ----
  deck.bullets({
    kicker: '03 赛道透视 · 服务机器人',
    title: '服务机器人：商用与特种场景快速放量',
    items: [
      '配送、清洁、导览、巡检等商用机器人渗透率提升，头部企业加速出海',
      '特种机器人（应急、电力巡检、农业植保）政策采购需求稳定',
      '家用清洁机器人进入智能升级与价格下沉阶段',
      'AI大模型接入使服务机器人从"功能机"走向"智能体"',
    ],
  });

  // ---- 9. 产业链 ----
  deck.split({
    kicker: '04 产业链全景',
    title: '机器人全产业链图谱',
    left: { heading: '上游 · 核心零部件', items: ['减速器 / 伺服电机', '控制器 / 传感器', '芯片 / AI大模型', '精密结构件 / 本体材料'] },
    right: { heading: '中游 · 本体与集成', items: ['工业机器人本体', '人形机器人本体', '服务机器人本体', '系统集成与解决方案'] },
  });
  deck.bullets({
    kicker: '04 产业链全景',
    title: '下游 · 应用场景',
    items: [
      '汽车、3C电子、锂电、光伏等制造业自动化产线',
      '物流仓储、商超配送、医疗康复、家庭服务',
      '电力巡检、应急救援、国防安防等特种场景',
    ],
  });

  // ---- 10. 结构性主线 ----
  deck.stats({
    kicker: '05 结构性主线',
    title: '四条值得关注的结构性主线',
    items: [
      { value: '核心零部件', label: '减速器国产替代空间大，龙头议价能力强', icon: 'gear' },
      { value: 'AI+具身智能', label: '大模型赋予自主决策能力，成下一代入口', icon: 'chip' },
      { value: '人形机器人', label: '量产放量在即，特斯拉/Figure/优必选卡位', icon: 'humanoid' },
      { value: '出海', label: '工业与服务机器人加速布局东南亚、欧美', icon: 'truck' },
    ],
  });

  // ---- 11. 风险提示 ----
  deck.bullets({
    kicker: '05 风险提示',
    title: '需要关注的潜在风险',
    items: [
      '技术路线不确定：人形商业节奏、成本下降可能不及预期',
      '核心零部件依赖：高端减速器、高精传感器、AI芯片部分进口',
      '竞争加剧：同质化与价格战压缩中游本体利润',
      '出口合规：关税、数据安全与本地化要求趋严',
    ],
  });

  // ---- 12. 展望 ----
  deck.close({
    title: '展望：从自动化到具身智能',
    subtitle: '数据来源：国家统计局、国际机器人联合会(IFR)、行业协会及公开报道；本报告基于公开信息整理，仅供参考，不构成投资建议。',
  });

  await deck.save('机器人产业研究报告.pptx');
})();

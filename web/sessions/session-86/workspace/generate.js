const { newDeck } = require('/Users/lisun/workplace/go-harness/web/skills/pptx/scripts/deck');

(async () => {
  const deck = newDeck({ subject: '新能源汽车' });

  // 图标
  await deck.icon('leaf', 'FaLeaf');
  await deck.icon('bolt', 'FaBolt');
  await deck.icon('plug', 'FaPlug');
  await deck.icon('shield', 'FaShieldAlt');
  await deck.icon('wifi', 'FaWifi');
  await deck.icon('car', 'FaCar');
  await deck.icon('coins', 'FaCoins');
  await deck.icon('check', 'FaCheck');

  // 1. 封面
  deck.cover({
    kicker: 'GREEN MOBILITY',
    title: '新能源汽车\n未来出行的绿色引擎',
    subtitle: '纯电 · 插电混动 · 氢燃料电池｜从代步工具到智能终端',
    icon: 'leaf',
    source: '新能源汽车专题 · 未来出行',
  });

  // 2. 章节：什么是新能源汽车
  deck.section({ label: '01', title: '什么是新能源汽车' });

  // 3. 三大技术路线（stats）
  deck.stats({
    title: '核心定义与三大技术路线',
    kicker: 'DEFINITION',
    items: [
      { value: '纯电动', label: 'BEV\n仅以动力电池供电，使用阶段几乎零排放', icon: 'bolt' },
      { value: '插电混动', label: 'PHEV\n电机 + 燃油双动力，可外接充电', icon: 'plug' },
      { value: '氢燃料电池', label: 'FCEV\n氢气反应发电，排放仅水', icon: 'leaf' },
    ],
  });

  // 4. 章节：意义
  deck.section({ label: '02', title: '为什么意义重大' });

  // 5. 意义 split 两栏
  deck.split({
    title: '两大核心价值',
    kicker: 'WHY IT MATTERS',
    left: {
      heading: '环保价值',
      items: [
        '使用阶段几乎零排放',
        '显著改善城市空气质量',
        '助力「碳达峰、碳中和」目标',
      ],
    },
    right: {
      heading: '能源安全',
      items: [
        '减少对石油的依赖',
        '优化能源结构',
        '保障国家能源安全',
      ],
    },
  });

  // 6. 章节：产业进展
  deck.section({ label: '03', title: '产业蓬勃发展' });

  // 7. 产业进展 stats
  deck.stats({
    title: '看得见的进步',
    kicker: 'PROGRESS',
    items: [
      { value: '200→800', label: '续航里程（公里）', icon: 'car' },
      { value: '快充', label: '充电能力大幅增强\n缓解里程焦虑', icon: 'bolt' },
      { value: '智能网联', label: '自动驾驶 · 语音交互\n车成为移动智能终端', icon: 'wifi' },
    ],
  });

  // 8. 章节：政策与市场
  deck.section({ label: '04', title: '政策助推 · 市场普及' });

  // 9. 政策支持 bullets
  deck.bullets({
    title: '政策组合拳',
    kicker: 'POLICY',
    items: [
      '购置补贴：直接降低购车成本',
      '免征购置税：持续让利消费者',
      '路权优惠：不限行、绿牌通行等便利',
      '充电设施补贴：加速补能网络布局',
    ],
  });

  // 10. 市场反应 stats
  deck.stats({
    title: '市场回应',
    kicker: 'MARKET',
    items: [
      { value: '稳步上升', label: '市场占有率', icon: 'check' },
      { value: '不断提高', label: '消费者接受度', icon: 'coins' },
      { value: '加速普及', label: '驶入寻常百姓家', icon: 'car' },
    ],
  });

  // 11. 挑战与展望 split
  deck.split({
    title: '挑战与展望',
    kicker: 'OUTLOOK',
    left: {
      heading: '仍需破局',
      items: [
        '充电设施分布不均',
        '电池回收体系尚不完善',
        '补能体验仍需优化',
      ],
    },
    right: {
      heading: '未来可期',
      items: [
        '技术持续进步迭代',
        '补能与回收体系完善',
        '终将走向主流、描绘清洁智能出行',
      ],
    },
  });

  // 12. 结尾
  deck.close({
    title: '清洁 · 智能 · 可持续',
    subtitle: '新能源汽车，正加速驶向未来',
  });

  await deck.save('新能源汽车.pptx');
  console.log('DONE');
})();

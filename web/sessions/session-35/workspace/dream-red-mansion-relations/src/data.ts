// 人物数据：x,y 为画布坐标（SVG viewBox 0 0 1200 760）
export interface Character {
  id: string
  name: string
  title: string
  color: string // 家族分组颜色
  x: number
  y: number
  desc: string
  group: string
}

// 关系：from - to 之间的一条线，label 为关系描述
export interface Relation {
  from: string
  to: string
  label: string
}

export const groups = [
  { key: 'zheng', name: '荣国府', color: '#b23a2f' },
  { key: 'ning', name: '宁国府', color: '#3a5a78' },
  { key: 'real', name: '四大家族', color: '#3a7a5a' },
  { key: 'other', name: '姻亲·外戚', color: '#8a6d3b' },
  { key: 'servant', name: '内眷·侍从', color: '#7a4a78' },
]

export const characters: Character[] = [
  // —— 荣国府 ——
  { id: 'jb', name: '贾母', title: '荣国府太夫人', color: '#b23a2f', x: 420, y: 470, group: 'zheng', desc: '荣国公儿媳，史太君。贾赦、贾政之母，凤姐/宝玉之祖母，全书至高辈分长者，众人心中慈爱而威严的老祖宗。' },
  { id: 'js', name: '贾赦', title: '荣国府·长子', color: '#b23a2f', x: 260, y: 560, group: 'zheng', desc: '贾母长子，袭一等将军，为人荒淫贪色，骄纵无度，与贾政一脉分房而居。' },
  { id: 'jz', name: '贾政', title: '荣国府·次子', color: '#b23a2f', x: 560, y: 560, group: 'zheng', desc: '贾母次子，工部员外郎。为人端方持重、崇尚纲常，对宝玉管教极严。' },
  { id: 'wf', name: '王夫人', title: '贾政之妻', color: '#b23a2f', x: 560, y: 430, group: 'zheng', desc: '贾政正妻，宝玉之母。出身金陵王家，笃信礼法，少言语而好斋僧，对宝玉溺爱却严于待其身边人。' },
  { id: 'fr', name: '凤姐', title: '王熙凤', color: '#b23a2f', x: 300, y: 340, group: 'zheng', desc: '贾琏之妻，贾赦、邢夫人儿媳。荣府实际掌权人，机灵泼辣、工于心计，人称"凤辣子"。' },
  { id: 'jl', name: '贾琏', title: '荣国府·贾赦次子', color: '#b23a2f', x: 170, y: 430, group: 'zheng', desc: '贾赦之子，凤姐之夫。帮理荣府家务，生性风流，暗中纳尤二姐为妾。' },
  { id: 'xy', name: '邢夫人', title: '贾赦之妻', color: '#b23a2f', x: 330, y: 650, group: 'zheng', desc: '贾赦续弦之妻，凤姐之姑母。禀性愚弱，一味讨好贾赦。' },
  { id: 'by', name: '宝玉', title: '荣国府二公子', color: '#b23a2f', x: 460, y: 220, group: 'zheng', desc: '贾政、王夫人之子，生于含玉，乃神瑛侍者转世。厌恶仕途八股，只看重女儿情分，深得贾母溺爱。' },
  { id: 'lxy', name: '林黛玉', title: '姑苏林氏', color: '#7a4a78', x: 240, y: 180, group: 'servant', desc: '林如海之女，贾母外孙女，宝玉之表妹。性孤高，才思敏捷，多愁善感，与宝玉有木石前盟，葬花吟咏传世。' },
  { id: 'xc', name: '薛宝钗', title: '薛家·金陵', color: '#8a6d3b', x: 660, y: 180, group: 'other', desc: '薛姨妈之女，宝玉之表姐，品格端方、豁达守分，博学多识，后嫁宝玉，有金玉良缘之说。' },
  { id: 'ty', name: '探春', title: '贾政庶女', color: '#b23a2f', x: 660, y: 340, group: 'zheng', desc: '贾政庶出之女，性格才智不输男儿，曾在凤姐病时协理荣府，人称"蕉下客"。' },

  // —— 宁国府 ——
  { id: 'zgg', name: '贾珍', title: '宁国公之后', color: '#3a5a78', x: 930, y: 360, group: 'ning', desc: '宁国公曾孙，袭三等威烈将军，性豪爽而放浪，与贾蓉为父子。' },
  { id: 'jr', name: '贾蓉', title: '宁国府·贾珍之子', color: '#3a5a78', x: 1030, y: 470, group: 'ning', desc: '贾珍之子，秦可卿之夫。游手好闲，后纳尤氏姐妹。' },
  { id: 'qkh', name: '秦可卿', title: '宁国府·贾蓉之妻', color: '#3a5a78', x: 1120, y: 560, group: 'ning', desc: '贾蓉之妻，宁国府长房长孙媳妇，温柔和平，早夭，死因之迷为全书一大公案。' },

  // —— 四大家族 ——
  { id: 'x', name: '薛宝琴', title: '薛家人', color: '#3a7a5a', x: 830, y: 120, group: 'real', desc: '薛宝钗堂妹，才貌双全，许配梅翰林之子。' },
  { id: 'st', name: '史湘云', title: '史家·史太君侄孙女', color: '#3a7a5a', x: 90, y: 200, group: 'real', desc: '贾母娘家的侄孙女，生性豪爽磊落，心直口快，醉卧芍药裀一段尤为动人。' },
  { id: 'wz', name: '王子腾', title: '金陵王家', color: '#3a7a5a', x: 90, y: 110, group: 'real', desc: '王夫人之兄，历任京营节度使、九省都统制，王家势大的靠山。' },

  // —— 姻亲·外戚 ——
  { id: 'lym', name: '薛姨妈', title: '王夫人之妹', color: '#8a6d3b', x: 780, y: 90, group: 'other', desc: '王夫人之妹，薛家女眷，宝钗、薛蟠之母，宽厚温和。' },
  { id: 'xp', name: '薛蟠', title: '薛姨妈之子', color: '#8a6d3b', x: 930, y: 90, group: 'other', desc: '薛姨妈之子，宝钗之兄。不学无术、横行霸道，人称"呆霸王"。' },

  // —— 内眷·侍从 ——
  { id: 'xr', name: '袭人', title: '宝玉身边丫头', color: '#7a4a78', x: 380, y: 90, group: 'servant', desc: '贾母之婢，后与宝玉。温柔和顺、恪尽职守，深得王夫人信任。' },
  { id: 'qwen', name: '晴雯', title: '宝玉身边丫头', color: '#7a4a78', x: 550, y: 90, group: 'servant', desc: '贾府家生丫头，模样标致、心气高傲，撕扇作千金一笑。' },
]

export const relations: Relation[] = [
  // 贾母 - 子辈
  { from: 'jb', to: 'js', label: '长子' },
  { from: 'jb', to: 'jz', label: '次子' },
  { from: 'jb', to: 'lxy', label: '外孙女' },
  { from: 'jb', to: 'by', label: '孙儿·最宠' },
  // 贾赦一房
  { from: 'js', to: 'jl', label: '子' },
  { from: 'js', to: 'xy', label: '夫妻' },
  { from: 'jl', to: 'fr', label: '夫妻' },
  { from: 'xy', to: 'fr', label: '婆媳' },
  // 贾政一房
  { from: 'jz', to: 'wf', label: '夫妻' },
  { from: 'jz', to: 'by', label: '子' },
  { from: 'jz', to: 'ty', label: '女' },
  { from: 'wf', to: 'by', label: '子' },
  { from: 'wf', to: 'ty', label: '母女' },
  // 宝玉
  { from: 'by', to: 'lxy', label: '表兄妹·知己' },
  { from: 'by', to: 'xc', label: '表姐弟·金玉' },
  { from: 'by', to: 'xr', label: '主仆' },
  { from: 'by', to: 'qwen', label: '主仆' },
  // 王夫人娘家 / 王家
  { from: 'wf', to: 'wz', label: '兄妹' },
  { from: 'wf', to: 'lym', label: '姐妹' },
  { from: 'lym', to: 'xc', label: '母女' },
  { from: 'lym', to: 'xp', label: '母子' },
  { from: 'xc', to: 'x', label: '堂姐妹' },
  // 宁国府
  { from: 'zgg', to: 'jr', label: '父子' },
  { from: 'jr', to: 'qkh', label: '夫妻' },
  { from: 'jb', to: 'zgg', label: '同宗·侄孙' },
  // 史家
  { from: 'jb', to: 'st', label: '侄孙女' },
]

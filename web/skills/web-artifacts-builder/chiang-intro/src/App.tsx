import './index.css'

/* ── 蒋介石特辑 ─────────────────────────────── */

const basicInfo = [
  { label: '姓名', value: '蒋介石（幼名瑞元，谱名周泰，学名志清，后改名中正，字介石）', en: 'Chiang Kai-shek' },
  { label: '字 / 号', value: '名中正，字介石，浙江奉化溪口人', en: 'Name & aliases' },
  { label: '生卒', value: '1887年10月31日 — 1975年4月5日', en: '1887–1975' },
  { label: '籍贯', value: '浙江省宁波府奉化县溪口镇（今属宁波市）', en: 'Fenghua, Zhejiang' },
  { label: '尊称', value: '总统、委员长、校长（黄埔）——两岸评价迥异', en: 'Historic titles' },
  { label: '学历', value: '先后入保定陆军速成学堂、日本振武学校，接受近代军事教育', en: 'Military education' },
  { label: '主要身份', value: '军政领袖、军事统帅，曾任军事委员会委员长、国民政府主席、中华民国总统', en: 'Statesman & general' },
  { label: '关键历程', value: '同盟会(1908) → 黄埔军校(1924) → 北伐(1926) → 新生活运动(1934) → 西安事变(1936) → 第二次世界大战同盟国中国战区最高统帅(1942)', en: 'Key chapters' },
]

const timeline = [
  { year: '1887', event: '生于浙江奉化溪口镇盐商家庭，家中排行第三' },
  { year: '1906', event: '留学日本，结识孙中山等革命党人，次年加入同盟会' },
  { year: '1911', event: '辛亥革命爆发后回国，参与光复沪杭、讨伐袁世凯等军事行动' },
  { year: '1924', event: '受孙中山信任出任黄埔军校校长，以黄埔系为根基迅速崛起' },
  { year: '1926', event: '就任国民革命军总司令，发动北伐，至1928年实现形式上的全国统一' },
  { year: '1927', event: '发动“四一二”政变，与中国共产党决裂，此后历经长期内战' },
  { year: '1931–1936', event: '对日本侵略一度采取“攘外必先安内”策略，疲于应付内战；1936年西安事变后被逼停止剿共、一致抗日' },
  { year: '1937–1945', event: '全面抗战爆发，任中国战区最高统帅，领导全民族坚持八年抗战直至胜利' },
  { year: '1946–1949', event: '内战全面爆发，国民政府军事失利；1949年败退台湾并宣布引退，次年复行视事' },
  { year: '1950–1975', event: '在台湾推行土地改革与经济重建，长期实行戒严与威权治理，坚持“反攻大陆”口号直至1975年病逝' },
]

const chapters = [
  {
    name: '军事起家',
    pinyin: 'Military rise',
    desc: '早年留学日本，历经保定军校与日本振武学校训练，辛亥革命中崭露头角。凭借黄埔军校校长身份累积军事与政治资本，成为孙中山晚年倚重的实力派人物。',
  },
  {
    name: '统一与统治',
    pinyin: 'Reunification & rule',
    desc: '1926年起领导北伐，1928年基本结束军阀割据，形式统一全国。其后推行“训政”，实行国民政府主导的高度集权体制，也对共产党人进行残酷的“清党”与围剿。',
  },
  {
    name: '抗战统帅',
    pinyin: 'Wartime leadership',
    desc: '抗日战争期间被推举为中国战区最高统帅，组织全民族抵抗日本侵略，领导中国最终跻身同盟国与联合国创始国行列，是其政治军事生涯中的巅峰与正面成就。',
  },
  {
    name: '退居台湾',
    pinyin: 'Retreat to Taiwan',
    desc: '内战失败后退守台湾，实施土地改革、外汇管制与经济恢复，建立相对稳定的“戒严”体制，长期制造两岸军事对峙，秉持“一个中国”立场，反对“台独”。',
  },
]

const governance = [
  { concept: '黄埔建军', meaning: '以黄埔学生为骨干的“党军”与嫡系体系', role: '军事根基' },
  { concept: '训政建国', meaning: '以党治国、军政训政宪政三阶段导引', role: '政体设计' },
  { concept: '新生活运动', meaning: '主张“礼义廉耻”，倡导传统文化与道德教化', role: '社会改造' },
  { concept: '土地改革', meaning: '迁台后推行“三七五减租”、公地放领等政策', role: '台湾新政' },
  { concept: '儒家长官', meaning: '尊崇传统儒家纲常，强调忠孝、修身与服从', role: '思想底色' },
]

const quotes = [
  { text: '和平、奋斗、救中国', source: '孙中山遗言中蒋介石常引以自励，视作继承革命之志' },
  { text: '抗战到底', source: '抗日战争时期方针，主张以空间换时间、积小胜为大胜' },
  { text: '礼义廉耻，国之四维', source: '新生活运动的道德纲领，源自《管子》' },
  { text: '反攻大陆', source: '退台后长期坚持的宣传口号与国策取向（后于晚年软化收敛）' },
]

/* 篆刻风格印章组件 */
function Seal({ char }: { char: string }) {
  return (
    <div className="grid h-12 w-12 shrink-0 place-items-center border border-[#1c3a5e] bg-[#1c3a5e] font-serif text-lg text-[#efe9d8]">
      {char}
    </div>
  )
}

function SectionTitle({ no, title, sub }: { no: string; title: string; sub: string }) {
  return (
    <div className="mb-6 flex items-baseline gap-4 border-b-2 border-[#1c3a5e] pb-3 sm:mb-8 sm:pb-4">
      <span className="font-serif text-3xl font-bold leading-none text-[#1c3a5e] sm:text-4xl">{no}</span>
      <div>
        <h2 className="font-serif text-2xl font-bold text-neutral-900 sm:text-3xl">{title}</h2>
        <p className="mt-0.5 text-[11px] tracking-[0.25em] text-neutral-400 sm:text-xs">{sub}</p>
      </div>
    </div>
  )
}

function App() {
  return (
    <main className="min-h-screen bg-[#f2efe6] text-neutral-800">
      {/* 顶栏 */}
      <header className="border-b border-[#1c3a5e]/20 bg-[#ece4d2] print:hidden">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-2 px-6 py-3">
          <p className="font-serif text-sm font-bold tracking-[0.3em] text-[#1c3a5e]">蒋介石先生特辑</p>
          <p className="text-xs tracking-wider text-neutral-500">民国领袖 · 军政权柄 · 1887–1975</p>
        </div>
      </header>

      {/* 封面首屏 */}
      <section className="border-b border-neutral-300 bg-[#f2efe6]">
        <div className="mx-auto grid max-w-5xl gap-8 px-6 py-10 sm:py-16 md:grid-cols-[auto_1fr] md:gap-12">
          <div className="flex flex-col items-center gap-4 md:items-start">
            <div className="flex h-64 w-48 flex-col items-center justify-center gap-3 border border-neutral-300 bg-[#ded4ba] p-3 shadow-md md:h-72 md:w-56">
              <div className="flex gap-3">
                <Seal char="蒋" />
                <Seal char="中" />
              </div>
              <p className="text-center font-serif text-lg font-bold tracking-[0.5em] text-[#1c3a5e]">介石</p>
              <p className="text-center text-[11px] tracking-widest text-neutral-500">中正 · 以一人系一时之局</p>
            </div>
            <p className="text-xs text-neutral-400">篆刻图腾 · 庄重肃穆</p>
          </div>

          <div className="self-end">
            <p className="mb-3 inline-block border-l-4 border-[#1c3a5e] pl-3 font-serif text-sm tracking-[0.3em] text-[#1c3a5e]">
              近现代中国极具争议的军政人物
            </p>
            <h1 className="font-serif text-5xl font-black leading-tight text-neutral-900 sm:text-6xl md:text-7xl">
              蒋介石
            </h1>
            <p className="mt-1 font-serif text-xl tracking-[0.3em] text-neutral-500">蒋中正 · 字介石 · 溪口人</p>
            <p className="mt-6 max-w-xl leading-relaxed text-neutral-700">
              他执掌民国权柄数十年，领导北伐完成形式上的国家统一，又在抗日战争中
              出任中国战区最高统帅，为民族存亡率众浴血八载；内战失败后退据台湾，
              一生功过交织，是两岸评价截然不同、却都无法回避的历史坐标。
            </p>
            <div className="mt-6 flex flex-wrap gap-3 font-serif text-sm">
              {['黄埔军校校长', '国军最高统帅', '抗战中国战区统帅', '“中华民国”总统'].map((t) => (
                <span key={t} className="border border-[#1c3a5e]/40 bg-white/60 px-3 py-1 text-neutral-700">
                  {t}
                </span>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* 壹 · 基本信息表 */}
      <section className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
        <SectionTitle no="壹" title="基本信息总览" sub="ONE · VITAL RECORDS" />
        <div className="grid overflow-hidden border border-neutral-300 bg-white shadow-sm md:grid-cols-2">
          {basicInfo.map((row, i) => (
            <div
              key={row.label}
              className={`grid grid-cols-[96px_1fr] gap-4 border-b border-neutral-200 px-5 py-4 sm:grid-cols-[124px_1fr] sm:px-6 ${
                i % 2 ? 'md:border-l' : ''
              } ${i >= basicInfo.length - 2 ? 'md:border-b-0' : ''}`}
            >
              <div className="border-r border-neutral-200 pr-4">
                <p className="font-serif font-bold text-neutral-900">{row.label}</p>
                <p className="mt-0.5 text-[11px] text-neutral-400">{row.en}</p>
              </div>
              <p className="text-sm leading-relaxed text-neutral-700">{row.value}</p>
            </div>
          ))}
        </div>
      </section>

      {/* 贰 · 生平年表 */}
      <section className="border-y border-neutral-300 bg-[#e9e0ca]">
        <div className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
          <SectionTitle no="贰" title="历史行程 · 生平年表" sub="TWO · A CHANGING CHINA" />
          <div className="overflow-hidden border border-neutral-300 bg-white shadow-sm">
            <table className="w-full border-collapse text-left">
              <thead>
                <tr className="bg-[#1c3a5e] text-[#efe9d8]">
                  <th className="w-44 px-5 py-3 text-left font-serif text-base font-bold tracking-widest">年份</th>
                  <th className="px-5 py-3 text-left font-serif text-base font-bold tracking-widest">大事</th>
                </tr>
              </thead>
              <tbody>
                {timeline.map((row, i) => (
                  <tr
                    key={row.year}
                    className={`${i % 2 ? 'bg-[#f9f6ec]' : 'bg-white'} border-b border-neutral-100 last:border-0`}
                  >
                    <td className="px-5 py-3">
                      <span className="inline-block bg-[#1c3a5e]/10 px-2 py-0.5 font-serif text-sm font-bold text-[#1c3a5e]">
                        {row.year}
                      </span>
                    </td>
                    <td className="px-5 py-3 text-sm leading-relaxed text-neutral-700">{row.event}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* 叁 · 权力版图 */}
      <section className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
        <SectionTitle no="叁" title="一生之变 · 四重角色" sub="THREE · FOUR FACETS OF A LIFE" />
        <div className="grid gap-5 md:grid-cols-2">
          {chapters.map((c, i) => (
            <div key={c.name} className="border border-neutral-300 bg-white p-6 shadow-sm">
              <p className="font-serif text-4xl leading-none text-[#1c3a5e]/15">{['軍', '統', '戰', '台'][i]}</p>
              <h3 className="mt-1 font-serif text-2xl font-bold text-neutral-900">{c.name}</h3>
              <p className="mt-0.5 text-xs tracking-widest text-neutral-400" style={{ fontStyle: 'italic' }}>
                {c.pinyin}
              </p>
              <p className="mt-4 text-sm leading-relaxed text-neutral-700">{c.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* 肆 · 治国主张 */}
      <section className="border-y border-neutral-300 bg-[#e9e0ca]">
        <div className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
          <SectionTitle no="肆" title="统治之道 · 政策主张" sub="FOUR · GOVERNANCE & POLICIES" />
          <div className="overflow-hidden border border-neutral-300 bg-white shadow-sm">
            <table className="w-full border-collapse text-left">
              <thead>
                <tr className="bg-[#1c3a5e] text-[#efe9d8]">
                  <th className="w-40 px-5 py-3 text-left font-serif text-base font-bold">主张</th>
                  <th className="px-5 py-3 text-left font-serif text-base font-bold">内涵</th>
                  <th className="hidden w-40 px-5 py-3 text-left font-serif text-base font-bold sm:table-cell">定位</th>
                </tr>
              </thead>
              <tbody>
                {governance.map((row, i) => (
                  <tr key={row.concept} className={`${i % 2 ? 'bg-[#f9f6ec]' : 'bg-white'} border-b border-neutral-100 last:border-0`}>
                    <td className="px-5 py-3 font-serif font-bold text-neutral-900">{row.concept}</td>
                    <td className="px-5 py-3 text-sm leading-relaxed text-neutral-700">{row.meaning}</td>
                    <td className="hidden px-5 py-3 sm:table-cell">
                      <span className="bg-[#1c3a5e]/10 px-2 py-0.5 text-xs font-medium text-[#1c3a5e]">{row.role}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* 伍 · 名言与评价 */}
      <section className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
        <SectionTitle no="伍" title="历史回响 · 名言与评说" sub="FIVE · WORDS & VERDICTS OF HISTORY" />
        <div className="grid gap-4 md:grid-cols-2">
          {quotes.map((q) => (
            <blockquote key={q.text} className="border-l-4 border-[#1c3a5e] bg-white px-6 py-5 shadow-sm">
              <p className="font-serif text-lg font-bold text-neutral-900">“{q.text}”</p>
              <footer className="mt-2 text-xs text-neutral-400">—— {q.source}</footer>
            </blockquote>
          ))}
        </div>

        <div className="mt-12 flex items-center gap-5 border border-[#1c3a5e]/30 bg-white px-6 py-6 shadow-sm">
          <Seal char="蒋" />
          <p className="text-sm leading-relaxed text-neutral-700">
            蒋介石是中国近代史上最复杂的人物之一：他领导北伐统一、坚持全民族抗战，
            为民族独立与强国国运作出过重要贡献；他对中国革命人民的残酷围剿、其统治时期
            的专制腐败，以及退台后的长期戒严，又留下了沉重而充满争议的篇章。
            功与过、得与失，皆须放在二十世纪中国的动荡洪流中加以审视，让历史作出公允的评断。
          </p>
        </div>
      </section>

      {/* 页脚 */}
      <footer className="border-t border-neutral-300 bg-[#ece4d2]">
        <div className="mx-auto flex max-w-5xl flex-col items-center gap-2 px-6 py-8 text-center">
          <p className="font-serif text-lg tracking-[0.3em] text-[#1c3a5e]">蒋中正 · 介石</p>
          <p className="text-xs tracking-widest text-neutral-500">蒋介石先生纪念特辑 · 理性审视 · 存史立鉴</p>
        </div>
      </footer>
    </main>
  )
}

export default App

import './index.css'

/* ── 孙中山先生特辑 ─────────────────────────────── */

const basicInfo = [
  { label: '姓名', value: '孙中山（原名孙文，又作孙逸仙）', en: 'Sun Yat-sen' },
  { label: '字 / 号', value: '字载之，号日新（后改逸仙），化名中山樵', en: 'Cuiheng, Guangdong' },
  { label: '生卒', value: '1866年11月12日 — 1925年3月12日', en: '1866–1925' },
  { label: '籍贯', value: '广东省广州府香山县（今中山市）翠亨村', en: 'Guangdong, China' },
  { label: '尊称', value: '国父、中华民国国父、中国革命先行者', en: 'Father of the Republic' },
  { label: '学历', value: '香港西医书院（今香港大学前身）首届毕业生，获医科资格', en: 'College of Medicine, Hong Kong' },
  { label: '主要身份', value: '革命家、政治家、医师、共和制度开创者', en: 'Revolutionary & statesman' },
  { label: '创党历程', value: '兴中会(1894) → 同盟会(1905) → 国民党(1912) → 中华革命党(1914) → 中国国民党(1919)', en: 'Founded many parties' },
]

const timeline = [
  { year: '1866', event: '生于广东香山县翠亨村农家' },
  { year: '1892', event: '毕业于香港西医书院，成为首届毕业生，后赴澳门行医' },
  { year: '1894', event: '上书李鸿章未见采纳；赴檀香山创立兴中会，提出“驱除鞑虏，恢复中华”' },
  { year: '1895', event: '发动第一次广州起义（未遂）后流亡海外' },
  { year: '1905', event: '在日本东京创立中国同盟会，提出“三民主义”纲领' },
  { year: '1911', event: '武昌起义爆发，辛亥革命成功，清朝覆亡' },
  { year: '1912', event: '就任中华民国临时大总统；同年辞职，让位于袁世凯' },
  { year: '1917–1923', event: '先后在广州发起护法运动，三次组建非常政府，任非常大总统' },
  { year: '1924', event: '与中国共产党合作，创办黄埔军校，重新解释三民主义（联俄、联共、扶助农工）' },
  { year: '1925', event: '3月12日病逝于北京，遗言：“革命尚未成功，同志仍须努力。”' },
]

const principles = [
  {
    name: '民族主义',
    pinyin: 'Mínzú zhǔyì',
    desc: '推翻满清专制统治，建立民族独立的国家；后期主张国内各民族一律平等，联合平等待我之民族，共同反抗帝国主义压迫。',
  },
  {
    name: '民权主义',
    pinyin: 'Mínquán zhǔyì',
    desc: '推翻封建专制，建立民主共和政体；主张主权在民、五权宪法——在立法、行政、司法之外增立考试、监察二权，分权制衡。',
  },
  {
    name: '民生主义',
    pinyin: 'Mínshēng zhǔyì',
    desc: '关注人民生活，主张“平均地权”与“节制资本”，最终实现国民生计的均足与富庶，是其思想体系的特色与归宿。',
  },
]

const conceptRows = [
  { concept: '三民主义', meaning: '民族、民权、民生', role: '核心纲领' },
  { concept: '五权宪法', meaning: '立法、行政、司法、考试、监察', role: '政体设计' },
  { concept: '建国方略', meaning: '心理 · 物质 · 社会三个层面', role: '治国方略' },
  { concept: '权能区分', meaning: '政权（人民）与治权（政府）分开', role: '民主原理' },
  { concept: '军政·训政·宪政', meaning: '革命建国的三个阶段', role: '革命程序' },
]

const quotes = [
  { text: '天下为公', source: '《礼记·礼运》，孙中山一生屡书以自勉并赠予同志' },
  { text: '革命尚未成功，同志仍须努力', source: '1925年临终遗嘱' },
  { text: '世界潮流，浩浩荡荡，顺之者昌，逆之者亡', source: '题赠旅居海外的革命志士' },
  { text: '博爱', source: '常题字以明志，视为终生信条' },
]

function Seal({ char }: { char: string }) {
  return (
    <div className="grid h-12 w-12 shrink-0 place-items-center border border-[#9e1f2e] bg-[#9e1f2e] font-serif text-lg text-[#f4e9c8]">
      {char}
    </div>
  )
}

function SectionTitle({ no, title, sub }: { no: string; title: string; sub: string }) {
  return (
    <div className="mb-6 flex items-baseline gap-4 border-b-2 border-[#9e1f2e] pb-3 sm:mb-8 sm:pb-4">
      <span className="font-serif text-3xl font-bold leading-none text-[#9e1f2e] sm:text-4xl">{no}</span>
      <div>
        <h2 className="font-serif text-2xl font-bold text-neutral-900 sm:text-3xl">{title}</h2>
        <p className="mt-0.5 text-[11px] tracking-[0.25em] text-neutral-400 sm:text-xs">{sub}</p>
      </div>
    </div>
  )
}

function App() {
  return (
    <main className="min-h-screen bg-[#f6f1e6] text-neutral-800">
      {/* 顶栏 */}
      <header className="border-b border-[#9e1f2e]/20 bg-[#f0e6cd] print:hidden">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-2 px-6 py-3">
          <p className="font-serif text-sm font-bold tracking-[0.3em] text-[#9e1f2e]">孙中山先生特辑</p>
          <p className="text-xs tracking-wider text-neutral-500">国父 · 中华民国奠基人 · 1866–1925</p>
        </div>
      </header>

      {/* 封面首屏 */}
      <section className="border-b border-neutral-300 bg-[#f6f1e6]">
        <div className="mx-auto grid max-w-5xl gap-8 px-6 py-10 sm:py-16 md:grid-cols-[auto_1fr] md:gap-12">
          <div className="flex flex-col items-center gap-4 md:items-start">
            <div className="flex h-64 w-48 flex-col items-center justify-center gap-3 border border-neutral-300 bg-[#e9d9b8] p-3 shadow-md md:h-72 md:w-56">
              <div className="flex gap-3">
                <Seal char="孙" />
                <Seal char="文" />
              </div>
              <p className="text-center font-serif text-lg font-bold tracking-[0.5em] text-[#9e1f2e]">天下为公</p>
              <p className="text-center text-[11px] tracking-widest text-neutral-500">国父遗训 · 未竟之志</p>
            </div>
            <p className="text-xs text-neutral-400">篆刻图腾 · 庄重肃穆</p>
          </div>

          <div className="self-end">
            <p className="mb-3 inline-block border-l-4 border-[#9e1f2e] pl-3 font-serif text-sm tracking-[0.3em] text-[#9e1f2e]">
              中国近代民主革命先行者
            </p>
            <h1 className="font-serif text-5xl font-black leading-tight text-neutral-900 sm:text-6xl md:text-7xl">
              孙中山
            </h1>
            <p className="mt-1 font-serif text-xl tracking-[0.3em] text-neutral-500">孙文 · 字载之 · 号日新</p>
            <p className="mt-6 max-w-xl leading-relaxed text-neutral-700">
              他领导辛亥革命，推翻延续两千余年的君主专制，创建亚洲第一个共和国，
              提出影响深远的三民主义与五权宪法。一生奔走革命四十余年，
              被两岸人民尊为“国父”，其“天下为公”的理想至今激荡人心。
            </p>
            <div className="mt-6 flex flex-wrap gap-3 font-serif text-sm">
              {['辛亥革命领导者', '中华民国首任临时大总统', '三民主义创始人', '亚洲民主共和先行者'].map((t) => (
                <span key={t} className="border border-[#9e1f2e]/40 bg-white/60 px-3 py-1 text-neutral-700">
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
      <section className="border-y border-neutral-300 bg-[#efe6d0]">
        <div className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
          <SectionTitle no="贰" title="革命足迹 · 生平年表" sub="TWO · THE REVOLUTIONARY LIFE" />
          <div className="overflow-hidden border border-neutral-300 bg-white shadow-sm">
            <table className="w-full border-collapse text-left">
              <thead>
                <tr className="bg-[#9e1f2e] text-[#f4e9c8]">
                  <th className="w-44 px-5 py-3 text-left font-serif text-base font-bold tracking-widest">年份</th>
                  <th className="px-5 py-3 text-left font-serif text-base font-bold tracking-widest">大事</th>
                </tr>
              </thead>
              <tbody>
                {timeline.map((row, i) => (
                  <tr
                    key={row.year}
                    className={`${i % 2 ? 'bg-[#faf6ec]' : 'bg-white'} border-b border-neutral-100 last:border-0`}
                  >
                    <td className="px-5 py-3">
                      <span className="inline-block bg-[#9e1f2e]/10 px-2 py-0.5 font-serif text-sm font-bold text-[#9e1f2e]">
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

      {/* 叁 · 三民主义 */}
      <section className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
        <SectionTitle no="叁" title="三民主义" sub="THREE · THE THREE PRINCIPLES OF THE PEOPLE" />
        <div className="grid gap-5 md:grid-cols-3">
          {principles.map((p, i) => (
            <div key={p.name} className="border border-neutral-300 bg-white p-6 shadow-sm">
              <p className="font-serif text-4xl leading-none text-[#9e1f2e]/15">{['民', '权', '生'][i]}</p>
              <h3 className="mt-1 font-serif text-2xl font-bold text-neutral-900">{p.name}</h3>
              <p className="mt-0.5 text-xs tracking-widest text-neutral-400" style={{ fontStyle: 'italic' }}>
                {p.pinyin}
              </p>
              <p className="mt-4 text-sm leading-relaxed text-neutral-700">{p.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* 肆 · 治国主张 */}
      <section className="border-y border-neutral-300 bg-[#efe6d0]">
        <div className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
          <SectionTitle no="肆" title="治国思想 · 制度主张" sub="FOUR · GOVERNANCE & INSTITUTIONS" />
          <div className="overflow-hidden border border-neutral-300 bg-white shadow-sm">
            <table className="w-full border-collapse text-left">
              <thead>
                <tr className="bg-[#9e1f2e] text-[#f4e9c8]">
                  <th className="w-44 px-5 py-3 text-left font-serif text-base font-bold">主张</th>
                  <th className="px-5 py-3 text-left font-serif text-base font-bold">内涵</th>
                  <th className="hidden w-40 px-5 py-3 text-left font-serif text-base font-bold sm:table-cell">定位</th>
                </tr>
              </thead>
              <tbody>
                {conceptRows.map((row, i) => (
                  <tr key={row.concept} className={`${i % 2 ? 'bg-[#faf6ec]' : 'bg-white'} border-b border-neutral-100 last:border-0`}>
                    <td className="px-5 py-3 font-serif font-bold text-neutral-900">{row.concept}</td>
                    <td className="px-5 py-3 text-sm leading-relaxed text-neutral-700">{row.meaning}</td>
                    <td className="hidden px-5 py-3 sm:table-cell">
                      <span className="bg-[#9e1f2e]/10 px-2 py-0.5 text-xs font-medium text-[#9e1f2e]">{row.role}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* 伍 · 名言 */}
      <section className="mx-auto max-w-5xl px-6 py-12 sm:py-16">
        <SectionTitle no="伍" title="传世名言" sub="FIVE · WORDS THAT ENDURE" />
        <div className="grid gap-4 md:grid-cols-2">
          {quotes.map((q) => (
            <blockquote key={q.text} className="border-l-4 border-[#9e1f2e] bg-white px-6 py-5 shadow-sm">
              <p className="font-serif text-lg font-bold text-neutral-900">“{q.text}”</p>
              <footer className="mt-2 text-xs text-neutral-400">—— {q.source}</footer>
            </blockquote>
          ))}
        </div>

        <div className="mt-12 flex items-center gap-5 border border-[#9e1f2e]/30 bg-white px-6 py-6 shadow-sm">
          <Seal char="孙" />
          <p className="text-sm leading-relaxed text-neutral-700">
            孙中山一生以“医国”为己任，以“天下为公”为志。他虽未及亲眼看见理想共和国实现，
            却为中国从帝制走向共和奠定了不可磨灭的基石。时至今日，其精神与思想仍是中国现代化
            进程中绕不过去的历史篇章。
          </p>
        </div>
      </section>

      {/* 页脚 */}
      <footer className="border-t border-neutral-300 bg-[#f0e6cd]">
        <div className="mx-auto flex max-w-5xl flex-col items-center gap-2 px-6 py-8 text-center">
          <p className="font-serif text-lg tracking-[0.3em] text-[#9e1f2e]">天下为公</p>
          <p className="text-xs tracking-widest text-neutral-500">孙中山先生纪念特辑 · 谨以此致敬伟大的革命先行者</p>
        </div>
      </footer>
    </main>
  )
}

export default App

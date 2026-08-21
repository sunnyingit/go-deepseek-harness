import { useState } from 'react'

interface TimelineItem {
  year: string
  title: string
  desc: string
}

const timeline: TimelineItem[] = [
  {
    year: '1911',
    title: '生于上海',
    desc: '祖籍浙江杭州，出生于上海。书香门第，父亲钱均夫致力于教育事业。',
  },
  {
    year: '1934',
    title: '毕业于交通大学',
    desc: '考取清华大学庚款留美公费生，赴美深造航空工程。',
  },
  {
    year: '1935',
    title: '赴美留学',
    desc: '入麻省理工学院航空系，次年转入加州理工学院，师从冯·卡门。',
  },
  {
    year: '1939',
    title: '获航空与数学博士',
    desc: '完成《高速气体动力学》研究，成为世界航空动力学领域青年权威。',
  },
  {
    year: '1955',
    title: '历尽艰难回国',
    desc: '冲破美国重重阻挠，历经五年软禁，携妻儿乘轮船回到祖国。',
  },
  {
    year: '1956',
    title: '组建国防科技体系',
    desc: '任中国科学院力学研究所所长，主持组建国防部第五研究院。',
  },
  {
    year: '1966',
    title: '两弹一星功勋',
    desc: '参与领导中国原子弹、导弹、人造卫星研制，"两弹一星"开启新纪元。',
  },
  {
    year: '1991',
    title: '国家杰出贡献科学家',
    desc: '获授"国家杰出贡献科学家"荣誉称号和一级英雄模范奖章。',
  },
  {
    year: '2009',
    title: '逝世',
    desc: '于北京逝世，享年98岁。被誉为"中国航天之父""中国导弹之父"。',
  },
]

const achievements = [
  {
    icon: '🚀',
    title: '中国航天之父',
    desc: '中国航天事业奠基人，开创了中国自主研制导弹、卫星与载人航天的先河。',
  },
  {
    icon: '🧭',
    title: '中国导弹之父',
    desc: '领导研制东风系列导弹，奠定中国战略威慑力量与国防现代化的基石。',
  },
  {
    icon: '🌌',
    title: '系统科学泰斗',
    desc: '创立"工程控制论"，提出复杂系统、总体设计部思想，影响深远。',
  },
  {
    icon: '🌳',
    title: '沙产业倡议者',
    desc: '晚年提出沙产业理论，倡导在西部干旱地区发展知识密集型的绿色产业。',
  },
]

const quotes = [
  {
    text: '我的事业在中国，我的成就在中国，我的归宿在中国。',
    source: '——钱学森，1955年回国途中',
  },
  {
    text: '外国人能搞的，难道中国人不能搞？中国人比他们矮一截？',
    source: '——钱学森，谈及研制导弹时',
  },
  {
    text: '科学不能迷信权威，要敢于向权威挑战。',
    source: '——钱学森',
  },
  {
    text: '我姓钱，但我不爱钱。我的一切都属于人民。',
    source: '——钱学森',
  },
]

function App() {
  const [activeQuote, setActiveQuote] = useState(0)

  return (
    <div className="min-h-screen bg-[#f7f5f0] text-[#1a2a3a]">
      {/* ============ Hero ============ */}
      <header className="relative overflow-hidden bg-[#10243c] text-[#f5efe0]">
        <div className="absolute inset-0 bg-gradient-to-br from-[#0b1b2d] via-[#12324e] to-[#0b1b2d]" />
        {/* 星点装饰 */}
        <div
          className="absolute inset-0 opacity-30"
          style={{
            backgroundImage:
              'radial-gradient(white 1px, transparent 1px)',
            backgroundSize: '40px 40px',
          }}
        />
        <div
          className="absolute -right-24 -top-24 h-[420px] w-[420px] rounded-full bg-[#c9a227]/10 blur-2xl"
        />
        <div className="relative mx-auto max-w-6xl px-6 py-24 md:py-32">
          <div className="inline-block rounded border border-[#c9a227]/40 bg-[#c9a227]/10 px-3 py-1 text-sm tracking-[0.3em] text-[#e8d18a]">
            1911 — 2009
          </div>
          <h1 className="serif-display mt-6 text-5xl font-normal leading-tight md:text-7xl">
            钱学森
          </h1>
          <p className="mt-3 text-xl tracking-[0.2em] text-[#e8d18a] md:text-2xl">
            中国航天之父 · 中国导弹之父
          </p>
          <p className="mt-8 max-w-2xl text-lg leading-relaxed text-[#cfe0f0] md:text-xl">
            他用一生践行科技报国的信念，把毕生所学奉献给祖国的国防与航天事业，
            铸就了中华民族自强不息的丰碑。
          </p>
          <div className="mt-10 flex flex-wrap gap-4">
            <a
              href="#life"
              className="rounded border border-[#c9a227] bg-[#c9a227] px-6 py-3 font-medium text-[#10243c] transition hover:bg-[#ddb63e]"
            >
              生平纪事
            </a>
            <a
              href="#achievements"
              className="rounded border border-[#5b7a99] px-6 py-3 text-[#eef4fa] transition hover:border-[#c9a227] hover:text-[#e8d18a]"
            >
              毕生贡献
            </a>
          </div>
        </div>
      </header>

      {/* ============ 引言 ============ */}
      <section className="mx-auto max-w-4xl px-6 py-20">
        <div className="border-l-4 border-[#c9a227] pl-6">
          <p className="serif-display text-2xl leading-relaxed text-[#2a3a4a] md:text-3xl">
            "我立志报国，我的愿望就是把自己所有的知识、才能和智慧，
            全部奉献给祖国和人民。"
          </p>
          <p className="mt-4 text-[#7a6a4a]">—— 钱学森</p>
        </div>
      </section>

      {/* ============ 生平纪事 timeline ============ */}
      <section id="life" className="bg-[#fffdf8] py-20">
        <div className="mx-auto max-w-5xl px-6">
          <h2 className="serif-display text-center text-3xl font-normal tracking-widest md:text-4xl">
            生平纪事
          </h2>
          <div className="mx-auto mt-4 h-px w-16 bg-[#c9a227]" />

          <div className="mt-16 space-y-0">
            {timeline.map((item, i) => (
              <div key={item.year} className="relative flex">
                {/* 左侧时间 */}
                <div className="w-24 shrink-0 pt-6 text-right text-sm font-semibold tracking-wider text-[#a8832c] md:w-32">
                  {item.year}
                </div>
                {/* 竖线与节点 */}
                <div className="mx-6 flex flex-col items-center">
                  <div
                    className={`mt-7 h-3 w-3 rounded-full border-2 ${
                      i === timeline.length - 1
                        ? 'border-[#c9a227] bg-[#c9a227]'
                        : 'border-[#a8832c] bg-[#f7f5f0]'
                    }`}
                  />
                  {i !== timeline.length - 1 && (
                    <div className="w-px flex-1 bg-[#e0d4b8]" />
                  )}
                </div>
                {/* 内容 */}
                <div className="flex-1 pb-12 pt-3">
                  <h3 className="serif-display text-xl text-[#1a2a3a]">
                    {item.title}
                  </h3>
                  <p className="mt-2 leading-relaxed text-[#5a6a7a]">
                    {item.desc}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============ 毕生贡献 ============ */}
      <section id="achievements" className="bg-[#10243c] py-20 text-[#f5efe0]">
        <div className="mx-auto max-w-5xl px-6">
          <h2 className="serif-display text-center text-3xl font-normal tracking-widest md:text-4xl">
            毕生贡献
          </h2>
          <div className="mx-auto mt-4 h-px w-16 bg-[#c9a227]" />

          <div className="mt-16 grid gap-6 md:grid-cols-2">
            {achievements.map((a) => (
              <div
                key={a.title}
                className="rounded border border-[#2d4a66] bg-[#16324b] p-7 transition hover:border-[#c9a227]/50"
              >
                <div className="text-3xl">{a.icon}</div>
                <h3 className="serif-display mt-4 text-xl">{a.title}</h3>
                <p className="mt-2 leading-relaxed text-[#b8cfe0]">{a.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============ 语录 ============ */}
      <section className="py-20">
        <div className="mx-auto max-w-4xl px-6">
          <h2 className="serif-display text-center text-3xl font-normal tracking-widest md:text-4xl">
            钱学森语录
          </h2>
          <div className="mx-auto mt-4 h-px w-16 bg-[#c9a227]" />

          <div className="mt-12 flex min-h-[200px] flex-col items-center justify-center rounded border border-[#e0d4b8] bg-[#fffdf8] p-8 text-center">
            <div className="mb-4 text-4xl text-[#c9a227]">"</div>
            <p className="serif-display text-2xl leading-relaxed text-[#1a2a3a]">
              {quotes[activeQuote].text}
            </p>
            <p className="mt-4 text-[#8a7a5a]">{quotes[activeQuote].source}</p>
            <div className="mt-8 flex gap-2">
              {quotes.map((_, i) => (
                <button
                  key={i}
                  aria-label={`显示第${i + 1}条语录`}
                  onClick={() => setActiveQuote(i)}
                  className={`h-2 w-2 rounded-full transition ${
                    i === activeQuote ? 'bg-[#c9a227]' : 'bg-[#d8cba8]'
                  }`}
                />
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ============ 致敬 ============ */}
      <section className="bg-gradient-to-br from-[#0b1b2d] to-[#12324e] py-20 text-[#f5efe0]">
        <div className="mx-auto max-w-3xl px-6 text-center">
          <div className="mx-auto mb-8 flex h-16 w-16 items-center justify-center rounded-full border-2 border-[#c9a227] text-3xl">
            ☀
          </div>
          <h2 className="serif-display text-3xl leading-snug md:text-4xl">
            中国航天事业的星辰大海
            <br />
            因他而点亮
          </h2>
          <p className="mt-6 text-lg leading-relaxed text-[#cfe0f0]">
            半个多世纪以来，钱学森先生的名字与中国航天事业的每一次跨越紧紧相连。
            从"两弹一星"到载人航天，再到探月工程，一代代航天人传承着他的精神，
            在浩瀚宇宙中镌刻下中国人自己的印记。
          </p>
          <div className="mt-12 text-sm tracking-[0.3em] text-[#e8d18a]">
            向钱学森先生致敬
          </div>
        </div>
      </section>

      {/* ============ Footer ============ */}
      <footer className="bg-[#0b1b2d] py-8 text-center text-sm text-[#6b8299]">
        <div className="mx-auto max-w-5xl px-6">
          <p className="serif-display text-base text-[#b8cfe0]">
            钱学森（1911—2009）
          </p>
          <p className="mt-1">
            中国航天之父 · 中国导弹之父 · 两弹一星元勋
          </p>
          <p className="mt-4 text-[#4a6278]">
            纪念页面 · 功勋永存 · 精神长在
          </p>
        </div>
      </footer>
    </div>
  )
}

export default App

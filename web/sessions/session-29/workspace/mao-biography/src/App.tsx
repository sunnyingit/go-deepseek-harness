import { useState } from 'react'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { Separator } from './components/ui/separator'
import { Tabs, TabsList, TabsTrigger, TabsContent } from './components/ui/tabs'

/* ---------- 数据 ---------- */

type Stage = {
  period: string
  year: string
  title: string
  desc: string
  icon: string
}

const stages: Stage[] = [
  {
    period: '少年求学',
    year: '1893—1918',
    title: '胸怀天下，立志救国',
    desc: '1893年12月26日出生于湖南湘潭韶山冲。少年时期在家乡读书，后入湘乡东山高等小学堂、湖南省立第一师范求学。青年时代萌发救国救民的远大志向，写下“孩儿立志出乡关，学不成名誓不还”的名言。'
  },
  {
    period: '建党投身革命',
    year: '1918—1927',
    title: '组织发动，探寻真理',
    desc: '五四运动前后接触并接受马克思主义，1921年出席中共一大，成为党的创始人之一。此后在广州、武汉开展农民运动和工人运动，发表《湖南农民运动考察报告》，提出“星星之火，可以燎原”的著名论断。'
  },
  {
    period: '开辟革命道路',
    year: '1927—1937',
    title: '工农武装，创建根据地',
    desc: '大革命失败后，发动秋收起义，创立井冈山革命根据地。在遵义会议上其在党和红军的领导地位得到确立。率中央红军完成二万五千里长征，为革命保存了宝贵力量。'
  },
  {
    period: '抗日战争时期',
    year: '1937—1945',
    title: '持久抗战，民族栋梁',
    desc: '领导敌后抗日斗争，发表《论持久战》等光辉著作，系统阐述了抗战的战略方针。团结各方面抗日力量，为中国人民抗日战争和世界反法西斯战争胜利作出重大贡献。'
  },
  {
    period: '解放战争时期',
    year: '1945—1949',
    title: '运筹帷幄，开启新篇',
    desc: '领导人民解放军进行战略决战，指挥辽沈、淮海、平津三大战役取得决定性胜利。1949年10月1日在北京天安门城楼庄严宣告：“中华人民共和国中央人民政府今天成立了！”'
  },
  {
    period: '社会主义建设探索',
    year: '1949—1976',
    title: '奠基立业，独立自主',
    desc: '领导全国人民进行社会主义革命和建设，建立起独立完整的工业体系和国民经济体系，维护国家主权和民族独立，为当代中国一切发展进步奠定了根本政治前提和制度基础。'
  }
]

type Quote = { text: string; src: string }

const quotes: Quote[] = [
  { text: '为有牺牲多壮志，敢教日月换新天。', src: '《七律·到韶山》' },
  { text: '雄关漫道真如铁，而今迈步从头越。', src: '《忆秦娥·娄山关》' },
  { text: '数风流人物，还看今朝。', src: '《沁园春·雪》' },
  { text: '星星之火，可以燎原。', src: '1930年给林彪的信' },
  { text: '己亥杂诗？不，是“一万年太久，只争朝夕”。', src: '《满江红·和郭沫若同志》' },
  { text: '世上无难事，只要肯登攀。', src: '《水调歌头·重上井冈山》' }
]

const works = [
  {
    title: '《毛泽东选集》',
    tag: '思想著作',
    desc: '汇集毛泽东思想精华，阐明马克思主义基本原理同中国具体实际相结合的科学体系。'
  },
  {
    title: '《论持久战》',
    tag: '军事理论',
    desc: '全面阐明抗战规律，指出“战争的伟力之最深厚的根源，存在于民众之中”。'
  },
  {
    title: '《实践论》',
    tag: '哲学思想',
    desc: '系统论述认识与实践的辩证关系，奠定了党的实事求是思想路线的哲学基础。'
  },
  {
    title: '《矛盾论》',
    tag: '哲学思想',
    desc: '阐述对立统一规律的唯物辩证法核心，指导中国革命和建设的科学方法论。'
  },
  {
    title: '《沁园春·雪》',
    tag: '诗词名篇',
    desc: '雄浑壮阔、气吞山河，堪称“千古绝唱”，展现革命浪漫主义与豪迈气概。'
  },
  {
    title: '书《满江红》',
    tag: '书法艺术',
    desc: '笔走龙蛇、汪洋恣肆，独树一帜的“毛体”草书，被誉为一代书法墨宝。'
  }
]

type CEvent = { year: string; text: string }

const timeline: CEvent[] = [
  { year: '1893', text: '出生于湖南湘潭韶山冲' },
  { year: '1913', text: '进入湖南省立第一师范求学' },
  { year: '1921', text: '出席中共一大，成为主要创始人' },
  { year: '1927', text: '发动秋收起义，创建井冈山根据地' },
  { year: '1935', text: '遵义会议，红军长征胜利到达陕北' },
  { year: '1945', text: '发表《论持久战》，指引抗战胜利' },
  { year: '1949', text: '宣告中华人民共和国成立' },
  { year: '1956', text: '领导社会主义改造基本完成' },
  { year: '1976', text: '在北京逝世，享年83岁' }
]

/* ---------- 组件 ---------- */

function ScrollButton({ target }: { target: string }) {
  return (
    <Button
      className="rounded-full border border-primary/40 bg-transparent text-primary hover:bg-primary hover:text-white"
      onClick={() => {
        document.getElementById(target)?.scrollIntoView({ behavior: 'smooth' })
      }}
    >
      了解生平故事 ↓
    </Button>
  )
}

function SectionHeading({
  kicker,
  title,
  sub
}: {
  kicker: string
  title: string
  sub?: string
}) {
  return (
    <div className="mx-auto mb-12 max-w-3xl text-center">
      <div className="mb-3 inline-flex items-center gap-2 text-sm tracking-[0.25em] text-red-700">
        <span className="h-px w-8 bg-red-700/60" />
        {kicker}
        <span className="h-px w-8 bg-red-700/60" />
      </div>
      <h2 className="font-display text-3xl font-bold tracking-wide text-stone-900 sm:text-4xl">
        {title}
      </h2>
      {sub && <p className="mt-4 text-stone-600">{sub}</p>}
    </div>
  )
}

export default function App() {
  return (
    <div className="min-h-screen bg-gradient-to-b from-red-50 via-stone-50 to-stone-100 text-stone-800">
      {/* ---------- 顶部导航 ---------- */}
      <header className="sticky top-0 z-50 border-b border-red-900/10 bg-white/85 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-5 py-4">
          <a href="#top" className="flex items-center gap-3">
            <span className="flex h-10 w-10 items-center justify-center rounded-full bg-red-700 font-display text-lg font-bold text-white">
              毛
            </span>
            <span className="font-display text-xl font-bold tracking-widest text-red-800">
              毛泽东生平
            </span>
          </a>
          <nav className="hidden items-center gap-1 text-sm font-medium text-stone-600 md:flex">
            <a href="#intro" className="px-3 py-2 hover:text-red-700">生平简介</a>
            <a href="#stages" className="px-3 py-2 hover:text-red-700">人生历程</a>
            <a href="#legacy" className="px-3 py-2 hover:text-red-700">思想贡献</a>
            <a href="#quotes" className="px-3 py-2 hover:text-red-700">诗词名言</a>
            <a href="#timeline" className="px-3 py-2 hover:text-red-700">大事年表</a>
          </nav>
          <a
            href="#top"
            className="text-sm font-semibold text-red-700 hover:text-red-900 md:hidden"
          >
            1893—1976
          </a>
        </div>
      </header>

      {/* ---------- 英雄区 ---------- */}
      <section id="top" className="relative overflow-hidden">
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_20%,rgba(220,38,38,0.15),transparent_50%)]" />
        <div className="mx-auto grid max-w-6xl items-center gap-10 px-6 py-20 md:grid-cols-[1fr_auto] lg:py-28">
          <div className="space-y-7">
            <div className="flex items-center gap-3">
              <span className="rounded-full bg-red-700/15 px-4 py-1 text-sm font-semibold text-red-700">
                中国人民的伟大领袖
              </span>
              <span className="font-display tracking-widest text-stone-400">
                1893 — 1976
              </span>
            </div>
            <h1 className="font-display leading-tight tracking-wide text-stone-900">
              <span className="block text-xl text-red-700 sm:text-2xl">
                伟大导师 · 伟大领袖 · 伟大统帅 · 伟大舵手
              </span>
              <span className="mt-2 block text-5xl font-bold sm:text-6xl lg:text-7xl">
                毛泽东
              </span>
            </h1>
            <p className="max-w-2xl text-lg leading-relaxed text-stone-600">
              毛泽东同志是伟大的马克思主义者，伟大的无产阶级革命家、战略家、理论家，
              是伟大的民族英雄和人民领袖。他领导党和人民开创了中国特色社会主义道路的
              历史源头，为民族的独立和人民的解放建立了不可磨灭的历史功勋。
            </p>
            <div className="flex flex-wrap items-center gap-4 pt-2">
              <ScrollButton target="intro" />
              <a
                href="#timeline"
                className="rounded-full border border-stone-300 px-6 py-2.5 text-sm font-semibold text-stone-700 transition hover:border-red-700 hover:text-red-700"
              >
                查看大事年表
              </a>
            </div>
            <div className="grid grid-cols-3 gap-4 pt-6 text-center">
              {[
                ['83', '载人生岁月'],
                ['6', '大人生阶段'],
                ['6', '部传世名篇']
              ].map(([n, label]) => (
                <div key={label} className="rounded-xl border border-red-100 bg-white/70 p-4">
                  <div className="font-display text-3xl font-bold text-red-700">{n}</div>
                  <div className="mt-1 text-xs text-stone-500">{label}</div>
                </div>
              ))}
            </div>
          </div>
          {/* 书法印章装饰 */}
          <div className="hidden select-none flex-col items-end gap-6 md:flex">
            <div className="rotate-2 border-2 border-double border-red-700/50 bg-white/60 px-10 py-8 text-center shadow-lg">
              <p className="font-calligraphy text-3xl tracking-wider text-red-800">数风流人物</p>
              <p className="mt-3 font-calligraphy text-2xl tracking-wider text-red-800">还看今朝</p>
              <div className="mt-5 inline-block border-2 border-red-700 bg-red-700 px-3 py-2 font-calligraphy text-white">
                润之
              </div>
            </div>
            <p className="max-w-xs text-right font-calligraphy text-stone-400">
              —— 毛泽东《沁园春·雪》
            </p>
          </div>
        </div>
      </section>

      {/* ---------- 生平简介 ---------- */}
      <section id="intro" className="mx-auto max-w-6xl px-6 py-16">
        <div className="mx-auto max-w-4xl">
          <SectionHeading kicker="生平简介" title="从韶山冲走向天安门" />
          <div className="space-y-6 rounded-2xl border border-red-100 bg-white/70 p-8 text-lg leading-loose text-stone-700 shadow-sm">
            <p>
              毛泽东（1893年12月26日—1976年9月9日），字润之，笔名子任，湖南湘潭人。他是中国共产党、
              中国人民解放军、中华人民共和国的主要缔造者，中国各族人民的伟大领袖。
            </p>
            <p>
              在新民主主义革命时期，他创造性地开辟了以农村包围城市、武装夺取政权的中国革命道路；
              在社会主义建设时期，他领导党和人民进行社会主义革命和建设，确立社会主义基本制度。
              他的光辉思想和伟大人格，深刻而长远地影响着一代又一代中华儿女。
            </p>
            <p>
              正如天安门城楼上那句庄严的宣告——1949年10月1日，他向全世界庄严宣告
              <span className="font-calligraphy font-semibold text-red-700">
                “中华人民共和国中央人民政府今天成立了”
              </span>
              ，中国人民从此站起来了。
            </p>
          </div>
        </div>
      </section>

      {/* ---------- 人生历程 ---------- */}
      <section id="stages" className="bg-white/50 py-16">
        <div className="mx-auto max-w-6xl px-6">
          <SectionHeading
            kicker="人生历程"
            title="光辉的一生"
            sub="六个篇章，记录他从少年立志到奠基伟业的峥嵘岁月"
          />
          <div className="space-y-8">
            {stages.map((s, i) => (
              <div
                key={s.title}
                className="group grid items-start gap-5 rounded-xl border border-stone-200 bg-white p-6 transition hover:border-red-700/40 hover:shadow-lg md:grid-cols-[150px_1fr_auto]"
              >
                <div className="flex md:block">
                  <span className="font-display text-5xl font-bold text-red-100 md:text-6xl">
                    {String(i + 1).padStart(2, '0')}
                  </span>
                  <Badge className="ml-3 mt-1 bg-red-700 text-white md:ml-0 md:mt-0">
                    {s.period}
                  </Badge>
                </div>
                <div>
                  <h3 className="font-display text-xl font-bold text-stone-900">
                    {s.title}
                  </h3>
                  <p className="mt-2 text-sm font-semibold text-red-700">{s.year}</p>
                  <p className="mt-3 leading-relaxed text-stone-600">{s.desc}</p>
                </div>
                <span className="hidden h-full w-1 rounded-full bg-gradient-to-b from-red-700 via-red-400 to-transparent md:block" />
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ---------- 思想与贡献 ---------- */}
      <section id="legacy" className="py-16">
        <div className="mx-auto max-w-6xl px-6">
          <SectionHeading
            kicker="思想贡献"
            title="永放光芒的思想与著作"
            sub="毛泽东思想是马克思列宁主义在中国的运用和发展，是经过长期实践检验的科学真理"
          />
          <Tabs defaultValue="works" className="mx-auto max-w-5xl">
            <TabsList className="mx-auto flex w-fit gap-1 rounded-full bg-red-100/70 p-1">
              <TabsTrigger value="works" className="rounded-full data-[state=active]:bg-red-700 data-[state=active]:text-white">
                著作名篇
              </TabsTrigger>
              <TabsTrigger value="achievements" className="rounded-full data-[state=active]:bg-red-700 data-[state=active]:text-white">
                历史贡献
              </TabsTrigger>
            </TabsList>

            <TabsContent value="works" className="mt-8">
              <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
                {works.map((w) => (
                  <div
                    key={w.title}
                    className="flex flex-col rounded-xl border border-stone-200 bg-white p-6 shadow-sm transition hover:-translate-y-1 hover:border-red-700/40 hover:shadow-md"
                  >
                    <div className="mb-4 flex h-11 w-11 items-center justify-center rounded-lg bg-red-700/10 text-lg font-bold text-red-700">
                      {w.title[1] || w.title[0]}
                    </div>
                    <h3 className="font-display text-xl font-bold text-stone-900">{w.title}</h3>
                    <Badge className="mt-2 w-fit bg-red-100 text-red-700">{w.tag}</Badge>
                    <p className="mt-3 leading-relaxed text-stone-600">{w.desc}</p>
                  </div>
                ))}
              </div>
            </TabsContent>

            <TabsContent value="achievements" className="mt-8">
              <div className="grid gap-5 sm:grid-cols-2">
                {[
                  ['缔造伟大国家', '领导中国人民经过艰苦卓绝的斗争，建立中华人民共和国，实现了民族独立和人民解放。'],
                  ['开辟革命道路', '开创农村包围城市、武装夺取政权的中国革命道路，指引中国革命走向胜利。'],
                  ['建立基本制度', '领导完成社会主义改造，确立社会主义基本制度，奠定中国特色社会主义的制度基础。'],
                  ['奠基工业体系', '领导建立独立的、比较完整的工业体系和国民经济体系，为国家现代化建设打下坚实基础。'],
                  ['维护国家主权', '捍卫国家主权和领土完整，维护民族团结和国家安全，赢得国际地位与尊重。'],
                  ['提供科学指导', '创立毛泽东思想这一革命理论体系，为党和人民事业提供了科学指导思想。']
                ].map(([t, d]) => (
                  <div key={t} className="flex gap-4 rounded-xl border border-stone-200 bg-white p-6 shadow-sm">
                    <span className="mt-1 h-3 w-3 shrink-0 rounded-full bg-red-700" />
                    <div>
                      <h3 className="font-display text-lg font-bold text-stone-900">{t}</h3>
                      <p className="mt-2 leading-relaxed text-stone-600">{d}</p>
                    </div>
                  </div>
                ))}
              </div>
            </TabsContent>
          </Tabs>
        </div>
      </section>

      {/* ---------- 诗词名言 ---------- */}
      <section id="quotes" className="bg-[#3b0f0f] py-16 text-white">
        <div className="mx-auto max-w-5xl px-6">
          <div className="mb-12 text-center">
            <div className="mb-3 inline-flex items-center gap-2 text-sm tracking-[0.25em] text-red-300">
              <span className="h-px w-8 bg-red-300/60" />
              诗词 · 名言
              <span className="h-px w-8 bg-red-300/60" />
            </div>
            <h2 className="font-display text-3xl font-bold tracking-wide sm:text-4xl">
              大气磅礴，光照千秋
            </h2>
            <p className="mt-4 text-red-100/70">
              毛泽东不仅是伟大的政治家、军事家，也是才华横溢的诗人、书法家
            </p>
          </div>
          <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            {quotes.map((q) => (
              <figure
                key={q.text}
                className="flex flex-col justify-between rounded-xl border border-red-400/20 bg-white/5 p-6 backdrop-blur transition hover:bg-white/10"
              >
                <blockquote className="font-calligraphy text-lg leading-relaxed text-red-50">
                  “{q.text}”
                </blockquote>
                <figcaption className="mt-5 text-sm text-red-200/70">—— {q.src}</figcaption>
              </figure>
            ))}
          </div>
        </div>
      </section>

      {/* ---------- 大事年表 ---------- */}
      <section id="timeline" className="py-16">
        <div className="mx-auto max-w-5xl px-6">
          <SectionHeading
            kicker="时间轴线"
            title="革命的一生，战斗的一生"
            sub="从韶山到北京，从建党到建国，一步步改写民族命运"
          />
          <div className="relative mx-auto max-w-3xl">
            <span className="absolute left-[15px] top-0 h-full w-0.5 bg-gradient-to-b from-red-700 via-red-400 to-transparent md:left-1/2 md:-translate-x-1/2" />
            <div className="space-y-8">
              {timeline.map((e, i) => (
                <div
                  key={e.year}
                  className={`relative flex items-start gap-5 md:w-1/2 ${
                    i % 2 === 0
                      ? 'md:mr-auto md:pr-10'
                      : 'md:ml-auto md:pl-10 md:flex-row-reverse'
                  }`}
                >
                  <span
                    className={`absolute top-1.5 h-3.5 w-3.5 rounded-full border-2 border-red-700 bg-white left-[7px] md:left-auto ${
                      i % 2 === 0 ? 'md:right-[-7px]' : 'md:left-[-7px]'
                    }`}
                  />
                  <div className="w-full rounded-xl border border-stone-200 bg-white p-5 shadow-sm hover:border-red-700/40">
                    <div className="font-display text-2xl font-bold text-red-700">{e.year}</div>
                    <p className="mt-2 leading-relaxed text-stone-700">{e.text}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ---------- 结束 ---------- */}
      <section className="border-t border-red-900/10 py-20 text-center">
        <div className="mx-auto max-w-3xl px-6">
          <h2 className="font-calligraphy text-3xl leading-relaxed text-stone-800 sm:text-4xl">
            东方红，太阳升，中国出了个毛泽东
          </h2>
          <p className="mt-6 text-stone-500">
            他为中国人民的解放事业和社会主义建设事业奋斗了一生，他的光辉形象和伟大精神
            永远活在中国人民心中。
          </p>
          <div className="mx-auto mt-10 flex items-center justify-center gap-4 text-stone-400">
            <Separator className="w-16 bg-red-200" />
            <span className="font-display tracking-widest">1893—1976</span>
            <Separator className="w-16 bg-red-200" />
          </div>
        </div>
      </section>

      <footer className="border-t border-red-900/10 bg-white/60 py-8 text-center text-sm text-stone-500">
        <p>毛泽东 · 生平事迹 · 纪念页面</p>
        <p className="mt-1 text-xs text-stone-400">伟大领袖和导师毛泽东同志永垂不朽</p>
      </footer>
    </div>
  )
}

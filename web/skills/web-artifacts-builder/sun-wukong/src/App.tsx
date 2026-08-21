import { useState } from 'react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Progress } from '@/components/ui/progress'
import {
  Crown,
  Flame,
  Feather,
  Cloud,
  Sparkles,
  Zap,
  Shield,
  Mountain,
  Scroll,
  Star,
  Swords,
  CloudLightning,
  Hexagon,
  Landmark,
  BookOpen,
} from 'lucide-react'

function StatBar({ label, value }: { label: string; value: number }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-sm">
        <span className="text-stone-600 font-medium">{label}</span>
        <span className="text-stone-400 tabular-nums">{value}</span>
      </div>
      <Progress value={value} className="h-2 bg-stone-200" />
    </div>
  )
}

function SectionEyebrow({ text }: { text: string }) {
  return (
    <div className="flex items-center gap-3 mb-3">
      <div className="h-px w-8 bg-amber-600" />
      <span className="text-amber-700 text-sm tracking-[0.25em] font-medium uppercase">
        {text}
      </span>
    </div>
  )
}

const journey = [
  {
    period: '出生',
    title: '灵石化生',
    desc: '东胜神洲花果山上一块受天地日月精华滋养的仙石崩裂，化作石猴，眼运金光，射冲斗府。',
    icon: Sparkles,
  },
  {
    period: '拜师',
    title: '灵台方寸山',
    desc: '远渡重洋，寻访名师，于菩提祖师门下得法号"悟空"，修得七十二变、筋斗云与长生之术。',
    icon: Mountain,
  },
  {
    period: '闹天宫',
    title: '齐天大圣',
    desc: '闯龙宫得金箍棒，入地府销生死簿，大闹天宫自封"齐天大圣"，丹炉中炼就火眼金睛。',
    icon: Flame,
  },
  {
    period: '西行',
    title: '大闹蟠桃会',
    desc: '因被封弼马温不满，偷吃蟠桃、盗饮御酒、大闹蟠桃盛会，最终被如来佛祖压在五行山下。',
    icon: Crown,
  },
  {
    period: '取经',
    title: '大闹蟠桃会',
    desc: '被压五行山下五百年后，受观音点化，保护唐僧西行取经，历经九九八十一难，终成正果。',
    icon: Scroll,
  },
]

const abilities = [
  {
    name: '七十二变',
    desc: '可随心变化身形，通达万物形貌，是悟空最核心的变化神通。',
    icon: Feather,
    color: 'bg-rose-50 text-rose-700 border-rose-200',
  },
  {
    name: '筋斗云',
    desc: '一个筋斗十万八千里，是《西游记》中声名赫赫的腾云之术。',
    icon: Cloud,
    color: 'bg-sky-50 text-sky-700 border-sky-200',
  },
  {
    name: '如意金箍棒',
    desc: '从东海的定海神针炼化而来，可大可小，重一万三千五百斤。',
    icon: Swords,
    color: 'bg-amber-50 text-amber-700 border-amber-200',
  },
  {
    name: '火眼金睛',
    desc: '在太上老君八卦炉中炼就，可辨识妖魔鬼怪，洞察幻形。',
    icon: Flame,
    color: 'bg-orange-50 text-orange-700 border-orange-200',
  },
  {
    name: '毫毛分身',
    desc: '拔下毫毛可吹出千万个分身，千变万化，难以捉摸。',
    icon: Zap,
    color: 'bg-violet-50 text-violet-700 border-violet-200',
  },
  {
    name: '金刚不坏之躯',
    desc: '服食蟠桃仙丹，在丹炉中三昧真火炼就，刀枪不入、水火不侵。',
    icon: Shield,
    color: 'bg-emerald-50 text-emerald-700 border-emerald-200',
  },
]

const names = [
  { title: '美猴王', period: '花果山时期', note: '群猴共尊，自号美猴王' },
  { title: '孙悟空', period: '拜师得名', note: '菩提祖师所赐法名' },
  { title: '弼马温', period: '天庭任职', note: '初上天庭所任卑微官职' },
  { title: '齐天大圣', period: '大闹天宫', note: '自封威名，震动三界' },
  { title: '斗战胜佛', period: '取经功德圆满', note: '西行归来封授佛位' },
]

const quotes = [
  {
    text: '俺老孙来也！',
    note: '最经典的登场台词',
  },
  {
    text: '皇帝轮流做，明年到我家。',
    note: '大闹天宫的豪言',
  },
  {
    text: '妖怪，哪里走！',
    note: '西行路上的战斗宣言',
  },
]

export default function App() {
  const [openQuote, setOpenQuote] = useState<number | null>(0)

  return (
    <div className="min-h-screen bg-[#faf7f2] text-stone-800 antialiased">
      {/* ---------- HERO ---------- */}
      <header className="relative overflow-hidden">
        <div
          className="absolute inset-0 bg-no-repeat bg-cover bg-center"
          style={{
            backgroundImage:
              'linear-gradient(rgba(20,16,8,0.62), rgba(20,16,8,0.72)), url(https://images.unsplash.com/photo-1583147831216-3ee4b5b6599f?q=80&w=1400&auto=format&fit=crop)',
          }}
        />
        {/* decorative grain */}
        <div
          className="absolute inset-0 opacity-20"
          style={{
            backgroundImage:
              'radial-gradient(rgba(255,210,120,0.12) 1px, transparent 1px)',
            backgroundSize: '22px 22px',
          }}
        />

        <div className="relative mx-auto max-w-6xl px-6 pt-16 pb-24 lg:pt-24 lg:pb-32">
          {/* gold band */}
          <div className="h-1 w-24 bg-gradient-to-r from-amber-500 to-amber-300 rounded-full mb-10" />

          <div className="grid gap-12 lg:grid-cols-[1.25fr_1fr] items-end">
            <div>
              <p className="text-amber-300/90 tracking-[0.5em] text-sm uppercase mb-6">
                花果山 水帘洞 东胜神洲
              </p>
              <h1 className="text-5xl lg:text-7xl font-black text-white leading-none tracking-tight">
                悟空
              </h1>
              <h2 className="text-3xl lg:text-5xl font-bold text-amber-200/95 mt-2 tracking-wide">
                孙悟空
              </h2>
              <p className="mt-6 text-stone-200 max-w-xl leading-relaxed text-lg">
                中国古典神魔小说《西游记》中的主角之一。他是从花果山仙石中诞生的一只灵猴，
                历经拜师、大闹天宫、五行山镇压，最终护持唐僧西行取经，证得斗战胜佛。
              </p>
            </div>

            <div className="rounded-2xl border border-amber-200/20 bg-black/25 backdrop-blur-md p-6 lg:mb-2">
              <div className="flex items-center gap-3 mb-4">
                <Crown className="h-6 w-6 text-amber-400" />
                <h3 className="text-lg font-bold text-white">齐天大圣 · 行者</h3>
              </div>
              <div className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
                <div className="text-stone-400">名号</div>
                <div className="text-stone-100 font-medium text-right">孙悟空</div>
                <div className="text-stone-400">神祇</div>
                <div className="text-stone-100 font-medium text-right">斗战胜佛</div>
                <div className="text-stone-400">兵器</div>
                <div className="text-stone-100 font-medium text-right">如意金箍棒</div>
                <div className="text-stone-400">故乡</div>
                <div className="text-stone-100 font-medium text-right">花果山水帘洞</div>
                <div className="text-stone-400">师承</div>
                <div className="text-stone-100 font-medium text-right">菩提祖师</div>
              </div>
            </div>
          </div>
        </div>

        {/* bottom scrolled fade */}
        <div className="absolute bottom-0 left-0 right-0 h-8 bg-gradient-to-t from-[#faf7f2] to-transparent" />
      </header>

      {/* ---------- MAIN CONTENT ---------- */}
      <main className="mx-auto max-w-6xl px-6">
        {/* intro strip */}
        <section className="py-14 border-b border-stone-200">
          <div className="grid gap-8 lg:grid-cols-3">
            <div className="lg:col-span-1">
              <SectionEyebrow text="人物速写" />
              <h2 className="text-3xl font-black leading-tight text-stone-900">
                生性桀骜、
                <br />
                但心怀慈悲。
              </h2>
            </div>
            <p className="lg:col-span-2 text-stone-600 leading-loose text-lg">
              孙悟空是中国文学史上最具魅力的英雄形象之一。他本领高强，敢于反抗天庭权威，
              是自由无畏的象征；他忠心护佑唐僧西行取经，在降妖除魔中展现出机智与神通。
              他的形象融合了神话、武侠与喜剧元素，跨越时空，成为中国文化中经久不衰的精神符号。
              其故事被改编为无数影视、戏曲、动画、游戏作品，影响深远。
            </p>
          </div>

          <div className="mt-10 grid gap-4 lg:grid-cols-3">
            <Card className="p-5 rounded-2xl border-stone-200 bg-white/60">
              <StatBar label="武艺神通" value={100} />
            </Card>
            <Card className="p-5 rounded-2xl border-stone-200 bg-white/60">
              <StatBar label="机智谋略" value={95} />
            </Card>
            <Card className="p-5 rounded-2xl border-stone-200 bg-white/60">
              <StatBar label="忠心毅力" value={90} />
            </Card>
          </div>
        </section>

        {/* Journey timeline */}
        <section className="py-16">
          <SectionEyebrow text="传奇一生" />
          <h2 className="text-4xl font-black text-stone-900 mb-3">西游历程</h2>
          <p className="text-stone-500 mb-12 max-w-2xl">
            从灵石化生到斗战胜佛，孙悟空的一生跌宕起伏，恰如一场修行。
          </p>

          <div className="relative">
            {/* vertical line */}
            <div className="absolute left-5 lg:left-1/2 top-0 bottom-0 w-px bg-stone-300 hidden lg:block" />
            <div className="absolute left-5 top-0 bottom-0 w-px bg-stone-300 lg:hidden" />

            <div className="space-y-8">
              {journey.map((item, i) => {
                const Icon = item.icon
                const isLeft = i % 2 === 0
                return (
                  <div
                    key={i}
                    className={`relative lg:w-1/2 pl-16 lg:pl-0 ${
                      isLeft
                        ? 'lg:pr-12 lg:text-right'
                        : 'lg:ml-auto lg:pl-12'
                    }`}
                  >
                    <div
                      className={`absolute top-2 lg:top-4 ${
                        isLeft
                          ? 'lg:right-[-11px] lg:translate-x-[50%]'
                          : 'left-0 lg:left-[-11px] lg:translate-x-[-50%]'
                      }`}
                    >
                      <div className="h-5 w-5 rounded-full bg-amber-500 ring-4 ring-amber-100 flex items-center justify-center">
                        <Icon className="h-3 w-3 text-white" />
                      </div>
                    </div>
                    <div className="text-sm font-bold text-amber-700 tracking-wider mb-1 lg:mb-2">
                      {item.period}
                    </div>
                    <h3 className="text-2xl font-extrabold text-stone-900 mb-2">
                      {item.title}
                    </h3>
                    <p className="text-stone-600 leading-relaxed">{item.desc}</p>
                  </div>
                )
              })}
            </div>
          </div>
        </section>

        {/* Abilities */}
        <section className="py-16 border-t border-stone-200">
          <SectionEyebrow text="神通本领" />
          <h2 className="text-4xl font-black text-stone-900 mb-12">
            盖世神通
          </h2>

          <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {abilities.map((a) => {
              const Icon = a.icon
              return (
                <div
                  key={a.name}
                  className="group rounded-2xl border border-stone-200 bg-white p-6 transition-all hover:shadow-lg hover:-translate-y-1 duration-300"
                >
                  <div
                    className={`inline-flex items-center justify-center h-12 w-12 rounded-xl border ${a.color} mb-4`}
                  >
                    <Icon className="h-6 w-6" />
                  </div>
                  <h3 className="text-lg font-extrabold text-stone-900 mb-2">
                    {a.name}
                  </h3>
                  <p className="text-sm text-stone-500 leading-relaxed">
                    {a.desc}
                  </p>
                </div>
              )
            })}
          </div>
        </section>

        {/* Names + Quotes */}
        <section className="py-16 border-t border-stone-200">
          <div className="grid lg:grid-cols-2 gap-12">
            {/* Names */}
            <div>
              <SectionEyebrow text="多面名号" />
              <h2 className="text-3xl font-black text-stone-900 mb-8">
                名号背后的故事
              </h2>
              <div className="space-y-3">
                {names.map((n) => (
                  <div
                    key={n.title}
                    className="flex items-center justify-between gap-4 rounded-xl border border-stone-200 bg-white px-5 py-4 hover:border-amber-300 transition-colors"
                  >
                    <div>
                      <div className="font-extrabold text-stone-900">
                        {n.title}
                      </div>
                      <div className="text-xs text-amber-700 font-medium mt-0.5">
                        {n.period}
                      </div>
                      <div className="text-sm text-stone-500 mt-1">
                        {n.note}
                      </div>
                    </div>
                    <BookOpen className="h-5 w-5 text-stone-300 shrink-0" />
                  </div>
                ))}
              </div>
            </div>

            {/* Quotes */}
            <div>
              <SectionEyebrow text="经典台词" />
              <h2 className="text-3xl font-black text-stone-900 mb-8">
                脍炙人口的台词
              </h2>
              <div className="space-y-3">
                {quotes.map((q, i) => (
                  <button
                    key={i}
                    onClick={() => setOpenQuote(i === openQuote ? null : i)}
                    className="w-full text-left rounded-xl border border-stone-200 bg-gradient-to-br from-amber-50 to-white px-6 py-5 hover:border-amber-300 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      <Star
                        className={`h-5 w-5 ${
                          i === openQuote
                            ? 'text-amber-500 fill-amber-400'
                            : 'text-amber-200'
                        }`}
                      />
                      <span className="text-lg font-extrabold text-stone-900">
                        {q.text}
                      </span>
                    </div>
                    {i === openQuote && (
                      <p className="text-sm text-stone-500 mt-2 pl-8">
                        {q.note}
                      </p>
                    )}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </section>
      </main>

      {/* ---------- FOOTER ---------- */}
      <footer className="mt-16 bg-stone-900 text-stone-400">
        <div className="mx-auto max-w-6xl px-6 py-12">
          <div className="flex flex-col lg:flex-row items-start lg:items-center justify-between gap-6">
            <div>
              <div className="flex items-center gap-3">
                <span className="text-amber-400 text-2xl font-black tracking-widest">
                  悟空
                </span>
                <span className="h-px w-16 bg-amber-500/50" />
                <span className="text-sm tracking-widest text-stone-500 uppercase">
                  Sun Wukong
                </span>
              </div>
              <p className="mt-3 text-sm">
                《西游记》— 中国四大古典名著之一 · 吴承恩
              </p>
            </div>
            <div className="text-sm flex gap-6">
              <span className="flex items-center gap-1.5">
                <Hexagon className="h-4 w-4 text-amber-500/70" />
                七十二变
              </span>
              <span className="flex items-center gap-1.5">
                <CloudLightning className="h-4 w-4 text-amber-500/70" />
                筋斗云
              </span>
              <span className="flex items-center gap-1.5">
                <Landmark className="h-4 w-4 text-amber-500/70" />
                花果山
              </span>
            </div>
          </div>
          <div className="mt-8 pt-6 border-t border-stone-800 text-xs text-stone-600 flex flex-wrap gap-x-6 gap-y-1">
            <span>灵感源于《西游记》</span>
            <span>本页面为虚构介绍</span>
            <span className="lg:ml-auto">
              斗战胜佛 · 上天入地，唯我独尊
            </span>
          </div>
        </div>
      </footer>
    </div>
  )
}

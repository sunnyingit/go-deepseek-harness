import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Separator } from "@/components/ui/separator";
import {
  ScrollText,
  BookOpen,
  Swords,
  Milk,
  Heart,
  Sparkles,
  ArrowRight,
  ChefHat,
  Ship,
  Moon,
  Wheat,
  Home,
  Quote,
} from "lucide-react";

const stats = [
  { label: "佛号", value: "净坛使者" },
  { label: "法宝", value: "九齿钉耙" },
  { label: "天界身份", value: "天蓬元帅" },
  { label: "兵器重", value: "五千零四十八斤" },
];

const traits = [
  {
    icon: Milk,
    color: "bg-amber-100 text-amber-700",
    title: "贪吃好色",
    desc: "见了就吃、见了就爱，又贪嘴又贪杯，是全队最接地气的存在。",
  },
  {
    icon: Heart,
    color: "bg-rose-100 text-rose-700",
    title: "憨厚可爱",
    desc: "嘴上不饶人，心里软得很。吵架归吵架，遇到难处从不真撂挑子。",
  },
  {
    icon: Home,
    color: "bg-sky-100 text-sky-700",
    title: "恋家念旧",
    desc: "做梦都想着回高老庄，动不动就想散伙分行李，可最后还是跟着走到了西天。",
  },
  {
    icon: Sparkles,
    color: "bg-violet-100 text-violet-700",
    title: "法术了得",
    desc: "三十六变、水战无双，天河水军出身，本事并不在悟空之下。",
  },
];

const journey = [
  { stop: "高老庄", note: "因好色被贬下界，在高老庄当了上门女婿，被悟空降服后随唐僧取经。" },
  { stop: "黄风岭", note: "虽贪吃误事，却也常在路上立下战功，护送师父一路前行。" },
  { stop: "通天河", note: "水下功夫在这里展露无遗，是他最出彩的一段战斗。" },
  { stop: "灵山", note: "历经八十一难最终修成正果，被封为净坛使者。" },
];

const weapons = ["九齿钉耙", "天蓬之钯", "紫云犁钯"];

export default function App() {
  const [tab, setTab] = useState("shenghua");

  return (
    <div className="min-h-screen bg-[#f7f2e8] text-stone-900 antialiased">
      {/* 顶部横幅 */}
      <header className="relative overflow-hidden">
        <div className="absolute inset-0 bg-gradient-to-r from-stone-800 via-stone-700 to-amber-800" />
        <div className="absolute inset-0 opacity-20 [background:repeating-linear-gradient(45deg,transparent,transparent_20px,rgba(255,255,255,0.08)_20px,rgba(255,255,255,0.08)_40px)]" />
        <div className="relative mx-auto max-w-6xl px-6 py-16 lg:py-24">
          <div className="flex flex-col gap-8 sm:flex-row sm:items-center">
            <div className="flex-1">
              <Badge className="mb-4 bg-amber-500/90 text-white hover:bg-amber-500">
                西游记 · 取经团成员
              </Badge>
              <div className="flex items-center gap-2 text-amber-300/90">
                <Moon className="h-5 w-5" />
                <span className="font-serif tracking-[0.4em] text-sm lg:text-base">
                  天蓬元帅 · 净坛使者
                </span>
              </div>
              <h1 className="mt-3 font-serif text-5xl font-bold tracking-wide text-white lg:text-7xl">
                猪八戒
              </h1>
              <p className="mt-4 max-w-xl text-base leading-relaxed text-amber-50/85 lg:text-lg">
                一头猪精的自我修养：见了饭就精精神神，见了妖怪就躲躲闪闪，
                却偏偏是这支取经队伍里，最凡胎肉骨、也最让人忍俊不禁的一个。
              </p>
            </div>

            {/* 数字标识 */}
            <div className="grid grid-cols-2 gap-3 sm:w-72">
              {stats.map((s) => (
                <div
                  key={s.label}
                  className="rounded-xl border border-white/10 bg-white/5 px-4 py-5 backdrop-blur"
                >
                  <p className="font-serif text-lg font-semibold text-amber-200">
                    {s.value}
                  </p>
                  <p className="mt-1 text-xs tracking-widest text-amber-50/60">
                    {s.label}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </header>

      {/* 快速标签 */}
      <div className="mx-auto max-w-6xl px-6 pt-8">
        <div className="flex flex-wrap gap-2">
          {weapons.map((w) => (
            <Badge
              key={w}
              variant="outline"
              className="border-amber-700/30 bg-white/60 text-amber-900"
            >
              <Swords className="mr-1.5 h-3 w-3" /> {w}
            </Badge>
          ))}
        </div>
      </div>

      <main className="mx-auto max-w-6xl px-6 pb-20">
        {/* 人物性格 */}
        <section className="pt-12">
          <div className="flex items-end justify-between">
            <div>
              <h2 className="font-serif text-2xl font-bold text-stone-800 lg:text-3xl">
                一幅性格肖像
              </h2>
              <p className="mt-2 text-sm text-stone-500">
                在神魔世界里，他是最像"人"的一个
              </p>
            </div>
            <Quote className="hidden h-8 w-8 text-amber-700/40 sm:block" />
          </div>

          <div className="mt-6 grid gap-5 sm:grid-cols-2 lg:grid-cols-4">
            {traits.map((t) => (
              <Card
                key={t.title}
                className="border-stone-200 bg-white/70 shadow-sm transition-shadow hover:shadow-md"
              >
                <CardContent className="pt-6">
                  <div
                    className={`inline-flex h-11 w-11 items-center justify-center rounded-xl ${t.color}`}
                  >
                    <t.icon className="h-5 w-5" />
                  </div>
                  <h3 className="mt-4 font-serif text-lg font-semibold text-stone-800">
                    {t.title}
                  </h3>
                  <p className="mt-2 text-sm leading-relaxed text-stone-600">
                    {t.desc}
                  </p>
                </CardContent>
              </Card>
            ))}
          </div>
        </section>

        {/* 生平故事 Tab */}
        <section className="pt-14">
          <h2 className="font-serif text-2xl font-bold text-stone-800 lg:text-3xl">
            历程与故事
          </h2>
          <p className="mt-2 text-sm text-stone-500">
            从天上到人间，再到西天，一路跌跌撞撞
          </p>

          <Tabs
            value={tab}
            onValueChange={setTab}
            className="mt-6"
          >
            <TabsList className="bg-stone-200/60">
              <TabsTrigger value="shenghua">
                <Moon className="mr-2 h-4 w-4" /> 贬谪下界
              </TabsTrigger>
              <TabsTrigger value="baozhang">
                <Wheat className="mr-2 h-4 w-4" /> 高老庄
              </TabsTrigger>
              <TabsTrigger value="tubing">
                <Ship className="mr-2 h-4 w-4" /> 取经路上
              </TabsTrigger>
              <TabsTrigger value="chengfo">
                <Sparkles className="mr-2 h-4 w-4" /> 终成正果
              </TabsTrigger>
            </TabsList>

            {["shenghua", "baozhang", "tubing", "chengfo"].map((key) => {
              const info = {
                shenghua: {
                  title: "从天蓬元帅到猪头",
                  text: "原是天庭的天蓬元帅，统领八万水军。因在蟠桃会上醉酒，戏弄嫦娥，被玉帝贬下凡间。谁料错投了猪胎，落得个猪头人身的模样。此前的威风，随着这一跤摔得荡然无存。",
                },
                baozhang: {
                  title: "入赘高老庄",
                  text: "下凡后化身壮汉，在高老庄做了女婿，靠着一身力气起早贪黑，把庄里过得红火。却因酒后露出原形吓坏了高家，被孙悟空一顿收拾，从此归顺唐僧，踏上取经之路。",
                },
                tubing: {
                  title: "一路降妖也一路偷懒",
                  text: "取经路上的他，嘴里喊着散伙分行李，手上却从没少卖力气。憨直使小性子，胆子不大，可论起水下功夫和那三十六变，连大师兄也要高看他三分。",
                },
                chengfo: {
                  title: "净坛使者",
                  text: "历经九九八十一难，熬到了灵山。佛祖说他「不好贪嘴」，赐他净坛使者之位——专管吃净斋坛贡品。瞧，即便修成了正果，这份差事还是离不了个「吃」字。",
                },
              }[key];

              return (
                <TabsContent
                  key={key}
                  value={key}
                  className="mt-4 rounded-2xl border border-stone-200 bg-white/70 p-6 lg:p-8"
                >
                  <h3 className="font-serif text-xl font-semibold text-amber-800">
                    {info.title}
                  </h3>
                  <p className="mt-3 max-w-3xl text-base leading-relaxed text-stone-700">
                    {info.text}
                  </p>

                  <div className="mt-6">
                    <p className="mb-3 text-xs font-medium tracking-widest text-stone-400">
                      关键节点
                    </p>
                    <div className="flex flex-wrap items-center gap-2 text-sm text-stone-600">
                      {journey.map((j, i) => (
                        <div key={j.stop} className="flex items-center">
                          <span className="rounded-full bg-amber-100 px-3 py-1 font-medium text-amber-800">
                            {j.stop}
                          </span>
                          {i < journey.length - 1 && (
                            <ArrowRight className="mx-2 h-4 w-4 text-stone-300" />
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                </TabsContent>
              );
            })}
          </Tabs>
        </section>

        {/* 底层档案 */}
        <section className="mt-14 grid gap-6 lg:grid-cols-2">
          <Card className="border-stone-200 bg-white/70">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 font-serif text-lg text-stone-800">
                <BookOpen className="h-5 w-5 text-amber-700" />
                角色档案
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-0">
              {[
                { k: "姓名", v: "猪悟能（八戒是唐僧所起的法名）" },
                { k: "出身", v: "天庭天蓬元帅 → 泾河猪胎" },
                { k: "兵器", v: "上宝沁金钯，即九齿钉耙" },
                { k: "法术", v: "三十六天罡变化、御风腾云、水下功夫" },
                { k: "性格", v: "贪吃好色、懒惰耍滑，却也憨直忠厚、有担当" },
                { k: "结局", v: "被如来封为净坛使者，位列仙班" },
              ].map((r, i) => (
                <div key={r.k}>
                  <div className="flex gap-6 py-3">
                    <span className="w-16 shrink-0 text-sm font-semibold text-amber-800">
                      {r.k}
                    </span>
                    <span className="text-sm leading-relaxed text-stone-600">
                      {r.v}
                    </span>
                  </div>
                  {i < 5 && <Separator className="bg-stone-200" />}
                </div>
              ))}
            </CardContent>
          </Card>

          <Card className="border-stone-200 bg-white/70">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 font-serif text-lg text-stone-800">
                <ScrollText className="h-5 w-5 text-amber-700" />
                一句点评
              </CardTitle>
            </CardHeader>
            <CardContent>
              <p className="font-serif text-xl leading-relaxed text-stone-700 lg:text-2xl">
                「世人只见他好吃懒做、贪恋红尘，却忘了——
                一个有过天蓬荣光的妖，肯弯下腰去挑担上肩、护师西行，
                这份扛得起平凡的凡心，才是他最大的不凡。」
              </p>
              <p className="mt-4 text-right text-sm text-stone-400">
                —— 随行小记
              </p>
              <div className="mt-6 flex flex-wrap gap-2">
                <Badge variant="outline" className="border-stone-300 text-stone-600">
                  <ChefHat className="mr-1 h-3 w-3" /> 自带干粮
                </Badge>
                <Badge variant="outline" className="border-stone-300 text-stone-600">
                  <Swords className="mr-1 h-3 w-3" /> 实战主力
                </Badge>
              </div>
            </CardContent>
          </Card>
        </section>

        <footer className="mt-16 border-t border-stone-200 pt-6 text-center text-xs text-stone-400">
          内容取材自《西游记》 · 一份关于猪八戒的平民小传
        </footer>
      </main>
    </div>
  );
}

import {
  Zap,
  Cpu,
  Sparkles,
  ArrowDown,
  Diamond,
  Rocket,
  TreePine,
  Ship,
  Droplets,
  CircuitBoard,
  Triangle,
  Accessibility,
} from "lucide-react";

/* ---------- Data ---------- */

const stats = [
  { key: "人口", value: "≈1756万", note: "常住人口 (2023)" },
  { key: "GDP", value: "3.46万亿元", note: "全国第3大经济体城市" },
  { key: "建成区面积", value: "1112 km²", note: "从边陲渔村到超大城市" },
  { key: "平均年龄", value: "≈33岁", note: "全国最年轻的一线城市" },
  { key: "摩天楼", value: "超2000座", note: "高度300米以上建筑居全球前列" },
  { key: "独角兽", value: "30余家", note: "硬科技与互联网独角兽之城" },
];

const timeline = [
  {
    year: "1979",
    title: "改革开放 · 深圳经济特区",
    desc: "一座边陲渔村被确立为经济特区，成为中国改革开放的试验田，迸发出深圳速度。",
  },
  {
    year: "1990",
    title: "证券交易所成立",
    desc: "深圳证券交易所开市，为全国资本市场注入活力，奠定金融与科技中心的基础。",
  },
  {
    year: "2004",
    title: "华为、中兴等巨头崛起",
    desc: "通信与电子制造业蓬勃兴起，深圳从世界工厂开始向创新之城转型。",
  },
  {
    year: "2016",
    title: "大疆改写全球无人机业",
    desc: "以大疆创新为代表的硬科技企业，让深圳成为全球无人机与智能制造的高地。",
  },
  {
    year: "2020",
    title: "先行示范区 · 大湾区引擎",
    desc: "被定位为中国特色社会主义先行示范区，成为粤港澳大湾区核心引擎。",
  },
  {
    year: "今天",
    title: "中国硅谷 · 全球创新策源地",
    desc: "从电子元器件到人工智能、新能源汽车，深圳正定义着城市的未来想象。",
  },
];

const techs = [
  {
    icon: Cpu,
    title: "华为 (Huawei)",
    desc: "通信设备与产品研发的全球领跑者，总部位于深圳，拥有庞大的研发矩阵。",
  },
  {
    icon: Triangle,
    title: "腾讯 (Tencent)",
    desc: "全球游戏与社交巨头，深圳南山腾讯滨海大厦重塑了城市天际线。",
  },
  {
    icon: Accessibility,
    title: "大疆创新 (DJI)",
    desc: "消费级无人机全球市场份额第一，总部隐身于南山的科技腹地。",
  },
  {
    icon: Rocket,
    title: "比亚迪 (BYD)",
    desc: "从电池到新能源汽车的全球巨头，深圳造的电动车驶向全球。",
  },
  {
    icon: CircuitBoard,
    title: "中兴通讯 (ZTE)",
    desc: "5G与光通信的重要玩家，与深圳的电子信息产业共生共长。",
  },
  {
    icon: Zap,
    title: "华大基因 (BGI)",
    desc: "全球基因组学研究与测序服务的先驱，深圳生命科学的名片。",
  },
];

const landmarks = [
  {
    name: "平安金融中心",
    type: "地标 · Ping An IFC",
    meta: "599m · 世界第9高楼",
    desc: "深圳的天际线之王，金融与CBD的象征，塔顶如悬空的艺术品。",
    tall: true,
  },
  {
    name: "深圳湾大桥",
    type: "界标 · Shenzhen Bay Bridge",
    meta: "连接深圳与香港",
    desc: "横跨深圳湾的海上大桥，是粤港澳大湾区融合的影像符号。",
    tall: false,
  },
  {
    name: "莲花山公园",
    type: "生态 · Lianhua Mountain",
    meta: "俯瞰城市绿肺",
    desc: "市民休闲的城市客厅，山顶可远眺CBD全景，见证城市生长的轨迹。",
    tall: false,
  },
  {
    name: "大梅沙海滨",
    type: "海岸 · Dameisha Beach",
    meta: "东部滨海度假区",
    desc: "金色的沙滩与蔚蓝海域，是深圳人工作之余的慢生活注脚。",
    tall: false,
  },
];

const districts = [
  {
    name: "南山",
    tagline: "科技创新核心",
    desc: "GDP超8000亿元的科技高地，腾讯、大疆、中兴云集，聚集全国最多的独角兽。",
  },
  {
    name: "福田",
    tagline: "金融与行政中心",
    desc: "平安金融中心、深圳证交所所在地，CBD商务氛围浓厚，现代建筑密度全球罕见。",
  },
  {
    name: "罗湖",
    tagline: "改革开放起点",
    desc: "深圳的发源地，国贸与京基100见证特区历史，是商贸年代的第一站。",
  },
  {
    name: "宝安/光明",
    tagline: "智造与科学城",
    desc: "先进制造和科学城在此生长，大族激光等智造企业产业聚集区。",
  },
  {
    name: "盐田/龙岗",
    tagline: "港口与制造腹地",
    desc: "全球繁忙的集装箱干港盐田港，与智能制造和高校集群的龙岗相互映衬。",
  },
];

const customs = [
  {
    title: "沙河 / 蛇口",
    clip: "来自五湖四海的深漂",
    desc: "前身是招商局南海的蛇口，这里是时间就是金钱、效率就是生命口号的诞生地。",
    tone: "warm",
  },
  {
    title: "夜经济 · 东门老街",
    clip: "烟火气与市井记忆",
    desc: "深圳最古老的商业街之一，潮牌、美食和霓虹交织出最接地气的城市脉络。",
    tone: "cool",
  },
  {
    title: "世界之窗 · 锦绣中华",
    clip: "微缩世界的主题乐园",
    desc: "浓缩全球地标与中华美景的主题公园，旅行者的深圳记忆之一。",
    tone: "warm",
  },
];

/* ---------- helpers ---------- */
const heroSkyline = Array.from({ length: 40 }, (_, i) => {
  const h = 30 + ((i * 47) % 120);
  const l = i * 10;
  return { h, l, id: i };
});

function App() {
  return (
    <div className="min-h-screen bg-[#0b0f14] text-zinc-100 antialiased selection:bg-amber-400 selection:text-black">
      {/* ============= NAV ============= */}
      <nav className="sticky top-0 z-50 border-b border-white/10 bg-[#0b0f14]/80 backdrop-blur-md">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <a href="#hero" className="flex items-center gap-2 font-semibold tracking-tight">
            <span className="flex h-8 w-8 items-center justify-center rounded-sm bg-amber-400 text-black">
              深
            </span>
            <span className="text-lg">SHENZHEN</span>
          </a>
          <div className="hidden items-center gap-6 text-sm text-zinc-400 md:flex">
            <a href="#stats" className="transition hover:text-amber-300">城市之最</a>
            <a href="#timeline" className="transition hover:text-amber-300">历史轨迹</a>
            <a href="#tech" className="transition hover:text-amber-300">科技与创新</a>
            <a href="#landmarks" className="transition hover:text-amber-300">地标</a>
            <a href="#districts" className="transition hover:text-amber-300">城区</a>
            <a href="#culinary" className="transition hover:text-amber-300">烟火气</a>
          </div>
          <a
            href="#timeline"
            className="rounded-sm bg-amber-400 px-4 py-2 text-sm font-semibold text-black transition hover:bg-amber-300"
          >
            即刻出发
          </a>
        </div>
      </nav>

      {/* ============= HERO ============= */}
      <header id="hero" className="relative overflow-hidden">
        <div
          className="pointer-events-none absolute inset-0"
          style={{
            background:
              "radial-gradient(120% 60% at 80% -10%, rgba(20,140,140,.35), transparent 55%), radial-gradient(80% 50% at 10% 110%, rgba(230,180,40,.22), transparent 55%)",
          }}
        />
        <div className="absolute inset-x-0 bottom-0 flex h-64 items-end gap-1.5 px-2 opacity-80">
          {heroSkyline.map((b) => (
            <div
              key={b.id}
              className="flex-1 rounded-t-sm bg-gradient-to-t from-amber-400/10 to-teal-300/40"
              style={{ height: `${b.h}%`, opacity: 0.25 + b.h / 150 }}
            />
          ))}
        </div>

        <div className="relative mx-auto flex min-h-[90vh] max-w-6xl flex-col justify-center px-6 py-24">
          <p className="mb-4 inline-flex items-center gap-2 rounded-full border border-white/15 px-3 py-1 text-xs font-medium tracking-wide text-amber-300/90">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />
            中国 · 广东省 · 粤港澳大湾区核心引擎
          </p>
          <h1 className="max-w-4xl text-6xl font-black leading-[0.95] tracking-tight md:text-8xl">
            深圳
            <span className="mt-2 block bg-gradient-to-r from-amber-300 via-teal-200 to-amber-300 bg-clip-text text-transparent">
              — 一座被时间催生的城市
            </span>
          </h1>
          <p className="mt-8 max-w-2xl text-lg leading-relaxed text-zinc-300 md:text-xl">
            四十多年前，这里还只是一片边陲渔村；今天，它是全球科技创新策源地、中国硅谷，
            以深圳速度和设计之都的称号，向世界讲述一个关于想象力的东方故事。
          </p>
          <div className="mt-10 flex flex-wrap gap-4">
            <a
              href="#stats"
              className="group inline-flex items-center gap-2 rounded-sm bg-amber-400 px-6 py-3 font-semibold text-black transition hover:bg-amber-300"
            >
              探索城市之最
              <ArrowDown className="h-4 w-4 transition group-hover:translate-y-0.5" />
            </a>
            <a
              href="#timeline"
              className="inline-flex items-center gap-2 rounded-sm border border-white/20 px-6 py-3 font-semibold text-white transition hover:border-amber-300 hover:text-amber-300"
            >
              看它的历史轨迹
            </a>
          </div>
        </div>
      </header>

      {/* ============= STATS ============= */}
      <section id="stats" className="relative border-t border-white/10 bg-white/[0.02] py-20">
        <div className="mx-auto max-w-6xl px-6">
          <div className="flex items-end justify-between gap-6">
            <div>
              <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
                01 · City Facts
              </p>
              <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">深圳有多猛？</h2>
            </div>
            <p className="hidden max-w-sm text-sm text-zinc-400 md:block">
              一组数字，勾勒出这座睡醒即变的超级城市轮廓。
            </p>
          </div>

          <div className="mt-12 grid grid-cols-2 gap-px overflow-hidden rounded-sm border border-white/10 bg-white/10 sm:grid-cols-3">
            {stats.map((s) => (
              <div key={s.key} className="group bg-[#0d1219] p-6 transition hover:bg-[#131b26] md:p-8">
                <p className="text-xs uppercase tracking-widest text-zinc-500">{s.key}</p>
                <p className="mt-3 text-3xl font-extrabold tracking-tight text-white md:text-4xl">
                  {s.value}
                </p>
                <p className="mt-2 text-sm text-zinc-400">{s.note}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= TIMELINE ============= */}
      <section id="timeline" className="relative py-24">
        <div className="mx-auto max-w-6xl px-6">
          <div className="max-w-2xl">
            <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
              02 · The Story
            </p>
            <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">
              从渔村到世界，只用了一代人的时间
            </h2>
            <p className="mt-4 text-zinc-400">
              这句话浓缩了这座城市的全部性格——只有快，才赶得上时代。
            </p>
          </div>

          <div className="mt-14 border-l-2 border-white/10 pl-8 md:pl-12">
            {timeline.map((t, i) => (
              <div key={t.year} className="relative border-b border-white/5 py-8 last:border-0">
                <span className="absolute -left-[42px] flex h-6 w-6 items-center justify-center rounded-full border-2 border-amber-400 bg-[#0b0f14] text-[10px] text-amber-300 md:-left-[58px]">
                  {i + 1}
                </span>
                <div className="flex flex-wrap items-baseline gap-x-4">
                  <span className="bg-amber-400/10 px-2 py-0.5 font-mono text-sm font-bold text-amber-300">
                    {t.year}
                  </span>
                  <h3 className="text-xl font-semibold md:text-2xl">{t.title}</h3>
                </div>
                <p className="mt-3 max-w-2xl text-zinc-400">{t.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= TECH ============= */}
      <section
        id="tech"
        className="relative border-t border-white/10 bg-gradient-to-br from-[#0a1a1a] to-[#12160f] py-24"
      >
        <div className="mx-auto max-w-6xl px-6">
          <div className="grid items-end gap-6 md:grid-cols-2">
            <div>
              <p className="text-sm font-semibold uppercase tracking-[0.25em] text-teal-300">
                03 · Innovation Engine
              </p>
              <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">中国硅谷与其巨头</h2>
            </div>
            <p className="text-zinc-400 md:text-right">
              华为、腾讯、大疆、比亚迪、中兴、华大基因……这些名字共同定义了一座城市的创新浓度。
            </p>
          </div>

          <div className="mt-12 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            {techs.map((t) => (
              <div
                key={t.title}
                className="group relative overflow-hidden rounded-sm border border-white/10 bg-white/[0.03] p-7 transition hover:-translate-y-1 hover:border-teal-300/50 hover:bg-white/[0.06]"
              >
                <t.icon className="h-8 w-8 text-teal-300 transition group-hover:text-amber-300" />
                <h3 className="mt-5 text-xl font-semibold">{t.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-zinc-400">{t.desc}</p>
                <div className="absolute -right-4 -top-4 h-16 w-16 rounded-full bg-teal-300/5 blur-xl transition group-hover:bg-amber-300/10" />
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= LANDMARKS ============= */}
      <section id="landmarks" className="relative py-24">
        <div className="mx-auto max-w-6xl px-6">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
              04 · Landmarks
            </p>
            <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">一座被天际线定义的城市</h2>
          </div>

          <div className="mt-12 grid gap-px overflow-hidden rounded-sm border border-white/10 bg-white/10 md:grid-cols-2">
            {landmarks.map((l, i) => (
              <div key={l.name} className="group relative overflow-hidden bg-[#0d1219] p-10">
                <div
                  className="absolute -bottom-10 -right-6 select-none text-[9rem] font-black leading-none text-white/[0.03] transition group-hover:text-amber-400/10"
                  aria-hidden
                >
                  {String(i + 1).padStart(2, "0")}
                </div>
                <div className={`flex items-end gap-3 ${l.tall ? "min-h-[150px]" : ""}`}>
                  {l.tall && (
                    <div className="flex h-[150px] items-start gap-1">
                      {[0, 1, 2].map((k) => (
                        <div
                          key={k}
                          className="w-3 rounded-t-sm bg-amber-400/30"
                          style={{ height: `${70 + k * 30}%` }}
                        />
                      ))}
                    </div>
                  )}
                  <div>
                    <span className="text-xs uppercase tracking-widest text-zinc-500">{l.type}</span>
                    <h3 className="mt-2 text-2xl font-bold">{l.name}</h3>
                    <p className="mt-1 text-sm font-semibold text-amber-300/80">{l.meta}</p>
                  </div>
                </div>
                <p className="mt-6 text-zinc-400">{l.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= DISTRICTS ============= */}
      <section id="districts" className="border-t border-white/10 bg-white/[0.02] py-24">
        <div className="mx-auto max-w-6xl px-6">
          <div className="max-w-2xl">
            <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
              05 · Districts
            </p>
            <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">每一区，都是一座城</h2>
          </div>

          <div className="mt-12 grid gap-px overflow-hidden rounded-sm border border-white/10 bg-white/10">
            {districts.map((d, i) => (
              <div
                key={d.name}
                className="group grid gap-2 bg-[#0d1219] px-6 py-6 transition hover:bg-[#131b26] md:grid-cols-[120px_200px_1fr] md:items-center md:gap-8"
              >
                <span className="font-mono text-2xl font-black text-white/10 transition group-hover:text-amber-400">
                  {String(i + 1).padStart(2, "0")}
                </span>
                <div>
                  <h3 className="text-2xl font-bold">{d.name}</h3>
                  <p className="text-xs font-semibold uppercase tracking-wide text-teal-300">
                    {d.tagline}
                  </p>
                </div>
                <p className="text-zinc-400">{d.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= CULINARY / CULTURE ============= */}
      <section id="culinary" className="relative py-24">
        <div className="mx-auto max-w-6xl px-6">
          <div className="grid items-end gap-6 md:grid-cols-2">
            <div>
              <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
                06 · Life &amp; Culture
              </p>
              <h2 className="mt-2 text-4xl font-bold tracking-tight md:text-5xl">烟火气里的深圳</h2>
            </div>
            <p className="text-zinc-400 md:text-right">
              在快节奏的都市叙事之外，深圳也有老街霓虹、海风沙滩与来自全国的美食江湖。
            </p>
          </div>

          <div className="mt-12 grid gap-6 md:grid-cols-3">
            {customs.map((c) => (
              <div
                key={c.title}
                className={`rounded-sm border p-8 transition ${
                  c.tone === "warm"
                    ? "border-amber-400/20 bg-amber-400/[0.04] hover:border-amber-300/60"
                    : "border-teal-300/20 bg-teal-400/[0.04] hover:border-teal-300/60"
                }`}
              >
                <Sparkles
                  className={`h-6 w-6 ${c.tone === "warm" ? "text-amber-300" : "text-teal-300"}`}
                />
                <h3 className="mt-5 text-xl font-semibold">{c.title}</h3>
                <p className="mt-1 text-sm font-medium text-zinc-500">{c.clip}</p>
                <p className="mt-4 text-sm leading-relaxed text-zinc-300">{c.desc}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ============= CTA / FOOTER ============= */}
      <footer className="relative overflow-hidden border-t border-white/10">
        <div
          className="pointer-events-none absolute inset-0"
          style={{
            background:
              "radial-gradient(70% 90% at 50% 120%, rgba(230,180,40,.18), transparent 60%)",
          }}
        />
        <div className="relative mx-auto max-w-6xl px-6 py-20 text-center">
          <p className="text-sm font-semibold uppercase tracking-[0.25em] text-amber-400">
            欢迎来到中国硅谷
          </p>
          <h2 className="mx-auto mt-4 max-w-3xl text-4xl font-black leading-tight tracking-tight md:text-6xl">
            来了，就是深圳人。
          </h2>
          <p className="mx-auto mt-6 max-w-xl text-zinc-400">
            这座城市从不设限，它向每一个敢于想象的人敞开大门。下一次旅途，不妨把深圳写进行程。
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
            <button
              onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
              className="inline-flex items-center gap-2 rounded-sm bg-amber-400 px-7 py-3 font-semibold text-black transition hover:bg-amber-300"
            >
              回到顶部
              <ArrowDown className="h-4 w-4 rotate-180" />
            </button>
          </div>
          <div className="mt-14 flex flex-wrap items-center justify-center gap-x-8 gap-y-3 text-xs text-zinc-500">
            <span className="inline-flex items-center gap-1.5">
              <Ship className="h-4 w-4" /> 大鹏湾 · 面向南海
            </span>
            <span className="inline-flex items-center gap-1.5">
              <Diamond className="h-4 w-4" /> 设计之都 · 联合国教科文组织认证
            </span>
            <span className="inline-flex items-center gap-1.5">
              <TreePine className="h-4 w-4" /> 千园之城 · 森林公园环绕
            </span>
            <span className="inline-flex items-center gap-1.5">
              <Droplets className="h-4 w-4" /> 海洋气质的移民城市
            </span>
          </div>
          <div className="mt-8 text-xs text-zinc-600">CHINA · GUANGDONG · SHENZHEN — 深圳 · 中国硅谷</div>
        </div>
      </footer>
    </div>
  );
}

export default App;

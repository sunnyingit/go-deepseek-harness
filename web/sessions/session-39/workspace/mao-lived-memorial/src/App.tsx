import { useState, useEffect } from 'react'
import './App.css'

/* ============================ 数据区 ============================ */

const milestones = [
  {
    year: '1893',
    title: '诞生于韶山',
    text: '12月26日，出生于湖南省湘潭县韶山冲一个农民家庭，是近代中国从积贫积弱走向独立自强的时代之子。',
    side: 'left',
  },
  {
    year: '1911–1918',
    title: '求学立志',
    text: '就读于长沙湖南第一师范，结识蔡和森、向警予等志士，立下"改造中国与世界"的远大抱负，主张"文明其精神，野蛮其体魄"。',
    side: 'right',
  },
  {
    year: '1918–1921',
    title: '投身革命启蒙',
    text: '受十月革命与新文化运动影响，在长沙创立新民学会，创办《湘江评论》，走上马克思主义道路。1921年参加中共一大，成为中国共产党创始人之一。',
    side: 'left',
  },
  {
    year: '1921–1927',
    title: '工农运动与革命道路探索',
    text: '深入调查研究，发表《中国社会各阶级的分析》；参加大革命，任中央农民运动讲习所所长，推动农民运动发展。提出"枪杆子里面出政权"的科学论断。',
    side: 'right',
  },
  {
    year: '1927–1935',
    title: '开辟革命根据地',
    text: '1927年领导秋收起义，创建井冈山革命根据地，开辟"农村包围城市、武装夺取政权"的中国革命新道路。领导四次反"围剿"，长征途中在遵义会议上确立领导地位，挽救了中国革命。',
    side: 'left',
  },
  {
    year: '1935–1945',
    title: '领导抗日战争',
    text: '发表《论持久战》，指明抗战胜利方向；领导八路军、新四军开辟敌后战场，为全民族抗战作出不可磨灭的贡献。整风运动与《新民主主义论》为党的建设和中国前途指明方向。',
    side: 'right',
  },
  {
    year: '1945–1949',
    title: '夺取全国胜利',
    text: '领导解放战争，指挥辽沈、淮海、平津三大战役；发表《论人民民主专政》。1949年10月1日在天安门城楼庄严宣告：中华人民共和国中央人民政府成立。',
    side: 'left',
  },
  {
    year: '1949–1957',
    title: '建设新中国',
    text: '领导制定新中国各项方针政策，完成土地改革，进行社会主义改造，建立社会主义制度；领导抗美援朝、保家卫国，奠定新中国国家与外交基础。',
    side: 'right',
  },
  {
    year: '1957–1976',
    title: '晚年的探索与局限',
    text: '领导社会主义革命和建设的艰辛探索，取得重大成就，也有失误和曲折。1976年9月9日在北京逝世，享年83岁，百万群众自发悼念。',
    side: 'left',
  },
]

const achievements = [
  {
    icon: '🏛️',
    title: '缔造新中国',
    desc: '领导人民经过28年艰苦奋斗，推翻了帝国主义、封建主义、官僚资本主义三座大山，建立了中华人民共和国，实现民族独立和人民解放。',
  },
  {
    icon: '🗺️',
    title: '开辟革命新道路',
    desc: '创造性地把马克思主义基本原理同中国具体实际相结合，开辟"农村包围城市、武装夺取政权"的正确革命道路，形成毛泽东思想。',
  },
  {
    icon: '✊',
    title: '奠基社会主义',
    desc: '领导完成社会主义改造，确立社会主义基本制度，建立起独立的比较完整的工业体系和国民经济体系，奠定现代化建设的物质基础。',
  },
  {
    icon: '📜',
    title: '著书立说',
    desc: '撰写《矛盾论》《实践论》《论持久战》《新民主主义论》等经典著作，其诗词气势磅礴、意境壮阔，是中华文化的瑰宝。',
  },
]

const quotes = [
  '星星之火，可以燎原',
  '恰同学少年，风华正茂',
  '数风流人物，还看今朝',
  '世上无难事，只要肯登攀',
  '为人民服务',
  '红军不怕远征难，万水千山只等闲',
  '雄关漫道真如铁，而今迈步从头越',
  '一万年太久，只争朝夕',
]

const poems = [
  {
    title: '沁园春·雪',
    year: '1936年',
    body: `北国风光，千里冰封，万里雪飘。\n望长城内外，惟余莽莽；大河上下，顿失滔滔。\n山舞银蛇，原驰蜡象，欲与天公试比高。\n须晴日，看红装素裹，分外妖娆。\n\n江山如此多娇，引无数英雄竞折腰。\n惜秦皇汉武，略输文采；唐宗宋祖，稍逊风骚。\n一代天骄，成吉思汗，只识弯弓射大雕。\n俱往矣，数风流人物，还看今朝。`,
  },
  {
    title: '七律·长征',
    year: '1935年',
    body: `红军不怕远征难，万水千山只等闲。\n五岭逶迤腾细浪，乌蒙磅礴走泥丸。\n金沙水拍云崖暖，大渡桥横铁索寒。\n更喜岷山千里雪，三军过后尽开颜。`,
  },
  {
    title: '卜算子·咏梅',
    year: '1961年',
    body: `风雨送春归，飞雪迎春到。\n已是悬崖百丈冰，犹有花枝俏。\n\n俏也不争春，只把春来报。\n待到山花烂漫时，她在丛中笑。`,
  },
]

const titles = ['革命家', '政治家', '军事家', '文学家', '书法家', '诗人']

/* ============================ 组件区 ============================ */

function App() {
  const [progress, setProgress] = useState(0)

  useEffect(() => {
    const onScroll = () => {
      const h = document.documentElement
      const total = h.scrollHeight - h.clientHeight
      setProgress(total > 0 ? (h.scrollTop / total) * 100 : 0)
    }
    window.addEventListener('scroll', onScroll)
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  return (
    <div className="page">
      {/* 阅读进度 */}
      <div className="progressbar" style={{ width: `${progress}%` }} />

      {/* ===== Hero ===== */}
      <header className="hero">
        <div className="hero__grain" />
        <div className="hero__content">
          <p className="hero__sub">一八九三 · 一九七六 · 人民领袖</p>
          <h1 className="hero__title">毛 泽 东</h1>
          <p className="hero__subsub">伟大的无产阶级革命家、战略家、理论家</p>
          <div className="hero__tags">
            {titles.map((t) => (
              <span key={t} className="hero__tag">{t}</span>
            ))}
          </div>
          <div className="hero__rule" />
          <p className="hero__epitaph">
            "为有牺牲多壮志，敢教日月换新天。"
          </p>
          <a href="#timeline" className="hero__cta">走进生平 ↘</a>
        </div>
      </header>

      {/* ===== 引言 ===== */}
      <section className="intro">
        <p className="intro__text">
          毛泽东同志是伟大的马克思主义者，伟大的无产阶级革命家、战略家和理论家，是近代以来中国伟大的爱国者和民族英雄，是领导中国人民彻底改变自己命运和国家面貌的伟人。
        </p>
        <div className="intro__sig">—— 序言</div>
      </section>

      {/* ===== 时间线 ===== */}
      <section id="timeline" className="timeline">
        <h2 className="section__title">生 平 · 纪 事</h2>
        <p className="section__lead">从韶山冲到天安门，一部长长的奋斗史</p>

        <div className="timeline__rail">
          {milestones.map((m, i) => (
            <div className={`timeline__item timeline__item--${m.side}`} key={i}>
              <div className="timeline__dot" />
              <div className="timeline__card">
                <span className="timeline__year">{m.year}</span>
                <h3 className="timeline__h">{m.title}</h3>
                <p className="timeline__p">{m.text}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* ===== 功绩 ===== */}
      <section className="achievements">
        <h2 className="section__title section__title--light">主 要 · 功 绩</h2>
        <p className="section__lead section__lead--light">一生奋斗，功在千秋</p>
        <div className="achievements__grid">
          {achievements.map((a, i) => (
            <article className="achievement__card" key={i}>
              <div className="achievement__icon">{a.icon}</div>
              <h3 className="achievement__h">{a.title}</h3>
              <p className="achievement__p">{a.desc}</p>
            </article>
          ))}
        </div>
      </section>

      {/* ===== 名言 ===== */}
      <section className="quotes">
        <h2 className="section__title">名 言 · 名 句</h2>
        <div className="quotes__grid">
          {quotes.map((q, i) => (
            <blockquote className="quote__card" key={i}>
              <span className="quote__mark">{i % 3 === 0 ? '“' : '“'}</span>
              {q}
            </blockquote>
          ))}
        </div>
      </section>

      {/* ===== 诗词 ===== */}
      <section className="poems">
        <h2 className="section__title">豪 情 · 诗 词</h2>
        <p className="section__lead">文章千古事，诗词照丹心</p>
        <div className="poems__list">
          {poems.map((p, i) => (
            <article className="poem__block" key={i}>
              <div className="poem__head">
                <h3 className="poem__title">{p.title}</h3>
                <span className="poem__year">{p.year}</span>
              </div>
              <pre className="poem__body">{p.body}</pre>
            </article>
          ))}
        </div>
      </section>

      {/* ===== 结尾 ===== */}
      <footer className="foot">
        <p className="foot__great">数风流人物，还看今朝</p>
        <p className="foot__note">1893 — 1976 · 纪念毛泽东同志诞辰</p>
        <div className="foot__rule" />
        <p className="foot__tiny">斯人已逝，精神永存 · 为人民服务</p>
      </footer>
    </div>
  )
}

export default App

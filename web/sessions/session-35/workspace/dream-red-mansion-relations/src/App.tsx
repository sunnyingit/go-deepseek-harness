import { useEffect, useMemo, useRef, useState } from 'react'
import { characters, relations, groups } from './data'
import './App.css'

const W = 1200
const H = 760

export default function App() {
  const [selected, setSelected] = useState<string | null>(null)
  const [hover, setHover] = useState<string | null>(null)

  const selectedChar = selected ? characters.find((c) => c.id === selected) : null

  const activeIds = useMemo(() => {
    if (!selected) return new Set<string>()
    const s = new Set<string>([selected])
    relations.forEach((r) => {
      if (r.from === selected) s.add(r.to)
      if (r.to === selected) s.add(r.from)
    })
    return s
  }, [selected])

  return (
    <div className="stage">
      {/* 左侧竖排标题 */}
      <div className="title-col">
        <div className="big-vert">红楼梦</div>
        <div className="sub-vert">人物关系图</div>
        <div className="rule" />
        <p className="note">点击一位人物，即可点亮它与其直接的纠葛关系</p>
        <div className="legend">
          {groups.map((g) => (
            <div key={g.key} className="legend-item">
              <span className="dot" style={{ background: g.color }} />
              <span className="lg-name">{g.name}</span>
            </div>
          ))}
        </div>
      </div>

      {/* 关系画布 */}
      <div className="canvas-wrap">
        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="canvas"
          onClick={() => setSelected(null)}
        >
          {/* 连线 */}
          {relations.map((r, i) => {
            const a = characters.find((c) => c.id === r.from)!
            const b = characters.find((c) => c.id === r.to)!
            const lit =
              activeIds.has(r.from) && activeIds.has(r.to)
            const hov =
              (hover === r.from || hover === r.to) &&
              (selected === null || selected === r.from || selected === r.to)
            const mx = (a.x + b.x) / 2
            const my = (a.y + b.y) / 2
            return (
              <line
                key={i}
                x1={a.x}
                y1={a.y}
                x2={b.x}
                y2={b.y}
                className={lit ? 'edge lit' : hov ? 'edge hovered' : 'edge'}
              />
            )
          })}
          {/* 连线标签 */}
          {relations.map((r, i) => {
            const a = characters.find((c) => c.id === r.from)!
            const b = characters.find((c) => c.id === r.to)!
            const lit = activeIds.has(r.from) && activeIds.has(r.to)
            const mx = (a.x + b.x) / 2
            const my = (a.y + b.y) / 2
            return (
              <text
                key={`l${i}`}
                x={mx}
                y={my}
                className={lit ? 'edge-label lit' : 'edge-label'}
              >
                {r.label}
              </text>
            )
          })}

          {/* 人物节点 */}
          {characters.map((c) => {
            const active = selected === null || activeIds.has(c.id)
            const isSel = selected === c.id
            const isDim = selected !== null && !activeIds.has(c.id)
            return (
              <g
                key={c.id}
                className={active ? 'char' : 'char dim'}
                onClick={(e) => {
                  e.stopPropagation()
                  setSelected(c.id)
                }}
                onMouseEnter={() => setHover(c.id)}
                onMouseLeave={() => setHover(null)}
              >
                <circle
                  r={isSel ? 30 : 24}
                  cx={c.x}
                  cy={c.y}
                  fill={c.color}
                  fillOpacity={isSel ? 0.9 : 0.85}
                  className="node-ring"
                />
                <text x={c.x} y={c.y} textAnchor="middle" className="char-name">
                  {c.name.length === 1 ? c.name + '　' : c.name.length === 2 ? c.name : c.name}
                </text>
                <text
                  x={c.x}
                  y={c.y + 44}
                  textAnchor="middle"
                  className="char-title"
                >
                  {c.title}
                </text>
              </g>
            )
          })}
        </svg>
      </div>

      {/* 人物详情卡 */}
      {selectedChar && (
        <div className="detail-card">
          <button className="close" onClick={() => setSelected(null)}>
            ×
          </button>
          <div
            className="dc-color"
            style={{ background: selectedChar.color }}
          />
          <div className="dc-body">
            <div className="dc-name">{selectedChar.name}</div>
            <div className="dc-title">{selectedChar.title}</div>
            <p className="dc-desc">{selectedChar.desc}</p>
            <div className="dc-links">
              {relations
                .filter((r) => r.from === selectedChar.id || r.to === selectedChar.id)
                .map((r, i) => {
                  const otherId = r.from === selectedChar.id ? r.to : r.from
                  const other = characters.find((c) => c.id === otherId)!
                  return (
                    <button
                      key={i}
                      className="link-chip"
                      onClick={(e) => {
                        e.stopPropagation()
                        setSelected(otherId)
                      }}
                    >
                      <span className="chip-dot" style={{ background: other.color }} />
                      {other.name} <em>· {r.label}</em>
                    </button>
                  )
                })}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

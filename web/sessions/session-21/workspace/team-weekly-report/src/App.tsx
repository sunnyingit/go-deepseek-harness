import { useEffect, useMemo, useState } from 'react'

/* ---------------- 类型 ---------------- */
type RiskLevel = '高' | '中' | '低'

interface Member {
  id: string
  name: string
  role: string
  done: string // 本周完成
  doing: string // 进行中
  next: string // 下周计划
}

interface Risk {
  id: string
  level: RiskLevel
  desc: string
  mitigation: string
}

interface Task {
  id: string
  text: string
  owner: string
  status: '待办' | '进行中' | '已完成'
}

/* ---------------- 常量 ---------------- */
const LEVEL_COLORS: Record<RiskLevel, string> = {
  高: 'bg-red-50 border-red-300 text-red-700',
  中: 'bg-amber-50 border-amber-300 text-amber-700',
  低: 'bg-emerald-50 border-emerald-300 text-emerald-700',
}

const STATUS_COLORS: Record<Task['status'], string> = {
  待办: 'bg-slate-100 text-slate-600',
  进行中: 'bg-sky-50 text-sky-600',
  已完成: 'bg-emerald-50 text-emerald-600',
}

const STATUS_OPTIONS: Task['status'][] = ['待办', '进行中', '已完成']

let uid = 100
const genId = () => `id-${++uid}-${Date.now().toString(36)}`

/* ---------------- 小组件 ---------------- */
function Section({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return (
    <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
      <header className="flex items-baseline justify-between border-b border-slate-100 px-5 py-4">
        <div>
          <h2 className="text-base font-semibold text-slate-800">{title}</h2>
          {subtitle && <p className="mt-0.5 text-xs text-slate-400">{subtitle}</p>}
        </div>
      </header>
      <div className="px-5 py-4">{children}</div>
    </section>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-slate-500">{label}</span>
      {children}
    </label>
  )
}

const textareaCls =
  'w-full resize-none rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm leading-relaxed text-slate-700 placeholder:text-slate-300 focus:border-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-100'

/* ---------------- 主组件 ---------------- */
export default function App() {
  const weekLabel = useMemo(() => {
    const now = new Date()
    const monday = new Date(now)
    const day = now.getDay() || 7
    monday.setDate(now.getDate() - day + 1)
    const fmt = (d: Date) => `${d.getMonth() + 1}月${d.getDate()}日`
    return `${fmt(monday)} – ${fmt(new Date(monday.getTime() + 4 * 86400000))}`
  }, [])

  const [members, setMembers] = useState<Member[]>([
    { id: genId(), name: '林晓', role: '前端工程师', done: '', doing: '', next: '' },
    { id: genId(), name: '陈浩', role: '后端工程师', done: '', doing: '', next: '' },
    { id: genId(), name: '王倩', role: '产品经理', done: '', doing: '', next: '' },
    { id: genId(), name: '赵磊', role: '测试工程师', done: '', doing: '', next: '' },
  ])
  const [tasks, setTasks] = useState<Task[]>([])
  const [risks, setRisks] = useState<Risk[]>([])
  const [teamSummary, setTeamSummary] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const [needHelp, setNeedHelp] = useState('')

  // 持久化草稿
  useEffect(() => {
    const saved = localStorage.getItem('weekly-draft')
    if (!saved) return
    try {
      const d = JSON.parse(saved)
      if (Array.isArray(d.members)) setMembers(d.members)
      if (Array.isArray(d.tasks)) setTasks(d.tasks)
      if (Array.isArray(d.risks)) setRisks(d.risks)
      if (typeof d.teamSummary === 'string') setTeamSummary(d.teamSummary)
      if (typeof d.needHelp === 'string') setNeedHelp(d.needHelp)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    localStorage.setItem(
      'weekly-draft',
      JSON.stringify({ members, tasks, risks, teamSummary, needHelp }),
    )
  }, [members, tasks, risks, teamSummary, needHelp])

  /* 成员字段更新 */
  const setMember = (id: string, patch: Partial<Member>) =>
    setMembers((ms) => ms.map((m) => (m.id === id ? { ...m, ...patch } : m)))

  const addMember = () => {
    if (members.length >= 8) return
    setMembers((ms) => [...ms, { id: genId(), name: '', role: '', done: '', doing: '', next: '' }])
  }
  const removeMember = (id: string) => setMembers((ms) => ms.filter((m) => m.id !== id))

  /* 任务 */
  const [taskText, setTaskText] = useState('')
  const [taskOwner, setTaskOwner] = useState('')
  const addTask = () => {
    const text = taskText.trim()
    if (!text) return
    setTasks((ts) => [...ts, { id: genId(), text, owner: taskOwner.trim() || '未指派', status: '待办' }])
    setTaskText('')
    setTaskOwner('')
  }
  const setTaskStatus = (id: string, status: Task['status']) =>
    setTasks((ts) => ts.map((t) => (t.id === id ? { ...t, status } : t)))
  const removeTask = (id: string) => setTasks((ts) => ts.filter((t) => t.id !== id))

  /* 风险 */
  const [riskLevel, setRiskLevel] = useState<RiskLevel>('中')
  const [riskDesc, setRiskDesc] = useState('')
  const [riskMitigation, setRiskMitigation] = useState('')
  const addRisk = () => {
    if (!riskDesc.trim()) return
    setRisks((rs) => [{ id: genId(), level: riskLevel, desc: riskDesc.trim(), mitigation: riskMitigation.trim() }, ...rs])
    setRiskDesc('')
    setRiskMitigation('')
    setRiskLevel('中')
  }
  const removeRisk = (id: string) => setRisks((rs) => rs.filter((r) => r.id !== id))

  /* 统计 */
  const nameCount = members.filter((m) => m.name.trim()).length
  const filledCount = members.filter((m) => m.name.trim() && m.done.trim()).length
  const doneTasks = tasks.filter((t) => t.status === '已完成').length
  const highRisk = risks.filter((r) => r.level === '高').length
  const progressPct = nameCount === 0 ? 0 : Math.round((filledCount / nameCount) * 100)

  const handleSubmit = () => {
    setSubmitted(true)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-800">
      {/* 顶部带 */}
      <header className="border-b border-slate-200 bg-slate-900 text-slate-100">
        <div className="mx-auto max-w-5xl px-6 py-8">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="text-xs uppercase tracking-widest text-slate-400">团队周报</p>
              <h1 className="mt-1 text-2xl font-bold">创新产品组 · 本周工作汇报</h1>
              <p className="mt-2 text-sm text-slate-300">
                汇报周期 <span className="font-medium text-slate-100">{weekLabel}</span>
              </p>
            </div>
            <div className="text-right">
              <p className="text-sm text-slate-400">
                成员进度 <span className="text-xl font-semibold text-slate-100">{nameCount ? `${filledCount}/${nameCount}` : `0`}</span>
              </p>
              <div className="mt-2 h-1.5 w-44 overflow-hidden rounded-full bg-slate-700">
                <div
                  className="h-full rounded-full bg-emerald-400 transition-all"
                  style={{ width: `${progressPct}%` }}
                />
              </div>
            </div>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        {/* 提交成功提示 */}
        {submitted && (
          <div className="flex items-start gap-3 rounded-xl border border-emerald-300 bg-emerald-50 px-5 py-4 text-sm text-emerald-800">
            <span className="text-lg leading-none">✓</span>
            <div>
              <p className="font-semibold">周报已提交</p>
              <p className="mt-0.5 text-emerald-700">
                本次 {nameCount} 名成员共完成 {doneTasks} 项任务，登记 {risks.length} 项风险。已模拟发送给 leader，可在下方继续编辑后再次提交。
              </p>
            </div>
            <button
              onClick={() => setSubmitted(false)}
              className="ml-auto rounded-lg border border-emerald-300 px-3 py-1 text-xs text-emerald-700 hover:bg-emerald-100"
            >
              知道了
            </button>
          </div>
        )}

        {/* 成员进度 */}
        <Section
          title="成员工作进度"
          subtitle="每位成员填写本周完成、进行中与下周计划，标 * 为必填项。"
        >
          <div className="space-y-4">
            {members.map((m) => (
              <div key={m.id} className="rounded-lg border border-slate-200 bg-slate-50/60 p-4">
                <div className="mb-3 flex flex-wrap items-center gap-3">
                  <input
                    className="w-32 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 placeholder:text-slate-300 focus:border-slate-400 focus:outline-none"
                    value={m.name}
                    placeholder="成员姓名"
                    onChange={(e) => setMember(m.id, { name: e.target.value })}
                  />
                  <input
                    className="w-36 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-500 placeholder:text-slate-300 focus:border-slate-400 focus:outline-none"
                    value={m.role}
                    placeholder="角色 / 职位"
                    onChange={(e) => setMember(m.id, { role: e.target.value })}
                  />
                  {members.length > 1 && (
                    <button
                      onClick={() => removeMember(m.id)}
                      className="ml-auto inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-slate-400 hover:bg-red-50 hover:text-red-600"
                    >
                      ✕ 移除
                    </button>
                  )}
                </div>
                <div className="grid gap-3 md:grid-cols-3">
                  <Field label="本周完成 *">
                    <textarea
                      className={textareaCls}
                      rows={3}
                      value={m.done}
                      placeholder="已完成的关键事项……"
                      onChange={(e) => setMember(m.id, { done: e.target.value })}
                    />
                  </Field>
                  <Field label="进行中">
                    <textarea
                      className={textareaCls}
                      rows={3}
                      value={m.doing}
                      placeholder="正在推进的工作……"
                      onChange={(e) => setMember(m.id, { doing: e.target.value })}
                    />
                  </Field>
                  <Field label="下周计划">
                    <textarea
                      className={textareaCls}
                      rows={3}
                      value={m.next}
                      placeholder="下周的安排……"
                      onChange={(e) => setMember(m.id, { next: e.target.value })}
                    />
                  </Field>
                </div>
              </div>
            ))}
          </div>
          {members.length < 8 && (
            <button
              onClick={addMember}
              className="mt-4 inline-flex items-center gap-2 rounded-lg border border-dashed border-slate-300 px-4 py-2 text-sm text-slate-500 hover:border-slate-400 hover:text-slate-700"
            >
              ＋ 添加成员
            </button>
          )}
        </Section>

        {/* 周任务清单 */}
        <Section
          title="本周任务清单"
          subtitle={`共 ${tasks.length} 项，已完成 ${doneTasks} 项。`}
        >
          <div className="mb-4 flex flex-wrap gap-2">
            <input
              className="min-w-0 flex-1 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-100"
              placeholder="任务内容，例如：完成登录模块联调"
              value={taskText}
              onChange={(e) => setTaskText(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && addTask()}
            />
            <input
              className="w-28 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-slate-400 focus:outline-none focus:ring-2 focus:ring-slate-100"
              placeholder="负责人"
              value={taskOwner}
              onChange={(e) => setTaskOwner(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && addTask()}
            />
            <button
              onClick={addTask}
              className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
            >
              添加
            </button>
          </div>
          <ul className="space-y-2">
            {tasks.map((t) => (
              <li
                key={t.id}
                className="flex flex-wrap items-center gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2.5"
              >
                <select
                  className={`rounded-full border px-2 py-0.5 text-xs font-medium focus:outline-none ${STATUS_COLORS[t.status]}`}
                  value={t.status}
                  onChange={(e) => setTaskStatus(t.id, e.target.value as Task['status'])}
                >
                  {STATUS_OPTIONS.map((s) => (
                    <option key={s} value={s}>
                      {s}
                    </option>
                  ))}
                </select>
                <span className={`flex-1 text-sm ${t.status === '已完成' ? 'text-slate-400 line-through' : 'text-slate-700'}`}>
                  {t.text}
                </span>
                <span className="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-500">{t.owner}</span>
                <button
                  onClick={() => removeTask(t.id)}
                  className="text-xs text-slate-300 hover:text-red-500"
                  aria-label="删除任务"
                >
                  ✕
                </button>
              </li>
            ))}
            {tasks.length === 0 && (
              <li className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-center text-sm text-slate-400">
                还没有任务，先在下方添加一项吧。
              </li>
            )}
          </ul>
        </Section>

        {/* 风险登记 */}
        <Section title="风险登记" subtitle="记录可能影响交付的风险及应对措施。">
          <div className="mb-4 rounded-lg border border-slate-200 bg-slate-50/60 p-4">
            <div className="mb-3 flex items-center gap-2">
              <span className="text-xs font-medium text-slate-500">风险等级</span>
              <div className="flex gap-1">
                {(['高', '中', '低'] as RiskLevel[]).map((lv) => (
                  <button
                    key={lv}
                    onClick={() => setRiskLevel(lv)}
                    className={`rounded-lg px-3 py-1 text-xs font-medium transition ${
                      riskLevel === lv
                        ? LEVEL_COLORS[lv] + ' border'
                        : 'border border-slate-200 bg-white text-slate-400'
                    }`}
                  >
                    {lv}
                  </button>
                ))}
              </div>
            </div>
            <div className="space-y-2">
              <textarea
                className={textareaCls}
                rows={2}
                placeholder="风险描述，例如：第三方支付接口延迟上线，影响联调排期"
                value={riskDesc}
                onChange={(e) => setRiskDesc(e.target.value)}
              />
              <textarea
                className={textareaCls}
                rows={2}
                placeholder="应对措施 / 计划（可选）"
                value={riskMitigation}
                onChange={(e) => setRiskMitigation(e.target.value)}
              />
            </div>
            <button
              onClick={addRisk}
              className="mt-3 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-700"
            >
              ＋ 添加风险
            </button>
          </div>

          <ul className="space-y-2">
            {risks.map((r) => (
              <li key={r.id} className={`flex flex-wrap items-start gap-3 rounded-lg border px-4 py-3 ${LEVEL_COLORS[r.level]}`}>
                <span className="rounded-full bg-white/70 px-2 py-0.5 text-xs font-semibold">{r.level}</span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm">{r.desc}</p>
                  {r.mitigation && (
                    <p className="mt-1 text-xs opacity-80">
                      <span className="font-medium">应对：</span>
                      {r.mitigation}
                    </p>
                  )}
                </div>
                <button onClick={() => removeRisk(r.id)} className="text-xs opacity-50 hover:opacity-100" aria-label="删除风险">
                  ✕
                </button>
              </li>
            ))}
            {risks.length === 0 && (
              <li className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-center text-sm text-slate-400">
                暂无风险。高风险项目会在此高亮提醒。
              </li>
            )}
          </ul>
          {highRisk > 0 && (
            <p className="mt-3 flex items-center gap-1 text-xs font-medium text-red-600">
              ⚠ 有 {highRisk} 项高风险，提交时请重点关注应对方案。
            </p>
          )}
        </Section>

        {/* 团队总结 */}
        <Section title="团队总结与诉求" subtitle="一句话概括本周进展，以及需要领导协调支持的地方。">
          <Field label="本周整体进展">
            <textarea
              className={textareaCls}
              rows={3}
              value={teamSummary}
              placeholder="例如：核心流程跑通，进入测试联调阶段……"
              onChange={(e) => setTeamSummary(e.target.value)}
            />
          </Field>
          <div className="mt-3">
            <Field label="需要支持 / 资源诉求">
              <textarea
                className={textareaCls}
                rows={2}
                value={needHelp}
                placeholder="例如：希望协调测试环境资源、评估加派人手……"
                onChange={(e) => setNeedHelp(e.target.value)}
              />
            </Field>
          </div>
        </Section>

        {/* 提交栏 */}
        <div className="sticky bottom-4 rounded-xl border border-slate-200 bg-white/90 p-4 shadow-lg backdrop-blur">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <p className="text-xs text-slate-400">
              草稿已自动保存。成员完成数 {filledCount}/{nameCount || 0} · 已完成任务 {doneTasks}/{tasks.length || 0} · 登记风险 {risks.length} 项。
            </p>
            <div className="flex gap-2">
              <button
                onClick={() => {
                  setMembers((ms) => ms.map((m) => ({ ...m, done: '', doing: '', next: '' })))
                  setTasks([])
                  setRisks([])
                  setTeamSummary('')
                  setNeedHelp('')
                }}
                className="rounded-lg border border-slate-200 px-4 py-2 text-sm text-slate-500 hover:bg-slate-50"
              >
                清空重填
              </button>
              <button
                onClick={handleSubmit}
                className="rounded-lg bg-emerald-600 px-6 py-2 text-sm font-semibold text-white shadow-sm hover:bg-emerald-500"
              >
                提交周报
              </button>
            </div>
          </div>
        </div>
      </main>

      <footer className="mx-auto max-w-5xl px-6 pb-8 text-center text-xs text-slate-400">
        本页为演示原型，数据仅保存在本地浏览器。当前会话工作区可交互预览。
      </footer>
    </div>
  )
}

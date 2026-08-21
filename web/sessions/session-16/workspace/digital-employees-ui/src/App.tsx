import { useMemo, useState } from "react";
import {
  Activity,
  Bell,
  Bot,
  ChevronDown,
  CircleGauge,
  CirclePlus,
  FileCode2,
  LayoutGrid,
  MapPin,
  MoreHorizontal,
  Play,
  Rows3,
  Search,
  Settings,
  Settings2,
  SlidersHorizontal,
  Sparkles,
  Square,
  SquareStack,
  UserPlus,
  Users,
  Wand2,
  Zap,
} from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/* ------------------------------- load bar ------------------------------- */

function LoadBar({
  value,
  className,
}: {
  value: number;
  className?: string;
}) {
  const color = value > 80 ? "bg-rose-500" : value > 50 ? "bg-amber-500" : "bg-indigo-500";
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-slate-100", className)}>
      <div
        className={cn("h-full rounded-full transition-all", color)}
        style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
      />
    </div>
  );
}

/* ---------------------------------- types --------------------------------- */

type Status = "online" | "busy" | "offline" | "idle";

interface Employee {
  id: string;
  name: string;
  initials: string;
  role: string;
  team: string;
  department: string;
  location: string;
  status: Status;
  avatar: string;
  skills: string[];
  load: number; // 0-100
  tasks: number;
  rating: number; // 0-5
  kind: "agent" | "copilot" | "human";
}

/* --------------------------------- mock data -------------------------------- */

const EMPLOYEES: Employee[] = [
  {
    id: "e01",
    name: "Athena · HR 助理",
    initials: "AT",
    role: "智能招聘 Agent",
    team: "人力资源中心",
    department: "招聘",
    location: "深圳 · 云端",
    status: "online",
    avatar: "",
    skills: ["简历解析", "面试排程", "候选人挖掘"],
    load: 68,
    tasks: 124,
    rating: 4.9,
    kind: "agent",
  },
  {
    id: "e02",
    name: "Orion · 数据分析师",
    initials: "OR",
    role: "经营分析 Copilot",
    team: "商业智能部",
    department: "数据",
    location: "上海 · 私有云",
    status: "busy",
    avatar: "",
    skills: ["SQL", "可视化", "异常检测"],
    load: 92,
    tasks: 86,
    rating: 4.7,
    kind: "copilot",
  },
  {
    id: "e03",
    name: "Nova · 客服机器人",
    initials: "NV",
    role: "客户服务 Agent",
    team: "客户成功",
    department: "客服",
    location: "北京 · 云端",
    status: "online",
    avatar: "",
    skills: ["语义理解", "工单系统", "多语言"],
    load: 45,
    tasks: 210,
    rating: 4.6,
    kind: "agent",
  },
  {
    id: "e04",
    name: "Elias · 代码审查师",
    initials: "EL",
    role: "研发 QA 助手",
    team: "研发效能",
    department: "工程",
    location: "杭州 · 私有云",
    status: "offline",
    avatar: "",
    skills: ["Code Review", "安全扫描", "测试生成"],
    load: 0,
    tasks: 38,
    rating: 4.4,
    kind: "copilot",
  },
  {
    id: "e05",
    name: "Luna · 财务专员",
    initials: "LU",
    role: "智能报账 Agent",
    team: "财经共享中心",
    department: "财务",
    location: "武汉 · 云端",
    status: "idle",
    avatar: "",
    skills: ["票据识别", "合规校验", "对账"],
    load: 22,
    tasks: 64,
    rating: 4.8,
    kind: "agent",
  },
  {
    id: "e06",
    name: "Vega · 供应链经理",
    initials: "VG",
    role: "供应链 Copilot",
    team: "运营中心",
    department: "供应链",
    location: "东莞 · 混合云",
    status: "online",
    avatar: "",
    skills: ["需求预测", "库存优化", "补货"],
    load: 58,
    tasks: 102,
    rating: 4.7,
    kind: "copilot",
  },
  {
    id: "e07",
    name: "Milo · 内容助手",
    initials: "ML",
    role: "数字化营销 Agent",
    team: "品牌市场部",
    department: "市场",
    location: "成都 · 云端",
    status: "busy",
    avatar: "",
    skills: ["文案生成", "SEO", "社媒投放"],
    load: 77,
    tasks: 156,
    rating: 4.5,
    kind: "agent",
  },
  {
    id: "e08",
    name: "Iris · 安全卫士",
    initials: "IR",
    role: "智能安防 Agent",
    team: "信息安全部",
    department: "安全",
    location: "广州 · 私有云",
    status: "online",
    avatar: "",
    skills: ["威胁检测", "事件响应", "风控"],
    load: 51,
    tasks: 47,
    rating: 4.9,
    kind: "agent",
  },
];

const DEPARTMENTS = ["全部", "人力", "数据", "客服", "工程", "财务", "供应链", "市场", "安全"];

/* ---------------------------------- helpers --------------------------------- */

const statusMeta: Record<
  Status,
  { label: string; dot: string; text: string; bg: string }
> = {
  online: {
    label: "运行中",
    dot: "bg-emerald-500",
    text: "text-emerald-600",
    bg: "bg-emerald-50",
  },
  idle: {
    label: "空闲",
    dot: "bg-amber-400",
    text: "text-amber-600",
    bg: "bg-amber-50",
  },
  busy: {
    label: "忙碌",
    dot: "bg-rose-500",
    text: "text-rose-600",
    bg: "bg-rose-50",
  },
  offline: {
    label: "已下线",
    dot: "bg-slate-300",
    text: "text-slate-500",
    bg: "bg-slate-100",
  },
};

const kindMeta: Record<Employee["kind"], { label: string; icon: typeof Bot; cls: string }> = {
  agent: { label: "智能体", icon: Bot, cls: "text-indigo-600 bg-indigo-50" },
  copilot: { label: "Copilot", icon: Sparkles, cls: "text-blue-600 bg-blue-50" },
  human: { label: "人工", icon: Users, cls: "text-slate-600 bg-slate-100" },
};

function statusColor(status: Status) {
  return statusMeta[status];
}

/* --------------------------------- main app --------------------------------- */

export default function App() {
  const [query, setQuery] = useState("");
  const [dept, setDept] = useState("全部");
  const [statusFilter, setStatusFilter] = useState<"all" | Status>("all");
  const [view, setView] = useState<"grid" | "rows">("grid");

  const filtered = useMemo(() => {
    return EMPLOYEES.filter((e) => {
      const hitQuery =
        !query ||
        e.name.toLowerCase().includes(query.toLowerCase()) ||
        e.role.toLowerCase().includes(query.toLowerCase()) ||
        e.team.toLowerCase().includes(query.toLowerCase()) ||
        e.skills.some((s) => s.toLowerCase().includes(query.toLowerCase()));
      const hitDept = dept === "全部" || e.department === dept;
      const hitStatus = statusFilter === "all" || e.status === statusFilter;
      return hitQuery && hitDept && hitStatus;
    });
  }, [query, dept, statusFilter]);

  const total = EMPLOYEES.length;
  const online = EMPLOYEES.filter((e) => e.status === "online").length;
  const busy = EMPLOYEES.filter((e) => e.status === "busy").length;
  const offline = EMPLOYEES.filter((e) => e.status === "offline").length;
  const totalTasks = EMPLOYEES.reduce((a, e) => a + e.tasks, 0);

  return (
    <div className="min-h-screen bg-[#f4f6fb] text-slate-800 antialiased">
      <TopBar />
      <div className="mx-auto max-w-[1400px] px-6 pb-16 lg:px-10">
        {/* header */}
        <PageHeader total={total} />

        {/* stats strip */}
        <StatsStrip
          total={total}
          online={online}
          busy={busy}
          offline={offline}
          totalTasks={totalTasks}
        />

        {/* toolbar */}
        <Toolbar
          query={query}
          setQuery={setQuery}
          dept={dept}
          setDept={setDept}
          statusFilter={statusFilter}
          setStatusFilter={setStatusFilter}
          view={view}
          setView={setView}
        />

        {/* employees */}
        {filtered.length === 0 ? (
          <EmptyState />
        ) : view === "grid" ? (
          <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
            {filtered.map((e) => (
              <EmployeeCard key={e.id} employee={e} />
            ))}
          </div>
        ) : (
          <EmployeeRows employees={filtered} />
        )}

        <div className="mt-8 flex items-center justify-center gap-2 text-xs text-slate-400">
          <span>共 {filtered.length} 位数字员工</span>
          <span>·</span>
          <span>每页最多 12 位</span>
        </div>
      </div>
    </div>
  );
}

/* --------------------------------- top bar --------------------------------- */

function TopBar() {
  return (
    <header className="sticky top-0 z-30 border-b border-slate-200/70 bg-white/80 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-[1400px] items-center justify-between px-6 lg:px-10">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-indigo-600 to-blue-500 text-white shadow-lg shadow-indigo-500/30">
            <Bot className="h-5 w-5" />
          </div>
          <div className="leading-tight">
            <p className="text-[15px] font-semibold tracking-tight text-slate-900">
              Aurora 数字员工平台
            </p>
            <p className="text-[11px] text-slate-400">Enterprise AI Workforce</p>
          </div>
        </div>

        <div className="hidden flex-1 justify-center px-8 md:flex">
          <div className="relative w-full max-w-md">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <Input
              placeholder="搜索员工、技能或任务…"
              className="h-10 rounded-lg border-slate-200 bg-slate-50 pl-9 pr-16 text-sm shadow-none focus-visible:ring-indigo-500/40"
            />
            <kbd className="absolute right-3 top-1/2 -translate-y-1/2 rounded border border-slate-200 bg-white px-1.5 py-0.5 font-mono text-[10px] text-slate-400">
              ⌘K
            </kbd>
          </div>
        </div>

        <div className="flex items-center gap-1.5">
          <Button
            variant="ghost"
            size="icon"
            className="relative h-10 w-10 rounded-lg text-slate-500 hover:bg-slate-100 hover:text-slate-700"
          >
            <Bell className="h-4.5 w-4.5 h-[18px] w-[18px]" />
            <span className="absolute right-2 top-2 h-2 w-2 rounded-full border-2 border-white bg-rose-500" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-10 w-10 rounded-lg text-slate-500 hover:bg-slate-100 hover:text-slate-700"
          >
            <Settings className="h-[18px] w-[18px]" />
          </Button>
          <div className="mx-1 h-6 w-px bg-slate-200" />
          <Button className="h-10 rounded-lg bg-indigo-600 px-4 text-sm font-medium shadow-lg shadow-indigo-500/25 hover:bg-indigo-700">
            <CirclePlus className="h-4 w-4" />
            创建智能体
          </Button>
        </div>
      </div>
    </header>
  );
}

/* -------------------------------- page header -------------------------------- */

function PageHeader({ total }: { total: number }) {
  return (
    <div className="flex flex-col gap-4 pt-8 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <div className="mb-1 flex items-center gap-2">
          <Badge className="gap-1 rounded-md border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[11px] font-medium text-indigo-600">
            <CircleGauge className="h-3 w-3" /> 工作台
          </Badge>
          <span className="text-xs text-slate-400">/ 数字员工</span>
        </div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900">
          数字员工列表
          <span className="ml-2 align-middle text-sm font-medium text-slate-400">
            {total} 位在职
          </span>
        </h1>
        <p className="mt-1 text-sm text-slate-500">
          管理、监控并编排你的 AI 劳动力
        </p>
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          className="h-10 rounded-lg border-slate-200 bg-white text-sm text-slate-600 shadow-sm hover:bg-slate-50"
        >
          <FileCode2 className="h-4 w-4 text-slate-400" />
          导出清单
        </Button>
        <Button className="h-10 rounded-lg bg-slate-900 px-4 text-sm font-medium shadow-sm hover:bg-slate-800">
          <UserPlus className="h-4 w-4" />
          批量导入
        </Button>
      </div>
    </div>
  );
}

/* --------------------------------- stats strip -------------------------------- */

function StatsStrip({
  total,
  online,
  busy,
  offline,
  totalTasks,
}: {
  total: number;
  online: number;
  busy: number;
  offline: number;
  totalTasks: number;
}) {
  const stats = [
    {
      label: "员工总数",
      value: total,
      unit: "位",
      hint: "+12 本月",
      icon: Users,
      tone: "text-indigo-600 bg-indigo-50",
    },
    {
      label: "运行中",
      value: online,
      unit: "位",
      hint: "正常服务中",
      icon: Activity,
      tone: "text-emerald-600 bg-emerald-50",
    },
    {
      label: "忙碌",
      value: busy,
      unit: "位",
      hint: "高负载处理",
      icon: Zap,
      tone: "text-rose-600 bg-rose-50",
    },
    {
      label: "已下线",
      value: offline,
      unit: "位",
      hint: "待维护",
      icon: Square,
      tone: "text-slate-500 bg-slate-100",
    },
    {
      label: "今日任务",
      value: totalTasks,
      unit: "项",
      hint: "完成率 96.3%",
      icon: SquareStack,
      tone: "text-blue-600 bg-blue-50",
    },
  ];

  return (
    <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
      {stats.map((s) => (
        <div
          key={s.label}
          className="rounded-xl border border-slate-200/80 bg-white p-4 shadow-sm transition hover:border-indigo-200 hover:shadow-md"
        >
          <div className="flex items-center gap-2">
            <div className={cn("flex h-8 w-8 items-center justify-center rounded-lg", s.tone)}>
              <s.icon className="h-4 w-4" />
            </div>
            <span className="text-xs font-medium text-slate-400">{s.label}</span>
          </div>
          <div className="mt-3 flex items-baseline gap-1">
            <span className="text-2xl font-bold tracking-tight text-slate-900">{s.value}</span>
            <span className="text-sm text-slate-400">{s.unit}</span>
          </div>
          <p className="mt-0.5 text-[11px] text-slate-400">{s.hint}</p>
        </div>
      ))}
    </div>
  );
}

/* ----------------------------------- toolbar ----------------------------------- */

function Toolbar({
  query,
  setQuery,
  dept,
  setDept,
  statusFilter,
  setStatusFilter,
  view,
  setView,
}: {
  query: string;
  setQuery: (v: string) => void;
  dept: string;
  setDept: (v: string) => void;
  statusFilter: "all" | Status;
  setStatusFilter: (v: "all" | Status) => void;
  view: "grid" | "rows";
  setView: (v: "grid" | "rows") => void;
}) {
  const statuses: { key: "all" | Status; label: string }[] = [
    { key: "all", label: "全部状态" },
    { key: "online", label: "运行中" },
    { key: "idle", label: "空闲" },
    { key: "busy", label: "忙碌" },
    { key: "offline", label: "已下线" },
  ];

  return (
    <div className="mt-6 flex flex-col gap-3 rounded-xl border border-slate-200/80 bg-white p-3 shadow-sm lg:flex-row lg:items-center">
      <div className="relative flex-1">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="按名称、角色、部门或技能筛选…"
          className="h-10 rounded-lg border-slate-200 bg-slate-50 pl-9 pr-9 text-sm shadow-none focus-visible:ring-indigo-500/40"
        />
        {query && (
          <button
            onClick={() => setQuery("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-400 hover:text-slate-600"
          >
            清除
          </button>
        )}
      </div>

      <div className="flex items-center gap-2">
        <div className="relative">
          <select
            value={dept}
            onChange={(e) => setDept(e.target.value)}
            className="h-10 appearance-none rounded-lg border border-slate-200 bg-white pl-3 pr-9 text-sm font-medium text-slate-700 shadow-none focus:border-indigo-400 focus:outline-none focus:ring-1 focus:ring-indigo-400"
          >
            {DEPARTMENTS.map((d) => (
              <option key={d} value={d}>
                {d === "全部" ? "全部部门" : d}
              </option>
            ))}
          </select>
          <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
        </div>

        <div className="relative">
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as "all" | Status)}
            className="h-10 appearance-none rounded-lg border border-slate-200 bg-white pl-3 pr-9 text-sm font-medium text-slate-700 shadow-none focus:border-indigo-400 focus:outline-none focus:ring-1 focus:ring-indigo-400"
          >
            {statuses.map((s) => (
              <option key={s.key} value={s.key}>
                {s.label}
              </option>
            ))}
          </select>
          <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
        </div>

        <Button
          variant="outline"
          size="icon"
          className="ml-1 h-10 w-10 shrink-0 rounded-lg border-slate-200 text-slate-500 hover:bg-slate-50"
        >
          <SlidersHorizontal className="h-4 w-4" />
        </Button>

        <div className="ml-1 flex h-10 items-center gap-0.5 rounded-lg border border-slate-200 bg-slate-50 p-0.5">
          <button
            onClick={() => setView("grid")}
            className={cn(
              "flex h-8 w-9 items-center justify-center rounded-md transition",
              view === "grid" ? "bg-white text-indigo-600 shadow-sm" : "text-slate-400 hover:text-slate-600"
            )}
          >
            <LayoutGrid className="h-4 w-4" />
          </button>
          <button
            onClick={() => setView("rows")}
            className={cn(
              "flex h-8 w-9 items-center justify-center rounded-md transition",
              view === "rows" ? "bg-white text-indigo-600 shadow-sm" : "text-slate-400 hover:text-slate-600"
            )}
          >
            <Rows3 className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  );
}

/* ---------------------------------- employee card ---------------------------------- */

function EmployeeCard({ employee: e }: { employee: Employee }) {
  const st = statusColor(e.status);
  const kind = kindMeta[e.kind];
  const KindIcon = kind.icon;

  return (
    <Card className="group relative overflow-hidden rounded-xl border border-slate-200/80 bg-white p-5 shadow-sm transition hover:-translate-y-0.5 hover:border-indigo-200 hover:shadow-xl hover:shadow-indigo-100/60">
      {/* top accent */}
      <div className="absolute inset-x-0 top-0 h-1 bg-gradient-to-r from-indigo-500 via-blue-400 to-sky-400 opacity-0 transition group-hover:opacity-100" />

      <div className="flex items-start justify-between">
        <div className="relative">
          <Avatar className="h-14 w-14 rounded-xl border border-slate-100 shadow-sm">
            <AvatarImage src={e.avatar} alt={e.name} />
            <AvatarFallback className="rounded-xl bg-gradient-to-br from-indigo-500 to-blue-500 text-lg font-semibold text-white">
              {e.initials}
            </AvatarFallback>
          </Avatar>
          <span
            className={cn(
              "absolute -bottom-0.5 -right-0.5 h-4 w-4 rounded-full border-[3px] border-white",
              st.dot
            )}
          />
        </div>
        <button className="rounded-md p-1.5 text-slate-300 opacity-0 transition group-hover:opacity-100 hover:bg-slate-100 hover:text-slate-500">
          <MoreHorizontal className="h-4 w-4" />
        </button>
      </div>

      <div className="mt-4">
        <div className="flex items-center gap-2">
          <h3 className="text-[15px] font-semibold tracking-tight text-slate-900">{e.name}</h3>
          <Badge
            variant="outline"
            className={cn(
              "gap-1 rounded-md border-transparent px-2 py-0 text-[10px] font-semibold",
              kind.cls
            )}
          >
            <KindIcon className="h-3 w-3" />
            {kind.label}
          </Badge>
        </div>
        <p className="mt-0.5 text-sm text-slate-500">{e.role}</p>
      </div>

      {/* meta */}
      <div className="mt-3 flex items-center gap-3 text-[11px] text-slate-400">
        <span className="flex items-center gap-1">
          <Users className="h-3.5 w-3.5" />
          {e.team}
        </span>
        <span className="flex items-center gap-1">
          <MapPin className="h-3.5 w-3.5" />
          {e.location}
        </span>
      </div>

      {/* skills */}
      <div className="mt-3 flex flex-wrap gap-1.5">
        {e.skills.map((s) => (
          <span
            key={s}
            className="rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-600"
          >
            {s}
          </span>
        ))}
      </div>

      {/* status bar */}
      <div className="mt-4">
        <div className="mb-1.5 flex items-center justify-between text-[11px]">
          <span className="flex items-center gap-1.5 font-medium text-slate-500">
            <CircleGauge className="h-3.5 w-3.5 text-slate-400" />
            负载
          </span>
          <span className="font-semibold text-slate-700">
            {e.load}
            <span className="font-normal text-slate-400">%</span>
          </span>
        </div>
        <LoadBar value={e.load} />
      </div>

      {/* footer */}
      <div className="mt-4 flex items-center justify-between border-t border-slate-100 pt-4">
        <div className="flex items-center gap-3 text-[11px] text-slate-500">
          <span className="flex items-center gap-1">
            <SquareStack className="h-3.5 w-3.5 text-slate-400" />
            <b className="font-semibold text-slate-700">{e.tasks}</b> 任务
          </span>
          <span className="flex items-center gap-1">
            <Zap className="h-3.5 w-3.5 text-amber-400" />
            <b className="font-semibold text-slate-700">{e.rating.toFixed(1)}</b>
            效率
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            className="h-8 gap-1 rounded-lg text-[12px] text-slate-500 hover:bg-slate-100"
          >
            <Play className="h-3.5 w-3.5" />
            调试
          </Button>
          <Button
            size="sm"
            className="h-8 gap-1 rounded-lg bg-indigo-600 px-3 text-[12px] shadow-md shadow-indigo-500/20 hover:bg-indigo-700"
          >
            <Settings2 className="h-3.5 w-3.5" />
            配置
          </Button>
        </div>
      </div>

      {/* status tag */}
      <div
        className={cn(
          "absolute right-3 top-3 rounded-full px-2 py-0.5 text-[10px] font-semibold",
          st.text,
          st.bg
        )}
      >
        ● {st.label}
      </div>
    </Card>
  );
}

/* ---------------------------------- rows view ---------------------------------- */

function EmployeeRows({ employees }: { employees: Employee[] }) {
  return (
    <div className="mt-4 overflow-hidden rounded-xl border border-slate-200/80 bg-white shadow-sm">
      <div className="grid grid-cols-12 items-center gap-3 border-b border-slate-100 bg-slate-50/70 px-5 py-3 text-[11px] font-semibold uppercase tracking-wide text-slate-400">
        <span className="col-span-4">员工</span>
        <span className="col-span-2">部门</span>
        <span className="col-span-3">负载</span>
        <span className="col-span-2">效率</span>
        <span className="col-span-1 text-right">操作</span>
      </div>
      {employees.map((e) => {
        const st = statusColor(e.status);
        const kind = kindMeta[e.kind];
        const KindIcon = kind.icon;
        return (
          <div
            key={e.id}
            className="grid grid-cols-12 items-center gap-3 border-b border-slate-50 px-5 py-4 transition hover:bg-slate-50/60"
          >
            <div className="col-span-4 flex items-center gap-3">
              <div className="relative">
                <Avatar className="h-10 w-10 rounded-lg">
                  <AvatarImage src={e.avatar} />
                  <AvatarFallback className="rounded-lg bg-gradient-to-br from-indigo-500 to-blue-500 text-sm font-semibold text-white">
                    {e.initials}
                  </AvatarFallback>
                </Avatar>
                <span className={cn("absolute -bottom-0 -right-0 h-3 w-3 rounded-full border-2 border-white", st.dot)} />
              </div>
              <div>
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-semibold text-slate-800">{e.name}</span>
                  <KindIcon className={cn("h-3.5 w-3.5", kind.cls.split(" ")[0])} />
                </div>
                <span className="text-[11px] text-slate-400">{e.role}</span>
              </div>
            </div>
            <div className="col-span-2 text-sm text-slate-600">
              {e.department}
              <span className="block text-[11px] text-slate-400">{e.team}</span>
            </div>
            <div className="col-span-3">
              <div className="flex items-center gap-3">
                <LoadBar value={e.load} className="max-w-[140px]" />
                <span className="w-9 text-right text-xs font-semibold text-slate-700">{e.load}%</span>
              </div>
            </div>
            <div className="col-span-2 flex items-center gap-2 text-sm text-slate-600">
              <Zap className="h-3.5 w-3.5 text-amber-400" />
              {e.rating.toFixed(1)}
              <span className="text-[11px] text-slate-400">· {e.tasks} 任务</span>
            </div>
            <div className="col-span-1 flex justify-end">
              <Button variant="ghost" size="icon" className="h-8 w-8 text-slate-400 hover:text-indigo-600">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </div>
          </div>
        );
      })}
    </div>
  );
}

/* ---------------------------------- empty state ---------------------------------- */

function EmptyState() {
  return (
    <div className="mt-10 flex flex-col items-center justify-center rounded-2xl border border-dashed border-slate-300 bg-white/60 py-20 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-full bg-slate-100">
        <Search className="h-6 w-6 text-slate-400" />
      </div>
      <h3 className="mt-4 text-sm font-semibold text-slate-700">未找到匹配的数字员工</h3>
      <p className="mt-1 text-sm text-slate-400">尝试调整搜索关键词或筛选条件</p>
      <Button variant="outline" className="mt-5 h-9 rounded-lg text-sm text-slate-600">
        <Wand2 className="h-4 w-4" />
        重置筛选
      </Button>
    </div>
  );
}

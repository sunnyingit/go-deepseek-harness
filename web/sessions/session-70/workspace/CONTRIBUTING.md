# 新人第一周贡献指南（From Zero to First PR）

> **这份文档写给谁**：刚刚加入团队、准备在未来 7 天内提交第一个 PR 的新同学。目标只有一个——**让你在问出「傻问题」之前，先在这里找到答案**。
>
> **阅读方式**：按顺序读。每一节末尾都有「✅ 完成标志」，做完了再往下走。

---

## 目录

- [第 0 天：先把环境跑起来](#第-0-天先把环境跑起来)
- [第 1 天：看懂代码库，别急着动手](#第-1-天看懂代码库别急着动手)
- [第 2 天：找一个合适的第一任务](#第-2-天找一个合适的第一任务)
- [第 3–5 天：动手改代码](#第-3-5-天动手改代码)
- [第 6 天：提交你的第一个 PR](#第-6-天提交你的第一个-pr)
- [第 7 天：应对 Code Review](#第-7-天应对-code-review)
- [高效提问指南（提问前必读）](#高效提问指南提问前必读)
- [常见「傻问题」与正确姿势对照表](#常见傻问题与正确姿势对照表)
- [速查手册](#速查手册)

---

## 第 0 天：先把环境跑起来

**最重要的一课：在问任何问题之前，先自己把环境搭通。** 90% 的新人第一天的问题，答案都写在这里。

### 前置条件

| 项目 | 要求 | 检查命令 |
|------|------|----------|
| Git | 2.30+ | `git --version` |
| Node.js | 18+ | `node -v` |
| 包管理器 | npm 9+ | `npm -v` |
| 数据库 | PostgreSQL 14+ | `psql --version` |

### 环境搭建四步走

```bash
# 1. 克隆并进入项目
git clone https://github.com/your-org/your-repo.git
cd your-repo

# 2. 安装依赖
npm install

# 3. 配置环境变量（模板里有说明）
cp .env.example .env

# 4. 启动开发服务器
npm run dev
```

看到 `Server listening on http://localhost:3000` 即为成功。

### 常见坑（先自己排查这几个）

> **Warning**：以下问题 99% 的情况下不用问人，按顺序排查即可。

1. **`npm install` 报网络错误** → 检查是否在公司内网，需要配置代理或私有 registry。
2. **`.env` 缺失导致启动失败** → 确认第 3 步 `cp .env.example .env` 是否执行，字段是否填全。
3. **端口被占用** → `lsof -i :3000` 找到占用进程，或改 `.env` 里的 `PORT`。
4. **数据库连不上** → 确认 PostgreSQL 已启动、`DATABASE_URL` 里的密码正确。

✅ **完成标志**：`npm run dev` 能跑起来，访问首页能看到界面。

---

## 第 1 天：看懂代码库，别急着动手

**第二课：先读后写。** 花一天时间读代码，比急着改三行代码有用十倍。

### 必读清单（按顺序）

1. `README.md` — 项目是做什么的
2. `docs/architecture.md` — 整体架构和数据流
3. `src/` 目录结构 — 每个目录是干嘛的
4. `.github/PULL_REQUEST_TEMPLATE.md` — 之后 PR 要用到的模板

### 目录结构速览

```
src/
├── components/   # UI 组件
├── pages/        # 页面
├── services/     # API 调用层
├── utils/        # 工具函数
├── hooks/        # 自定义 React Hook
└── types/        # TypeScript 类型定义
```

### 三层提问法（自己先查，查不到再问）

| 层级 | 你要做什么 | 耗时上限 |
|------|-----------|---------|
| 1. 自己查文档/README/wiki | 先动手找答案 | 15 分钟 |
| 2. 搜索 issue 和讨论区 | `关键词 site:github.com/org/repo` | 10 分钟 |
| 3. 问同事（附上你已查过的内容） | 带着上下文提问 | — |

> **Important**：前两层都没做就提问，就是本指南要帮你消灭的「傻问题」。

✅ **完成标志**：能对着代码库说出「数据从哪来、到哪去」的大致流程。

---

## 第 2 天：找一个合适的第一任务

选对第一个任务，比选「厉害」的任务重要。

### 找任务的三个标准

- ✅ 带 `good first issue` 或 `help wanted` 标签
- ✅ 改动范围小（通常一个文件、几十行以内）
- ✅ 有清晰的验收标准（能明确判断「做完了」）

### 认领流程（认领前不算你的）

```bash
# 1. 找到感兴趣的任务
# 2. 在 issue 下评论认领，例如：
#    "Hey, I'd like to work on this. Could you assign it to me?"
# 3. 等待被 assign（assign 之后才算正式认领）
# 4. 若有疑问，直接在 issue 里问（比私聊更好，有记录）
```

> **Note**：不要默默开工。没被 assign 就提交的 PR，可能和别人的工作重复，白费力气。

✅ **完成标志**：你被 assign 了一个任务，且能复述它的验收标准。

---

## 第 3–5 天：动手改代码

### 1. 拉最新代码 + 建分支

```bash
git checkout main
git pull origin main

# 分支命名规范，见下方速查手册
git checkout -b fix/issue-123-wrapper-bug
```

### 2. 写代码时的三条铁律

1. **小步提交**：一个 commit 只做一件事，别攒一堆再提交。
2. **先测试后实现**（如果团队用 TDD）：先写失败的测试，再让它通过。
3. **改完自己跑一遍**：

```bash
npm run lint        # 代码风格
npm run test        # 单元测试
npm run build       # 确认能编译通过
```

> **Warning**：提交前没跑 `lint` 和 `test`，是 Code Review 中最常见的「返工理由」。

### 3. 提交信息规范（Conventional Commits）

```bash
git add .
git commit -m "fix(wrapper): correct padding on mobile layout"
```

格式：`<type>(<scope>): <subject>`，subject 用一般现在时、动词原形开头，不加句号。

| type | 用途 | 示例 |
|------|------|------|
| `feat` | 新功能 | `feat(auth): add login form` |
| `fix` | 修 bug | `fix(api): handle null response` |
| `docs` | 文档 | `docs(readme): update setup steps` |
| `test` | 测试 | `test(hooks): cover error branch` |
| `refactor` | 重构（不改行为） | `refactor(utils): extract formatDate` |

✅ **完成标志**：代码能在本地跑通所有测试，提交信息符合规范。

---

## 第 6 天：提交你的第一个 PR

### 提交前自查清单

- [ ] 本地 `npm run lint` 通过
- [ ] 本地 `npm run test` 全部通过
- [ ] 只改动了与任务相关的文件
- [ ] 没有把调试用 `console.log` 留在代码里
- [ ] PR 描述里已填写模板全部字段
- [ ] 关联了对应 issue（写 `Closes #123`）

### PR 描述要点（评委看什么）

```markdown
## 描述
修复了 wrapper 组件在移动端 padding 缺失的问题。

## 改动类型
- [x] Bug 修复

## 关联 Issue
Closes #123

## 测试
- 手动验证：iPhone 12 尺寸下 padding 正常
- 新增单测：`test/hooks/wrapper.test.js`
```

✅ **完成标志**：PR 已提交，CI 流水线全部变绿（无红色叉叉）。

---

## 第 7 天：应对 Code Review

**第三课：Review 意见是针对代码，不是针对你。** 改就完事了。

### 收到意见后的正确姿势

1. **逐条回复**：每条意见都回应，同意的标 `Done`，不同意的说明理由。
2. **不争论情绪**：用「你的考虑是 X，我的做法是 Y，因为 Z」的句式。
3. **改完 push 到同一分支**：不需要开新 PR。

```bash
git add .
git commit -m "fix(wrapper): address review comment on breakpoint"
git push origin fix/issue-123-wrapper-bug
```

> **Note**：Review 期间不要主动 merge 别人的分支，也不要私自 force push（除非 reviewer 明确要求 squash）。

✅ **完成标志**：PR 被 approve 并合并，你收到了第一张贡献者徽章（或团队的小小庆祝）。

---

## 高效提问指南（提问前必读）

**终极原则：问「聪明问题」的唯一方法，是证明你已经自己努力过。**

### 好问题的三个要素

1. **你试过什么**（避免「我什么都不会」的空白提问）
2. **卡在哪一步**（精确到具体命令、报错信息）
3. **你期望的结果是什么**

### 提问模板（复制即用）

```markdown
**我想做什么**
运行 `npm run dev` 启动开发服务器。

**我试过什么**
- 已执行 `cp .env.example .env`，填了 DATABASE_URL
- 已重启 PostgreSQL（`brew services restart postgresql`）
- 已按 README 排查了端口占用，确认 3000 空闲

**卡在哪里**
报错信息如下：
```
Error: connect ECONNREFUSED 127.0.0.1:5432
```

**我的环境**
- macOS 14.5
- Node v20.11.0
- PostgreSQL 14.2
```

> **Important**：把上面模板填好再发。填的过程本身就是解决问题的过程，很多问题会在这个过程中自己消失。

### 提问的时机与渠道

| 渠道 | 适合什么 | 不适合什么 |
|------|---------|-----------|
| Issue 评论区 | 和任务直接相关的疑问 | 闲聊、一般性问题 |
| 团队群聊 | 环境/工具类即时求助 | 能被文档回答的问题 |
| 私聊 | 需要一对一讨论的设计问题 | 公开可查的技术细节 |

---

## 常见「傻问题」与正确姿势对照表

| ❌ 傻问题 | ✅ 正确姿势 |
|----------|-----------|
| 「这个项目是干嘛的？」 | 先读 README，再带着具体问题来问 |
| 「怎么启动？（没人教过我）」 | 先跑一遍 `第 0 天` 的步骤，卡住了带着报错来问 |
| 「我代码写好了，你帮我看看哪里错？」（无任何上下文） | 附上复现步骤、报错信息、已尝试的排查 |
| 「这个函数为什么不 work？」（不贴代码） | 贴出最小可复现代码片段 + 期望 vs 实际 |
| 「assign 我吧」（认领前没说清要做什么） | 在 issue 下说明打算怎么做，再请对方 assign |
| 「测不测都行吧，先合了」 | 老老实实跑完 lint/test 再提交 |
| 「我 force push 了，把你的改动冲掉了」 | 永远不要对共享分支 force push |

---

## 速查手册

### 常用命令

```bash
# 环境
npm install                 # 装依赖
npm run dev                 # 启动开发服务器
npm run lint                # 检查代码风格
npm run test                # 跑测试
npm run build               # 生产构建

# Git 工作流
git checkout -b <branch>    # 新建分支
git add -p                  # 交互式暂存（推荐，避免误提交）
git commit -m "..."         # 提交
git push origin <branch>    # 推送到远程
git pull origin main        # 拉取最新 main
```

### 分支命名规范

- `feat/<一句话>` — 新功能
- `fix/<一句话>` — 修复 bug
- `docs/<一句话>` — 文档更新
- `refactor/<一句话>` — 重构

### 关键时刻找谁

| 情况 | 找谁 |
|------|------|
| 环境搭不起来 | 团队群聊（附上你已排查的步骤） |
| 任务需求不清楚 | 对应 issue 的负责人（在 issue 下问） |
| Code Review 意见看不懂 | 直接在该条评论下回复追问 |

---

## 最后一句

没有人第一天就什么都会。**「不问傻问题」不代表「不提问」，而是「把能自己查的先查完，再带着上下文提问」。** 做到这一点，你就已经超过大多数新人了。

祝你的第一个 PR 顺利！🚀

---

**遇到本文档没覆盖的问题？** 欢迎提 issue 或在本仓库发起文档改进 PR——你的困惑，可能就是下一位新人会踩的坑。

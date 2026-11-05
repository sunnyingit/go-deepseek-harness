# native

Electron 客户端，窗口里加载 go-harness 的三页 UI：

| 菜单 | 地址 |
| --- | --- |
| 工作台 | `/`（`index.html`） |
| 对话 | `/dialog.html` |
| 个人工作区 | `/space.html` |

正式安装包会**自带** web 服务、便携 Python（已预装文档库）、官方 Node，以及 PPT 用的 Node 库（`pptxgenjs`、`react` / `react-dom` / `react-icons`、`sharp`）。Windows 另外带 MinGit 的 `bash`。对方电脑不需要先装 Go / Python / Node。

## 运行（开发）

```sh
export DEEPSEEK_API_KEY=sk-...
cd native
make start          # 若还没有 runtime，会先下载 Python/Node 并编译 web
```

开发模式下如果 `native/runtime/bin/pabuddy-web` 已存在，会直接拉起它（PATH 指向包内 Python/Node）；否则退回 `web/` 里的 `go run .`。

## 打包

仓库根目录或 `native/` 下：

```sh
make mac          # 下载当前 Mac 架构的 Python/Node，go build web，打出 .zip
make win          # 下载 win-x64 运行时并交叉编译 web，打出 .zip
```

产物在 `native/dist/`。体积会比较大（Python + Node + wheels）。下载的 `.tar.gz` / `.zip` 默认留在 `native/runtime/` 供下次复用（不会打进安装包）。要解压后删掉压缩包：`make mac PRUNE_ARCHIVES=1`。

用户第一次打开：

1. 若没有 API Key，会弹出填写框，保存在系统应用数据目录（不会写进安装包）
2. 启动内置 `pabuddy-web`。macOS 上 Python 包已打进便携解释器，启动即可用；Windows 首次会用包内 wheels 建 venv（可能要几分钟）
3. `PATH` 指向安装包里的 `python`、`node`（以及 Windows 的 Git Bash）。`NODE_PATH` 指向包内 `runtime/node-packages`，会话脚本可以直接 `require('pptxgenjs')` 等，不必再 `npm install`。`npm install -g` 写到用户数据目录，不会改只读的 `.app`

日志：`~/Library/Application Support/PABuddy/web.log`（macOS）。

未签名包在 macOS 上需要在 Finder 里右键「打开」一次。

- `PORT`（默认 `8081`）和 `HARNESS_URL`（默认 `http://127.0.0.1:$PORT`）可覆盖地址。
- 快捷键：`Cmd/Ctrl+1` 工作台，`+2` 对话，`+3` 个人工作区。

## 托盘常驻、开机自启、定时任务与通知

`host.js` 管客户端宿主这一层；定时任务和通知本身在 Python 股票后端（`stock_agent_assitant`，默认 `127.0.0.1:8090`）里跑，设计见那个仓库的 `docs/scheduler-and-notify-spec.md`。

- **托盘常驻**：关窗口只是藏起来，右键托盘 → 退出才真退出。托盘显示未读通知数。
- **开机自启**：默认开启，托盘菜单里可关；自启时只起托盘，不弹窗口。偏好存在用户数据目录的 `host.json`。
- **单实例**：重复打开只会把已有窗口叫出来（第二个实例不会去动后端端口）。
- **休眠恢复**：`resume` / 解锁屏幕时调用 `POST /sched/tick?reason=resume`，错过的任务按各自的补跑策略处理（盘中的提醒不补，收盘复盘等会补）。
- **桌面通知**：每 10 秒取一次 `GET /notify/desktop/pending`，弹系统通知，点击打开对应页面，并 `POST /notify/desktop/ack`。
- **股票后端**：8090 没有人起时，开发模式会从 `../../stock_agent_assitant`（或 `STOCK_AGENT_DIR`）用 `python3 -m app.main` 拉起，日志在用户数据目录的 `stock-backend.log`。安装包里还没有打进股票后端，这是后续要补的。

单测：`node host.test.js`。

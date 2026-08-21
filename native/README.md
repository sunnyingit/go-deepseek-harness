# native

Electron 客户端，窗口里加载 go-harness 的三页 UI：

| 菜单 | 地址 |
| --- | --- |
| 工作台 | `/`（`index.html`） |
| 对话 | `/dialog.html` |
| 个人空间 | `/space.html` |

正式安装包会**自带** web 服务、便携 Python（已预装文档库）、官方 Node，Windows 另外带 MinGit 的 `bash`。对方电脑不需要先装 Go / Python / Node。

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
3. `PATH` 指向安装包里的 `python`、`node`（以及 Windows 的 Git Bash）。`npm install -g` 写到用户数据目录，不会改只读的 `.app`

日志：`~/Library/Application Support/PABuddy/web.log`（macOS）。

未签名包在 macOS 上需要在 Finder 里右键「打开」一次。

- `PORT`（默认 `8081`）和 `HARNESS_URL`（默认 `http://127.0.0.1:$PORT`）可覆盖地址。
- 快捷键：`Cmd/Ctrl+1` 工作台，`+2` 对话，`+3` 个人空间。

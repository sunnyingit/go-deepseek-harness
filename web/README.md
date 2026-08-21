# web

An HTTP agent server stringing together every package in this module —
modeled on deepseek-harness's
[`examples/web-cordis`](https://github.com/deepseek-ai/deepseek-harness/tree/master/examples/web-cordis).

## UI

`GET /` serves a single self-contained page (embedded via `go:embed`, no
build step — `static/index.html`) that drives every endpoint below:
session list/create, chat, file upload, skills, plan-mode toggle, fork.
Sending a message there navigates to `static/dailog.html?session=...&text=...`
(also embedded, served at `GET /dialog.html` and the legacy `GET /dailog.html`),
which triggers and renders that turn via `GET /sessions/{id}/stream`. Personal
space lives at `GET /space.html` and lists uploaded vs generated workspace
files across sessions (`GET /files`) with preview, download, and delete.
The Electron shell in `native/` loads these three pages in a desktop window.
`make mac` / `make win` bundle a compiled `web` binary plus portable Python and Node so the installed app does not need those toolchains on the user's machine.

This is **not** a port of [`apps/web/src`](https://github.com/deepseek-ai/deepseek-harness/tree/master/apps/web/src)
— that file is a two-line bootstrap that mounts an entire separate
`@deepseek-ai/dsh-client-web` package spread across many
`packages/client/ui-*` packages (conversation UI, tool-call rendering,
settings panels, a bash-call view, ...). There's no faithful line-for-line
port of that scope; `static/index.html` is a from-scratch UI sized to
exercise this server's actual API, nothing more.

## What's different from `web-cordis`

That example centers on `@deepseek-ai/dsh-tool-cordis`: a model can mount or
unmount *its own* plugins into the running process at runtime — genuinely
Cordis-specific (a plugin system with live mount/unmount, dependency
injection, and disposal isn't something Go has an equivalent to; it's the
same category of thing every other package in this codebase has already
declined to port). This example keeps the part that generalizes: one
process serving multiple concurrent agent sessions, each backed by a real
model, real tools, a real sandbox, and a real skill catalog — the part of
`web-cordis` that's actually about composing the harness, not about Cordis
itself.

## What it wires together

| Package | Role |
|---|---|
| `core/session` | `Store` — one `Session` per HTTP session, `Fork` for the `/fork` endpoint |
| `core/agentloop` | the ReAct driver — one `Agent` per session |
| `core/agent` | `Registry` — tracks every live agent by session id, with fork lineage as ownership |
| `core/tools` + `tools/python` | `run_python`, a real sandboxed interpreter pre-installed with document-processing packages |
| display tools (`show_table` …) | pseudo-tools: the model fills a strict schema (headers/rows, chart series, …); the server acks `"Displayed to the user."` so the loop can continue, and the SSE stream re-labels the call as a `table`/`chart`/… event for the UI |
| `sandbox/local` | confines every `run_python`/`bash`/`read`/`write`/`edit` call — real Seatbelt on macOS, bwrap on Linux |
| `shell/bashsandbox` + `shell/toolbash` | `bash`/`bash_job_result` — a general sandboxed shell, with per-call sandbox-permission escalation and background jobs; also how a skill's own bundled script (see `sales-report`, below) actually runs — pass its path as `command` |
| `fs/fssandbox` + `fs/toolfs` | `read`/`write`/`edit` — sandboxed file access, always workspace-write (no read-only-then-escalate step, unlike `bash`); also how `/files` uploads land, via `fssandbox.Provider.WriteBytes` |
| `skill/skill` + `skill/filesystem` | `list_skills`/`load_skill`, backed by the runtime `explain-jargon` skill and the `web/skills` SKILL.md catalog |
| `plan/planmode` | plan-mode collaboration state, toggled via `/plan-mode` |
| `llm/deepseek` | the model |
| `llm/llmretry` | automatic retry (bounded exponential backoff + jitter) for rate limits, 5xx, timeouts, and transport errors from `llm/deepseek` — wired onto `agentloop.Hooks.RequestError` in `buildAgent` |
| `llm/tokenmeter` | heuristic context-usage estimate (4 chars/token) for `GET /sessions/{id}/tokens`, priced off `Agent.SystemPrompt()`/`Agent.ToolSchemas()` plus the session's surface |
| `core/sessionquery` | read/filter/lineage-trace surface over `core/session.Store`, with an optional fallback to `session/sessionpersistence` for a session that isn't currently live |
| `session/sessionpersistence` | durable, crash-tolerant JSONL logs under `$WEB_DEMO_DATA_DIR/session-logs` — every session is `Sync`'d after each completed turn, and every session found on disk is restored (log replayed, agent rebuilt) when the server starts |

## Run

```sh
export DEEPSEEK_API_KEY=sk-...
go run .
```

First run provisions the shared Python environment (downloads packages —
see `tools/python`'s README for where and how); later runs reuse the
cached venv.

## Config

Runtime settings live in [`config.yaml`](config.yaml) — server port and
data dirs, LLM endpoint/retry, sandbox modes, Python venv, and tool
timeouts. Start with that file next to the process (`go run .` from
`web/`), or point at another copy:

```sh
export DEEPSEEK_API_KEY=sk-...
go run .                          # ./config.yaml
go run . -config /path/to.yaml
WEB_DEMO_CONFIG=/path/to.yaml go run .
```

Empty YAML fields take the package defaults. These environment variables
still override the file when set (useful for secrets and Docker):

`DEEPSEEK_API_KEY` (required unless `llm.api_key` is set),
`DEEPSEEK_BASE_URL`, `DEEPSEEK_MODEL`, `PORT`, `WEB_DEMO_DATA_DIR`,
`WEB_DEMO_PYTHON_CACHE_DIR`, `WEB_DEMO_CONFIG`.

Python cache default: `os.UserCacheDir()/go-harness/python-venv`
(`~/Library/Caches/go-harness/...` on macOS). Data dir default:
`$TMPDIR/go-harness-web-demo` (session workspaces + `session-logs/`).

## API

```
GET  /healthz
GET  /skills
POST /sessions                    {"cwd": "..."}                  -> session
GET  /sessions
GET  /sessions/{id}
GET  /sessions/{id}/messages
POST /sessions/{id}/messages      {"text": "..."}                 -> {"messages": [...new ones...]}
GET  /sessions/{id}/stream?text=...                                -> text/event-stream
POST /sessions/{id}/fork          {"boundary": -1}                -> session
POST /sessions/{id}/plan-mode     {"active": true}                -> {"outcome": "..."}
POST /sessions/{id}/files         multipart "file" field          -> {"path": "...", "bytes": N}
GET  /sessions/{id}/files                                          -> [{"path", "name", "size", "modTime", "source"}, ...]
GET  /sessions/{id}/files/{path}  ?download=1 for an attachment    -> the file's raw bytes
DELETE /sessions/{id}/files/{path}                                 -> 204
GET  /files                                                        -> same entries across all sessions, plus sessionId
GET  /sessions/{id}/tokens                                         -> heuristic {"totalTokens", "surfaceTokens", "headerTokens", ...}
```

```sh
id=$(curl -s -X POST localhost:8080/sessions | jq -r .id)
curl -s -X POST localhost:8080/sessions/$id/messages \
  -d '{"text":"What packages does run_python have installed?"}'
curl -s localhost:8080/sessions/$id/messages
curl -s -X POST localhost:8080/sessions/$id/fork | jq -r .id

# a skill whose instructions point at a bundled script instead of prose —
# the model calls list_skills, load_skill, then bash (with the script's
# path from the skill's resourceBase hint as the command) in turn
curl -s -X POST localhost:8080/sessions/$id/messages -d '{"text":"generate the sales report"}'

# upload a file into the session's workspace, then ask the model to read it —
# the response's "path" is what the model's read/bash/run_python tools see
curl -s -X POST localhost:8080/sessions/$id/files -F file=@quarterly.txt
curl -s -X POST localhost:8080/sessions/$id/messages -d '{"text":"summarize the file I uploaded"}'

# stream a turn instead of blocking for it: triggers Followup itself, then
# tails the session log — event: chunk (per assistant token/reasoning
# delta), tool-call, tool-result, table/chart/code/suggestion/image/error/
# progress (from the show_* display tools), metadata, file, done
curl -N "localhost:8080/sessions/$id/stream?text=what%20packages%20does%20run_python%20have%20installed%3F"

# list whatever the model has written into the workspace (write/edit/bash/
# run_python can all create files there, not just /files uploads), then
# fetch one back — inline by default, ?download=1 for Content-Disposition:
# attachment
curl -s localhost:8080/sessions/$id/files
curl -s localhost:8080/sessions/$id/files/report.csv
curl -s -OJ "localhost:8080/sessions/$id/files/report.csv?download=1"
curl -s -X DELETE localhost:8080/sessions/$id/files/report.csv
curl -s localhost:8080/files
```

## Deliberate simplifications

- **`exit_plan_mode` auto-approves every plan, and `bash`'s
  `sandbox_permissions`/`network` escalations auto-approve too**
  (`autoApproveReview`/`autoApproveEscalation` in `server.go` — the latter
  backs both, since `network` reuses the same `Approver` callback, see
  `shell/toolbash`'s README). Both are headless-API stand-ins with no human
  at a terminal to ask, unlike `examples/planmode`'s interactive demo — a
  real deployment would hold the call open, or route it through the same
  approval surface, instead of rubber-stamping either.
- **Streaming is poll-based server-side, not push-based**: `GET
  /sessions/{id}/stream` (used by `static/dailog.html`) tails
  `Session.Events()` on a 16ms ticker rather than subscribing to a
  notification — `core/session.Session` has no such source, it's just a
  growing slice. Good enough for one browser tab watching its own session;
  doesn't scale to many concurrent watchers of the same fast-moving session.
  `POST /sessions/{id}/messages` still exists and still blocks until
  `Agent.WhenIdle()` — simpler for scripting/curl, unchanged.
- **Rich UI types go through display tools, not prose**: `show_table`,
  `show_chart`, `show_code`, `show_suggestion`, `show_image`, `show_error`,
  `show_progress` are registered tools with JSON Schema the registry
  enforces (`ValidateArgs` — a malformed payload is an invalid call, never
  Execute). They do no work; `Execute` returns the synthetic ack
  `"Displayed to the user."` because the agent loop requires a `tool_result`
  for every model-issued call before the next step. `writeStreamEvent`
  swallows the button-press and the ack, and on a successful ack emits the
  matching frontend `MessageType` (`table`, `chart`, …) with the model's
  arguments as the payload. Schema-invalid calls emit nothing to the user
  (the model sees the validation error and can retry).
- **In-memory only**: `core/session.Store` has no persistence (see its own
  README) — sessions and the agent registry are gone on restart. Only the
  Python venv and skill files on disk survive.
- **One process, no auth**: every endpoint is open, and one Go process
  serves every session — matching a local demo, not a multi-tenant
  deployment.
- **`/files` always lands directly in the workspace root, flat**: the
  uploaded filename's directory components are stripped
  (`filepath.Base`) — an upload can't choose a subpath, and two uploads
  with the same name overwrite each other (`fs.Unconditional()`, no
  create-only guard). Good enough for "hand the agent a file to read";
  a real deployment wanting subdirectories, collision handling, or a
  content-addressed store (see `attachment/attachment`'s design, though
  that package is specifically for model-facing *image* references, not
  arbitrary file uploads) would need more here. Capped at 64MiB
  (`maxUploadBytes` in `handlers.go`), held fully in memory.
- **A successful `/files` upload injects a `[Uploaded file] <path>` note**
  (`Agent.Inject`, `handleUploadFile` in `handlers.go`) so the model has an
  unambiguous path for "the file I uploaded" instead of having to `bash "ls"`
  the workspace and guess — which, with more than one file sitting in the
  flat workspace root, previously meant it couldn't tell the just-uploaded
  file apart from anything left over from an earlier turn and would act on
  all of them. `Inject`, not `Followup`: the note sits in the inbox and
  only actually reaches the model folded in alongside whatever's asked next
  — it doesn't trigger a turn by itself. The same path is recorded in a
  hidden `.harness-uploads` manifest so `GET /files` (and the per-session
  listing) can tag `source: "upload"` versus `"generated"`. `DELETE
  /sessions/{id}/files/{path}` removes the file and drops it from that
  manifest; the manifest itself is not listed and cannot be deleted.

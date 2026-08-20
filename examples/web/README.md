# web

An HTTP agent server stringing together every package in this module —
modeled on deepseek-harness's
[`examples/web-cordis`](https://github.com/deepseek-ai/deepseek-harness/tree/master/examples/web-cordis).

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
| `sandbox/local` | confines every `run_python` call — real Seatbelt on macOS, bwrap on Linux |
| `skill/skill` + `skill/filesystem` | `list_skills`/`load_skill`, backed by one embedded runtime skill and a `SKILL.md` bundle on disk |
| `plan/planmode` | plan-mode collaboration state, toggled via `/plan-mode` |
| `llm/deepseek` | the model |

## Run

```sh
export DEEPSEEK_API_KEY=sk-...
go run .
```

First run provisions the shared Python environment (downloads packages —
see `tools/python`'s README for where and how); later runs reuse the cached
venv under `$WEB_DEMO_DATA_DIR` (default `$TMPDIR/go-harness-web-demo`).

Env vars: `DEEPSEEK_API_KEY` (required), `DEEPSEEK_BASE_URL`,
`DEEPSEEK_MODEL` (default `deepseek-chat`), `PORT` (default `8080`),
`WEB_DEMO_DATA_DIR`.

## API

```
GET  /healthz
GET  /skills
POST /sessions                    {"cwd": "..."}                  -> session
GET  /sessions
GET  /sessions/{id}
GET  /sessions/{id}/messages
POST /sessions/{id}/messages      {"text": "..."}                 -> {"messages": [...new ones...]}
POST /sessions/{id}/fork          {"boundary": -1}                -> session
POST /sessions/{id}/plan-mode     {"active": true}                -> {"outcome": "..."}
```

```sh
id=$(curl -s -X POST localhost:8080/sessions | jq -r .id)
curl -s -X POST localhost:8080/sessions/$id/messages \
  -d '{"text":"What packages does run_python have installed?"}'
curl -s localhost:8080/sessions/$id/messages
curl -s -X POST localhost:8080/sessions/$id/fork | jq -r .id
```

## Deliberate simplifications

- **`exit_plan_mode` auto-approves every plan** (`autoApproveReview` in
  `server.go`). This is a headless API with no human at a terminal to ask,
  unlike `examples/planmode`'s interactive demo — a real deployment would
  hold the call open, or return a "pending" state a separate endpoint
  resolves, instead of rubber-stamping.
- **No streaming**: `POST /sessions/{id}/messages` blocks until
  `Agent.WhenIdle()`, then returns the whole turn's new messages at once.
  `core/agentloop`'s driver already exposes step/tool-call events on the
  session log in real time (see `core/session`'s surface); an SSE endpoint
  tailing them would be a natural addition, just not built here.
- **In-memory only**: `core/session.Store` has no persistence (see its own
  README) — sessions and the agent registry are gone on restart. Only the
  Python venv and skill files on disk survive.
- **One process, no auth**: every endpoint is open, and one Go process
  serves every session — matching a local demo, not a multi-tenant
  deployment.

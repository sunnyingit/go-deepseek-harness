# agentloop

A Go port of four [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)
packages:

- `packages/core/agent-loop` (this directory) — the ReAct-style turn/step
  driver (`ReactLoopAgent`, `agent.ts`) plus its tool-call scheduler
  (`tool-calls.ts`)
- `packages/core/session` ([`../session/`](../session/)) — the append-only,
  event-sourced session log and its surface projection. This package's
  conversation vocabulary (`Message`, `ContentBlock`, `Role`, `BlockKind`)
  and `Session`/`SessionEvent` are type aliases onto `core/session`'s types,
  not separate definitions — matching the TS dependency direction
  (`agent-loop` depends on `session`, not the reverse).
- `packages/llm/llm-deepseek` ([`../../llm/deepseek/`](../../llm/deepseek/))
  — a direct HTTP+SSE adapter against the DeepSeek API implementing this
  package's `LLM` interface, so the loop above can drive a real model.
- `packages/plan/plan-mode` ([`../../plan/planmode/`](../../plan/planmode/))
  — logged, per-session plan-collaboration state built entirely on this
  package's public surface (`Hooks.PreStep`, `ToolExecutor`, `Agent.Session()`).

This package imports `core/session`; `llm/deepseek` and `plan/planmode` import
this one (to implement `LLM`/`ToolExecutor` or to hook `Hooks.PreStep`); none
of the three import back down the chain — every dependency is
one-directional, not mutual.

It keeps the original design's core mechanics:

- the append-only session log as the single source of truth, now owned by
  `core/session` — tool results reach the model through `DeriveMessages()`
  re-reading `tool/result` log events, never through the inbox (a bug in an
  earlier version of this port conflated the two). Every surface-eligible
  append (`user/message`, `assistant/message`, `tool/result`) carries a
  `session.WithSurfaceOp(session.AppendOp())`, and `tool/result` wraps its
  observation in a full `Message` under a `"message"` key — the shape
  `core/session`'s projection rule expects.
- the two-layer turn/step loop and the four extension-point hooks
- abort-based cancellation, reimplemented with `context.CancelCauseFunc`
  instead of `AbortSignal`, goroutines instead of async driver kicks
- the tool-call scheduler: exclusive calls run as a one-at-a-time barrier,
  parallel calls share a bounded rolling pool (`WithMaxParallelToolCalls`,
  default 10); results commit in model order regardless of dispatch order;
  a `ConcludesTurn` result ends the turn immediately even though a tool ran;
  an abort mid-execution records synthetic "aborted before dispatch" results
  for calls that never got a pool slot, so every model-issued call still has
  a matching result in the log
- panic containment at both goroutine boundaries the driver spawns: a
  panicking `ToolExecutor.Execute` (its own goroutine inside `runGroup`) is
  recovered by `safeExecute` and turned into an error `ToolResult`; a panic
  anywhere else in `turn()`'s own synchronous body (its own goroutine, spawned
  by `kick()`) is recovered by a deferred handler that also guarantees
  exactly one `turn/end` gets appended no matter which of `turn()`'s several
  return points fires. Go has no exceptions — `recover()` only works within
  the panicking call's own goroutine, so each spawn point needs its own
  guard; an unrecovered panic anywhere crashes the whole process, not just
  the offending agent. An earlier version of this port had neither.
- `GenerateRequest`/`ToolExecutor` extended with `Model`, `Tools`
  (`ToolExecutor.Schemas()`), `MaxTokens`, `Temperature`, `Stop`, and
  `ReasoningEffort` — added so a real adapter (`llm/deepseek`) has enough on
  the request to actually call a model; `Agent` gained matching
  `WithModel`/`WithMaxTokens`/`WithTemperature`/`WithReasoningEffort` setters.
- `Agent.Session()`, plus `*Agent` added to `Hooks.PreStep` and
  `ToolExecutor.Execute`'s signatures — added so a session-state plugin
  (`plan/planmode`) can fold and extend durable state from inside a hook or a
  tool, mirroring the TS `agent/pre-step` payload's `{ agent, ... }` and a
  tool's `exec.agent`. Before this, neither seam could reach the session at
  all, which made a plugin like plan mode impossible to write without editing
  this file.

## Deliberately not ported

- `runtime-context.ts` — auto-injecting a deduplicated dynamic-context
  snapshot (cwd, time, …) before each step. A caller can approximate this
  today via the `PreStep` hook; the dedup-tracking class itself isn't ported.
- `invariant.ts` — a debug-only runtime assertion that a dispatched request
  matches the log-derived history. A safety net, not core behavior.
- `index.ts` — the Cordis plugin/factory wrapper (declarative config-driven
  agent boot, session-persistence integration, settings, teardown races).
  Deployment plumbing, not ReAct logic — out of scope by design, same as the
  rest of the Cordis plugin system.

## Run

```sh
go build ./...
go test ./... -race -v
```

## Files

File boundaries mirror the original package where the split still makes sense
in a package this small:

| File | Ported from | Contents |
|---|---|---|
| `agent.go` | `agent.ts` | aliases onto `core/session`'s vocabulary/session types, `Inbox`, `Hooks`, the `phase` state machine, `Agent`, and the `turn()` / `step()` loop |
| `tool_calls.go` | `tool-calls.ts` | `ToolExecutor`, `ExecutionMode`, `ToolResult`, and the scheduler (`executeToolCalls` / `runGroup` / `appendSkippedToolCall`) |
| `constants.go` | `constants.ts` | `DefaultMaxParallelToolCalls` |
| `agent_test.go` | — | coverage for single-step completion, multi-step tool-call continuation, sticky max-tokens, pre-step rejection, mid-stream cancellation, bounded parallel tool execution, `ConcludesTurn`, abort-mid-tool-execution, and panic containment at both goroutine boundaries |

`runtime-context.ts`, `invariant.ts`, and `index.ts` have no Go file — see
"Deliberately not ported" above.

See [`../../llm/deepseek/README.md`](../../llm/deepseek/README.md) for the
DeepSeek LLM adapter package.

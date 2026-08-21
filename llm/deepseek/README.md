# deepseek

A Go port of `packages/llm/llm-deepseek` from
[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness): a direct
HTTP + SSE adapter against the DeepSeek (OpenAI-compatible) chat-completions
endpoint, implementing `agentloop.LLM` from [`../../core/agentloop`](../../core/agentloop).

[`go-deepseek/deepseek`](https://github.com/go-deepseek/deepseek) was used as
a reference for idiomatic Go request/response struct shapes, but this package
does not depend on it: its streaming `Delta` has no `tool_calls` field at
all, and it hardcodes `model` to literally `"deepseek-chat"` /
`"deepseek-reasoner"` — both are blockers for an adapter whose whole point is
driving a tool-calling ReAct loop with an arbitrary pass-through model name.
The TS original is the source of truth this port follows instead.

## Usage

```go
adapter, err := deepseek.New(deepseek.Config{
    APIKey: os.Getenv("DEEPSEEK_API_KEY"),
    // BaseURL, Thinking, StreamIdleTimeout, ConnectTimeout,
    // TLSHandshakeTimeout, HTTPClient all optional
})
agent := agentloop.NewAgent(session, adapter, tools, "you are a helpful assistant", agentloop.Hooks{}).
    WithModel("deepseek-chat")
```

## What's ported from `llm-deepseek`

- **`serialize.ts` → `serialize.go`**: system/user/assistant/tool message
  shapes, tool-call/tool-result mapping, the thinking/`reasoning_effort`
  resolution (`off` maps to `thinking:{type:disabled}` and never crosses the
  wire as an effort value), and the reasoning passback rule — `reasoning_content`
  is replayed only on assistant turns that also carried tool calls.
- **`sse.ts` → `sse.go`**: a spec-strict-enough SSE reader (blank-line
  terminated events, multi-`data:` joining, comments skipped) that yields the
  literal `[DONE]` sentinel and treats EOF before it as truncation.
- **`translate.ts` → `translate.go`**: wire chunks assembled into blocks by
  streamed index (one text block, one reasoning block, one tool-call block
  per index), `EMPTY_RESPONSE` for a `stop`/`tool_calls` finish that opened
  nothing, unrecognized finish reasons (`content_filter`, …) as errors,
  cache-hit-subtracted usage accounting.
- **`adapter.ts` → `adapter.go`**: one stable idle watchdog covering the
  whole request+stream, caller cancellation vs. idle-timeout distinguished in
  the resulting error's `Code`, and the same HTTP-status → error-`Code`
  classification (`AUTH`, `RATE_LIMIT`, `QUOTA_EXCEEDED`,
  `CONTEXT_WINDOW_EXCEEDED`, `INVALID_REQUEST`, `SERVER`, `HTTP_<status>`).

## Deliberate simplifications vs. the TS original

- **Live text/reasoning deltas, assembled tool calls.** Wire SSE tokens
  for `content` / `reasoning_content` are forwarded as `StreamChunk`s as
  they arrive so a UI can paint them. Tool-call arguments still assemble
  internally and emit once at `[DONE]` (partial JSON is not executable).
  `agentloop.step()` coalesces consecutive text/reasoning deltas onto the
  durable `assistant/message`, so the transcript shape is unchanged.
- **No dynamic settings/credentials re-resolution.** `Config` is fixed at
  construction; the TS adapter re-reads connection facts through a thunk once
  per call so a live settings/credential change reaches the next request
  without restart. Out of scope here — this port has no settings service to
  integrate with.
- **No app-attribution / anonymous-user-id / session-id headers.** Harness-
  deployment identity concerns, not part of the wire protocol itself.
- **Quota/context-window detection is a plain substring match** on the
  provider error body (`quota`, `balance`, `credit`, `context`+`exceed`, …),
  same idea as `isQuotaExceededError`/`isContextWindowExceededError` in the
  TS original but not byte-for-byte the same detector list.
- **No settings-driven model catalog / `resolveModel` / `listModels`.** A
  caller sets `GenerateRequest.Model` directly; nothing here advertises a
  catalog to a UI selector.

## Run

```sh
go test ./llm/deepseek/... -race -v
```

Every test runs against an `httptest` server — no network access or API key
needed.

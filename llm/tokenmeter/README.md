# tokenmeter

A Go port of [`packages/llm/token-meter`](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/token-meter/README.zh.md)
from DeepSeek Harness: fixed-heuristic token pricing for a session's surface
and a request envelope's structural overhead, so a caller can show
"approximately how much context this session is using" without a real
provider tokenizer.

## Configuration

The estimator has no configuration, same as the TS original: it always uses
one fixed heuristic — four characters per token, plus per-block/role
structural overhead. Model capacity belongs to whatever resolves exact
provider/model routing (`llm/deepseek`'s own config), not this package.

## Usage

```go
meter := tokenmeter.New()

m, err := meter.Measure(session, &tokenmeter.Header{
    System: systemPrompt,
    Tools:  agent.ToolSchemas(),
})
// m.SurfaceTokens  — the session's current surface, heuristically priced
// m.HeaderTokens   — system prompt + tool schemas' structural overhead
// m.TotalTokens    — the two summed
// m.Nodes          — per-event prices, in surface order
```

Pass `nil` for `header` to price the surface alone. `Meter.EstimateMessage`
prices one message directly, without a session, for a caller that already
has a `session.Message` in hand (e.g. previewing a message before it's
appended).

See `web/server.go`'s `buildAgent`/`GET /sessions/{id}/tokens` for how this
is wired into the demo server — `Agent.SystemPrompt()`/`Agent.ToolSchemas()`
supply exactly the two `Header` fields this package prices.

## What this port leaves out relative to the TS original

The TS original is built around **replaying a session's provider-usage
history to reuse an exact reported token count instead of a heuristic
estimate whenever it safely can** — most of its own README is about that
reuse rule (`baseline: 'usage'` vs `'estimated'`, anchored to a matching
canonical request envelope). None of that machinery exists in this port,
for a concrete, checkable reason:

- **`core/agentloop` never logs a durable request envelope.** The TS
  original replays `request/header` events to know what system prompt/tools/
  model config produced each assistant message. This port's `step()`
  builds a `GenerateRequest` inline per call and never appends it to the
  session log — there is no durable envelope to replay or compare against,
  so there is nothing to key a reuse decision on.
- **`core/session`'s `assistant/message` events never carry provider
  usage.** `llm/deepseek` does parse an API response's `usage` object
  (`mapUsage` in `translate.go`) — but nothing in the runtime streaming path
  (`adapter.go`'s `pump`) attaches it to a `StreamChunk`, and
  `agentloop.StreamChunk` has no `Usage` field to carry it even if it did.
  `mapUsage` today is only ever called from a test. Without usage on the
  log, there is no provider-reported number to ever prefer over the
  heuristic.

Given that, this port keeps exactly what still makes sense without either
piece: `estimate.go` (`estimateContent`/`estimateMessage`/`estimateHeader`,
byte-for-byte the same pricing formula) and `surface.go`
(`foldSurfaceTokens`, replaying `core/session`'s append/replace surface
ops into priced nodes — this *is* how the TS original pockets the effect of
a compaction-style replace: shrink the surface, and every subsequent
measurement is smaller). `Measurement` is deliberately a smaller shape than
the TS original's `TokenMeasurement`: no `Baseline` (`none`/`estimated`/
`usage`) and no `SurfaceDeltaTokens`, since both exist there only to
support the usage-anchor reuse above — every measurement here is always a
full heuristic reprice, so `TotalTokens` is just `HeaderTokens +
SurfaceTokens`, and (being a sum of non-negative per-node prices) can never
go negative the way a signed delta could, so there is no `max(0, ...)`
clamp to port either.

Beyond the usage-anchor system specifically:

- **No incremental per-session replay cache.** The TS `TokenMeter` keeps a
  `WeakMap<Session, ReplayState>` so repeated `measure()` calls only fold
  the log's *new* tail, making each call O(surface) rather than O(log).
  This port's `Meter` holds no state at all — `Measure` replays a session's
  full event log from scratch every call. Simpler and concurrency-free, at
  the cost of O(log) instead of O(surface) per call; acceptable for this
  port's in-memory, short-lived demo sessions (see `core/session.Store`'s
  own README on why persistence was never in scope here either).
- **No session-projection units** (`tokenUsage`/`contextPressure`/
  `contextBreakdown` in the TS original's `usage-projection.ts`/
  `breakdown-projection.ts`). Those register into `ctx.sessionProjections`,
  a generic incremental-projection registry nothing in this Go port has —
  no other package here has one either, so there was nothing to plug into.
  A caller wanting a live "context usage" readout calls `Measure` directly
  instead, same as this package's own `web` wiring does.
- **No Cordis `Service` singleton** (`ctx.tokenMeter`). This port has no
  dependency-injection container; `Meter` is a plain struct a caller
  constructs and holds, the same pattern `plan/planmode.Controller` and
  every other stateful package in this module already uses.
- **No invariant companion.** The TS package ships `invariant.ts` to
  validate provider-usage anchoring in durable session history
  independently of the running process. With no usage anchoring in this
  port, there is nothing for an equivalent to check.
- **`tool-result` prices as flat text, not recursively.** The TS original's
  `estimateContent` recurses into a tool-result block's own nested content
  array. This port's `session.ContentBlock` stores a tool result's content
  as flat `Text` (see `core/session`'s `DeriveEventMessage` and every
  tool's own `renderStringResult`) — there is no nested structure to
  recurse into, so it prices the same way `BlockText` does. A structural
  difference between the two type systems, not a simplification.

None of this changes the pricing *formula* itself — `estimateContent`/
`estimateMessage`/`estimateHeader` compute byte-for-byte the same numbers
the TS original would for the same content. What's missing is the layer
above it that would let a fully-wired deployment prefer an exact provider
count over the heuristic when one is available; adding that later needs,
in order: a `TokenUsage` field on `agentloop.StreamChunk`, `adapter.go`
attaching parsed usage to it, and `step()` folding it onto the
`assistant/message` event it already appends.

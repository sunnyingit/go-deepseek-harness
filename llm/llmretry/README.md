# llmretry

A Go port of [`packages/llm/llm-retry`](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-retry/README.zh.md)
from DeepSeek Harness: a retry policy for `core/agentloop`'s
`Hooks.RequestError` extension point. A failed model request — rate limit,
5xx, timeout, transport error, or a degenerate empty completion — waits out
a bounded exponential backoff (with jitter) and retries in place, in the
same step, invisible to the model: a retried request re-sends the exact
same prompt, so nothing about the retry itself ever reaches the
conversation.

## Usage

```go
hooks := agentloop.Hooks{
    RequestError: llmretry.Hook(llmretry.NormalPolicy()),
}
agent := agentloop.NewAgent(sess, llm, tools, systemPrompt, hooks)
```

`Hook` closes over per-`(turn, step)` retry counters, so build one `Hook`
per `Agent` — see `web/server.go`'s `buildAgent`, which does exactly that
alongside the rest of that function's per-session tool construction.

`NormalPolicy()` matches the TS original's own default: five retries,
500ms..10s bounded exponential backoff, 10% jitter, over
`DefaultRetryableCodes` (`EMPTY_RESPONSE`, `RATE_LIMIT`, `SERVER`,
`TIMEOUT`, `TRANSPORT` — `llm/deepseek`'s own `Code` vocabulary). Adjust any
field on the returned `Policy` before passing it to `Hook`:

```go
policy := llmretry.NormalPolicy()
policy.MaxRetries = 8
hooks := agentloop.Hooks{RequestError: llmretry.Hook(policy)}
```

`AlwaysPolicy(backoff)` retries every request failure — any code, no
budget — until the step's context is cancelled (turn cancellation, agent
disposal). The caller owns the cost and latency that implies; there is no
budget to fall back on.

A provider's error type opts into classification by implementing:

```go
type classifiable interface {
    RetryCode() string                 // e.g. "RATE_LIMIT"
    ProviderRetryDelay() time.Duration  // 0 if the provider suggested nothing
}
```

`llm/deepseek`'s `*Error` implements it already (`RetryCode`,
`ProviderRetryDelay` — the latter named apart from its own `RetryAfter`
field, since a method can't share a field's name). An error that doesn't
implement it classifies as code `""`, which is never in
`DefaultRetryableCodes` — so an adapter that hasn't opted in fails fast
under `ModeNormal`, and still retries unconditionally under `ModeAlways`.

## A bug this package's wiring exposed and fixed

`core/agentloop.Agent.step()`'s only call to `Hooks.RequestError` used to be
on a **mid-stream** chunk error (`StreamChunk.Err`). But `llm/deepseek`'s
adapter reports every HTTP-level failure — rate limits, 5xx, auth,
transport errors, the exact classes `DefaultRetryableCodes` targets — as
`Stream`'s own **synchronous** error return, before any chunk is ever sent.
That path returned straight to the caller without ever consulting the hook,
so a `RequestError` retry policy would almost never actually fire. Fixed in
the same change that added this package: `step()` now runs the hook on both
error paths.

## What this port leaves out relative to the TS original

- **No durable session retry events.** The TS plugin appends `llm/retry`
  (before the wait) and `llm/retry-started` (after it) to the session log —
  durable, replayable state a remote renderer or the companion `./invariant`
  module can validate independently of the running process.
  `core/session.Session` could carry these the same way `assistant/chunk`
  already does, but nothing in this codebase consumes them yet (`web`'s SSE
  stream has no `retry` event type), so it isn't built speculatively. A
  caller wanting retry visibility today gets it for free at the log level:
  a retried request simply repeats `assistant/chunk` from the point the
  prior attempt failed, since only the *durable* delta (the successful
  completion) is ever appended to `DeriveMessages()`'s view.
- **No `retryId` chains or `policyKey`-scoped retry counts across a
  provider swap.** The TS design supports one route serving many provider
  adapters, each with its own `retryPolicy`, and tracks retry counts
  per-`(turn, step, provider, policyKey)` reconstructed from session
  history so that swapping providers mid-session starts a fresh count. This
  port's `agentloop.Agent` has exactly one `LLM` for its whole lifetime (see
  `core/agentloop`'s own docs) — there is no provider to swap, so the
  `(turn, step)` in-memory counters `Hook` keeps are sufficient without
  reconstructing anything from history.
- **No Cordis waterfall composition.** The TS plugin is one listener in a
  chain (`agent/request-error`'s waterfall): `always` mode explicitly
  requests downstream recovery first, then applies its own fallback only if
  that chain declines. `core/agentloop.Hooks.RequestError` is a single
  function slot, not a chain — so `AlwaysPolicy` has no downstream to defer
  to; it simply always retries.
- **No separate invariant companion.** The TS package ships
  `./invariant` as an independently-installable validator for the durable
  events above. With no durable events in this port, there's nothing for an
  equivalent to check.

None of these change the retry *behavior* a caller observes — backoff math,
jitter, retryable-code gating, provider-`Retry-After` handling, and
cancellation all match the TS original. They remove observability/audit
machinery this codebase has nowhere to plug in yet.

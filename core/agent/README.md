# agent

A Go port of `packages/core/agent` from
[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness): the
`Agent` interface, a live-agent registry with creator ownership, and a
process-local "initiator" scope — all independent of the concrete driver
(`core/agentloop`), so a caller programs against `agent.Agent` /
`agent.Registry` without depending on `agentloop`'s driver internals beyond
the seam it already exposes.

## What's ported

- **`Agent` interface** (`agent.go`): the live surface every plugin should
  program against — `Status`, `Session`, `Followup`/`Steer`/`Inject`,
  `Cancel`, `WhenIdle`. `*agentloop.Agent` satisfies it (checked at compile
  time), and so could a different concrete driver.
- **`Registry`** (`registry.go`): `Register`/`Get`/`List`/`Roots`/`IsOwnedBy`,
  plus `SetFactory`/`Create` — tracks live agents by an externally supplied
  id and creator ownership, mirroring `ctx.agents`. The returned disposer is
  a capability: a stale disposer from an already-removed entry can never
  remove a *different* agent later registered under the same id (tested by
  `TestStaleDisposerCannotRemoveAReplacementEntry`).
- **Initiator scope** (`initiator.go`): `WithInitiator`/`WithoutInitiator`/
  `CurrentInitiator`/`RequireInitiator` — this port's answer to the TS
  `AsyncLocalStorage`-based scope. Go has no ambient goroutine-local storage,
  so the idiomatic substitute is a `context.Context` value the caller
  threads explicitly through whatever call path needs to know "which agent
  initiated this."

## Deliberate simplifications vs. the TS original

- **The initiator scope isn't wired into `agentloop`'s own execution.** In
  TS, `AgentLoop` runs its whole driver lifetime inside
  `ctx.agents.withInitiator(this, () => this.kick())`, so every tool's
  `exec.agent` and every nested async call downstream genuinely inherits it
  ambiently — and `dsh-agent-loop` depends on `dsh-agent` to do this, the
  reverse of this port's `agent → agentloop` import direction (see below).
  This port's `agentloop.Agent` builds its own internal `context.Context`
  fresh from `context.Background()` in `wakeDriver()`, and `Followup`/
  `Steer`/`Inject` don't even accept a `context.Context` to thread one
  through — so nothing external can inject an initiator-tagged context into
  a tool's `Execute(ctx, ...)` call. `Execute` already receives the calling
  `*Agent` as an explicit parameter, which is the reliable way to know the
  caller; `WithInitiator`/`CurrentInitiator` remain useful as a
  caller-side convention for a tool's *own* nested bookkeeping (see
  `examples/subagent`'s `delegateTool.Execute`), just not as something
  `agentloop` populates for you.
- **The import direction is reversed from TS.** `dsh-agent` has "zero loop
  dependency" in the original; this package imports `agentloop` for its
  conversation vocabulary (`Message`, `Session`, `CancelCause`) instead of
  the other way around. Splitting that vocabulary into a session-only
  foundation package both `agent` and `agentloop` could depend on (matching
  TS's `dsh-session` boundary) would fix this properly, but would ripple
  through every file in `agentloop`, `llm/deepseek`, and `plan/planmode`
  that already imports `agentloop.Message` et al. — judged disproportionate
  scope for this port alone.
- **No identity on `Agent` itself.** The TS interface carries `agent.id`;
  this port keeps identity entirely in the `Registry` (`Register(id, agent,
  ownerID)`) instead of adding an `ID()` method to `agentloop.Agent`. Every
  other port in this codebase has extended `agentloop`'s surface only when a
  real feature needed it (`Session()`, the `*Agent` parameter on `Hooks`/
  `ToolExecutor`); an id nothing here yet consumes didn't clear that bar.
- **No `agent.ctx` scoped DI container.** The TS `Agent` carries a Cordis
  scope plugins register tools/prompt-sections/listeners through, unwound on
  disposal. `agentloop` has no Cordis-equivalent DI system to expose here —
  same boundary the rest of this codebase draws around Cordis plumbing.
- **No public inbox read/mutate surface.** `agentloop.Inbox` is intentionally
  unexported; `Followup`/`Steer`/`Inject` remain the only way in.
- **No `agent/*` live event bus.** The TS package declares the full
  `agent/created`/`agent/disposed`/`agent/pre-step`/… vocabulary as Cordis
  events so plugins never import the loop. This port has no event bus to
  declare it on; a caller that needs to react to registry changes reads the
  `Handle`/disposer it already holds instead of subscribing to a
  notification.
- **No session persistence, so no `Resume`.** `Create` is the only factory
  path — it builds a fresh agent through `Factory`; there is nothing here to
  load a persisted session from.
- **No `foldConsumedWork` or `installModelSelection`.** Both are real pieces
  of the TS package, but narrower utilities layered on facilities this port
  doesn't have in the same shape (a `request/header` log entry to compare
  against, a multi-section system prompt to install a selection into) —
  out of scope rather than approximated.
- **Concurrent same-id creation is not race-free the way TS's is.** `Create`
  calls the factory, then registers; TS explicitly supports "more than one
  operation may prepare, but only one can enter" with rollback for every
  loser. This port's `Registry.Create` is simpler: the factory runs
  unconditionally (verified by `TestCreateDuplicateIDDoesNotLeakTheConstructedAgent`
  — the constructed-but-unregistered agent is simply discarded, uncleaned,
  by the loser), and `Register` is the sole atomicity boundary.

## Run

```sh
go test ./core/agent/... -race -v
```

See [`examples/subagent/main.go`](../../examples/subagent/main.go) for a
demo: a root agent delegates a sub-task to a specialist agent it creates
through the registry, and the demo inspects ownership and the initiator
scope during that delegation.

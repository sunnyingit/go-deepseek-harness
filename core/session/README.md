# session

A Go port of [`packages/core/session`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/core/session)
(`dsh-session`) from DeepSeek Harness: the append-only, event-sourced session
log, its surface projection, and an in-memory store with fork support.

> A `Session` is the append-only source of truth for an agent's whole
> interaction history — the LLM message history is *derived* from it.

`core/agentloop` now depends on this package (via type aliases — see its
README) instead of owning its own duplicate, simplified session code. This
matches the TS dependency direction: `agent-loop` depends on `session`, not
the reverse.

## Ported

- **The vocabulary** (`types.ts`): `Role`, `BlockKind`, `ContentBlock`,
  `Message`, `Header` (TS `SessionHeader`), `Event` (TS `SessionEvent`).
- **`Session`**: `Append` (commits a validated event), `DeriveMessages`
  (projects the surface into an LLM message array, cached and only
  re-extended for nodes appended since the last call), `Events`, `Header`,
  `LastTurn`.
- **The surface** (`surface.ts`): the ordered, message-producing view
  `DeriveMessages` actually walks. An `append` op adds to the tail; a
  `replace` op shadows an existing contiguous range with one new event
  (compaction/pruning) without deleting the shadowed events from the log.
  Validated before commit: a replace range must currently be on the surface,
  `SourceEventSeqs` must cite every shadowed node (and only earlier events),
  and a `tool/result` replacement may change only its result content — not
  the call it answers, its role, or its shape.
- **`Store`**: `Create` (with TS's `session-<n>` auto-id scheme), `Get`,
  `List`, and `Fork` — seeding a child session from a source's log up to a
  boundary event, recording `ParentSession`/`SeedLength` lineage, and
  rejecting a boundary that lands inside an open turn (a fork mid-turn would
  seed a child with a step that never finished).

## Deliberately not ported

- `chunk-rows.ts` — the durability codec converting event sequences to
  compact storage rows. This port has no persistence backend at all: a
  `Session`/`Store` lives only in process memory, same as `core/agentloop`'s
  session was before this refactor.
- `repair.ts` — crash recovery that synthesizes error results for tool calls
  interrupted mid-flight on process restart. With no persistence, there is
  nothing to recover *from*.
- `invariant.ts` — a debug-only relational trace checker (monotonic
  sequences, turn/step enclosure, tool-call/result pairing) that runs
  alongside the store as an assertion layer. A safety net, not core
  behavior; `Append`'s own validation already rejects the structural
  violations that matter for correctness (see "Ported" above).
- `json.ts` — lossless deep validation/copying of arbitrary durable event
  payloads before they're allowed into the log. `Append` here accepts `any`
  and trusts the caller, same posture `core/agentloop` already had.
- `preparation.ts` — Cordis's ordered `prepare()`/`enter()`/`announce()`
  service teardown lifecycle. Deployment plumbing, out of scope by design,
  same as every other Cordis concern in this codebase.
- `request-header.ts` — reconstructing a canonical snapshot of the request
  envelope (model, tools, system prompt, …) actually sent for a turn, folded
  from logged `request/header`-style events. `core/agentloop` never logs
  those events, so there is nothing here to fold.
- `known-event-types.ts` — a persistence catalog documenting every
  registered event type and its schema. This port has no persistence layer
  to register against; event types are the plain string literals
  `core/agentloop` already logs (`"turn/start"`, `"user/message"`, …).

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../examples/session/`](../../examples/session/) for a runnable demo
of this package's own additions over `core/agentloop`'s old ad-hoc session:
`Store.Fork`'s open-turn rejection and lineage, and surface-replace
compaction (pruning a verbose tool result in place without deleting it from
the log, then forking a session that inherits the *compacted* view). No LLM
involved — this package is pure log/projection plumbing.

## Files

| File | Ported from | Contents |
|---|---|---|
| `types.go` | `types.ts` | conversation vocabulary, `Header`, `SurfaceOp`, `Event` |
| `surface.go` | `surface.ts` | `DeriveEventMessage` (per-event projection rule), the validate-then-commit `surfacePlan`/`surfaceFold` the ordered surface is built from |
| `session.go` | `index.ts`'s `Session` class | `Session`, `Append`, `DeriveMessages`, `NewSeeded` |
| `store.go` | `index.ts`'s `SessionStore` service | `Store`, `Create`/`Get`/`List`/`Fork`, `ForkError` |
| `session_test.go` | — | `Append`'s surface-eligibility and replace validation, `DeriveMessages` projection/caching/mutation-safety, seeding |
| `store_test.go` | — | `Create`/`Get`/`List`, and `Fork`'s boundary/lineage/open-turn/liveness rules |

`chunk-rows.ts`, `repair.ts`, `invariant.ts`, `json.ts`, `preparation.ts`,
`request-header.ts`, and `known-event-types.ts` have no Go file — see
"Deliberately not ported" above.

See [`../agentloop/README.md`](../agentloop/README.md) for how
`core/agentloop` consumes this package via type aliases.

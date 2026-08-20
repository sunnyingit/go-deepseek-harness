# tools

A Go port of [`packages/core/tools`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/core/tools)
(`dsh-tools`) from DeepSeek Harness: a tool registry and execution pipeline.
A tool registers a schema, a mandatory output declaration, and an `Execute`
function; every call then runs through an ordered pipeline — pre-execute
policy, monotonic guards, dispatch (timeout + panic containment), post
-execute policy, a definition-owned content finalizer, and a result
observer — before settling into one materialized `ExecutionResult`.

[`Bridge`](bridge.go) adapts a `Registry` to `core/agentloop`'s
`ToolExecutor`, so an `Agent` can dispatch every model-issued call through
this package's pipeline instead of a bare hand-rolled `ToolExecutor` (compare
[`examples/react`](../../examples/react/) with [`examples/tools`](../../examples/tools/)).
This package imports `core/session` (for `Message`/`ContentBlock`) and
`core/agentloop` (for `Bridge`); nothing imports back — one-directional, same
as every other satellite package in this codebase.

## Ported

- **The parameter DSL** (`schema.ts`): `ValueSchema`/`Param`/`ParameterSchema`
  cover `string`/`number`/`integer`/`boolean`/`null`/`array`/`object`, the
  author-only `json` escape hatch, and `oneOf`, each with `description`
  /`title`/`default`/`examples` annotations plus `enum`/`const` on scalars.
  One flattened `ValueSchema` struct with a `Kind` field stands in for the
  TS original's eight-member tagged union — Go has no tagged unions, and a
  `Kind` switch reads the same as the DSL's own type discrimination.
- **JSON Schema generation**: `ValueSchema.ToJSONSchema()` /
  `ParameterSchema.ToJSONSchema()` render the wire schema a `ToolSchema`
  carries.
- **Argument and output validation**: `ValidateArgs` walks a `ParameterSchema`
  against caller-supplied arguments (required/type/enum/const/nested
  checks), producing one violation message per problem — ported from
  `validateArgs`/`validateJsonSchemaValue`. The same recursive validator
  checks a tool's *returned* value against its declared `Output.Schema`
  before rendering, so a broken `Execute` implementation surfaces as an
  ordinary error result instead of corrupting the model-facing transcript.
- **The execution pipeline** (`index.ts`'s `ToolRuntime.execute`):
  argument validation → pre-dispatch cancellation check → `PreExecute`
  waterfall → `Guard` chain (monotonic — a guard can never un-deny a call
  pre-execute already denied) → dispatch (`PostExecute`-adjacent timeout and
  panic containment) → `PostExecute` waterfall → `FinalizeContent` → result
  observers. Two failures — an unknown tool name, and arguments that fail
  validation — bypass `PreExecute`/`Guard`/`PostExecute` entirely (policy
  never got a chance to run over something the registry itself already
  rejected) but still reach `FinalizeContent` and the observers, matching
  the TS original's two-tier bypass (`'final-result'` vs `'post-result'`).
- **Cancellation semantics**: a caller-cancelled `context.Context` before
  dispatch starts produces `ABORTED_BEFORE_DISPATCH`; cancellation arriving
  after a successful dispatch supersedes it with `ABORTED`, but a *tool's
  own* error from an already-started call is preserved, never overwritten.
  `context.Context` replaces `AbortSignal` throughout, same substitution
  `core/agentloop` already makes.
- **`ExecutionMode`**: parallel only when a tool declares
  `IsConcurrencySafe` and it returns exactly `true` for these arguments — a
  panic or any other return is exclusive, fail-closed like the original.
- **Monotonic guards and waterfall hooks**: `Guard`, `PreExecuteHook`, and
  `PostExecuteHook` are registrable (not single callback fields), matching
  Cordis's `tools/pre-execute`/`tools/post-execute` events being genuinely
  multi-listener chains rather than one fixed hook.

## Deliberate simplifications vs. the TS original

- **Code Mode is not ported at all** (`code-mode.ts`, `py-types.ts`,
  `ts-types.ts`): the `run_code` transport, the generated TypeScript/Python
  SDK, and the per-run dispatch bridge are a large, separate subsystem
  layered on top of the same registry this package already provides. A
  caller wanting Code Mode-style batching can build it against `Registry`
  directly; nothing here assumes its absence.
- **No agent-scoped `presentAs`/`restrict`**: the TS original's
  scope-shadowing (`ctx.tools` resolving differently per agent, restricting
  a subagent's visible tools) is a Cordis `ctx` concept this Go port has no
  equivalent context system for. `Registry` has exactly one global scope;
  see `core/agent` if per-agent tool visibility is what you need — it would
  compose with a `Registry` per agent rather than one shared scoped registry.
- **`ask` (approval-service integration) is dropped**: `PreDecision` only has
  `Deny`, not TS's third `ask` variant. This port has no approval service,
  and the TS original's own documented fallback for a missing one is
  immediate denial — so `ask` would add nothing here `Deny` doesn't already
  cover directly.
- **`TimeoutMs` is actually enforced, not just declarative**: TS's own docs
  call `timeoutMs` "declarative only... enforcement requires a separate
  timeout-policy wrapper" because `AbortSignal` has no built-in timer. Go's
  `context.WithTimeout` makes real enforcement nearly free, so `dispatch`
  wraps the body's context in one when `TimeoutMs > 0` instead of leaving it
  as documentation a caller has to act on themselves.
- **No `invariant.ts`**: a debug-only relational trace checker (sequencing,
  pairing). `Register`/`Execute`'s own validation already reject the
  structural violations that matter for correctness.
- **`oneOf` is "any branch matches", not strict exclusivity**: real JSON
  Schema `oneOf` requires *exactly* one branch to validate; this port
  accepts a value valid against *any* branch. The TS README's "exact-one"
  language describes a type-level constraint (the DSL's tuple type requires
  at least two variants), not a documented runtime exclusivity check, so
  this keeps validation simple rather than chasing ambiguous-match edge
  cases a disjoint schema design shouldn't hit in practice.
- **No `known-event-types.ts` persistence catalog**: this port has no
  persistence layer (see `core/session`'s own README) to register event
  types against.
- **`testing.ts` has no Go equivalent**: its helpers are TS-test-runner
  specific; this port's own tests build fixtures directly (see
  `echoDef` in `registry_test.go`).

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../examples/tools/`](../../examples/tools/) for a runnable demo: a
`Guard` enforcing a `/workspace`-only read policy no tool body can override,
and an `OnResult` observer auditing every call (including denied ones),
driving a real DeepSeek + ReAct loop through `Bridge`.

## Files

| File | Ported from | Contents |
|---|---|---|
| `types.go` | `index.ts` (types) | `Token`, `Execution`/`ExecutionInput`/`RunContext`, `ExecutionResult`/`Failure`, `PreDecision`/`PostDecision`/`Guard`, the hook function types, `ExecutionMode`, `ToolSchema` |
| `schema.go` | `schema.ts` | `ValueSchema`/`Param`/`ParameterSchema`, `ToJSONSchema`, `ValidateArgs`/`validateValue` |
| `registry.go` | `index.ts` (`ToolRuntime`) | `ToolDefinition`, `Output`, `Registry` and the full execution pipeline |
| `bridge.go` | — | `Bridge`, adapting `Registry` to `core/agentloop.ToolExecutor` |
| `schema_test.go` | — | DSL validation and JSON Schema rendering coverage |
| `registry_test.go` | — | registration, the full pipeline (bypass tiers, guards, waterfalls, cancellation, timeout, panic containment, `DeferContext`/`ConcludeTurn`, output validation) |
| `bridge_test.go` | — | `Bridge` driving a real `core/agentloop.Agent` end to end |

`code-mode.ts`, `py-types.ts`, `ts-types.ts`, `invariant.ts`,
`known-event-types.ts`, and `testing.ts` have no Go file — see "Deliberate
simplifications" above.

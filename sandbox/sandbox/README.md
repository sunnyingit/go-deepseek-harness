# sandbox

A Go port of [`packages/sandbox/sandbox`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sandbox/sandbox)
(`dsh-sandbox`) from DeepSeek Harness: the platform-agnostic vocabulary a
confinement backend is built against — modes, policy, the `Provider`
contract, writable-root derivation, and the escalation state machine a
denied call uses to ask for a wider mode.

This package defines the contract; it confines nothing itself. See
[`../local/`](../local/) ([`go-harness/sandbox/local`](../local/)) for a
real implementation — the only package in this codebase that imports this
one, one-directional like every other pairing here.

## Ported

- **`Mode`** (`read-only` / `workspace-write` / `danger-full-access`) and
  `Mode.Confined()`: `read-only` permits reads everywhere but writes only to
  essential sinks (`/dev/null`); `workspace-write` additionally permits the
  workspace root and platform temp directories; `danger-full-access` bypasses
  confinement entirely and must never reach a `Provider`.
- **`ExecutionPolicy` / `Policy`**: `Policy` is an `ExecutionPolicy` already
  known to need confining — `NewPolicy` is the only constructor, and it
  rejects `DangerFullAccess` outright rather than silently downgrading it,
  since reaching a `Provider` with that mode is a caller bug (that mode means
  "run the command directly, no `Provider` involved at all").
- **`Provider`**: `Confine(argv, policy) (ConfinedArgv, error)` — wraps a
  command's argv with a runner, profile, and separator; never runs anything
  itself. "Silent unconfined passthrough is forbidden": an unsupported
  platform or missing runner binary must return `*UnavailableError`, never
  the original argv back unwrapped.
- **`WritableRoots`/`CanonicalPath`** (`roots.ts`): the canonical,
  deduplicated allow-list every backend's own dialect (Seatbelt profile,
  bwrap binds, …) is built from, so "what workspace-write actually means" is
  defined once. `CanonicalPath` resolves symlinks via
  `filepath.EvalSymlinks`, falling back to the original path when resolution
  fails (a workspace root that doesn't exist yet is still meaningful to
  grant). This isn't a cosmetic detail — see `local`'s README for a real
  bug it fixes on macOS.
- **Escalation** (`escalation.ts`): `EscalationOutcome`
  (`allowed-once`/`rejected`/`cancelled`/`unavailable`), `EscalationRequest`,
  `EscalationApprover`, `ValidateEscalationArgs`, `SandboxDenialMarker`
  /`EscalationHintMarker`, and `ApproveEscalation` — the state machine a
  call denied under a narrow mode uses to ask, once, for a wider one.
  `ApproveEscalation` rejects a non-widening request (e.g. asking for
  `read-only` from an already-`workspace-write` call) before ever consulting
  the approver.

## Deliberate simplifications vs. the TS original

- **`EscalationApprover` drops TS's generic `agent`/`signal` fields**: this
  port's version is `func(ctx, toolName, callID, reason) EscalationOutcome`
  — one blocking call, the same simplification `plan/planmode.ReviewFunc`
  already makes for plan-mode approval. A generic type parameter for a field
  no caller in this codebase would populate isn't worth the API complexity.
  A missing approver (`nil`) resolves to `Unavailable`, matching the TS
  original's documented fallback ("missing approval support turns ask into
  denial").
- **No `invariant.ts`**: a debug-only relational trace checker. `NewPolicy`
  and `ApproveEscalation`'s own validation already reject the structural
  violations that matter for correctness.

## Run

```sh
go build ./...
go test ./... -race -v
```

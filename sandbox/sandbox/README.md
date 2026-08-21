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
- **`RunSupervised`** (`run_unix.go`/`run_windows.go`, not a port of any
  specific TS file — this port's own answer to a need every executor that
  spawns a `Confine`d argv shares): starts an already-configured `*exec.Cmd`
  in its own process group and kills the whole group, not just the direct
  child, if its context is cancelled first. This matters specifically
  because a confined argv's first element is a *wrapper* (`sandbox-exec`,
  `bwrap`) — Seatbelt re-execs in place so the grandchild problem doesn't
  arise there, but `bwrap` forks and supervises its target, so killing only
  the wrapper leaves the real process (and whatever it's doing to the
  filesystem) running unbounded. Originally lived in `tools/python`; moved
  here and shared once `shell/bashsandbox` needed the identical logic — the
  grandchild problem is a property of sandbox wrapping itself, not of
  what's actually being run.

## Additions beyond the TS original

- **`ExecutionPolicy.Network`**: a second, independent confinement axis
  alongside `Mode` — `Mode` governs file writes, `Network` governs outbound
  networking, defaulting to `false` (no run gets networking unless a
  `Policy` explicitly asks for it, on every backend — see `local`'s README
  for what each runner actually does with it). Not part of `Mode` or the
  escalation state machine above: `EscalationTargets`/`ApproveEscalation`
  are specifically about one `Mode` being strictly wider than another, and
  networking isn't a point on that scale (`workspace-write` with network is
  neither wider nor narrower than `workspace-write` without it). A caller
  wanting network access sets `Policy.Network` directly after `NewPolicy`
  returns, through its own approval path if it wants one gated — see
  `shell/toolbash`'s `network` tool parameter for how the `bash` tool
  exposes this to a model, reusing the same `EscalationApprover` callback
  `sandbox_permissions` already uses, just not through `ApproveEscalation`
  itself. Not a port of anything in `packages/sandbox/sandbox` — the TS
  original has no network dimension in its policy vocabulary at all.

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

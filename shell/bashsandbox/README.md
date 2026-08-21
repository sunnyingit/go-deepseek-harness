# bashsandbox

A Go port of [`packages/shell/bash-sandbox`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/shell/bash-sandbox)
(`dsh-bash-sandbox`) from DeepSeek Harness: a sandbox-consuming bash
executor. Every command runs as `bash -c <command>` — this package never
parses or rewrites the command itself, only wraps it.

This package imports `go-harness/sandbox/sandbox`; nothing imports back. See
[`../toolbash/`](../toolbash/) for the model-facing tool built on top of it
— a separate package, matching the TS original's own
`bash-sandbox`/`tool-bash` split (`bash-sandbox` is the executor `tool-bash`
consumes, not a tool itself).

## Ported

- **`Executor.Run`**: hands `["bash", "-c", command]` to a `sandbox.Provider`
  and spawns the returned (wrapped) argv directly, exactly as the TS
  original documents ("every command is confined by handing the provider
  the exact argv ... and spawning the returned argv directly"). `Mode`
  defaults to `sandbox.ReadOnly`; `sandbox.DangerFullAccess` bypasses the
  provider entirely — `Confine` is never called (verified by
  `TestRunDangerFullAccessBypassesProvider`, which fails the test outright
  if it is).
- **A non-zero exit is not a Go error**: `RunResult.ExitCode` carries it;
  `Run` only returns an error for something outside the command's own
  control — the sandbox backend unavailable (`*sandbox.UnavailableError`,
  retrievable via `errors.As`), a timeout (`context.DeadlineExceeded`, via
  `errors.Is`), or argv failing to even start. Matches "propagates
  unrelated errors unchanged."
- **Denial classification**: `SandboxOutcome.Denied` is a conservative,
  stderr-signature match against `sandbox.ConfinedArgv.DenialSignatures` —
  "a reported fact," not a certainty the executor itself judges. Same
  posture for `RunnerFailed` against `RunnerFailureRules`. This is exactly
  what `sandbox/sandbox`'s `ConfinedArgv` fields were already shaped for.
- **Background jobs**: `RunBackground` returns a `*Job` immediately
  (mirroring `run_in_background: true`); `Job.Wait`/`Job.Status` retrieve it
  later, matching the TS original's separate `job_output` retrieval —
  except this port hands back the handle directly rather than a
  registry-assigned id (`toolbash`'s `bash`/`bash_job_result` tool pair adds
  an id-keyed registry on top, since a stateless JSON tool call has nowhere
  else to keep a Go pointer between calls).
- **Command-execution supervision**: the process-group-kill-on-timeout logic
  (`sandbox.RunSupervised`) is shared with `tools/python` — extracted from
  `tools/python` into `sandbox/sandbox` while building this package, since
  the grandchild problem it solves (`bwrap` forks and supervises its target
  as a child the direct process doesn't see) is a property of sandbox
  wrapping itself, not of what's actually being run.

## Additions beyond the TS original

- **`RunOptions.Network`**: threads `sandbox.ExecutionPolicy.Network`
  through — a second, independent axis from `Mode`, defaulting to `false`.
  `Run` sets `policy.Network = opts.Network` on the `Policy` `NewPolicy`
  returns before calling `Provider.Confine`, and reports it back on
  `RunResult.Sandbox.Network` (always `true` under `DangerFullAccess`,
  since nothing sandboxes that run at all). `toolbash`'s `network` tool
  parameter is what actually exposes this to a model. See
  `sandbox/sandbox/README.md`'s "Additions" section for why this isn't part
  of the escalation state machine `sandbox_permissions` uses.

## Deliberate simplifications vs. the TS original

- **One confinement mode per call, not per-session override chaining**: the
  TS original resolves a policy through `ctx.sandboxPolicy` merging
  session-level overrides, deployment fallbacks, and per-call escalation.
  This port has `RunOptions.Mode` set directly per call (defaulting to
  `ReadOnly`) — a caller wanting session-level defaults applies them itself
  before calling `Run`. Escalation (widening the mode for one call) is
  `toolbash`'s job, not this package's — see its README.
- **No `BASH_ENV` / shell-startup-file handling**: not addressed either
  way — this port doesn't set or clear it, unlike the TS original's explicit
  "establish confinement before evaluating `BASH_ENV`" ordering concern.
- **No `bash-local` counterpart**: the TS original's unconfined executor
  (`dsh-bash-local`) that `bash-sandbox` is a drop-in replacement for isn't
  ported — `RunOptions.Mode: sandbox.DangerFullAccess` already covers "run
  this one command unconfined" without a whole second package.

## Run

```sh
go build ./...
go test ./... -race -v
```

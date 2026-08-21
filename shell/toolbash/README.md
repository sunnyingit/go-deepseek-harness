# toolbash

A Go port of [`packages/shell/tool-bash`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/shell/tool-bash)
(`dsh-tool-bash`) from DeepSeek Harness: the model-facing `bash` tool, built
on [`../bashsandbox/`](../bashsandbox/)'s executor.

This package imports `go-harness/shell/bashsandbox`, `go-harness/sandbox/sandbox`,
and `go-harness/core/tools`; nothing imports back.

## Ported

- **The `bash` tool schema**: `command` (required), `workdir`,
  `run_in_background`, `sandbox_permissions`
  (`workspace-write`/`danger-full-access`), `justification` — the same five
  parameters the TS original documents. `NewTools` returns it alongside a
  `bash_job_result` companion (this port's name for the TS original's
  separate job-output retrieval path); register both, since `run_in
  _background`'s output is only a job id, useless without somewhere to
  redeem it.
- **Escalation wiring**: `sandbox_permissions` + `justification` go straight
  through `sandbox.ApproveEscalation` (see `sandbox/sandbox`'s README) —
  "deny-only at the seam: this executor never negotiates permissions
  itself," approval lives entirely in `ToolOptions.Approver`. A `nil`
  Approver means every escalation resolves to `Unavailable`
  (`TestBashToolEscalationWithoutApproverIsDenied`); a configured one that
  returns `AllowedOnce` actually widens the *next* call's mode
  (`TestBashToolEscalationApprovedWidensMode` — a real denied write, retried
  once at `workspace-write`, actually succeeds).
- **A non-zero exit is not a tool error**: matches `bashsandbox.Run`'s own
  posture — `exit code: 3` renders into the tool's returned text, `IsError`
  stays false. Only an infrastructure failure (unavailable sandbox, denied
  escalation, a missing `command`) fails the call itself.

## Additions beyond the TS original

- **The `network` tool parameter** — not part of the TS original's `bash`
  schema, since `sandbox.ExecutionPolicy` had no network dimension to
  expose there either. Requesting it (with `justification`, required the
  same way `sandbox_permissions` requires one) routes through
  `ToolOptions.Approver` directly — the same human-in-the-loop callback
  `sandbox_permissions` uses — rather than through
  `sandbox.ApproveEscalation`, since that function specifically validates
  one `Mode` being strictly wider than another and has no concept of
  networking to validate. `resolveNetwork` is the small parallel function
  that does this; a `nil` Approver or a missing justification both fail the
  call the same way a denied `sandbox_permissions` escalation does
  (`TestBashToolNetworkRequestWithoutApproverIsDenied`,
  `TestBashToolNetworkRequestWithoutJustificationIsDenied`). Verified with a
  real confined `curl` against a local `httptest` server, denied without
  the request and succeeding with it and an approving `Approver`
  (`TestBashToolWithoutNetworkRequestDeniesOutboundAccess`,
  `TestBashToolNetworkApprovedAllowsOutboundAccess`).

## Deliberate simplifications vs. the TS original

- **No conditional parameter exposure**: the TS original only exposes
  `sandbox_permissions`/`justification` when the underlying executor
  advertises sandboxing support (`ctx.shell.sandboxMode`). This port's
  `bash` tool always exposes them; a deployment with no `Approver`
  configured just has every escalation attempt fail with a clear message
  instead of the parameters not existing at all.
- **`bash_job_result` is this port's own naming**, not a literal TS symbol —
  the TS original's job-output retrieval lives in the broader shell-layer
  plumbing this port doesn't otherwise carry (durable job registries,
  `job_output` as a distinct message-source kind). Here it's a second
  ordinary tool sharing an in-memory job store with `bash`.
- **No `description` UI-display parameter**: the TS schema's `description`
  field ("5-10 words, active voice" for UI/log display) isn't ported — this
  codebase has no UI layer to render it for.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../web/`](../../web/) for `bash`/`bash_job_result` wired into a
live agent server, including a real read-only-denial → escalation →
background-job sequence run against the real sandbox.

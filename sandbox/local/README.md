# local

A Go port of [`packages/sandbox/sandbox-local`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/sandbox/sandbox-local)
(`dsh-sandbox-local`) from DeepSeek Harness: a [`sandbox.Provider`](../sandbox/)
that selects and caches one platform runner — macOS uses Seatbelt
(`sandbox-exec`), Linux prefers bwrap, every other platform (Windows
included) fails closed with `*sandbox.UnavailableError`.

This package imports `go-harness/sandbox/sandbox` for the vocabulary
(`Mode`, `Policy`, `ConfinedArgv`, `WritableRoots`); nothing imports back.

## Ported

- **`Provider`**: resolves the first available candidate runner once and
  caches it — "selects and caches one platform runner." A machine's runner
  availability can't change mid-process, so re-probing on every call would
  only add cost, never accuracy. `New()` picks the default platform-ordered
  candidate list from `runtime.GOOS`; unsupported platforms get an empty
  list, which resolves to `*sandbox.UnavailableError` through the exact same
  path as "the right binary just isn't installed" — no special-casing needed.
- **The macOS Seatbelt runner (`profiles.ts`)**: **real, working
  confinement**, not a structural stand-in — `confine` wraps argv as
  `sandbox-exec -p <profile> -- <argv>` with a generated TinyScheme profile:
  reads allowed everywhere, writes denied by default except `/dev/null`,
  plus `sandbox.WritableRoots(policy)` under `workspace-write`. Verified
  against the real `/usr/bin/sandbox-exec` binary in
  `seatbelt_test.go`'s `TestSeatbeltRealConfinementDeniesWritesOutsideGrantedRoots`
  — an actual confined `touch` denied outside the granted roots and allowed
  inside them, no mocking.

  **This is exactly what caught a real bug during development**: granting
  the *un-resolved* workspace path silently confines nothing on macOS,
  because `/tmp` is itself a symlink to `/private/tmp` and Seatbelt's
  `subpath` check compares against the kernel's resolved path. This is
  precisely why `sandbox.CanonicalPath` exists and why every root
  `WritableRoots` returns is already canonicalized before a profile ever
  embeds it.

  **A second real gap, caught the same way** — running an actual confined
  process, not a synthetic test: the profile also grants `(allow
  sysctl-read)` unconditionally, in the same read-only spirit as
  `file-read*`. Without it, `import numpy` (and so `import pandas`, and so
  a `bash` call running any pandas-using skill script — see
  `shell/toolbash`) fails at import time — numpy's platform detection calls
  `os.uname()`,
  which macOS implements via `sysctl`, and Seatbelt's default deny blocked
  it even though nothing about it is a write. A script that only prints or
  writes a file never exercises this path; only a real third-party import
  does, which is why no earlier test (including this package's own) caught
  it until `web`'s end-to-end wiring did.

  **`policy.Network` adds `(allow network*)`** — not part of `profiles.ts`,
  since `sandbox.ExecutionPolicy` didn't have a network dimension to port in
  the first place (see `../sandbox/README.md`'s "Additions" section). Every
  other rule above is unconditional; this is the one gated by policy, off
  by default, matching the TS profile's own network-less baseline. Verified
  the same rigorous way as the write-confinement tests — a real `curl`
  through the real `sandbox-exec` binary against a local `httptest` server
  (`TestSeatbeltRealConfinementNetworkAccess`), denied without `Network` set
  and succeeding with it — plus empirical spot-checks during development
  against a real internet host confirming `network*` alone is enough for
  DNS resolution too, no separate `mach-lookup`/`system-socket` rule needed.
- **The Linux bwrap runner**: `confine` builds a `bwrap --ro-bind / /
  --tmpfs /tmp --unshare-all … --bind <root> <root> … -- <argv>` command,
  adding `--share-net` only when `policy.Network` is true (off by default,
  same as Seatbelt above — this is a behavior change from an earlier version
  of this port, which shared the network namespace unconditionally).
  Structurally correct, but **unverified against a real confined process**
  — this development machine has no Linux or `bwrap` to run it against.
  `bwrap_test.go` covers argv shape only (separator placement, root binds
  present under `workspace-write` and absent under `read-only`,
  `--share-net` present iff `policy.Network`), not actual enforcement.

## Deliberately not ported

- **Landlock** (the TS original's Linux fallback when `bwrap` isn't
  available): a Linux Security Module configured through raw
  `landlock_create_ruleset`/`landlock_add_rule`/`prctl` syscalls, not an
  external binary this port can just wrap argv around. Real support would
  need direct syscall numbers (or cgo) this codebase doesn't carry. A
  machine with `bwrap` on `PATH` is unaffected; a Linux machine with
  neither fails closed, which is the correct fallback behavior even though
  the *coverage* here is narrower than the TS original's.
- **`sandbox-windows-acl`** (the real Windows backend): win32 ACL/restricted
  -token manipulation through FFI (`ffi.ts`, `win32-abi.ts`) — a large,
  separate native-interop package, entirely out of scope for a Go port with
  no cgo. Windows has no candidate runner at all here, so `Provider` fails
  closed with `*sandbox.UnavailableError` through its ordinary "no available
  runner" path — not a special case, just what an empty candidate list
  already does.
- **`sandbox-policy`** (deriving a `Mode` from session/deployment config
  layers): a separate, thin TS package not directly needed to port
  `sandbox-local` itself; a caller of this package supplies a `Policy`
  directly.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../examples/sandbox/`](../../examples/sandbox/) for a runnable
demo: three real confined `touch` attempts (read-only denied,
workspace-write inside the workspace allowed, workspace-write outside it
denied) against the actual OS sandbox — no mocking.

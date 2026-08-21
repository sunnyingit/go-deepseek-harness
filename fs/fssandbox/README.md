# fssandbox

A Go port of [`packages/fs/fs-sandbox`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/fs/fs-sandbox)
(`dsh-fs-sandbox`) from DeepSeek Harness: an [`fs.FileSystem`](../fs/)
implementation that does real filesystem I/O directly — no subprocess
spawning, "the operations are the seam's own (open, rename), only the
target path is untrusted" — and fences `WriteText`/`EditText` by a
`sandbox.Mode`. Reads always pass through unconditionally: **every mode
permits reading**, the same posture every other confinement backend in
this module already has.

This package imports `go-harness/fs/fs` and `go-harness/sandbox/sandbox`;
nothing imports back.

## Ported

- **Mode-based write fencing**: `read-only` denies every mutation with
  `FS_SANDBOX_DENIED`; `workspace-write` allows one only when the target
  canonicalizes under a writable root; `danger-full-access` allows
  everything, unfenced.
- **Shared writable-root derivation with `shell/bashsandbox`**: the
  writable set comes from `sandbox.WritableRoots` — the *same* function
  Seatbelt's profile and bwrap's binds derive their own set from — "so the
  fs fence and the bash runner cannot drift." Change what
  `workspace-write` means once, in `sandbox/sandbox`, and every backend in
  this module (process confinement, filesystem confinement) picks it up.
- **TOCTOU narrowing**: the target is re-canonicalized (via
  `sandbox.CanonicalPath`) immediately before the write-confinement check,
  not reused from `Resolve` — so an ancestor symlink swapped in between is
  still caught. Like the TS original, this is a policy fence, not a kernel
  boundary: "the residual resolve-to-syscall TOCTOU is narrowed but not
  eliminated; adversarial host processes are out of scope."
- **`FS_SANDBOX_DENIED` is structured, not stderr-sniffed**: unlike
  `shell/bashsandbox`'s denial classification (which has to pattern-match a
  spawned process's stderr, since it doesn't control what ran), an in
  -process fence knows exactly what it refused and says so directly.

**A real bug this surfaced, not a hypothetical one**: `fssandbox`'s own
tests exposed a gap in `sandbox.CanonicalPath` itself — it gave up entirely
(falling back to the unresolved path) the moment the *full* target path
failed to resolve, which missed a symlinked ancestor whenever *multiple*
trailing path components didn't exist yet (e.g. writing a brand new
`workspace/nested/dir/file.txt` when `workspace` itself sits behind a
symlink, as `t.TempDir()` does on macOS via `/var` → `/private/var`, three
levels down from anything real). `CanonicalPath` now walks up to the
nearest existing ancestor, resolves *that*, and rejoins the missing tail —
see `sandbox/sandbox`'s own README and
`TestCanonicalPathResolvesAncestorSymlinkForAMultiLevelMissingPath` for the
regression test this added.

## Deliberate simplifications vs. the TS original

- **One process-wide mutation lock, not per-target locks**: the TS original
  runs each write/edit "inside a per-target backend lock" so unrelated
  files never contend. This port has one `sync.Mutex` covering the whole
  `Provider` — every mutation is still atomic (the `WriteExpectation` check
  and the write it gates can never interleave with another mutation), just
  serialized against unrelated files too. A per-target lock map is a
  reasonable follow-up if concurrent-but-unrelated-file throughput ever
  matters here; nothing about this port's actual demo load needs it yet.
- **No `dsh-fs-local` split**: the TS original layers `fs-sandbox` on top
  of a separate local-backend package it "extends." This port folds both
  into one `Provider` — there's no other implementation this module needs
  the split for.
- **`versionOf` is `mtime+size`, not a content hash**: cheap, and changes
  whenever either does — every case this port's own `WriteText`/`EditText`
  produce. A hash would catch a write that happens to reproduce the exact
  same mtime+size (vanishingly unlikely on any real filesystem's clock
  resolution), at the cost of reading the whole file on every `Stat`.
- **No Windows long-name/8.3-alias canonicalization**: matches this
  module's existing Windows posture everywhere else (`sandbox/local` has no
  real Windows runner either) — out of scope, not attempted.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../toolfs/`](../toolfs/) for the model-facing `read`/`write`/`edit`
tools built on top of this `Provider`.

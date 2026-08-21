# fs

A Go port of [`packages/fs/fs`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/fs/fs)
(`dsh-fs`) from DeepSeek Harness: the abstract filesystem contract other
packages implement ([`fs/fssandbox`](../fssandbox/)) or consume
([`fs/toolfs`](../toolfs/)). This package defines the contract only — it
touches no disk itself.

> `ctx.fs` handles storage primitives (read, write, mutation, path
> resolution); sandbox confinement handles subprocess execution boundaries.
> The contract is intentionally agnostic to confinement mechanism.

This package has **no dependency on `go-harness/sandbox/sandbox`**, for
exactly that reason — nothing in `FileSystem`'s signatures says whether or
how an implementation restricts what it will actually do. It's stdlib-only.

## Ported

- **`Target`**: an opaque resolved location — `Key` is what an
  implementation compares/opens by (two different spellings of the same
  file must resolve to an equal `Key`), `DisplayPath` is what a caller
  shows a human or model, kept separate so a symlinked or relatively
  -spelled path still reads back the way it was asked for.
- **`Info`/`PathInfo`/`DirEntry`**: stat metadata — `Version` is opaque and
  comparable, changing whenever content changes; never content itself.
- **`WriteExpectation`**: the optional guard `WriteText`/`EditText`
  enforce — `Unconditional()` (the zero value), `CreateIfAbsent()`
  (`CodeAlreadyExists` if the target already exists), or
  `ReplaceIfVersion(v)` (`CodeStaleVersion` on mismatch) — the standard
  optimistic-concurrency pattern a caller uses after a prior `Stat`/read.
- **`FileSystem`**: `Resolve`/`ProcessPath`/`FileURL`/`Contains` (path
  identity), `Stat`/`Lstat` (metadata), `ReadText`/`ReadBytes`/`ListDir`
  (reads), `WriteText`/`WriteBytes`/`EditText` (atomic mutation —
  `WriteBytes` is `WriteText`'s binary counterpart, for content that isn't
  known to be UTF-8 text, e.g. a `web/` file upload; the literal-match
  -then-replace `EditText` does happens inside the same critical section as
  the `WriteExpectation` check, so a concurrent writer can't invalidate a
  match between it being computed and applied).
- **Error codes** (`Code`/`Error`): `FS_NOT_FOUND`, `FS_NOT_DIRECTORY`,
  `FS_NOT_REGULAR_FILE`, `FS_NOT_TEXT`, `FS_TOO_LARGE`,
  `FS_PERMISSION_DENIED`, `FS_IO_ERROR`, `FS_STALE_VERSION`,
  `FS_ALREADY_EXISTS`, `FS_AMBIGUOUS_EDIT`, `FS_EDIT_NOT_FOUND`, plus
  `FS_SANDBOX_DENIED` (declared here as part of the same recognizable
  family, though only `fs/fssandbox` ever produces it).

## Deliberate simplifications vs. the TS original

- **No `streamText`**: large-file streaming. `fs/toolfs`'s own `read` tool
  already paginates/caps its output on top of a full `ReadText`/`ReadBytes`
  call, which covers the practical need this port actually has; a separate
  streaming method on the interface isn't pulled its own weight yet.
- **`FS_NOT_OBSERVED` is not ported**: that code, and the "must read before
  overwriting" enforcement behind it, belongs to a separate TS package
  (`dsh-fs-observation-policy`) this port doesn't carry — `fs/toolfs`'s
  `write`/`edit` tools run unconditionally by default, matching the TS
  original's own documented fallback for when that policy plugin isn't
  composed. `WriteExpectation` (the mechanism such a policy would be built
  on) is still fully ported at the `fs.FileSystem` layer.
- **`FS_ABORTED` is not a separate code**: Go's `context.Context`
  cancellation already surfaces as an ordinary `context.Canceled`
  /`context.DeadlineExceeded` error; a dedicated wrapper code would only
  rename what callers already handle idiomatically.
- **`Target.Key` is a plain canonicalized absolute path**, not an opaque
  per-backend handle — this port has exactly one implementation
  (`fs/fssandbox`, local disk only), so there's no remote-backend identity
  scheme the opacity needs to hide.

## Run

```sh
go build ./...
go test ./... -race -v
```

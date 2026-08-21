# toolfs

A Go port of [`packages/fs/tool-fs`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/fs/tool-fs)
(`dsh-tool-fs`) from DeepSeek Harness: the model-facing `read`/`write`
/`edit` tools, built on any [`fs.FileSystem`](../fs/) — typically an
[`fs/fssandbox.Provider`](../fssandbox/).

This package imports `go-harness/fs/fs` and `go-harness/core/tools`;
nothing imports back.

## Ported

- **`read`**: `file_path`, `offset` (1-based, default 1), `limit` (default
  and cap 2000 lines). Line-numbered output, each line truncated past 2000
  characters, total output capped at 51200 bytes — the exact three defaults
  the TS original documents (`readLimit`/`readMaxLineLength`
  /`readMaxBytes`). A pagination footer names the next `offset` to continue
  from, whichever limit is what actually cut the response short.
- **`write`**: `file_path`, `content` — full create-or-replace, reporting
  which one happened.
- **`edit`**: `file_path`, `old_string`, `new_string`, `replace_all` —
  literal-text matching (not regex, not diff-based); `old_string` must
  appear exactly once unless `replace_all` is set. The model-facing success
  messages match the TS original's own wording exactly ("The file `<path>`
  has been updated successfully." / "... All occurrences were successfully
  replaced.").
- **Structured error codes stay visible to the model**: every `*fs.Error`
  a call surfaces renders as `"<CODE>: <message>"` — `FS_NOT_FOUND`,
  `FS_SANDBOX_DENIED`, `FS_AMBIGUOUS_EDIT`, and so on — not just prose, so
  the model can react to *which* thing went wrong the same way the TS
  original's `{ name, code }` result field lets a caller do.

## Deliberate simplifications vs. the TS original

- **No `read_image` tool**: needs a multimodal attachments service
  (`ctx.attachments`) and a model-capability check
  (`ctx.llm.resolveModelInfo`) this codebase doesn't have — already
  documented as conditional in the TS original itself ("appears only when
  `ctx.attachments` is mounted"), so dropping it doesn't remove anything
  that was guaranteed to exist there either.
- **No mandatory "must read before overwrite" gate**: that enforcement
  belongs to the separate `dsh-fs-observation-policy` package this port
  doesn't carry (see `fs/fs`'s README) — `write`/`edit` here call
  `fs.Unconditional()`, matching the TS original's own documented no
  -policy fallback exactly. The underlying `fs.WriteExpectation` guard
  (`ReplaceIfVersion`) a policy layer would be built on is still there for
  a caller that wants to wire up the check itself.
- **No `glob`/`grep` tools**: those ship in a separate TS package
  (`dsh-tool-fs-search`, ripgrep-backed) not requested for this port.
- **No `fs/write-intent`/`fs/edit-intent` waterfall hooks**: the TS
  original lets a plugin intercept a write/edit before it happens to
  supply its own `WriteExpectation`. This port's tools always pass
  `fs.Unconditional()` directly — a caller wanting that hook point can
  wrap `ToolOptions.FS` in its own `fs.FileSystem` that intercepts before
  delegating.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../web/`](../../web/) for `read`/`write`/`edit` wired into a live
agent server alongside every other tool this module builds.

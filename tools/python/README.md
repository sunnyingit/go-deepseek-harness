# python

A purpose-built capability, not a port: provisions a Python virtualenv with
a curated document-processing toolkit, and wraps running a script against it
as a [`core/tools`](../../core/tools/) tool confined by
[`sandbox/local`](../../sandbox/local/) — so an agent can generate or read
Word/PowerPoint/Excel/PDF documents without ever running unconfined code.

Nothing in [deepseek-harness](https://github.com/deepseek-ai/deepseek-harness)
maps onto this directly. The closest upstream concept is Code Mode
(`packages/core/tools/src/code-mode.ts`), a much larger, different thing this
codebase deliberately didn't port — see
[`core/tools/README.md`](../../core/tools/README.md#deliberate-simplifications-vs-the-ts-original).
This package exists because `core/tools` + `sandbox/local` already provide
everything a document-processing capability needs; wiring them together for
Python specifically was worth doing directly.

## What it provides

- **`EnsureEnv`** (`env.go`): creates (or reuses) a virtualenv at a cache
  directory with an exact package set installed, recording what's installed
  in a marker file so a repeated call with the same package set is a fast
  no-op instead of a re-download. `DefaultDocumentPackages` covers Word
  (`python-docx`), PowerPoint (`python-pptx`), Excel (`openpyxl`), PDF
  (`pypdf`, `reportlab`), tabular data (`pandas`), and images (`Pillow`).
  This step needs real filesystem and network access and runs **outside**
  any `sandbox.Provider` — it produces the environment a later confined run
  treats as read-only input, the same way a container image is built
  unconfined and then run confined.
- **`NewRunTool`** (`tool.go`): a `core/tools.ToolDefinition` named
  `run_python`. It stages the model-supplied code as a script file under a
  workspace root, confines `<venv python> <script>` through a
  `sandbox.Provider`, runs it with its working directory set to the
  workspace (so a script's relative paths — `open("report.docx", "w")` —
  land where they're supposed to, not wherever the host process happened to
  start), and returns combined stdout+stderr, capped at 64 KiB. A non-zero
  exit or a timeout becomes an ordinary tool failure, not a panic or a hung
  process.

## A cancellation bug this caught

`exec.CommandContext`'s default cancellation only signals the *direct*
child. Argv's first element here is always a confinement wrapper
(`sandbox-exec`, `bwrap`) — Seatbelt happens to re-exec in place (no
grandchild), but bwrap forks and supervises its target, so killing only the
wrapper would leave the real Python process running unbounded and leave
`Cmd.Wait()` blocked forever on a stdout pipe an orphaned grandchild still
holds open. The fix — start the confined command in its own process group
(`Setpgid: true`) and kill the whole group (`Kill(-pid, ...)`) on context
cancellation — was caught by `TestRunToolTimeout` actually hanging during
development, not by inspection. It originally lived here as
`run_unix.go`/`run_windows.go`; once `shell/bashsandbox` needed the
identical supervision for a different command entirely, it moved to
`sandbox.RunSupervised` (see that package's README) — the grandchild
problem is a property of sandbox wrapping itself, not of running Python
specifically. `runConfined` in `tool.go` now just builds the `*exec.Cmd`
and calls it.

A related bug the same test session caught: the confined process's working
directory wasn't being set at all, so a script's relative
`open("output.txt", "w")` resolved against the *test binary's* cwd — outside
every granted root — and was (correctly) denied by the sandbox. Fixed by
setting `cmd.Dir` to the workspace root before running.

## Verified for real, not mocked

`examples/python/` runs the actual pipeline end to end: provisions the full
`DefaultDocumentPackages` venv, wires `run_python` into a real
`core/agentloop.Agent` via `core/tools.Bridge` and the `llm/deepseek`
adapter, and has the model create a Word document and a PowerPoint deck by
writing and running Python. Verified against a scripted mock DeepSeek server
(no real API key available in this environment) with the resulting
`report.docx`/`summary.pptx` read back through `python-docx`/`python-pptx`
themselves to confirm they're structurally valid files with the exact
requested content, not just non-empty output.

## Run

```sh
go build ./...
go test ./... -race -v      # env_test.go does real (small, fast) pip installs
```

See [`../../examples/python/`](../../examples/python/) for the full demo
(needs `DEEPSEEK_API_KEY`, and installs the full document-processing package
set on first run — cached after).

## Platform notes

Real confinement exists only on macOS (Seatbelt) and Linux-with-`bwrap` —
see [`sandbox/local/README.md`](../../sandbox/local/README.md). On any other
platform, `sandbox.Provider.Confine` returns `*sandbox.UnavailableError`
before a script ever runs; `run_python`'s `Execute` surfaces that as an
ordinary tool failure, the same as any other error this tool can produce.

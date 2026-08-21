# systemprompt

A Go port of [`packages/core/system-prompt`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/core/system-prompt)
from DeepSeek Harness: composable, ordered prompt sections plus strict
`{{variable}}` interpolation, assembled into one final prompt string
alongside an ordered tool-schema list — instead of one hand-maintained
string constant.

## Usage

```go
b := systemprompt.New(). // includes the fixed harness-identity line by default
    WithPersona("You help with {{project}} in {{cwd}}.").
    WithToolOrder("read", "write", "edit", systemprompt.ToolOrderRest)

b.Section(systemprompt.Section{
    Name: "tool:bash", Order: systemprompt.OrderToolGuidanceMin,
    Text: "bash: a general shell...",
})
b.Variable("project", "go-harness")
b.Variable("cwd", workspaceRoot)
b.Tools(toolRegistry.Schemas())

assembly, err := b.Assemble()
prompt, err := systemprompt.RenderPrompt(assembly)
```

See `web/server.go`'s `buildSystemPrompt` for how this is actually wired
into the demo server: harness identity + a persona section + one ordered
section per tool group, with `{{model}}`/`{{cwd}}` variables, replacing
what used to be a single static string constant. A `system_prompt` set in
`config.yaml`/`agent.system_prompt` is fed in as a `Complete: true`
section instead of bypassing this package — see that function's own
comments for why that mapping is the natural one.

## What this port leaves out relative to the TS original

- **No live `ctx.systemPrompt` service.** The TS original is a Cordis
  service every concurrent agent injects and contributes to at runtime,
  with agent-scoped layers that shadow global contributions and are
  disposed when that agent's own fiber ends. This port has no
  dependency-injection container and no live per-agent registry — a
  `Builder` is a plain Go value a caller constructs, fills in, and calls
  `Assemble()` on once, the same pattern `plan/planmode.Controller`,
  `llm/tokenmeter.Meter`, and every other stateful package in this module
  already uses.
- **No `system-prompt/assemble` waterfall.** The TS original lets
  registered listeners collaboratively rewrite or replace one assembly's
  sections/tools/variables before rendering. This port has no
  waterfall/middleware-chain concept anywhere (the same simplification
  `llm/llmretry` and `session/sessionpersistence` already made for their
  own single-hook and single-backend designs) — a caller wanting to modify
  an `Assembly` just does so directly on the Go value between `Assemble()`
  and `RenderPrompt()`.
- **No per-assembly variable *providers*, only resolved values.** The TS
  original's `variable()` takes a function re-evaluated on every
  assembly, because its agent loop re-assembles the prompt on every step.
  This port's `core/agentloop.Agent` takes one static system string at
  construction and never reassembles it — there is nothing to
  re-evaluate a provider against — so `Builder.Variable` takes a plain
  resolved string. This also collapses the TS original's two interpolation
  failure modes ("unknown reference" vs. "registered but no value this
  time") into one, since a registered variable in this port always has a
  value.
- **No dynamic runtime-context system** (`context()`/
  `suppressRuntimeContext()`). In the TS original this becomes a
  separate, sourced snapshot folded into model history under the attached
  agent loop — a genuinely different mechanism from prompt *sections*,
  and one tied to session/loop internals this port's `core/agentloop`
  doesn't expose a hook for. Not built speculatively; nothing in this
  codebase needs it yet.
- **No invariant companion.** Same reason as every other port in this
  module: no durable event log for one to validate against here (prompt
  assembly isn't itself logged), so there's nothing for an equivalent to
  check.

None of this changes what a single assembly actually computes — section
ordering (with the same `-100`/`0`/`100-199` conventional bands), the
`Complete`-section-suppresses-everything rule, tool-order validation
(`ToolOrderRest` placement, duplicate/unknown-name rejection), and
`RenderPrompt`'s strict interpolation rules all match the TS original's
own documented behavior.

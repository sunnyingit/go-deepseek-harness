# planmode

A Go port of `packages/plan/plan-mode` from
[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness): plan
mode as logged, per-session collaboration state. While active, guidance goes
out with the model's request and `exit_plan_mode` presents a completed plan
for human review before leaving. Sandbox mode and approval policy are
separate concerns — this package guides, it does not enforce.

## Try it

```sh
export DEEPSEEK_API_KEY=sk-...
go run ./examples/planmode
```

A real DeepSeek model plans a 4-day Japan trip: it researches the
destination with three simulated tools (`check_weather`, `estimate_budget`,
`search_attractions`), then proposes a day-by-day itinerary through
`exit_plan_mode`. You review it for real on the terminal — reject with
feedback and it revises, approve and it wraps up. See
[`examples/planmode/main.go`](../../examples/planmode/main.go); verified
end-to-end against a scripted mock DeepSeek server before this note was
written, so the wiring is sound even before you point it at a real key.

## Usage

```go
controller, err := planmode.New("Explore and design before presenting the complete plan through exit_plan_mode.")

tools := planmode.NewMultiTool().
    Add(&planmode.ExitTool{
        Controller: controller,
        Review: func(ctx context.Context, plan string) (approved bool, feedback string) {
            // present `plan` to a human however this deployment does that;
            // block until they answer.
            return askUser(plan)
        },
    }).
    Add(myOtherTool)

agent := agentloop.NewAgent(session, llm, tools, "you are a helpful assistant", agentloop.Hooks{
    PreStep: controller.PreStepHook(),
}).WithModel("deepseek-chat")

controller.Set(agent, true) // enter plan mode, e.g. from a /plan command
```

## What's ported from `plan-mode`

- **The logged state model**: `plan/mode` (`{active: bool}`) is a log-only,
  whole-value-replace session event; `foldPlanMode` returns the last one
  logged, defaulting to inactive — same fold, same default as the TS
  original's `foldPlanMode`.
- **Committed vs. queued selection**: `Controller.Set` mirrors
  `PlanModeController.set` exactly — between turns (`!hasOpenTurn`) it
  commits immediately; during an open turn it queues until the next accepted
  pre-step, so a mid-flight step's already-assembled context never changes
  under it. Same four outcomes (`Committed`/`Queued`/`Cancelled`/`Noop`).
- **`exit_plan_mode`**: the tool stays registered regardless of plan-mode
  state (stable tool catalog across the transition), validates the plan
  starts with an H1 heading, and queues its exit through the same pending
  mechanism `Set` uses rather than committing immediately — so the rest of
  that step's tool batch still sees plan-mode guidance.
- **`/plan [message]`**: `Controller.Command` mirrors the TS command handler
  — bare selects plan mode, the exact argument `"off"` selects inactive and
  cancels a pending entry, anything else selects plan mode and hands back the
  trimmed message for the caller to `Steer()` in.
- **The narration notice**: a one-sentence message telling the model the user
  switched modes (see the simplification below for what changed).

## Deliberate simplifications vs. the TS original

- **The guidance section is a logged message, not a re-rendered system-prompt
  section.** `agentloop.GenerateRequest` has one flat `System` string, not
  TS's ordered, per-request-recomputed section list, so this port can't
  contribute ephemeral prompt text the way `ctx.systemPrompt.section(...)`
  does. Instead, `PreStepHook` prepends the guidance as a message — but only
  on a turn's first step, to avoid logging (and paying for) the same text
  once per step of a multi-step turn. It stays in derived history for the
  rest of that turn regardless.
- **`ReviewFunc` replaces `ctx.userQuestions`.** The TS review flow is a
  full multi-provider interaction service (Web UI plan-review renderer,
  dismissal handling, `ASK_CANCELLED` detection, …). This port collapses all
  of it to one blocking callback — `func(ctx, plan) (approved bool, feedback
  string)` — that the caller wires to however their deployment actually asks
  a human. There's no separate "dismissed vs. rejected" distinction; a
  reviewer that wants one can encode it in `feedback`.
- **Narration always fires on a direct `Set`, not only when the model was
  "told" the other state.** The TS original checks `planModeAtLastHeader`
  (what the last `request/header` event described) before narrating; this
  port doesn't log `request/header` events at all (see the `agentloop`
  README), so it narrates every committed switch instead of only the ones
  where the model's last view of the world would otherwise be stale.
- **`Cancelled` clears the pending entry outright.** The TS original leaves a
  same-valued entry in `pendingIntents` for the next pre-step boundary to
  lazily clean up, so `get()` can report a `pending` value equal to the
  logged state. This port deletes it immediately in `Set`, so `Get` never
  reports a pending selection that isn't a genuine, different-valued one.
- **No session-projection unit, no `/plan` command registry, no Cordis
  plugin wrapper.** All deployment/UI plumbing — same category of thing this
  whole codebase's other ports leave out. `Controller.Command` is the
  library-level equivalent; a host wires it to its own command dispatch.
- **`MultiTool` isn't part of the TS package.** `agentloop.NewAgent` takes
  exactly one `ToolExecutor`; TS's tool registry composes many by design.
  `MultiTool` is the minimal by-name dispatcher this port needed to use
  `ExitTool` alongside any other tool at all.

## Two bugs this port's own tests caught

- `Set`'s open-turn branch wrote a same-valued pending entry to the map even
  on the `Cancelled` path (mirroring the TS lazy-cleanup behavior exactly,
  which turned out not to be worth replicating — see above). Caught by
  `TestSetCancelledRevertsQueuedSelectionBackToLogged`.
- `firstHeading` and the plan-argument validator shared one regexp that only
  matched an H1; `firstHeading` needs H1–H6. Caught by `TestFirstHeading`
  failing on an H2 input.

## Run

```sh
go test ./plan/planmode/... -race -v
```

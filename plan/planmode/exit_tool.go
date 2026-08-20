package planmode

import (
	"context"
	"regexp"
	"strings"

	"go-harness/core/agentloop"
)

// ExitPlanModeName is exit_plan_mode's model-facing tool name. It stays
// registered regardless of plan-mode state (Schemas never changes), so
// entering or leaving plan mode only changes the guidance message, never the
// request's tool catalog.
const ExitPlanModeName = "exit_plan_mode"

// ReviewFunc presents a completed plan to a human and returns their
// decision: approved (leave plan mode) or not (feedback goes back to the
// model so it can revise). This is this port's whole answer to TS's
// ctx.userQuestions review flow — one blocking callback in place of a
// separate multi-provider interaction service; see README.md.
type ReviewFunc func(ctx context.Context, plan string) (approved bool, feedback string)

// planHeadingPattern validates the plan argument itself: it must start with
// an H1 specifically ("a # heading that names it"), matching the TS
// original's exit_plan_mode check exactly.
var planHeadingPattern = regexp.MustCompile(`^#\s+\S`)

// anyHeadingPattern extracts a heading of any level (H1–H6) for display
// purposes (firstHeading) — deliberately a different, looser pattern than
// planHeadingPattern; conflating the two was this port's first bug, caught
// by TestFirstHeading failing on an H2 line.
var anyHeadingPattern = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)

// firstHeading returns the plan's first markdown heading (any level), text
// only, or "" when it has none.
func firstHeading(plan string) string {
	for _, line := range strings.Split(plan, "\n") {
		if m := anyHeadingPattern.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// ExitTool is the exit_plan_mode tool: it validates the plan's shape, then
// asks Review to approve it. Approval queues the exit through the same
// pending mechanism a direct Controller.Set uses (applied at the next
// accepted pre-step), so the rest of this step's tool batch still sees
// plan-mode guidance; rejection fails the call with the reviewer's feedback
// as the error text, which the model reads like any other tool error.
type ExitTool struct {
	Controller *Controller
	Review     ReviewFunc
}

var _ agentloop.ToolExecutor = (*ExitTool)(nil)

func (t *ExitTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}

func (t *ExitTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name: ExitPlanModeName,
		Description: "Use only in plan mode. Present your plan for the user's review and, on approval, leave plan mode. " +
			"Send the COMPLETE plan as markdown, starting with a # heading that names it. " +
			"The user may approve (carry out the plan from your next step) or keep " +
			"planning — their feedback comes back in the tool result; revise and present again.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan": map[string]any{
					"type":        "string",
					"description": "The complete plan, as markdown, starting with a # heading that names it.",
				},
			},
			"required": []string{"plan"},
		},
	}}
}

func (t *ExitTool) Execute(ctx context.Context, agent *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	fail := func(msg string) agentloop.ToolResult {
		return agentloop.ToolResult{Content: agentloop.ContentBlock{
			Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: "Error: " + msg,
		}}
	}

	if t.Controller == nil {
		return fail(ExitPlanModeName + " has no plan-mode controller configured")
	}
	if active, _ := t.Controller.Get(agent); !active {
		return fail(ExitPlanModeName + " is only available in plan mode")
	}

	plan, _ := call.Args["plan"].(string)
	if !planHeadingPattern.MatchString(strings.TrimSpace(plan)) {
		return fail(ExitPlanModeName + " requires a non-empty markdown plan starting with a # heading")
	}
	if t.Review == nil {
		return fail("no reviewer is configured to approve the plan; ask the user to switch modes directly")
	}

	approved, feedback := t.Review(ctx, plan)
	if !approved {
		if feedback == "" {
			return fail("the user chose to keep planning; revise the plan and present it again")
		}
		return fail("the user chose to keep planning; their feedback: " + feedback)
	}

	t.Controller.queueExit(agent.Session())
	return agentloop.ToolResult{Content: agentloop.ContentBlock{
		Kind: agentloop.BlockToolResult, CallID: call.CallID,
		Text: "Plan approved — plan mode exited; carry out the plan starting with your next step.",
	}}
}

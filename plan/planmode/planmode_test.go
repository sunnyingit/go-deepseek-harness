package planmode

import (
	"context"
	"testing"

	"go-harness/core/agentloop"
)

// ---- test doubles -----------------------------------------------------------

type noopLLM struct{}

func (noopLLM) Stream(context.Context, agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	ch := make(chan agentloop.StreamChunk)
	close(ch)
	return ch, nil
}

type noopTools struct{}

func (noopTools) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}
func (noopTools) Execute(context.Context, *agentloop.Agent, agentloop.ContentBlock) agentloop.ToolResult {
	return agentloop.ToolResult{}
}
func (noopTools) Schemas() []agentloop.ToolSchema { return nil }

// newBareAgent builds an *agentloop.Agent purely as a Session handle for
// Controller tests: its loop is never started (no Followup/WhenIdle), so
// tests simulate open/closed turns by appending turn/start and turn/end
// directly, then call Controller methods synchronously against it.
func newBareAgent() *agentloop.Agent {
	session := agentloop.NewMemorySession()
	return agentloop.NewAgent(session, noopLLM{}, noopTools{}, "sys", agentloop.Hooks{})
}

func openTurn(agent *agentloop.Agent, turn int) {
	agent.Session().Append("turn/start", turn)
}

func closeTurn(agent *agentloop.Agent, turn int) {
	agent.Session().Append("turn/end", map[string]any{"turn": turn, "reason": "completed"})
}

// ---- foldPlanMode / hasOpenTurn ---------------------------------------------

func TestFoldPlanModeDefaultsInactive(t *testing.T) {
	if foldPlanMode(nil) {
		t.Fatal("empty log folded active, want inactive")
	}
}

func TestFoldPlanModeLastEventWins(t *testing.T) {
	events := []agentloop.SessionEvent{
		{Type: "plan/mode", Data: map[string]any{"active": true}},
		{Type: "plan/mode", Data: map[string]any{"active": false}},
		{Type: "plan/mode", Data: map[string]any{"active": true}},
	}
	if !foldPlanMode(events) {
		t.Fatal("want active (last event wins)")
	}
}

func TestHasOpenTurn(t *testing.T) {
	cases := []struct {
		name   string
		events []agentloop.SessionEvent
		want   bool
	}{
		{"empty", nil, false},
		{"opened", []agentloop.SessionEvent{{Type: "turn/start"}}, true},
		{"closed", []agentloop.SessionEvent{{Type: "turn/start"}, {Type: "turn/end"}}, false},
		{"reopened", []agentloop.SessionEvent{{Type: "turn/start"}, {Type: "turn/end"}, {Type: "turn/start"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasOpenTurn(tc.events); got != tc.want {
				t.Fatalf("hasOpenTurn = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- Set / Get outcome matrix ------------------------------------------------

func TestSetCommitsImmediatelyWhenIdle(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()

	if got := c.Set(agent, true); got != Committed {
		t.Fatalf("Set = %q, want committed", got)
	}
	if !foldPlanMode(agent.Session().Events()) {
		t.Fatal("plan/mode was not appended to the log")
	}
	active, pending := c.Get(agent)
	if !active || pending != nil {
		t.Fatalf("Get = (active=%v, pending=%v), want (true, nil)", active, pending)
	}
}

func TestSetQueuesDuringOpenTurn(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)

	if got := c.Set(agent, true); got != Queued {
		t.Fatalf("Set = %q, want queued", got)
	}
	if foldPlanMode(agent.Session().Events()) {
		t.Fatal("plan/mode must not be appended yet — the turn is still open")
	}
	active, pending := c.Get(agent)
	if active || pending == nil || !*pending {
		t.Fatalf("Get = (active=%v, pending=%v), want (false, *true)", active, pending)
	}
}

func TestSetNoopWhenAlreadyActive(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true) // committed

	if got := c.Set(agent, true); got != Noop {
		t.Fatalf("Set = %q, want noop", got)
	}
}

func TestSetNoopWhenAlreadyQueued(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)
	c.Set(agent, true) // queued

	if got := c.Set(agent, true); got != Noop {
		t.Fatalf("Set = %q, want noop (same value re-queued)", got)
	}
}

func TestSetCancelledRevertsQueuedSelectionBackToLogged(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)

	c.Set(agent, true) // queued: logged=false, pending=true
	if got := c.Set(agent, false); got != Cancelled {
		t.Fatalf("Set = %q, want cancelled (reverting to the still-logged state)", got)
	}
	active, pending := c.Get(agent)
	if active || pending != nil {
		t.Fatalf("Get = (active=%v, pending=%v), want (false, nil) — the cancelled selection must not linger", active, pending)
	}
}

func TestSetCancelledWhenIdleAndTargetMatchesLogged(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)
	c.Set(agent, true) // queued
	closeTurn(agent, 1)

	// The turn closed with the selection still pending and never applied by
	// a pre-step; Set must compare against the still-false logged state, not
	// silently keep the stale pending value.
	if got := c.Set(agent, false); got != Cancelled {
		t.Fatalf("Set = %q, want cancelled", got)
	}
}

// ---- PreStepHook --------------------------------------------------------------

func TestPreStepHookAppliesPendingSelectionAndNarrates(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)
	c.Set(agent, true) // queued

	hook := c.PreStepHook()
	enter, messages := hook(context.Background(), agent, 1, 2, nil)
	if !enter {
		t.Fatal("PreStepHook rejected the step, want accepted")
	}
	if !foldPlanMode(agent.Session().Events()) {
		t.Fatal("pending selection was not committed by the pre-step boundary")
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want exactly the narration (step != 1, so no guidance prepended)", messages)
	}
	if messages[0].Content[0].Text == "" {
		t.Fatal("narration message has no text")
	}
	if _, pending := c.Get(agent); pending != nil {
		t.Fatal("pending selection must be cleared once applied")
	}
}

func TestPreStepHookPrependsGuidanceOnlyOnFirstStep(t *testing.T) {
	c, _ := New("Explore before you act.")
	agent := newBareAgent()
	c.Set(agent, true) // idle -> committed immediately
	openTurn(agent, 1)

	hook := c.PreStepHook()
	userMsg := agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "hello"}}}

	_, step1 := hook(context.Background(), agent, 1, 1, []agentloop.Message{userMsg})
	if len(step1) != 2 || step1[0].Content[0].Text != "Explore before you act." || step1[1].Content[0].Text != "hello" {
		t.Fatalf("step 1 messages = %+v, want [guidance, hello]", step1)
	}

	_, step2 := hook(context.Background(), agent, 1, 2, nil)
	if len(step2) != 0 {
		t.Fatalf("step 2 messages = %+v, want none — guidance is not repeated mid-turn", step2)
	}
}

func TestPreStepHookAddsNothingWhenInactiveAndNoPending(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	openTurn(agent, 1)

	hook := c.PreStepHook()
	_, messages := hook(context.Background(), agent, 1, 1, []agentloop.Message{})
	if len(messages) != 0 {
		t.Fatalf("messages = %+v, want none", messages)
	}
}

// ---- Command ------------------------------------------------------------------

func TestCommandBareEntersPlanMode(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()

	reply, steer := c.Command(agent, "")
	if steer != "" {
		t.Fatalf("steer = %q, want empty for a bare /plan", steer)
	}
	if !foldPlanMode(agent.Session().Events()) {
		t.Fatal("plan mode was not entered")
	}
	if reply == "" {
		t.Fatal("want a non-empty reply")
	}
}

func TestCommandWithMessageEntersAndReturnsSteerText(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()

	_, steer := c.Command(agent, "  design the auth flow  ")
	if steer != "design the auth flow" {
		t.Fatalf("steer = %q, want the trimmed message", steer)
	}
	if !foldPlanMode(agent.Session().Events()) {
		t.Fatal("plan mode was not entered")
	}
}

func TestCommandOffExitsPlanMode(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)

	reply, steer := c.Command(agent, "off")
	if steer != "" {
		t.Fatalf("steer = %q, want empty for /plan off", steer)
	}
	if foldPlanMode(agent.Session().Events()) {
		t.Fatal("plan mode was not exited")
	}
	if reply == "" {
		t.Fatal("want a non-empty reply")
	}
}

func TestCommandOffOnAlreadyInactiveSessionIsIdempotent(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()

	reply, _ := c.Command(agent, "off")
	if reply != "Plan mode is already inactive." {
		t.Fatalf("reply = %q, want the idempotent message", reply)
	}
}

package planmode

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go-harness/core/agentloop"
)

func TestFirstHeading(t *testing.T) {
	cases := map[string]string{
		"# Title\nbody":        "Title",
		"intro\n## Subtitle\n": "Subtitle",
		"no heading here":      "",
		"":                     "",
	}
	for input, want := range cases {
		if got := firstHeading(input); got != want {
			t.Errorf("firstHeading(%q) = %q, want %q", input, got, want)
		}
	}
}

func toolCall(id, name string, args map[string]any) agentloop.ContentBlock {
	return agentloop.ContentBlock{Kind: agentloop.BlockToolCall, CallID: id, ToolName: name, Args: args}
}

func TestExitToolRejectsWhenPlanModeInactive(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	tool := &ExitTool{Controller: c, Review: func(context.Context, string) (bool, string) { return true, "" }}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Plan\nsteps"}))
	if result.Content.Text != "Error: "+ExitPlanModeName+" is only available in plan mode" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExitToolRejectsPlanWithoutHeading(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)
	tool := &ExitTool{Controller: c, Review: func(context.Context, string) (bool, string) { return true, "" }}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "just some prose"}))
	if result.Content.Text != "Error: "+ExitPlanModeName+" requires a non-empty markdown plan starting with a # heading" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExitToolNoReviewerConfigured(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)
	tool := &ExitTool{Controller: c}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Plan\nsteps"}))
	if result.Content.Text == "" || result.Content.Text[:6] != "Error:" {
		t.Fatalf("result = %+v, want an error", result)
	}
}

func TestExitToolApprovalQueuesExitWithoutNarration(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)
	tool := &ExitTool{Controller: c, Review: func(_ context.Context, plan string) (bool, string) {
		if plan != "# Plan\nsteps" {
			t.Fatalf("Review saw plan = %q", plan)
		}
		return true, ""
	}}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Plan\nsteps"}))
	if result.Content.Text != "Plan approved — plan mode exited; carry out the plan starting with your next step." {
		t.Fatalf("result = %+v", result)
	}

	// Still logged active (queued, not committed) — the tool call alone
	// never appends plan/mode; that happens at the next accepted pre-step.
	active, pending := c.Get(agent)
	if !active || pending == nil || *pending {
		t.Fatalf("Get = (active=%v, pending=%v), want (true, *false) — exit queued, not yet committed", active, pending)
	}
}

func TestExitToolRejectionReturnsFeedbackAndLeavesPlanModeActive(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)
	tool := &ExitTool{Controller: c, Review: func(context.Context, string) (bool, string) {
		return false, "add error handling"
	}}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Plan\nsteps"}))
	want := "Error: the user chose to keep planning; their feedback: add error handling"
	if result.Content.Text != want {
		t.Fatalf("result text = %q, want %q", result.Content.Text, want)
	}
	if active, pending := c.Get(agent); !active || pending != nil {
		t.Fatalf("Get = (active=%v, pending=%v), want (true, nil) — a rejection changes nothing", active, pending)
	}
}

func TestExitToolRejectionWithoutFeedbackUsesDefaultMessage(t *testing.T) {
	c, _ := New("guidance")
	agent := newBareAgent()
	c.Set(agent, true)
	tool := &ExitTool{Controller: c, Review: func(context.Context, string) (bool, string) { return false, "" }}

	result := tool.Execute(context.Background(), agent, toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Plan\nsteps"}))
	want := "Error: the user chose to keep planning; revise the plan and present it again"
	if result.Content.Text != want {
		t.Fatalf("result text = %q, want %q", result.Content.Text, want)
	}
}

// ---- end-to-end: a real Agent driving the whole plan -> exit flow ----------

type scriptedLLM struct {
	mu        sync.Mutex
	responses [][]agentloop.StreamChunk
	calls     int
}

func (s *scriptedLLM) Stream(context.Context, agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	s.mu.Unlock()
	if i >= len(s.responses) {
		return nil, fmt.Errorf("scriptedLLM: no response scripted for call %d", i)
	}
	ch := make(chan agentloop.StreamChunk, len(s.responses[i]))
	for _, c := range s.responses[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func TestEndToEndPlanThenApprovedExit(t *testing.T) {
	c, err := New("Explore and design before presenting the complete plan.")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	call := toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Add auth\nsteps"})
	llm := &scriptedLLM{responses: [][]agentloop.StreamChunk{
		{{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "Here is my plan."}}, {Delta: call}},
		{{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "Proceeding with the plan."}}},
	}}
	tools := NewMultiTool().Add(&ExitTool{
		Controller: c,
		Review:     func(context.Context, string) (bool, string) { return true, "" },
	})

	session := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(session, llm, tools, "sys", agentloop.Hooks{PreStep: c.PreStepHook()}).WithModel("m")

	if got := c.Set(agent, true); got != Committed {
		t.Fatalf("initial Set = %q, want committed", got)
	}

	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "design auth"}}})
	waitIdle(t, agent)

	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2 (plan step, then the post-approval step)", llm.calls)
	}
	planModeEvents := 0
	for _, ev := range session.Events() {
		if ev.Type == "plan/mode" {
			planModeEvents++
		}
	}
	if planModeEvents != 2 {
		t.Fatalf("plan/mode events = %d, want 2 (enter, then exit committed at the next pre-step)", planModeEvents)
	}
	if active, pending := c.Get(agent); active || pending != nil {
		t.Fatalf("Get = (active=%v, pending=%v), want (false, nil) after the approved exit committed", active, pending)
	}

	// The first step's messages must have carried the guidance section.
	msgs := session.DeriveMessages()
	found := false
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Text == "Explore and design before presenting the complete plan." {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("derived messages never carried the plan guidance: %+v", msgs)
	}
}

func TestEndToEndPlanRejectedThenRevised(t *testing.T) {
	c, err := New("guidance")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	firstCall := toolCall("c1", ExitPlanModeName, map[string]any{"plan": "# Draft\nv1"})
	secondCall := toolCall("c2", ExitPlanModeName, map[string]any{"plan": "# Draft\nv2 with tests"})
	reviewCalls := 0
	tools := NewMultiTool().Add(&ExitTool{
		Controller: c,
		Review: func(_ context.Context, plan string) (bool, string) {
			reviewCalls++
			if reviewCalls == 1 {
				return false, "add tests"
			}
			return true, ""
		},
	})
	llm := &scriptedLLM{responses: [][]agentloop.StreamChunk{
		{{Delta: firstCall}},
		{{Delta: secondCall}},
		{{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "done"}}},
	}}

	session := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(session, llm, tools, "sys", agentloop.Hooks{PreStep: c.PreStepHook()}).WithModel("m")
	c.Set(agent, true)

	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "go"}}})
	waitIdle(t, agent)

	if reviewCalls != 2 {
		t.Fatalf("Review called %d times, want 2 (rejected once, then approved)", reviewCalls)
	}
	if active, _ := c.Get(agent); active {
		t.Fatal("plan mode should have exited after the second, approved review")
	}
	// The rejection feedback must have reached the model as a tool result.
	found := false
	for _, ev := range session.Events() {
		if ev.Type != "tool/result" {
			continue
		}
		msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
		if msg.Content[0].Text == "Error: the user chose to keep planning; their feedback: add tests" {
			found = true
		}
	}
	if !found {
		t.Fatal("rejection feedback never appeared in the tool/result log")
	}
}

func waitIdle(t *testing.T, agent *agentloop.Agent) {
	t.Helper()
	done := make(chan struct{})
	go func() { agent.WhenIdle(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("agent never went idle")
	}
}

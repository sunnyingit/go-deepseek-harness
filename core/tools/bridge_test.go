package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

// scriptedLLM plays back one fixed chunk sequence per call — same test
// double shape as core/agentloop's own, duplicated here since it's
// unexported there.
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

func TestBridgeSatisfiesToolExecutor(t *testing.T) {
	var _ agentloop.ToolExecutor = Bridge{Registry: NewRegistry()}
}

// TestBridgeDrivesARealAgentLoop runs a full turn through core/agentloop
// with a Bridge-wrapped Registry as the ToolExecutor, proving the pipeline
// (argument validation, dispatch, Output.Render) produces exactly what the
// loop expects: an Observation the next model call's DeriveMessages() picks
// up, ending the turn once the model stops calling tools.
func TestBridgeDrivesARealAgentLoop(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, ToolDefinition{
		Name: "add",
		Parameters: ParameterSchema{
			"a": {ValueSchema: ValueSchema{Kind: KindNumber}, Required: true},
			"b": {ValueSchema: ValueSchema{Kind: KindNumber}, Required: true},
		},
		Output: Output{
			Schema: ValueSchema{Kind: KindNumber},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: fmt.Sprintf("%v", value)}}
			},
		},
		Execute: func(_ context.Context, args map[string]any, _ *RunContext) (any, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return a + b, nil
		},
	})

	call := agentloop.ContentBlock{
		Kind: agentloop.BlockToolCall, CallID: "c1", ToolName: "add",
		Args: map[string]any{"a": 2.0, "b": 3.0},
	}
	llm := &scriptedLLM{responses: [][]agentloop.StreamChunk{
		{{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "let me add"}}, {Delta: call}},
		{{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: "done"}}},
	}}

	sess := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(sess, llm, Bridge{Registry: r}, "sys", agentloop.Hooks{})
	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "what is 2 + 3?"}}})
	agent.WhenIdle()

	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2 (tool call must trigger a second step)", llm.calls)
	}

	found := false
	for _, m := range sess.DeriveMessages() {
		for _, b := range m.Content {
			if b.Kind == agentloop.BlockToolResult && b.CallID == "c1" && b.Text == "5" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("derived messages never carried the registry's observation: %+v", sess.DeriveMessages())
	}
}

// TestBridgeSurfacesArgsErrorAsAToolResult confirms an invalid call from the
// model becomes an ordinary error ToolResult the loop can feed back, not a
// crash — the same contract agentloop's own bare ToolExecutor examples make.
func TestBridgeSurfacesArgsErrorAsAToolResult(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, ToolDefinition{
		Name:       "add",
		Parameters: ParameterSchema{"a": {ValueSchema: ValueSchema{Kind: KindNumber}, Required: true}},
		Output: Output{
			Schema: ValueSchema{Kind: KindNumber},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: fmt.Sprintf("%v", value)}}
			},
		},
		Execute: func(context.Context, map[string]any, *RunContext) (any, error) { return 0.0, nil },
	})

	bridge := Bridge{Registry: r}
	result := bridge.Execute(context.Background(), nil, agentloop.ContentBlock{CallID: "c1", ToolName: "add", Args: map[string]any{}})
	if result.Content.CallID != "c1" || result.Content.Text[:6] != "Error:" {
		t.Fatalf("result = %+v, want an error ToolResult tagged with the call id", result)
	}
}

func TestBridgeExecutionModeAndSchemas(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.IsConcurrencySafe = func(map[string]any) bool { return true }
	mustRegister(t, r, def)
	bridge := Bridge{Registry: r}

	if bridge.ExecutionMode(agentloop.ContentBlock{ToolName: "echo"}) != agentloop.ModeParallel {
		t.Fatal("want ModeParallel translated through")
	}
	schemas := bridge.Schemas()
	if len(schemas) != 1 || schemas[0].Name != "echo" {
		t.Fatalf("schemas = %+v", schemas)
	}
}

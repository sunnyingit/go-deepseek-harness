package session

import "testing"

func TestCloseUnansweredToolCallsFromAssistantMessage(t *testing.T) {
	s := New("s1", Header{})
	s.Append("turn/start", 1)
	s.Append("assistant/message", map[string]any{
		"turn": 1, "step": 3,
		"message": Message{Role: RoleAssistant, Content: []ContentBlock{
			{Kind: BlockText, Text: "writing"},
			{Kind: BlockToolCall, CallID: "c-write", ToolName: "write", Args: map[string]any{"file_path": "a.html"}},
		}},
	}, WithSurfaceOp(AppendOp()))

	if n := CloseUnansweredToolCalls(s); n != 1 {
		t.Fatalf("repaired %d calls, want 1", n)
	}
	if n := CloseUnansweredToolCalls(s); n != 0 {
		t.Fatalf("second repair = %d, want 0 (idempotent)", n)
	}

	found := false
	for _, ev := range s.Events() {
		if ev.Type != "tool/result" {
			continue
		}
		for _, b := range messageBlocks(ev) {
			if b.CallID == "c-write" && b.Kind == BlockToolResult {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("want a synthetic tool/result for the assistant-message tool-call")
	}
}

func TestCloseUnansweredToolCallsSkipsAnswered(t *testing.T) {
	s := New("s1", Header{})
	call := ContentBlock{Kind: BlockToolCall, CallID: "c1", ToolName: "bash"}
	s.Append("tool/call", map[string]any{"turn": 1, "step": 1, "call": call})
	s.Append("tool/result", map[string]any{
		"turn": 1, "step": 1,
		"message": Message{Role: RoleUser, Content: []ContentBlock{
			{Kind: BlockToolResult, CallID: "c1", Text: "ok"},
		}},
	}, WithSurfaceOp(AppendOp()))

	if n := CloseUnansweredToolCalls(s); n != 0 {
		t.Fatalf("repaired %d, want 0 (already answered)", n)
	}
}

package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"go-harness/core/session"
	coretools "go-harness/core/tools"
)

func TestDisplayEventMapsShowTable(t *testing.T) {
	typ, payload, ok := displayEvent(session.ContentBlock{
		Kind:     session.BlockToolCall,
		ToolName: "show_table",
		Args:     map[string]any{"headers": []any{"A"}, "rows": []any{[]any{"1"}}},
	})
	if !ok || typ != "table" {
		t.Fatalf("displayEvent = (%q, ok=%v), want table", typ, ok)
	}
	if _, has := payload["headers"]; !has {
		t.Fatalf("payload missing headers: %+v", payload)
	}
}

func TestDisplayEventIgnoresRealTools(t *testing.T) {
	if _, _, ok := displayEvent(session.ContentBlock{ToolName: "bash"}); ok {
		t.Fatal("bash must not be treated as a display tool")
	}
}

func TestDisplayToolsRejectInvalidArgs(t *testing.T) {
	reg := coretools.NewRegistry()
	for _, def := range displayTools() {
		if _, err := reg.Register(def); err != nil {
			t.Fatalf("register %s: %v", def.Name, err)
		}
	}

	result := reg.Execute(t.Context(), coretools.ExecutionInput{
		CallID: "c1", Name: "show_table",
		Arguments: map[string]any{"rows": []any{}}, // headers required
	})
	if !result.IsError {
		t.Fatal("missing headers must fail validation, not Execute")
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "headers") {
		t.Fatalf("error text = %v, want it to mention headers", result.Content)
	}
}

func TestDisplayToolsAckOnValidArgs(t *testing.T) {
	reg := coretools.NewRegistry()
	for _, def := range displayTools() {
		if _, err := reg.Register(def); err != nil {
			t.Fatalf("register %s: %v", def.Name, err)
		}
	}

	result := reg.Execute(t.Context(), coretools.ExecutionInput{
		CallID: "c1", Name: "show_table",
		Arguments: map[string]any{
			"headers": []any{"Name", "Qty"},
			"rows":    []any{[]any{"apples", 3.0}, []any{"pears", 1.0}},
		},
	})
	if result.IsError {
		t.Fatalf("valid table must succeed, got %+v", result.Error)
	}
	if len(result.Content) == 0 || result.Content[0].Text != displayedAck {
		t.Fatalf("result text = %v, want the synthetic ack %q", result.Content, displayedAck)
	}
}

func TestWriteStreamEventDisplayToolEmitsMessageTypeNotToolCall(t *testing.T) {
	rec := httptest.NewRecorder()
	pending := map[string]displayPending{}

	writeStreamEvent(rec, session.Event{
		Type: "tool/call",
		Data: map[string]any{"call": session.ContentBlock{
			Kind: session.BlockToolCall, ToolName: "show_table", CallID: "c1",
			Args: map[string]any{"headers": []any{"A"}, "rows": []any{[]any{"1"}}},
		}},
	}, pending)
	if rec.Body.Len() != 0 {
		t.Fatalf("tool/call of a display tool must not emit yet (validation hasn't run), got %q", rec.Body.String())
	}

	writeStreamEvent(rec, session.Event{
		Type: "tool/result",
		Data: map[string]any{"message": session.Message{
			Role: session.RoleUser,
			Content: []session.ContentBlock{{
				Kind: session.BlockToolResult, CallID: "c1", Text: displayedAck,
			}},
		}},
	}, pending)

	body := rec.Body.String()
	if !strings.Contains(body, "event: table\n") {
		t.Fatalf("want event: table, got %q", body)
	}
	if strings.Contains(body, "event: tool-call") || strings.Contains(body, "event: tool-result") {
		t.Fatalf("must not leak the button-press or the synthetic ack, got %q", body)
	}
	if !strings.Contains(body, `"headers"`) {
		t.Fatalf("table payload missing headers, got %q", body)
	}
}

func TestWriteStreamEventDisplayToolInvalidArgsStayHidden(t *testing.T) {
	rec := httptest.NewRecorder()
	pending := map[string]displayPending{}

	writeStreamEvent(rec, session.Event{
		Type: "tool/call",
		Data: map[string]any{"call": session.ContentBlock{
			Kind: session.BlockToolCall, ToolName: "show_table", CallID: "c1",
			Args: map[string]any{"rows": []any{}},
		}},
	}, pending)
	writeStreamEvent(rec, session.Event{
		Type: "tool/result",
		Data: map[string]any{"message": session.Message{
			Role: session.RoleUser,
			Content: []session.ContentBlock{{
				Kind: session.BlockToolResult, CallID: "c1",
				Text: `Error: invalid arguments: "headers" is required`,
			}},
		}},
	}, pending)

	if rec.Body.Len() != 0 {
		t.Fatalf("a schema-invalid display call must emit nothing to the user, got %q", rec.Body.String())
	}
}

func TestWriteStreamEventRealToolUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	pending := map[string]displayPending{}

	writeStreamEvent(rec, session.Event{
		Type: "tool/call",
		Data: map[string]any{"call": session.ContentBlock{
			Kind: session.BlockToolCall, ToolName: "bash", CallID: "c2",
			Args: map[string]any{"command": "ls"},
		}},
	}, pending)
	writeStreamEvent(rec, session.Event{
		Type: "tool/result",
		Data: map[string]any{"message": session.Message{
			Role: session.RoleUser,
			Content: []session.ContentBlock{{
				Kind: session.BlockToolResult, CallID: "c2", Text: "ok",
			}},
		}},
	}, pending)

	body := rec.Body.String()
	if !strings.Contains(body, "event: tool-call\n") || !strings.Contains(body, "event: tool-result\n") {
		t.Fatalf("real tools must still stream as tool-call/tool-result, got %q", body)
	}
}

func TestEveryDisplayToolAcksWithMinimalValidArgs(t *testing.T) {
	reg := coretools.NewRegistry()
	for _, def := range displayTools() {
		if _, err := reg.Register(def); err != nil {
			t.Fatalf("register %s: %v", def.Name, err)
		}
	}

	minimal := map[string]map[string]any{
		"show_table":      {"headers": []any{"A"}, "rows": []any{[]any{"1"}}},
		"show_chart":      {"type": "bar", "labels": []any{"x"}, "series": []any{map[string]any{"name": "s", "values": []any{1.0}}}},
		"show_code":       {"language": "go", "code": "package main"},
		"show_suggestion": {"items": []any{map[string]any{"label": "next"}}},
		"show_image":      {"path": "plot.png"},
		"show_error":      {"message": "boom"},
		"show_progress":   {"status": "running", "progress": 40.0},
	}
	for _, def := range displayTools() {
		args, ok := minimal[def.Name]
		if !ok {
			t.Errorf("%s: no minimal valid args in the table", def.Name)
			continue
		}
		result := reg.Execute(t.Context(), coretools.ExecutionInput{CallID: "c", Name: def.Name, Arguments: args})
		if result.IsError {
			t.Errorf("%s: valid args failed: %+v", def.Name, result.Error)
			continue
		}
		if len(result.Content) == 0 || result.Content[0].Text != displayedAck {
			t.Errorf("%s: want ack %q, got %v", def.Name, displayedAck, result.Content)
		}
	}
}

func TestEveryDisplayToolHasAMessageType(t *testing.T) {
	for _, def := range displayTools() {
		if !isDisplayTool(def.Name) {
			t.Errorf("%s is registered as a display tool but missing from displayToolTypes", def.Name)
		}
	}
}

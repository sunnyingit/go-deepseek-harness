package tokenmeter

import (
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

func TestEstimateContentText(t *testing.T) {
	blocks := []session.ContentBlock{{Kind: session.BlockText, Text: "12345678"}} // 8 chars -> 2 tokens
	got := EstimateContent(blocks)
	want := ceilDiv(8, charsPerToken) + blockOverhead
	if got != want {
		t.Fatalf("EstimateContent(text) = %d, want %d", got, want)
	}
}

func TestEstimateContentReasoningPricesLikeText(t *testing.T) {
	text := []session.ContentBlock{{Kind: session.BlockText, Text: "same length text"}}
	reasoning := []session.ContentBlock{{Kind: session.BlockReasoning, Text: "same length text"}}
	if EstimateContent(text) != EstimateContent(reasoning) {
		t.Fatalf("reasoning should price identically to text of the same length")
	}
}

func TestEstimateContentToolResultPricesLikeText(t *testing.T) {
	// This port's tool/result content is flat Text (see core/session's
	// DeriveEventMessage), unlike the TS original's nested content array —
	// so it prices the same way BlockText does, by construction.
	text := []session.ContentBlock{{Kind: session.BlockText, Text: "tool output here"}}
	result := []session.ContentBlock{{Kind: session.BlockToolResult, Text: "tool output here"}}
	if EstimateContent(text) != EstimateContent(result) {
		t.Fatalf("tool-result should price identically to text of the same length")
	}
}

func TestEstimateContentToolCallPricesNameAndArgs(t *testing.T) {
	blocks := []session.ContentBlock{{
		Kind:     session.BlockToolCall,
		ToolName: "read",
		Args:     map[string]any{"file_path": "a.txt"},
	}}
	got := EstimateContent(blocks)
	if got <= blockOverhead {
		t.Fatalf("EstimateContent(tool-call) = %d, want more than bare overhead %d", got, blockOverhead)
	}

	bigger := []session.ContentBlock{{
		Kind:     session.BlockToolCall,
		ToolName: "read",
		Args:     map[string]any{"file_path": "a much longer file path that costs more tokens to encode as JSON"},
	}}
	if EstimateContent(bigger) <= got {
		t.Fatalf("a tool call with larger arguments should price higher")
	}
}

func TestEstimateContentEmptyIsZero(t *testing.T) {
	if got := EstimateContent(nil); got != 0 {
		t.Fatalf("EstimateContent(nil) = %d, want 0", got)
	}
}

func TestEstimateMessageAddsRoleOverhead(t *testing.T) {
	msg := session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: "hi"}}}
	want := EstimateContent(msg.Content) + RoleOverhead
	if got := EstimateMessage(msg); got != want {
		t.Fatalf("EstimateMessage = %d, want %d", got, want)
	}
}

func TestEstimateSystemTokensEmptyIsZero(t *testing.T) {
	if got := EstimateSystemTokens(""); got != 0 {
		t.Fatalf("EstimateSystemTokens(\"\") = %d, want 0", got)
	}
	if got := EstimateSystemTokens("be a helpful assistant"); got == 0 {
		t.Fatalf("EstimateSystemTokens(non-empty) = 0, want > 0")
	}
}

func TestEstimateToolsTokensEmptyIsZero(t *testing.T) {
	if got := EstimateToolsTokens(nil); got != 0 {
		t.Fatalf("EstimateToolsTokens(nil) = %d, want 0", got)
	}
	tools := []agentloop.ToolSchema{{Name: "read", Description: "read a file", Parameters: map[string]any{"type": "object"}}}
	if got := EstimateToolsTokens(tools); got == 0 {
		t.Fatalf("EstimateToolsTokens(non-empty) = 0, want > 0")
	}
}

func TestEstimateHeaderNilIsZero(t *testing.T) {
	if got := EstimateHeader(nil); got != 0 {
		t.Fatalf("EstimateHeader(nil) = %d, want 0", got)
	}
}

func TestEstimateHeaderSumsSystemAndTools(t *testing.T) {
	header := &Header{
		System: "be a helpful assistant",
		Tools:  []agentloop.ToolSchema{{Name: "read", Description: "read a file"}},
	}
	want := EstimateSystemTokens(header.System) + EstimateToolsTokens(header.Tools)
	if got := EstimateHeader(header); got != want {
		t.Fatalf("EstimateHeader = %d, want %d", got, want)
	}
}

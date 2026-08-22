package deepseek

import "testing"

func TestClassifyStatusDoesNotTreatInsufficientToolMessagesAsQuota(t *testing.T) {
	detail := "an assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. (insufficient tool messages following tool_calls message)"
	if got := classifyStatus(400, detail); got != CodeInvalidRequest {
		t.Fatalf("classifyStatus = %q, want %s (insufficient tool messages is a conversation-shape error, not quota)", got, CodeInvalidRequest)
	}
}

func TestClassifyStatusStillMapsInsufficientBalanceToQuota(t *testing.T) {
	if got := classifyStatus(402, "insufficient balance"); got != CodeQuotaExceeded {
		t.Fatalf("classifyStatus = %q, want %s", got, CodeQuotaExceeded)
	}
}

package sessionpersistence

import (
	"testing"

	"go-harness/core/session"
)

func roundTrip(t *testing.T, ev session.Event) session.Event {
	t.Helper()
	line, err := encodeEvent(ev)
	if err != nil {
		t.Fatalf("encodeEvent: %v", err)
	}
	decoded, err := decodeEvent(line)
	if err != nil {
		t.Fatalf("decodeEvent: %v", err)
	}
	return decoded
}

func TestCodecUserMessageRoundTripsAsMessage(t *testing.T) {
	ev := session.Event{Type: "user/message", Seq: 0, Time: 123, Data: userMsg("hi there")}
	got := roundTrip(t, ev)

	msg, ok := got.Data.(session.Message)
	if !ok {
		t.Fatalf("Data = %T, want session.Message", got.Data)
	}
	if msg.Role != session.RoleUser || msg.Content[0].Text != "hi there" {
		t.Fatalf("got %+v", msg)
	}
	// DeriveEventMessage is what actually matters downstream — assert the
	// round-tripped event still works with it, not just the raw type.
	if _, ok := session.DeriveEventMessage(got); !ok {
		t.Fatal("DeriveEventMessage found no message in the round-tripped event")
	}
}

func TestCodecAssistantMessageRoundTripsNestedMessage(t *testing.T) {
	ev := session.Event{Type: "assistant/message", Seq: 1, Data: map[string]any{
		"turn": 1, "step": 1, "message": assistantMsg("hello"),
	}}
	got := roundTrip(t, ev)

	wrapped, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data = %T, want map[string]any", got.Data)
	}
	msg, ok := wrapped["message"].(session.Message)
	if !ok {
		t.Fatalf(`Data["message"] = %T, want session.Message`, wrapped["message"])
	}
	if msg.Content[0].Text != "hello" {
		t.Fatalf("got %+v", msg)
	}
	if _, ok := session.DeriveEventMessage(got); !ok {
		t.Fatal("DeriveEventMessage found no message in the round-tripped assistant/message")
	}
}

func TestCodecToolResultPreservesErrorFlag(t *testing.T) {
	ev := session.Event{Type: "tool/result", Seq: 2, Data: map[string]any{
		"turn": 1, "step": 1,
		"message": session.Message{Role: session.RoleUser, Content: []session.ContentBlock{
			{Kind: session.BlockToolResult, CallID: "c1", Text: "failed"},
		}},
		"error": true,
	}}
	got := roundTrip(t, ev)
	wrapped := got.Data.(map[string]any)
	if wrapped["error"] != true {
		t.Fatalf(`Data["error"] = %v, want true`, wrapped["error"])
	}
}

func TestCodecToolCallRoundTripsContentBlock(t *testing.T) {
	ev := session.Event{Type: "tool/call", Seq: 3, Data: map[string]any{
		"turn": 1, "step": 1,
		"call": session.ContentBlock{Kind: session.BlockToolCall, CallID: "c1", ToolName: "read", Args: map[string]any{"file_path": "a.txt"}},
	}}
	got := roundTrip(t, ev)
	wrapped := got.Data.(map[string]any)
	call, ok := wrapped["call"].(session.ContentBlock)
	if !ok {
		t.Fatalf(`Data["call"] = %T, want session.ContentBlock`, wrapped["call"])
	}
	if call.ToolName != "read" || call.Args["file_path"] != "a.txt" {
		t.Fatalf("got %+v", call)
	}
}

func TestCodecTurnStartIsPlainInt(t *testing.T) {
	ev := session.Event{Type: "turn/start", Seq: 0, Data: 7}
	got := roundTrip(t, ev)
	if v, ok := got.Data.(int); !ok || v != 7 {
		t.Fatalf("Data = %#v, want int(7)", got.Data)
	}
}

func TestCodecSurfaceOpAndSourceSeqsRoundTrip(t *testing.T) {
	op := session.ReplaceOp(2, 4)
	ev := session.Event{
		Type: "tool/result", Seq: 5,
		Data:            map[string]any{"turn": 1, "step": 1, "message": userMsg("compact")},
		SurfaceOp:       &op,
		SourceEventSeqs: []int{2, 3, 4},
	}
	got := roundTrip(t, ev)
	if got.SurfaceOp == nil || !got.SurfaceOp.Replace || got.SurfaceOp.Start != 2 || got.SurfaceOp.End != 4 {
		t.Fatalf("SurfaceOp = %+v, want Replace{2,4}", got.SurfaceOp)
	}
	if len(got.SourceEventSeqs) != 3 {
		t.Fatalf("SourceEventSeqs = %v, want [2 3 4]", got.SourceEventSeqs)
	}
}

func TestCodecUnknownEventTypeDecodesBestEffort(t *testing.T) {
	ev := session.Event{Type: "plan/mode", Seq: 0, Data: map[string]any{"active": true}}
	got := roundTrip(t, ev)
	wrapped, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data = %T, want map[string]any (best-effort generic decode)", got.Data)
	}
	if wrapped["active"] != true {
		t.Fatalf("got %+v", wrapped)
	}
}

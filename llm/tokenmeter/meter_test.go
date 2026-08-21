package tokenmeter

import (
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

func userMsg(text string) session.Message {
	return session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}}
}

func assistantMsg(text string) session.Message {
	return session.Message{Role: session.RoleAssistant, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}}
}

func TestMeasureEmptySessionIsZero(t *testing.T) {
	sess := session.New("", session.Header{})
	m, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.TotalTokens != 0 || m.SurfaceTokens != 0 || m.HeaderTokens != 0 {
		t.Fatalf("Measure(empty) = %+v, want all zero", m)
	}
	if m.LogRevision != 0 {
		t.Fatalf("LogRevision = %d, want 0", m.LogRevision)
	}
}

func TestMeasureSumsSurfaceMessages(t *testing.T) {
	sess := session.New("", session.Header{})
	sess.Append("user/message", userMsg("hello there"), session.WithSurfaceOp(session.AppendOp()))
	sess.Append("assistant/message", map[string]any{"turn": 1, "step": 1, "message": assistantMsg("hi, how can I help?")},
		session.WithSurfaceOp(session.AppendOp()))

	m, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.SurfaceTokens <= 0 {
		t.Fatalf("SurfaceTokens = %d, want > 0", m.SurfaceTokens)
	}
	if m.TotalTokens != m.SurfaceTokens {
		t.Fatalf("TotalTokens = %d, want == SurfaceTokens (%d) with no header", m.TotalTokens, m.SurfaceTokens)
	}
	if len(m.Nodes) != 2 {
		t.Fatalf("Nodes = %+v, want 2 (one per surface-eligible event)", m.Nodes)
	}
	if m.LogRevision != len(sess.Events()) {
		t.Fatalf("LogRevision = %d, want %d", m.LogRevision, len(sess.Events()))
	}
}

func TestMeasureIgnoresNonSurfaceEvents(t *testing.T) {
	sess := session.New("", session.Header{})
	sess.Append("turn/start", 1)
	sess.Append("user/message", userMsg("hi"), session.WithSurfaceOp(session.AppendOp()))
	sess.Append("step/start", map[string]any{"turn": 1, "step": 1})
	sess.Append("assistant/chunk", map[string]any{"turn": 1, "step": 1, "chunk": session.ContentBlock{Kind: session.BlockText, Text: "h"}})

	m, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 1 {
		t.Fatalf("Nodes = %+v, want exactly the one surface-eligible event", m.Nodes)
	}
}

func TestMeasureWithHeaderAddsStructuralOverhead(t *testing.T) {
	sess := session.New("", session.Header{})
	sess.Append("user/message", userMsg("hi"), session.WithSurfaceOp(session.AppendOp()))

	without, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	header := &Header{
		System: "You are a helpful assistant with several tools.",
		Tools:  []agentloop.ToolSchema{{Name: "read", Description: "read a file"}},
	}
	with, err := New().Measure(sess, header)
	if err != nil {
		t.Fatal(err)
	}

	if with.SurfaceTokens != without.SurfaceTokens {
		t.Fatalf("adding a header must not change surface pricing: %d vs %d", with.SurfaceTokens, without.SurfaceTokens)
	}
	if with.HeaderTokens == 0 {
		t.Fatal("HeaderTokens = 0, want > 0 for a non-nil header")
	}
	if with.TotalTokens != with.HeaderTokens+with.SurfaceTokens {
		t.Fatalf("TotalTokens = %d, want HeaderTokens+SurfaceTokens = %d", with.TotalTokens, with.HeaderTokens+with.SurfaceTokens)
	}
}

func TestMeasureCompactionShrinksSurfaceTokens(t *testing.T) {
	sess := session.New("", session.Header{})
	verbose := sess.Append("tool/result", map[string]any{"message": session.Message{
		Role: session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult,
			Text: "a very long tool result with lots of verbose output that costs many tokens to keep around forever"}},
	}}, session.WithSurfaceOp(session.AppendOp()))

	before, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}

	sess.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "short"}},
	}}, session.WithSurfaceOp(session.ReplaceOp(verbose.Seq, verbose.Seq)), session.WithSourceEventSeqs(verbose.Seq))

	after, err := New().Measure(sess, nil)
	if err != nil {
		t.Fatal(err)
	}

	if after.SurfaceTokens >= before.SurfaceTokens {
		t.Fatalf("compaction should shrink surface tokens: before=%d after=%d", before.SurfaceTokens, after.SurfaceTokens)
	}
	if len(after.Nodes) != 1 {
		t.Fatalf("Nodes = %+v, want exactly one (the replacement shadows the original)", after.Nodes)
	}
}

func TestMeterEstimateMessageMatchesPackageFunction(t *testing.T) {
	msg := userMsg("hello world")
	if got, want := New().EstimateMessage(msg), EstimateMessage(msg); got != want {
		t.Fatalf("Meter.EstimateMessage = %d, want %d (package function)", got, want)
	}
}

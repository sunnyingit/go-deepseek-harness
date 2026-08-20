package session

import (
	"testing"
)

func userMsg(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockText, Text: text}}}
}

func assistantMsg(blocks ...ContentBlock) Message {
	return Message{Role: RoleAssistant, Content: blocks}
}

func TestNewStampsHeader(t *testing.T) {
	s := New("s1", Header{})
	h := s.Header()
	if h.ID != "s1" || h.Version != FormatVersion || h.CreatedAt == 0 {
		t.Fatalf("header = %+v", h)
	}
	if s.ID() != "s1" {
		t.Fatalf("ID() = %q, want s1", s.ID())
	}
}

func TestAppendNonSurfaceEventNeedsNoOp(t *testing.T) {
	s := New("s1", Header{})
	ev := s.Append("turn/start", 1)
	if ev.Seq != 0 || ev.Type != "turn/start" || ev.SurfaceOp != nil {
		t.Fatalf("ev = %+v", ev)
	}
	if s.Seq() != 1 {
		t.Fatalf("Seq() = %d, want 1", s.Seq())
	}
}

func TestAppendSurfaceEventWithoutOpPanics(t *testing.T) {
	s := New("s1", Header{})
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic when a surface-eligible event has no WithSurfaceOp")
		}
	}()
	s.Append("user/message", userMsg("hi"))
}

func TestAppendNonSurfaceEventWithOpPanics(t *testing.T) {
	s := New("s1", Header{})
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic when a non-surface event carries a SurfaceOp")
		}
	}()
	s.Append("turn/start", 1, WithSurfaceOp(AppendOp()))
}

func TestDeriveMessagesWalksTheSurfaceInOrder(t *testing.T) {
	s := New("s1", Header{})
	s.Append("turn/start", 1)
	s.Append("user/message", userMsg("hello"), WithSurfaceOp(AppendOp()))
	s.Append("assistant/message", map[string]any{
		"message": assistantMsg(ContentBlock{Kind: BlockText, Text: "hi there"}),
	}, WithSurfaceOp(AppendOp()))
	s.Append("assistant/chunk", map[string]any{"chunk": "irrelevant"}) // non-surface, must not appear

	msgs := s.DeriveMessages()
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Content[0].Text != "hello" || msgs[1].Content[0].Text != "hi there" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestDeriveMessagesSkipsEmptyAssistantMessage(t *testing.T) {
	s := New("s1", Header{})
	s.Append("assistant/message", map[string]any{
		"message": assistantMsg(), // no content blocks — a max-tokens-only step
	}, WithSurfaceOp(AppendOp()))

	if msgs := s.DeriveMessages(); len(msgs) != 0 {
		t.Fatalf("msgs = %+v, want none", msgs)
	}
}

func TestDeriveMessagesCachesAndOnlyExtendsForNewNodes(t *testing.T) {
	s := New("s1", Header{})
	s.Append("user/message", userMsg("one"), WithSurfaceOp(AppendOp()))
	first := s.DeriveMessages()
	s.Append("user/message", userMsg("two"), WithSurfaceOp(AppendOp()))
	second := s.DeriveMessages()

	if len(first) != 1 || len(second) != 2 {
		t.Fatalf("first=%d second=%d, want 1 then 2", len(first), len(second))
	}
	// The array returned per call is a fresh snapshot — mutating one must not
	// affect a later DeriveMessages() call.
	first[0] = userMsg("mutated")
	third := s.DeriveMessages()
	if third[0].Content[0].Text != "one" {
		t.Fatalf("third[0] = %+v, want unaffected by mutating an earlier returned slice", third[0])
	}
}

func TestReplaceShadowsEarlierSurfaceNodeWithoutDeletingTheLogRecord(t *testing.T) {
	s := New("s1", Header{})
	orig := s.Append("user/message", userMsg("original"), WithSurfaceOp(AppendOp()))
	s.Append("user/message", userMsg("summary"), WithSurfaceOp(ReplaceOp(orig.Seq, orig.Seq)), WithSourceEventSeqs(orig.Seq))

	msgs := s.DeriveMessages()
	if len(msgs) != 1 || msgs[0].Content[0].Text != "summary" {
		t.Fatalf("msgs = %+v, want just the replacement", msgs)
	}
	if len(s.Events()) != 2 {
		t.Fatalf("Events() has %d entries, want 2 — replace must not delete the shadowed log record", len(s.Events()))
	}
}

func TestReplaceWithoutCitingTheShadowedSeqPanics(t *testing.T) {
	s := New("s1", Header{})
	orig := s.Append("user/message", userMsg("original"), WithSurfaceOp(AppendOp()))
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic when SourceEventSeqs omits a shadowed node")
		}
	}()
	s.Append("user/message", userMsg("summary"), WithSurfaceOp(ReplaceOp(orig.Seq, orig.Seq)))
}

func TestReplaceRangeMustExistOnTheSurface(t *testing.T) {
	s := New("s1", Header{})
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic when the replace range references a seq never on the surface")
		}
	}()
	s.Append("user/message", userMsg("x"), WithSurfaceOp(ReplaceOp(99, 99)), WithSourceEventSeqs(99))
}

func TestToolResultReplacementMayChangeOnlyContent(t *testing.T) {
	s := New("s1", Header{})
	orig := s.Append("tool/result", map[string]any{
		"message": Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockToolResult, CallID: "c1", Text: "big output"}}},
	}, WithSurfaceOp(AppendOp()))

	// Allowed: same call id/kind, only Text differs (a pruning rewrite).
	s.Append("tool/result", map[string]any{
		"message": Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockToolResult, CallID: "c1", Text: "[pruned]"}}},
	}, WithSurfaceOp(ReplaceOp(orig.Seq, orig.Seq)), WithSourceEventSeqs(orig.Seq))

	msgs := s.DeriveMessages()
	if msgs[0].Content[0].Text != "[pruned]" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestToolResultReplacementChangingCallIDPanics(t *testing.T) {
	s := New("s1", Header{})
	orig := s.Append("tool/result", map[string]any{
		"message": Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockToolResult, CallID: "c1", Text: "out"}}},
	}, WithSurfaceOp(AppendOp()))

	defer func() {
		if recover() == nil {
			t.Fatal("want a panic when a tool/result replacement changes more than content")
		}
	}()
	s.Append("tool/result", map[string]any{
		"message": Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockToolResult, CallID: "c2", Text: "out"}}},
	}, WithSurfaceOp(ReplaceOp(orig.Seq, orig.Seq)), WithSourceEventSeqs(orig.Seq))
}

func TestLastTurnDefaultsToZero(t *testing.T) {
	s := New("s1", Header{})
	if s.LastTurn() != 0 {
		t.Fatalf("LastTurn() = %d, want 0", s.LastTurn())
	}
	s.Append("turn/start", 1)
	s.Append("turn/start", 2)
	if s.LastTurn() != 2 {
		t.Fatalf("LastTurn() = %d, want 2", s.LastTurn())
	}
}

func TestNewSeededReplaysValidationAndSetsSeedLength(t *testing.T) {
	source := New("s1", Header{})
	source.Append("turn/start", 1)
	source.Append("user/message", userMsg("hi"), WithSurfaceOp(AppendOp()))
	source.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	child, err := NewSeeded("s2", Header{}, source.Events())
	if err != nil {
		t.Fatalf("NewSeeded: %v", err)
	}
	if child.Header().SeedLength != 3 {
		t.Fatalf("SeedLength = %d, want 3", child.Header().SeedLength)
	}
	if len(child.DeriveMessages()) != 1 {
		t.Fatalf("child derived %d messages, want 1 (inherited from the seed)", len(child.DeriveMessages()))
	}
	// The child's own appends continue the seq sequence.
	next := child.Append("turn/start", 2)
	if next.Seq != 3 {
		t.Fatalf("next.Seq = %d, want 3", next.Seq)
	}
}

func TestNewSeededRejectsNonContiguousSeed(t *testing.T) {
	bad := []Event{{Type: "turn/start", Seq: 5, Data: 1}}
	if _, err := NewSeeded("s2", Header{}, bad); err == nil {
		t.Fatal("want an error for a seed that doesn't start at seq 0")
	}
}

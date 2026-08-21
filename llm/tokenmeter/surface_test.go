package tokenmeter

import (
	"testing"

	"go-harness/core/session"
)

func userMsgEvent(seq int, text string) session.Event {
	op := session.AppendOp()
	return session.Event{
		Type:      "user/message",
		Seq:       seq,
		Data:      session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}},
		SurfaceOp: &op,
	}
}

func toolResultEvent(seq int, text string, sourceSeqs []int, replace *session.SurfaceOp) session.Event {
	op := session.AppendOp()
	if replace != nil {
		op = *replace
	}
	return session.Event{
		Type: "tool/result",
		Seq:  seq,
		Data: map[string]any{"message": session.Message{
			Role:    session.RoleUser,
			Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: text}},
		}},
		SourceEventSeqs: sourceSeqs,
		SurfaceOp:       &op,
	}
}

func TestFoldSurfaceTokensAppendGrowsNodes(t *testing.T) {
	ev0 := userMsgEvent(0, "hello")
	fold0, err := FoldSurfaceTokens(nil, ev0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fold0.Nodes) != 1 || fold0.Nodes[0].Seq != 0 {
		t.Fatalf("fold0.Nodes = %+v", fold0.Nodes)
	}
	if fold0.Tokens != fold0.DeltaTokens || fold0.DeltaTokens <= 0 {
		t.Fatalf("an append's delta should equal its own price and be positive: %+v", fold0)
	}

	ev1 := userMsgEvent(1, "world, a bit longer this time")
	fold1, err := FoldSurfaceTokens(fold0.Nodes, ev1)
	if err != nil {
		t.Fatal(err)
	}
	if len(fold1.Nodes) != 2 {
		t.Fatalf("fold1.Nodes = %+v, want 2 entries", fold1.Nodes)
	}
	if fold1.Nodes[0].Seq != 0 || fold1.Nodes[1].Seq != 1 {
		t.Fatalf("fold1.Nodes out of order: %+v", fold1.Nodes)
	}
}

func TestFoldSurfaceTokensReplaceShadowsRangeWithSignedDelta(t *testing.T) {
	verbose := toolResultEvent(0, "a very long tool result that costs a lot of tokens to keep around", nil, nil)
	fold0, err := FoldSurfaceTokens(nil, verbose)
	if err != nil {
		t.Fatal(err)
	}

	replaceOp := session.ReplaceOp(0, 0)
	compact := toolResultEvent(1, "short", []int{0}, &replaceOp)
	fold1, err := FoldSurfaceTokens(fold0.Nodes, compact)
	if err != nil {
		t.Fatal(err)
	}

	if len(fold1.Nodes) != 1 || fold1.Nodes[0].Seq != 1 {
		t.Fatalf("fold1.Nodes = %+v, want exactly one node at seq 1", fold1.Nodes)
	}
	if fold1.DeltaTokens >= 0 {
		t.Fatalf("compacting a verbose result down should produce a negative delta, got %d", fold1.DeltaTokens)
	}
	wantDelta := fold1.Tokens - fold0.Tokens
	if fold1.DeltaTokens != wantDelta {
		t.Fatalf("DeltaTokens = %d, want %d (new price minus shadowed price)", fold1.DeltaTokens, wantDelta)
	}
}

func TestFoldSurfaceTokensInvalidRangeErrors(t *testing.T) {
	nodes := []SurfaceNode{{Seq: 0, Tokens: 5}}
	badOp := session.ReplaceOp(0, 99) // seq 99 was never on the surface
	ev := toolResultEvent(1, "x", []int{0}, &badOp)

	if _, err := FoldSurfaceTokens(nodes, ev); err == nil {
		t.Fatal("expected an error for an unresolvable replace range")
	}
}

func TestFoldSurfaceTokensNilSurfaceOpErrors(t *testing.T) {
	ev := session.Event{Type: "user/message", Seq: 0, Data: session.Message{Role: session.RoleUser}}
	if _, err := FoldSurfaceTokens(nil, ev); err == nil {
		t.Fatal("expected an error for a nil SurfaceOp")
	}
}

func TestFoldSurfaceTokensNonSurfaceMessageEventPricesZero(t *testing.T) {
	// A malformed/foreign event type still folds (append semantics from its
	// SurfaceOp), but DeriveEventMessage finds no message to price.
	op := session.AppendOp()
	ev := session.Event{Type: "assistant/message", Seq: 0, Data: "not the expected map shape", SurfaceOp: &op}
	fold, err := FoldSurfaceTokens(nil, ev)
	if err != nil {
		t.Fatal(err)
	}
	if fold.Tokens != 0 {
		t.Fatalf("Tokens = %d, want 0 for an event DeriveEventMessage can't price", fold.Tokens)
	}
}

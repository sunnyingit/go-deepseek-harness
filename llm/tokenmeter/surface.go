package tokenmeter

import (
	"fmt"

	"go-harness/core/session"
)

// SurfaceNode is one token-priced node in an ordered session surface —
// mirrors core/session's own internal surface fold (surface.go), but keyed
// by seq/tokens instead of by seq alone, since pricing (not just ordering)
// is what this package folds.
type SurfaceNode struct {
	Seq    int `json:"seq"`
	Tokens int `json:"tokens"`
}

// SurfaceFold is the result of folding one surface event onto a priced
// surface.
type SurfaceFold struct {
	// Tokens is the heuristic price of the event's own message; 0 when it
	// derives none.
	Tokens int
	// Nodes is the surface after the event, detached from the input slice.
	Nodes []SurfaceNode
	// DeltaTokens is the signed change in the surface total: Tokens minus
	// anything shadowed by a replace.
	DeltaTokens int
}

// FoldSurfaceTokens folds one surface-eligible event onto nodes, the
// priced surface preceding it (in model-visible order). Total and
// allocation-fresh: nodes is never mutated, so a returned error leaves the
// caller's state untouched and the same malformed event fails identically
// on every retry.
//
// ev must be surface-eligible (session.SurfaceEligible(ev.Type)) with a
// non-nil SurfaceOp — core/session.Session.Append already enforces this for
// every event a real session can ever contain, so an ev violating it here
// signals a caller bug or a hand-built Event, not ordinary replay.
func FoldSurfaceTokens(nodes []SurfaceNode, ev session.Event) (SurfaceFold, error) {
	if ev.SurfaceOp == nil {
		return SurfaceFold{}, fmt.Errorf("token surface: event %q at seq %d is not surface-eligible (no SurfaceOp)", ev.Type, ev.Seq)
	}

	message, ok := session.DeriveEventMessage(ev)
	tokens := 0
	if ok {
		tokens = EstimateMessage(message)
	}

	if !ev.SurfaceOp.Replace {
		next := make([]SurfaceNode, len(nodes)+1)
		copy(next, nodes)
		next[len(nodes)] = SurfaceNode{Seq: ev.Seq, Tokens: tokens}
		return SurfaceFold{Tokens: tokens, Nodes: next, DeltaTokens: tokens}, nil
	}

	op := *ev.SurfaceOp
	startIdx := indexOfSeq(nodes, op.Start)
	endIdx := indexOfSeq(nodes, op.End)
	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return SurfaceFold{}, fmt.Errorf("token surface: replace at seq %d has invalid current range %d-%d", ev.Seq, op.Start, op.End)
	}

	removed := 0
	for _, n := range nodes[startIdx : endIdx+1] {
		removed += n.Tokens
	}
	next := make([]SurfaceNode, 0, len(nodes)-(endIdx-startIdx))
	next = append(next, nodes[:startIdx]...)
	next = append(next, SurfaceNode{Seq: ev.Seq, Tokens: tokens})
	next = append(next, nodes[endIdx+1:]...)
	return SurfaceFold{Tokens: tokens, Nodes: next, DeltaTokens: tokens - removed}, nil
}

func indexOfSeq(nodes []SurfaceNode, seq int) int {
	for i, n := range nodes {
		if n.Seq == seq {
			return i
		}
	}
	return -1
}

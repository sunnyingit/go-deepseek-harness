package sessionquery

import (
	"testing"

	"go-harness/core/session"
)

func TestTraceSessionAncestorsAndDescendants(t *testing.T) {
	store := session.NewStore()
	root, err := store.Create("root", session.Header{CreatedAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	root.Append("turn/start", 1)
	root.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	child, err := store.Fork(root, -1, "child")
	if err != nil {
		t.Fatal(err)
	}
	child.Append("turn/start", 1)
	child.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	grandchild, err := store.Fork(child, -1, "grandchild")
	if err != nil {
		t.Fatal(err)
	}
	_ = grandchild

	e := New(store, 0)

	trace, err := e.TraceSession("child")
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Complete {
		t.Fatal("expected a complete ancestor chain (root is live)")
	}
	if len(trace.Ancestors) != 1 || trace.Ancestors[0].ID != "root" {
		t.Fatalf("Ancestors = %+v, want just root", trace.Ancestors)
	}
	if len(trace.Descendants) != 1 || trace.Descendants[0].Header.ID != "grandchild" {
		t.Fatalf("Descendants = %+v, want just grandchild", trace.Descendants)
	}

	rootTrace, err := e.TraceSession("root")
	if err != nil {
		t.Fatal(err)
	}
	if len(rootTrace.Ancestors) != 0 {
		t.Fatalf("root should have no ancestors, got %+v", rootTrace.Ancestors)
	}
	if len(rootTrace.Descendants) != 1 || rootTrace.Descendants[0].Header.ID != "child" {
		t.Fatalf("root.Descendants = %+v, want just child at the top level", rootTrace.Descendants)
	}
	if len(rootTrace.Descendants[0].Children) != 1 || rootTrace.Descendants[0].Children[0].Header.ID != "grandchild" {
		t.Fatalf("child's own children = %+v, want just grandchild", rootTrace.Descendants[0].Children)
	}
}

func TestTraceSessionIncompleteWhenParentIsGone(t *testing.T) {
	// Store.Create accepts an arbitrary Header verbatim (New only fills in
	// Version/ID/CreatedAt) — so a session naming a ParentSession that was
	// never itself created in this Store is the direct way to construct
	// "the parent isn't live," without needing a second Store or pretending
	// to evict anything (this port's Store never evicts at all).
	store := session.NewStore()
	if _, err := store.Create("orphan", session.Header{ParentSession: "ghost"}); err != nil {
		t.Fatal(err)
	}

	trace, err := New(store, 0).TraceSession("orphan")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Complete {
		t.Fatal("expected an incomplete trace when the parent session isn't live in this Store")
	}
	if len(trace.Ancestors) != 0 {
		t.Fatalf("Ancestors = %+v, want none (parent unresolvable)", trace.Ancestors)
	}
}

func TestTraceEventSourceLinksAndReplacementChain(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	verbose := s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "verbose"}},
	}}, session.WithSurfaceOp(session.AppendOp()))
	compact := s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "compact"}},
	}}, session.WithSurfaceOp(session.ReplaceOp(verbose.Seq, verbose.Seq)), session.WithSourceEventSeqs(verbose.Seq))

	e := New(store, 0)

	trace, err := e.TraceEvent("s1", verbose.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.ReplacementChain) != 1 || trace.ReplacementChain[0].Seq != compact.Seq {
		t.Fatalf("ReplacementChain = %+v, want just the compact event", trace.ReplacementChain)
	}

	compactTrace, err := e.TraceEvent("s1", compact.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(compactTrace.ReplacementChain) != 0 {
		t.Fatalf("the current event should have an empty replacement chain, got %+v", compactTrace.ReplacementChain)
	}
	if len(compactTrace.SourceEventSeqs) != 1 || compactTrace.SourceEventSeqs[0] != verbose.Seq {
		t.Fatalf("SourceEventSeqs = %v, want [%d]", compactTrace.SourceEventSeqs, verbose.Seq)
	}
}

func TestTraceEventNotFound(t *testing.T) {
	store, _ := newStoreWithSession(t, "s1", "")
	_, err := New(store, 0).TraceEvent("s1", 42)
	qerr, ok := err.(*Error)
	if !ok || qerr.Code != CodeEventNotFound {
		t.Fatalf("err = %v, want *Error{Code: CodeEventNotFound}", err)
	}
}

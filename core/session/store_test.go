package session

import (
	"errors"
	"testing"
)

func TestStoreCreateMintsSequentialDefaultIDs(t *testing.T) {
	st := NewStore()
	a, err := st.Create("", Header{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, err := st.Create("", Header{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID() != "session-1" || b.ID() != "session-2" {
		t.Fatalf("ids = %q, %q", a.ID(), b.ID())
	}
}

func TestStoreCreateRejectsDuplicateID(t *testing.T) {
	st := NewStore()
	if _, err := st.Create("s1", Header{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create("s1", Header{}); err == nil {
		t.Fatal("want an error creating a duplicate id")
	}
}

func TestStoreGetAndList(t *testing.T) {
	st := NewStore()
	s1, _ := st.Create("s1", Header{})
	s2, _ := st.Create("s2", Header{})

	if got, ok := st.Get("s1"); !ok || got != s1 {
		t.Fatalf("Get(s1) = %v, %v", got, ok)
	}
	if _, ok := st.Get("missing"); ok {
		t.Fatal("Get(missing) should report false")
	}

	list := st.List()
	if len(list) != 2 {
		t.Fatalf("List() has %d entries, want 2", len(list))
	}
	seen := map[SessionId]bool{}
	for _, s := range list {
		seen[s.ID()] = true
	}
	if !seen[s1.ID()] || !seen[s2.ID()] {
		t.Fatalf("List() = %+v, missing an entry", list)
	}
}

func TestStoreAdoptRegistersAnAlreadyBuiltSession(t *testing.T) {
	st := NewStore()
	built := New("restored", Header{CWD: "/work/proj"})
	built.Append("turn/start", 1)

	if err := st.Adopt(built); err != nil {
		t.Fatal(err)
	}

	got, ok := st.Get("restored")
	if !ok || got != built {
		t.Fatalf("Get(restored) = %v, %v, want the exact adopted *Session", got, ok)
	}
	if len(st.List()) != 1 {
		t.Fatalf("List() has %d entries, want 1", len(st.List()))
	}
}

func TestStoreAdoptRejectsDuplicateID(t *testing.T) {
	st := NewStore()
	st.Create("s1", Header{})

	if err := st.Adopt(New("s1", Header{})); err == nil {
		t.Fatal("expected an error adopting a session under an id that's already live")
	}
}

func TestStoreForkDefaultsBoundaryToLastEventAndRecordsLineage(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{CWD: "/work"})
	parent.Append("turn/start", 1)
	parent.Append("user/message", userMsg("hi"), WithSurfaceOp(AppendOp()))
	parent.Append("turn/end", map[string]any{"turn": 1})

	child, err := st.Fork(parent, -1, "")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	h := child.Header()
	if h.ParentSession != "parent" || h.SeedLength != 3 || h.CWD != "/work" {
		t.Fatalf("child header = %+v", h)
	}
	if got, ok := st.Get(child.ID()); !ok || got != child {
		t.Fatal("forked child was not registered in the store")
	}
}

func TestStoreForkAtExplicitBoundary(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{})
	parent.Append("turn/start", 1)
	parent.Append("user/message", userMsg("hi"), WithSurfaceOp(AppendOp()))
	boundary := parent.Append("turn/end", map[string]any{"turn": 1})
	parent.Append("turn/start", 2)
	parent.Append("turn/end", map[string]any{"turn": 2})

	child, err := st.Fork(parent, boundary.Seq, "child")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if child.Header().SeedLength != boundary.Seq+1 {
		t.Fatalf("SeedLength = %d, want %d", child.Header().SeedLength, boundary.Seq+1)
	}
}

func TestStoreForkRejectsBoundaryInsideOpenTurn(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{})
	parent.Append("turn/start", 1)
	ev := parent.Append("user/message", userMsg("hi"), WithSurfaceOp(AppendOp()))
	// No turn/end — the turn is still open at ev.Seq.

	_, err := st.Fork(parent, ev.Seq, "")
	var forkErr *ForkError
	if !errors.As(err, &forkErr) || forkErr.Code != "OPEN_TURN" {
		t.Fatalf("err = %v, want ForkError{Code: OPEN_TURN}", err)
	}
}

func TestStoreForkRejectsOutOfRangeBoundary(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{})
	parent.Append("turn/start", 1)

	_, err := st.Fork(parent, 99, "")
	var forkErr *ForkError
	if !errors.As(err, &forkErr) || forkErr.Code != "INVALID_BOUNDARY" {
		t.Fatalf("err = %v, want ForkError{Code: INVALID_BOUNDARY}", err)
	}
}

func TestStoreForkRejectsNonLiveSession(t *testing.T) {
	st := NewStore()
	detached := New("ghost", Header{})
	detached.Append("turn/start", 1)

	_, err := st.Fork(detached, -1, "")
	var forkErr *ForkError
	if !errors.As(err, &forkErr) || forkErr.Code != "SESSION_NOT_LIVE" {
		t.Fatalf("err = %v, want ForkError{Code: SESSION_NOT_LIVE}", err)
	}
}

func TestStoreForkRejectsExistingChildID(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{})
	parent.Append("turn/start", 1)
	if _, err := st.Create("taken", Header{}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := st.Fork(parent, -1, "taken")
	var forkErr *ForkError
	if !errors.As(err, &forkErr) || forkErr.Code != "SESSION_ALREADY_EXISTS" {
		t.Fatalf("err = %v, want ForkError{Code: SESSION_ALREADY_EXISTS}", err)
	}
}

func TestStoreForkOfEmptySessionSeedsNothing(t *testing.T) {
	st := NewStore()
	parent, _ := st.Create("parent", Header{})

	child, err := st.Fork(parent, -1, "")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if child.Header().SeedLength != 0 || len(child.Events()) != 0 {
		t.Fatalf("child = %+v, want an empty seed", child.Header())
	}
}

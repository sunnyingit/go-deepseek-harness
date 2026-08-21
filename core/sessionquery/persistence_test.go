package sessionquery

import (
	"testing"

	"go-harness/core/session"
	"go-harness/session/sessionpersistence"
)

func newPersistedOnlySession(t *testing.T, ps *sessionpersistence.Store, id session.SessionId, cwd string, parent session.SessionId) {
	t.Helper()
	s := session.New(id, session.Header{CWD: cwd, ParentSession: parent})
	s.Append("turn/start", 1)
	s.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})
	if err := ps.Append(s.Header(), s.Events()); err != nil {
		t.Fatal(err)
	}
}

func TestReadSessionFallsBackToPersistence(t *testing.T) {
	store := session.NewStore()
	ps, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newPersistedOnlySession(t, ps, "archived", "/work/proj", "")

	e := New(store, 0)
	if _, err := e.ReadSession("archived"); err == nil {
		t.Fatal("expected a not-found error before WithPersistence is attached")
	}

	e.WithPersistence(ps)
	s, err := e.ReadSession("archived")
	if err != nil {
		t.Fatalf("ReadSession after WithPersistence: %v", err)
	}
	if s.Header().ID != "archived" {
		t.Fatalf("got header %+v", s.Header())
	}
}

func TestReadSessionPrefersLiveOverPersisted(t *testing.T) {
	store := session.NewStore()
	ps, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A stale on-disk copy under the same id as a live session — the live
	// one (with a 2nd turn the disk copy never saw) must win.
	newPersistedOnlySession(t, ps, "s1", "", "")
	live, err := store.Create("s1", session.Header{})
	if err != nil {
		t.Fatal(err)
	}
	live.Append("turn/start", 1)
	live.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})
	live.Append("turn/start", 2)

	got, err := New(store, 0).WithPersistence(ps).ReadSession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got != live {
		t.Fatal("expected the exact live *session.Session, not a freshly-loaded persisted copy")
	}
	if len(got.Events()) != 3 {
		t.Fatalf("got %d events, want 3 (the live session's own state, not the 2-event disk copy)", len(got.Events()))
	}
}

func TestListSessionsMergesLiveAndPersisted(t *testing.T) {
	store := session.NewStore()
	ps, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newPersistedOnlySession(t, ps, "archived", "", "")
	if _, err := store.Create("live", session.Header{}); err != nil {
		t.Fatal(err)
	}

	// Without persistence attached, only the live one is visible.
	recs := New(store, 0).ListSessions()
	if len(recs) != 1 || recs[0].Header.ID != "live" {
		t.Fatalf("without persistence: got %+v, want just live", recs)
	}

	recs = New(store, 0).WithPersistence(ps).ListSessions()
	if len(recs) != 2 {
		t.Fatalf("with persistence: got %d records, want 2", len(recs))
	}
	ids := map[session.SessionId]bool{}
	for _, r := range recs {
		ids[r.Header.ID] = true
	}
	if !ids["live"] || !ids["archived"] {
		t.Fatalf("expected both live and archived, got %+v", recs)
	}
}

func TestListSessionsLiveHeaderWinsOverPersistedDuplicate(t *testing.T) {
	store := session.NewStore()
	ps, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newPersistedOnlySession(t, ps, "s1", "", "")
	if _, err := store.Create("s1", session.Header{}); err != nil {
		t.Fatal(err)
	}

	recs := New(store, 0).WithPersistence(ps).ListSessions()
	if len(recs) != 1 {
		t.Fatalf("got %d records for one id live+persisted, want exactly 1 (deduplicated)", len(recs))
	}
}

func TestTraceSessionAcrossPersistedAncestor(t *testing.T) {
	store := session.NewStore()
	ps, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// "root" only exists on disk (e.g. this process restarted and hasn't
	// reloaded it into the live Store); "child" is live and points at it.
	newPersistedOnlySession(t, ps, "root", "", "")
	child, err := store.Create("child", session.Header{ParentSession: "root"})
	if err != nil {
		t.Fatal(err)
	}
	child.Append("turn/start", 1)
	child.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	trace, err := New(store, 0).WithPersistence(ps).TraceSession("child")
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Complete {
		t.Fatal("expected a complete ancestor chain — root is resolvable via persistence")
	}
	if len(trace.Ancestors) != 1 || trace.Ancestors[0].ID != "root" {
		t.Fatalf("Ancestors = %+v, want just root", trace.Ancestors)
	}

	// The reverse direction: tracing from the persisted-only root should
	// find its live child as a descendant.
	rootTrace, err := New(store, 0).WithPersistence(ps).TraceSession("root")
	if err != nil {
		t.Fatal(err)
	}
	if len(rootTrace.Descendants) != 1 || rootTrace.Descendants[0].Header.ID != "child" {
		t.Fatalf("Descendants = %+v, want just child", rootTrace.Descendants)
	}
}

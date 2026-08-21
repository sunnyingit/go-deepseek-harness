package sessionquery

import (
	"testing"

	"go-harness/core/session"
)

func newStoreWithSession(t *testing.T, id session.SessionId, cwd string) (*session.Store, *session.Session) {
	t.Helper()
	store := session.NewStore()
	s, err := store.Create(id, session.Header{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	return store, s
}

func userMsg(text string) session.Message {
	return session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}}
}

func TestListSessionsNewestFirst(t *testing.T) {
	store := session.NewStore()
	a, _ := store.Create("a", session.Header{CreatedAt: 100})
	b, _ := store.Create("b", session.Header{CreatedAt: 300})
	c, _ := store.Create("c", session.Header{CreatedAt: 200})
	_ = a
	_ = b
	_ = c

	recs := New(store, 0).ListSessions()
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	if recs[0].Header.ID != "b" || recs[1].Header.ID != "c" || recs[2].Header.ID != "a" {
		t.Fatalf("order = %v, want b,c,a (newest-created first)", []session.SessionId{recs[0].Header.ID, recs[1].Header.ID, recs[2].Header.ID})
	}
}

func TestReadSessionNotFound(t *testing.T) {
	store := session.NewStore()
	_, err := New(store, 0).ReadSession("nope")
	qerr, ok := err.(*Error)
	if !ok || qerr.Code != CodeSessionNotFound {
		t.Fatalf("err = %v, want *Error{Code: CodeSessionNotFound}", err)
	}
}

func TestFilterSessionsByCWDAndParent(t *testing.T) {
	store := session.NewStore()
	store.Create("a", session.Header{CWD: "/work/a"})
	store.Create("b", session.Header{CWD: "/work/b"})
	store.Create("c", session.Header{CWD: "/work/a", ParentSession: "a"})

	e := New(store, 0)
	cwd := "/work/a"
	got := e.FilterSessions(SessionResultFilter{CWD: &cwd})
	if len(got) != 2 {
		t.Fatalf("CWD filter: got %d, want 2", len(got))
	}

	root := session.SessionId("")
	got = e.FilterSessions(SessionResultFilter{ParentSession: &root})
	if len(got) != 2 { // a and b have no parent
		t.Fatalf("root filter: got %d, want 2", len(got))
	}

	parent := session.SessionId("a")
	got = e.FilterSessions(SessionResultFilter{ParentSession: &parent})
	if len(got) != 1 || got[0].Header.ID != "c" {
		t.Fatalf("parent=a filter: got %+v, want just c", got)
	}
}

func TestFilterSessionsByCreatedRange(t *testing.T) {
	store := session.NewStore()
	store.Create("a", session.Header{CreatedAt: 100})
	store.Create("b", session.Header{CreatedAt: 200})
	store.Create("c", session.Header{CreatedAt: 300})

	after := int64(150)
	before := int64(250)
	got := New(store, 0).FilterSessions(SessionResultFilter{CreatedAfter: &after, CreatedBefore: &before})
	if len(got) != 1 || got[0].Header.ID != "b" {
		t.Fatalf("got %+v, want just b", got)
	}
}

func TestListEventsClassifiesCurrentShadowedLogOnly(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	s.Append("turn/start", 1)
	verbose := s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "verbose output"}},
	}}, session.WithSurfaceOp(session.AppendOp()))
	s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "compact"}},
	}}, session.WithSurfaceOp(session.ReplaceOp(verbose.Seq, verbose.Seq)), session.WithSourceEventSeqs(verbose.Seq))

	classified, err := New(store, 0).ListEvents("s1")
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]SurfaceClass{}
	for _, ce := range classified {
		if ce.Event.Seq == verbose.Seq {
			byType["shadowed-candidate"] = ce.Surface
		}
	}
	if byType["shadowed-candidate"] != SurfaceShadowed {
		t.Fatalf("the replaced tool/result should classify as shadowed, got %+v", classified)
	}

	var sawLogOnly, sawCurrent bool
	for _, ce := range classified {
		switch {
		case ce.Event.Type == "turn/start":
			if ce.Surface != SurfaceLogOnly {
				t.Errorf("turn/start should classify as log-only, got %v", ce.Surface)
			}
			sawLogOnly = true
		case ce.Event.Type == "tool/result" && ce.Surface == SurfaceCurrent:
			sawCurrent = true
		}
	}
	if !sawLogOnly || !sawCurrent {
		t.Fatalf("expected both a log-only and a current event, classified=%+v", classified)
	}
}

func TestFilterEventsByTextTypeAndSurface(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	s.Append("user/message", userMsg("please summarize the quarterly report"), session.WithSurfaceOp(session.AppendOp()))
	s.Append("user/message", userMsg("what's the weather like"), session.WithSurfaceOp(session.AppendOp()))

	e := New(store, 0)
	text := "QUARTERLY" // case-insensitive
	got, err := e.FilterEvents("s1", SessionEventResultFilter{Text: &text})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("text filter: got %d, want 1", len(got))
	}

	got, err = e.FilterEvents("s1", SessionEventResultFilter{Types: []string{"user/message"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("type filter: got %d, want 2", len(got))
	}

	got, err = e.FilterEvents("s1", SessionEventResultFilter{Surface: []SurfaceClass{SurfaceLogOnly}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("surface filter: got %d, want 0 (no log-only events appended)", len(got))
	}
}

func TestFilterEventsUnknownSession(t *testing.T) {
	store := session.NewStore()
	if _, err := New(store, 0).FilterEvents("nope", SessionEventResultFilter{}); err == nil {
		t.Fatal("expected an error for an unknown session")
	}
}

func TestReadSurfaceMatchesDeriveMessages(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	s.Append("user/message", userMsg("hi"), session.WithSurfaceOp(session.AppendOp()))

	header, msgs, err := New(store, 0).ReadSurface("s1")
	if err != nil {
		t.Fatal(err)
	}
	if header.ID != "s1" {
		t.Fatalf("header.ID = %q, want s1", header.ID)
	}
	want := s.DeriveMessages()
	if len(msgs) != len(want) {
		t.Fatalf("ReadSurface returned %d messages, want %d", len(msgs), len(want))
	}
}

func TestReadEventWindowAndClamp(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	for i := 0; i < 10; i++ {
		s.Append("user/message", userMsg("m"), session.WithSurfaceOp(session.AppendOp()))
	}

	e := New(store, 3) // small readWindowMax to exercise the clamp
	win, err := e.ReadEvent(ReadEventRequest{SessionID: "s1", Seq: 5, Before: 100, After: 100})
	if err != nil {
		t.Fatal(err)
	}
	if win.Target.Seq != 5 {
		t.Fatalf("Target.Seq = %d, want 5", win.Target.Seq)
	}
	if len(win.Before) != 3 {
		t.Fatalf("Before clamped: got %d events, want 3 (readWindowMax)", len(win.Before))
	}
	if len(win.After) != 3 {
		t.Fatalf("After clamped: got %d events, want 3 (readWindowMax)", len(win.After))
	}
}

func TestReadEventNotFound(t *testing.T) {
	store, _ := newStoreWithSession(t, "s1", "")
	_, err := New(store, 0).ReadEvent(ReadEventRequest{SessionID: "s1", Seq: 99})
	qerr, ok := err.(*Error)
	if !ok || qerr.Code != CodeEventNotFound {
		t.Fatalf("err = %v, want *Error{Code: CodeEventNotFound}", err)
	}
}

func TestReadEventWindowNearLogEdges(t *testing.T) {
	store, s := newStoreWithSession(t, "s1", "")
	s.Append("user/message", userMsg("only one"), session.WithSurfaceOp(session.AppendOp()))

	win, err := New(store, 0).ReadEvent(ReadEventRequest{SessionID: "s1", Seq: 0, Before: 5, After: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(win.Before) != 0 || len(win.After) != 0 {
		t.Fatalf("expected an empty window around the only event, got before=%d after=%d", len(win.Before), len(win.After))
	}
}

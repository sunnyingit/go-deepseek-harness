package sessionpersistence

import (
	"os"
	"path/filepath"
	"testing"

	"go-harness/core/session"
)

func userMsg(text string) session.Message {
	return session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}}
}

func assistantMsg(text string) session.Message {
	return session.Message{Role: session.RoleAssistant, Content: []session.ContentBlock{{Kind: session.BlockText, Text: text}}}
}

func TestAppendLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	s := session.New("s1", session.Header{CWD: "/work/proj"})
	s.Append("turn/start", 1)
	s.Append("step/start", map[string]any{"turn": 1, "step": 1})
	s.Append("user/message", userMsg("hello there"), session.WithSurfaceOp(session.AppendOp()))
	s.Append("assistant/chunk", map[string]any{"turn": 1, "step": 1, "chunk": session.ContentBlock{Kind: session.BlockText, Text: "hi"}})
	s.Append("assistant/message", map[string]any{"turn": 1, "step": 1, "message": assistantMsg("hi")},
		session.WithSurfaceOp(session.AppendOp()))
	s.Append("step/end", map[string]any{"turn": 1, "step": 1})
	s.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	if err := store.Append(s.Header(), s.Events()); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("s1", "/work/proj")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Header().ID != "s1" || loaded.Header().CWD != "/work/proj" {
		t.Fatalf("header = %+v, want id=s1 cwd=/work/proj", loaded.Header())
	}
	if len(loaded.Events()) != len(s.Events()) {
		t.Fatalf("loaded %d events, want %d", len(loaded.Events()), len(s.Events()))
	}

	origMsgs := s.DeriveMessages()
	loadedMsgs := loaded.DeriveMessages()
	if len(loadedMsgs) != len(origMsgs) {
		t.Fatalf("DeriveMessages: loaded %d messages, want %d", len(loadedMsgs), len(origMsgs))
	}
	for i := range origMsgs {
		if loadedMsgs[i].Role != origMsgs[i].Role || len(loadedMsgs[i].Content) != len(origMsgs[i].Content) {
			t.Fatalf("message %d = %+v, want %+v", i, loadedMsgs[i], origMsgs[i])
		}
		for j := range origMsgs[i].Content {
			if loadedMsgs[i].Content[j].Text != origMsgs[i].Content[j].Text || loadedMsgs[i].Content[j].Kind != origMsgs[i].Content[j].Kind {
				t.Fatalf("message %d block %d = %+v, want %+v", i, j, loadedMsgs[i].Content[j], origMsgs[i].Content[j])
			}
		}
	}
}

func TestAppendRejectsNonContiguousSeq(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)
	header := session.Header{ID: "s1"}

	if err := store.Append(header, []session.Event{{Type: "turn/start", Seq: 5, Data: 1}}); err == nil {
		t.Fatal("expected an error for a non-contiguous first append")
	}
}

func TestAppendAcrossMultipleCalls(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)
	header := session.Header{ID: "s1"}

	if err := store.Append(header, []session.Event{{Type: "turn/start", Seq: 0, Data: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(header, []session.Event{{Type: "turn/end", Seq: 1, Data: map[string]any{"turn": 1, "reason": "completed"}}}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events()) != 2 {
		t.Fatalf("got %d events, want 2", len(loaded.Events()))
	}
}

func TestSyncOnlyAppendsNewEvents(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	s := session.New("s1", session.Header{})
	s.Append("turn/start", 1)
	if err := store.Sync(s); err != nil {
		t.Fatal(err)
	}
	s.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})
	if err := store.Sync(s); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events()) != 2 {
		t.Fatalf("got %d events, want 2", len(loaded.Events()))
	}

	// A third Sync with nothing new appended must be a no-op, not an error.
	if err := store.Sync(s); err != nil {
		t.Fatalf("no-op Sync returned an error: %v", err)
	}
}

func TestListAndListSnapshots(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	a := session.New("a", session.Header{CWD: "/proj/one"})
	a.Append("turn/start", 1)
	store.Append(a.Header(), a.Events())

	b := session.New("b", session.Header{CWD: "/proj/two"})
	b.Append("turn/start", 1)
	store.Append(b.Header(), b.Events())

	headers, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 {
		t.Fatalf("List returned %d headers, want 2", len(headers))
	}

	snaps, err := store.ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("ListSnapshots returned %d, want 2", len(snaps))
	}
	for _, snap := range snaps {
		if snap.Size <= 0 {
			t.Errorf("snapshot %+v has non-positive size", snap)
		}
	}
}

func TestListSkipsCorruptSessionsWithoutFailing(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	good := session.New("good", session.Header{})
	good.Append("turn/start", 1)
	store.Append(good.Header(), good.Events())

	// Hand-corrupt a second session's file directly on disk.
	badDir := filepath.Join(root, noCWDDir, "bad")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "session.jsonl"), []byte("not json at all\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	headers, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 1 || headers[0].ID != "good" {
		t.Fatalf("List = %+v, want just the good session", headers)
	}
}

func TestLoadRecoversFromTornTail(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	s := session.New("s1", session.Header{})
	s.Append("turn/start", 1)
	s.Append("step/start", map[string]any{"turn": 1, "step": 1})
	s.Append("user/message", userMsg("hi"), session.WithSurfaceOp(session.AppendOp()))
	if err := store.Append(s.Header(), s.Events()); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash mid-write: append a truncated, invalid JSON fragment
	// directly, bypassing Store's own Append (which would never produce this).
	path := sessionFile(root, "s1", "")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"assistant/chunk","seq":3,"data":{"turn":1`); err != nil { // deliberately truncated, no trailing newline
		t.Fatal(err)
	}
	f.Close()

	sizeBefore, _ := os.Stat(path)

	loaded, err := store.LoadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	// The torn line must not survive as an event.
	for _, ev := range loaded.Events() {
		if ev.Seq == 3 {
			t.Fatalf("torn event at seq 3 should not have loaded: %+v", ev)
		}
	}
	if len(loaded.Events()) != 3 {
		t.Fatalf("got %d events, want 3 (turn/start, step/start, user/message)", len(loaded.Events()))
	}

	sizeAfter, _ := os.Stat(path)
	if sizeAfter.Size() >= sizeBefore.Size() {
		t.Fatalf("expected the torn tail to be truncated on disk: before=%d after=%d", sizeBefore.Size(), sizeAfter.Size())
	}

	// A subsequent Append must succeed at seq 3, proving the on-disk
	// durable count was correctly rolled back to the recovered prefix.
	if err := store.Append(loaded.Header(), []session.Event{
		{Type: "step/end", Seq: 3, Data: map[string]any{"turn": 1, "step": 1}},
	}); err != nil {
		t.Fatalf("append after recovery failed: %v", err)
	}
}

func TestLoadClosesUnansweredToolCall(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	s := session.New("s1", session.Header{})
	s.Append("turn/start", 1)
	s.Append("step/start", map[string]any{"turn": 1, "step": 1})
	call := session.ContentBlock{Kind: session.BlockToolCall, CallID: "c1", ToolName: "bash"}
	s.Append("tool/call", map[string]any{"turn": 1, "step": 1, "call": call})
	// Crash happens here — no tool/result was ever appended.
	if err := store.Append(s.Header(), s.Events()); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("s1", "")
	if err != nil {
		t.Fatal(err)
	}

	var foundResult bool
	for _, ev := range loaded.Events() {
		if ev.Type != "tool/result" {
			continue
		}
		wrapped := ev.Data.(map[string]any)
		msg := wrapped["message"].(session.Message)
		for _, block := range msg.Content {
			if block.CallID == "c1" {
				foundResult = true
			}
		}
	}
	if !foundResult {
		t.Fatalf("expected a synthesized tool/result for the unanswered call, events: %+v", loaded.Events())
	}

	// The repair must also be durable — reloading from disk again should
	// show the same synthesized result, not lose it.
	reloaded, err := store.Load("s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Events()) != len(loaded.Events()) {
		t.Fatalf("reload after repair: got %d events, want %d (repair should be durable, not resynthesized)",
			len(reloaded.Events()), len(loaded.Events()))
	}
}

func TestLoadClosesUnansweredToolCallFromAssistantMessage(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)

	s := session.New("s2", session.Header{})
	s.Append("turn/start", 1)
	s.Append("assistant/message", map[string]any{
		"turn": 1, "step": 3,
		"message": session.Message{Role: session.RoleAssistant, Content: []session.ContentBlock{
			{Kind: session.BlockToolCall, CallID: "c-write", ToolName: "write"},
		}},
	}, session.WithSurfaceOp(session.AppendOp()))
	if err := store.Append(s.Header(), s.Events()); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("s2", "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ev := range loaded.Events() {
		if ev.Type != "tool/result" {
			continue
		}
		wrapped := ev.Data.(map[string]any)
		msg := wrapped["message"].(session.Message)
		for _, block := range msg.Content {
			if block.CallID == "c-write" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a synthesized tool/result for the assistant-message tool-call, events: %+v", loaded.Events())
	}
}

func TestLoadUnknownSession(t *testing.T) {
	root := t.TempDir()
	store, _ := Open(root)
	if _, err := store.Load("nope", ""); err == nil {
		t.Fatal("expected an error loading a session that was never appended")
	}
}

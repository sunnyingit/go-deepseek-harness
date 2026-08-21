package main

import (
	"context"
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	"go-harness/session/sessionpersistence"
)

func TestSyncPersistenceNilIsANoOp(t *testing.T) {
	s := &Server{} // persistence intentionally left nil, as newTestServer's bare Servers do
	sess := session.New("s1", session.Header{})
	sess.Append("turn/start", 1)

	// Must not panic.
	s.syncPersistence(sess)
}

func TestSyncPersistenceWritesThrough(t *testing.T) {
	store, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{persistence: store}

	sess := session.New("s1", session.Header{})
	sess.Append("turn/start", 1)
	s.syncPersistence(sess)

	sess.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})
	s.syncPersistence(sess)

	loaded, err := store.Load("s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events()) != 2 {
		t.Fatalf("got %d durable events, want 2", len(loaded.Events()))
	}
}

// TestTurnPersistsWithoutAnyHTTPRequestWatching is the end-to-end
// regression test for the bug where a turn's data could be lost if the
// client that started it disconnected (or never connected in the first
// place, as here) before the turn finished: syncPersistence used to run
// only from inside handleStreamSession's own poll loop, reached only by a
// connection still open when the turn went idle. This wires the exact
// Hooks.TurnEnded pattern buildAgent uses — TurnEnded calling
// syncPersistence — directly onto a real *agentloop.Agent, drives a turn
// to completion with zero HTTP handlers or connections involved at all,
// and checks the result landed on disk anyway.
func TestTurnPersistsWithoutAnyHTTPRequestWatching(t *testing.T) {
	store, err := sessionpersistence.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{persistence: store}

	sess := session.New("no-watcher", session.Header{})
	agent := agentloop.NewAgent(sess, oneShotLLM{reply: "hi there"}, fakeTools{}, "sys", agentloop.Hooks{
		TurnEnded: func(context.Context, int, string) { s.syncPersistence(sess) },
	})

	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: "hello"}}})
	agent.WhenIdle()

	loaded, err := store.Load("no-watcher", "")
	if err != nil {
		t.Fatalf("session was never persisted: %v", err)
	}
	if len(loaded.DeriveMessages()) < 2 {
		t.Fatalf("loaded %d messages from disk, want at least 2 (the user message + the assistant reply)", len(loaded.DeriveMessages()))
	}
}

func TestRestorePersistedSessionsNilPersistenceIsANoOp(t *testing.T) {
	s := &Server{agents: nil} // no store either — restorePersistedSessions must return before touching either
	n, err := s.restorePersistedSessions()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("got %d, want 0", n)
	}
}

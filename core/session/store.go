package session

import (
	"fmt"
	"sync"
)

// ForkError is returned by Store.Fork on any validation failure, with a
// stable Code a caller can branch on.
type ForkError struct {
	Message string
	Code    string
}

func (e *ForkError) Error() string { return fmt.Sprintf("session: %s: %s", e.Code, e.Message) }

// Store creates and holds Session instances, mirroring ctx.sessions. This
// port has no persistence to make durable — see README.md — so Store is
// purely an in-memory registry plus Fork's boundary logic.
type Store struct {
	mu       sync.Mutex
	sessions map[SessionId]*Session
	counter  int
}

func NewStore() *Store {
	return &Store{sessions: map[SessionId]*Session{}}
}

// Create builds a fresh session and adds it to the store. An empty id mints
// "session-<n>" for the first unused n, mirroring the TS store's default.
func (st *Store) Create(id SessionId, meta Header) (*Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	resolvedID, err := st.resolveID(id)
	if err != nil {
		return nil, err
	}
	s := New(resolvedID, meta)
	st.sessions[resolvedID] = s
	return s, nil
}

// resolveID must be called with mu held.
func (st *Store) resolveID(id SessionId) (SessionId, error) {
	if id == "" {
		for {
			st.counter++
			candidate := SessionId(fmt.Sprintf("session-%d", st.counter))
			if _, exists := st.sessions[candidate]; !exists {
				return candidate, nil
			}
		}
	}
	if _, exists := st.sessions[id]; exists {
		return "", fmt.Errorf("session: %q already exists", id)
	}
	return id, nil
}

// Get returns the live session registered under id, if any.
func (st *Store) Get(id SessionId) (*Session, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	return s, ok
}

// List returns every live session, in no particular order.
func (st *Store) List() []*Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]*Session, 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}

// Fork creates a live child session seeded from source's log up to and
// including boundary (inclusive; the current last event when boundary is
// -1), recording lineage in the child's header. boundary must land outside
// an open turn — a fork mid-turn would seed a child with a step that never
// finished, and it must be a real, contiguous seq in source's log.
func (st *Store) Fork(source *Session, boundary int, childID SessionId) (*Session, error) {
	st.mu.Lock()
	if childID != "" {
		if _, exists := st.sessions[childID]; exists {
			st.mu.Unlock()
			return nil, &ForkError{Code: "SESSION_ALREADY_EXISTS", Message: fmt.Sprintf("session %q already exists", childID)}
		}
	}
	live, ok := st.sessions[source.ID()]
	st.mu.Unlock()
	if !ok || live != source {
		return nil, &ForkError{Code: "SESSION_NOT_LIVE", Message: fmt.Sprintf("session %q is not a live store instance", source.ID())}
	}

	seed, err := forkSeed(source, boundary)
	if err != nil {
		return nil, err
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	resolvedID, err := st.resolveID(childID)
	if err != nil {
		return nil, err
	}
	header := source.Header()
	child, err := NewSeeded(resolvedID, Header{
		CWD:           header.CWD,
		ParentSession: source.ID(),
	}, seed)
	if err != nil {
		return nil, err
	}
	st.sessions[resolvedID] = child
	return child, nil
}

// forkSeed selects and validates the boundary prefix Fork seeds a child
// from. boundary of -1 means "the current last event."
func forkSeed(source *Session, boundary int) ([]Event, error) {
	events := source.Events()
	if boundary == -1 {
		if len(events) == 0 {
			return nil, nil
		}
		boundary = events[len(events)-1].Seq
	}
	if boundary < 0 {
		return nil, &ForkError{Code: "INVALID_BOUNDARY", Message: fmt.Sprintf("fork boundary for session %q must be a non-negative integer, got %d", source.ID(), boundary)}
	}
	if boundary >= len(events) {
		last := -1
		if len(events) > 0 {
			last = events[len(events)-1].Seq
		}
		return nil, &ForkError{Code: "INVALID_BOUNDARY", Message: fmt.Sprintf("fork boundary %d does not exist in session %q (last seq: %d)", boundary, source.ID(), last)}
	}
	if events[boundary].Seq != boundary {
		return nil, &ForkError{Code: "INVALID_BOUNDARY", Message: fmt.Sprintf("fork boundary %d does not match a contiguous event seq in session %q", boundary, source.ID())}
	}

	// The last turn/start or turn/end at or before boundary decides whether
	// the prefix ends inside an open turn.
	openTurn := -1
	for i := 0; i <= boundary; i++ {
		switch events[i].Type {
		case "turn/start":
			openTurn = turnNumberOf(events[i])
		case "turn/end":
			openTurn = -1
		}
	}
	if openTurn != -1 {
		return nil, &ForkError{Code: "OPEN_TURN", Message: fmt.Sprintf("fork boundary %d in session %q ends inside open turn %d", boundary, source.ID(), openTurn)}
	}

	return events[:boundary+1], nil
}

func turnNumberOf(ev Event) int {
	if turn, ok := ev.Data.(int); ok {
		return turn
	}
	return -1
}

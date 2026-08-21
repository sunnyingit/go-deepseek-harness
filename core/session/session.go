package session

import (
	"fmt"
	"sync"
	"time"
)

// AppendOption configures one Append call — currently just the surface
// placement a message-producing event must declare.
type AppendOption func(*appendOptions)

type appendOptions struct {
	surfaceOp       *SurfaceOp
	sourceEventSeqs []int
}

// WithSurfaceOp declares how a surface-eligible event enters the ordered
// surface. Required on user/message, assistant/message, and tool/result;
// Append rejects it on any other type.
func WithSurfaceOp(op SurfaceOp) AppendOption {
	return func(o *appendOptions) { o.surfaceOp = &op }
}

// WithSourceEventSeqs cites the earlier events this one derives from (e.g.
// the assistant/chunk seqs behind an assistant/message, or every node a
// replace shadows — Append rejects a replace that doesn't cite all of them).
func WithSourceEventSeqs(seqs ...int) AppendOption {
	return func(o *appendOptions) { o.sourceEventSeqs = seqs }
}

// Session is the append-only, event-sourced log for one agent interaction —
// the single source of truth. Message history is derived from it via
// DeriveMessages, never stored separately. Safe for concurrent use.
type Session struct {
	mu     sync.Mutex
	header Header
	log    []Event

	surface surfaceFold

	derived           []Message
	derivedNodeCount  int
	derivedGeneration int
}

// New constructs a detached session (not registered in any Store) with a
// freshly minted Header. Use Store.Create for a store-tracked session, or
// this directly for standalone/test use — mirrors TS's Session.create()
// static factory, which likewise needs no store.
func New(id SessionId, meta Header) *Session {
	meta.Version = FormatVersion
	meta.ID = id
	if meta.CreatedAt == 0 {
		meta.CreatedAt = time.Now().UnixMilli()
	}
	return &Session{header: meta}
}

// NewSeeded constructs a session whose log starts from seed (a fork or
// replay boundary), replaying every event through the same validation
// Append enforces before accepting it — so a seed can never construct a log
// no ordinary append sequence could have produced.
func NewSeeded(id SessionId, meta Header, seed []Event) (*Session, error) {
	s := New(id, meta)
	s.header.SeedLength = len(seed)
	for i, ev := range seed {
		if ev.Seq != i {
			return nil, fmt.Errorf("seed event at index %d has seq %d (expected %d); seed must be contiguous from 0", i, ev.Seq, i)
		}
		plan, err := planSurfaceEvent(&s.surface, ev, s.log)
		if err != nil {
			return nil, fmt.Errorf("invalid seed event at index %d: %w", i, err)
		}
		s.log = append(s.log, ev)
		commitSurfacePlan(&s.surface, plan)
	}
	return s, nil
}

// ID is the session's identity, from its header.
func (s *Session) ID() SessionId {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header.ID
}

// Header returns the session's immutable creation metadata.
func (s *Session) Header() Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.header
}

// Seq is the next event's sequence number — always the log length.
func (s *Session) Seq() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.log)
}

// Events returns a snapshot of the append-only log in order.
func (s *Session) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.log))
	copy(out, s.log)
	return out
}

// LastTurn scans for the last turn/start event's turn number, defaulting to
// 0 — how a resumed driver picks up its turn counter. Not part of the TS
// package (it has no equivalent concept at this layer); added because
// core/agentloop's resume-boundary logic needs it and this is its natural
// home now that agentloop no longer owns the log.
func (s *Session) LastTurn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	last := 0
	for _, ev := range s.log {
		if ev.Type == "turn/start" {
			if turn, ok := ev.Data.(int); ok {
				last = turn
			}
		}
	}
	return last
}

// Append records one event and returns it with its assigned Seq/Time. A
// surface-eligible type (user/message, assistant/message, tool/result)
// requires WithSurfaceOp; any other type must not carry one. Append panics
// on a structural violation (missing/extraneous surface op, an invalid
// replace range, incomplete provenance) — a programming error in the
// caller, not a recoverable runtime condition, mirroring the TS original's
// synchronous throw. core/agentloop's own turn()/kick() already recover
// panics from their internal call chain, so a violation here is contained
// the same way any other internal bug would be, not a process crash.
func (s *Session) Append(eventType string, data any, opts ...AppendOption) Event {
	var o appendOptions
	for _, opt := range opts {
		opt(&o)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ev := Event{
		Type:            eventType,
		Seq:             len(s.log),
		Time:            time.Now().UnixMilli(),
		Data:            data,
		SourceEventSeqs: o.sourceEventSeqs,
		SurfaceOp:       o.surfaceOp,
	}
	plan, err := planSurfaceEvent(&s.surface, ev, s.log)
	if err != nil {
		panic(fmt.Sprintf("session: %v", err))
	}
	s.log = append(s.log, ev)
	commitSurfacePlan(&s.surface, plan)
	return ev
}

// DeriveMessages projects the LLM message history by walking the ordered
// surface: every message-producing append records how it joined the
// surface, so a raw non-surface event (a chunk, a turn boundary) is
// correctly absent, and a replace removes the shadowed nodes from the
// derivation. Cached: each surface node is projected once, when first seen;
// a surface rewrite (replaceGeneration change) rebuilds from scratch.
func (s *Session) DeriveMessages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.surface.replaceGeneration != s.derivedGeneration {
		s.derived = nil
		s.derivedNodeCount = 0
		s.derivedGeneration = s.surface.replaceGeneration
	}
	for _, seq := range s.surface.nodes[s.derivedNodeCount:] {
		if msg, ok := DeriveEventMessage(s.log[seq]); ok {
			s.derived = append(s.derived, msg)
		}
	}
	s.derivedNodeCount = len(s.surface.nodes)
	out := make([]Message, len(s.derived))
	copy(out, s.derived)
	return out
}

// SurfaceSeqs returns the seq of every event currently on the surface, in
// positional order — the same ordered node list DeriveMessages walks,
// exposed directly for a caller that needs to know which log events are
// shadowed rather than derive their content (e.g. core/sessionquery's
// current/shadowed/log-only classification).
func (s *Session) SurfaceSeqs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, len(s.surface.nodes))
	copy(out, s.surface.nodes)
	return out
}

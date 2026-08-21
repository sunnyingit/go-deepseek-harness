package sessionquery

import (
	"fmt"
	"sort"

	"go-harness/core/session"
)

// TraceSession returns id's direct-parent-outward ancestor chain and its
// full descendant tree — live sessions and, once WithPersistence is
// attached, persisted-but-not-live ones alike. Complete is false when the
// walk runs off a parent id that resolves in neither place (the session
// was genuinely discarded, or persistence was never attached to recover
// it from).
func (e *Engine) TraceSession(id session.SessionId) (LineageTrace, error) {
	if _, err := e.ReadSession(id); err != nil {
		return LineageTrace{}, err
	}

	var ancestors []session.Header
	visited := map[session.SessionId]bool{id: true}
	complete := true
	cur := id
	for {
		s, ok := e.getSession(cur)
		if !ok {
			complete = false
			break
		}
		parent := s.Header().ParentSession
		if parent == "" {
			break
		}
		if visited[parent] {
			return LineageTrace{}, &Error{Code: CodeInvalidLineage,
				Message: fmt.Sprintf("session %q's ancestor chain cycles back to %q", id, parent)}
		}
		parentSession, ok := e.getSession(parent)
		if !ok {
			complete = false
			break
		}
		ancestors = append(ancestors, parentSession.Header())
		visited[parent] = true
		cur = parent
	}

	descendants := e.descendantTree(id, map[session.SessionId]bool{id: true})
	return LineageTrace{Ancestors: ancestors, Complete: complete, Descendants: descendants}, nil
}

// descendantTree finds every session (live or, once WithPersistence is
// attached, persisted) whose ParentSession is id, recursively, oldest
// -created first. visited guards against a cycle — structurally
// unreachable through the ordinary Store.Fork API (a header's
// ParentSession is set once at creation and Fork always mints a new id),
// but defended against explicitly rather than assumed, matching the same
// invariant TraceSession's ancestor walk enforces.
func (e *Engine) descendantTree(id session.SessionId, visited map[session.SessionId]bool) []LineageNode {
	var children []LineageNode
	for _, h := range e.allHeaders() {
		if h.ParentSession != id || visited[h.ID] {
			continue
		}
		visited[h.ID] = true
		children = append(children, LineageNode{Header: h, Children: e.descendantTree(h.ID, visited)})
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Header.CreatedAt < children[j].Header.CreatedAt })
	return children
}

// TraceEvent returns one event's own source-event links plus the chain of
// positional replacements that shadowed it, if any, ending at whichever
// event currently holds its surface position.
func (e *Engine) TraceEvent(sessionID session.SessionId, seq int) (EventTrace, error) {
	s, err := e.ReadSession(sessionID)
	if err != nil {
		return EventTrace{}, err
	}
	events := s.Events()
	if seq < 0 || seq >= len(events) || events[seq].Seq != seq {
		return EventTrace{}, &Error{Code: CodeEventNotFound, Message: fmt.Sprintf("no event at seq %d in session %q", seq, sessionID)}
	}
	target := events[seq]

	var chain []session.Event
	cur := seq
	for {
		next := findReplacer(events, cur)
		if next == nil {
			break
		}
		chain = append(chain, *next)
		cur = next.Seq
	}

	return EventTrace{
		Header:           s.Header(),
		Event:            target,
		SourceEventSeqs:  append([]int(nil), target.SourceEventSeqs...),
		ReplacementChain: chain,
	}, nil
}

// findReplacer linear-scans for the later surface-eligible replace event
// that cites seq in its SourceEventSeqs — core/session's Append validates
// that a replace names every node it shadows (see surface.go's
// assertProvenance), so a shadowed seq appears in exactly one later
// event's SourceEventSeqs. O(n) per hop rather than an index, matching
// this package's stateless, no-incremental-cache posture — see README.md.
func findReplacer(events []session.Event, seq int) *session.Event {
	for i := range events {
		ev := events[i]
		if ev.SurfaceOp == nil || !ev.SurfaceOp.Replace {
			continue
		}
		for _, s := range ev.SourceEventSeqs {
			if s == seq {
				return &events[i]
			}
		}
	}
	return nil
}

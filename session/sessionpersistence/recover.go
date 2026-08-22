// Crash recovery for a persisted log: tolerating a torn tail (the process
// died mid-write, so the last line on disk may be incomplete or absent)
// and closing out any tool call that was logged but never got its result
// (the process died between dispatching a tool and recording its
// outcome). See README.md for what this deliberately does not attempt —
// notably, synthesizing turn/end and step/end closers for a dangling open
// turn the way the TS original does, which this port's core/agentloop
// doesn't need (Session.LastTurn() only reads the highest turn/start seen
// and Agent always opens a fresh next turn on resume — it never tries to
// continue an interrupted one — so a dangling turn/start is inert here,
// not a correctness hazard the way an unanswered tool call is).
package sessionpersistence

import (
	"fmt"

	"go-harness/core/session"
)

// recoverPrefix returns the longest prefix of events that session.NewSeeded
// accepts, trying the full set first (the common, uncorrupted case) and
// shrinking from the end only when validation actually fails — reusing
// core/session's own seed validation as the source of truth for "is this
// prefix well-formed" rather than duplicating its private surface-fold
// logic here.
func recoverPrefix(id session.SessionId, header session.Header, events []session.Event) (*session.Session, int, error) {
	for n := len(events); n >= 0; n-- {
		s, err := session.NewSeeded(id, header, events[:n])
		if err == nil {
			return s, n, nil
		}
		if n == 0 {
			return nil, 0, fmt.Errorf("session-persistence: no valid prefix for session %q: %w", id, err)
		}
	}
	panic("unreachable")
}

// closeUnansweredToolCalls is the load-time wrapper around the session
// repair: unanswered tool-call blocks (a crash mid-dispatch, or a
// max-tokens cut-off that persisted tool_calls without execute) would
// otherwise replay to a real LLM as a malformed request.
func closeUnansweredToolCalls(s *session.Session) {
	session.CloseUnansweredToolCalls(s)
}

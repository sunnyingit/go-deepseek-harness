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

// closeUnansweredToolCalls appends a synthetic tool/result for every
// tool/call in s's recovered log that has no matching result — the one
// piece of crash-recovery repair this port treats as load-bearing: an
// assistant message with unanswered tool-call blocks would otherwise
// replay to a real LLM provider as a malformed request (most chat-
// completion APIs require a tool response to immediately follow a
// tool-calling assistant turn). The synthesized result's text is
// deliberately cautious — matching the TS original's own TOOL_OUTCOME_UNKNOWN
// guidance — since the tool may or may not have completed before the
// crash.
func closeUnansweredToolCalls(s *session.Session) {
	events := s.Events()

	answered := map[string]bool{}
	for _, ev := range events {
		if ev.Type != "tool/result" {
			continue
		}
		wrapped, ok := ev.Data.(map[string]any)
		if !ok {
			continue
		}
		msg, ok := wrapped["message"].(session.Message)
		if !ok {
			continue
		}
		for _, block := range msg.Content {
			if block.Kind == session.BlockToolResult && block.CallID != "" {
				answered[block.CallID] = true
			}
		}
	}

	for _, ev := range events {
		if ev.Type != "tool/call" {
			continue
		}
		wrapped, ok := ev.Data.(map[string]any)
		if !ok {
			continue
		}
		call, ok := wrapped["call"].(session.ContentBlock)
		if !ok || call.CallID == "" || answered[call.CallID] {
			continue
		}
		turn, _ := wrapped["turn"].(int)
		step, _ := wrapped["step"].(int)

		s.Append("tool/result", map[string]any{
			"turn": turn, "step": step,
			"message": session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{
				Kind:   session.BlockToolResult,
				CallID: call.CallID,
				Text: fmt.Sprintf(
					"Interrupted before this tool call's outcome was recorded (call: %s). "+
						"Its side effects, if any, are unverified — check before retrying, "+
						"and only retry work that is read-only or safely idempotent.",
					call.ToolName),
			}}},
			"recovered": true,
		}, session.WithSurfaceOp(session.AppendOp()))
		answered[call.CallID] = true
	}
}

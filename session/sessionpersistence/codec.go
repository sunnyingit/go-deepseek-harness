// Codec for this package's own on-disk JSONL line shape. This is a
// private wire format this package alone reads and writes — not an
// attempt to match the TS original's byte layout — so the only
// requirement is that encode and decode round-trip every event shape
// core/agentloop and plan/planmode actually produce back into the same
// concrete Go types DeriveEventMessage and every other consumer expect
// (a plain json.Unmarshal into `any` would hand them a generic
// map[string]any instead of a session.Message/session.ContentBlock, which
// silently breaks surface derivation — see decodeEventData).
package sessionpersistence

import (
	"encoding/json"
	"fmt"

	"go-harness/core/session"
)

// wireEvent is the on-disk JSON shape for one log line.
type wireEvent struct {
	Type            string             `json:"type"`
	Seq             int                `json:"seq"`
	Time            int64              `json:"time"`
	Data            json.RawMessage    `json:"data"`
	Ignorable       bool               `json:"ignorable,omitempty"`
	SourceEventSeqs []int              `json:"sourceEventSeqs,omitempty"`
	SurfaceOp       *session.SurfaceOp `json:"surfaceOp,omitempty"`
}

func encodeEvent(ev session.Event) ([]byte, error) {
	raw, err := json.Marshal(ev.Data)
	if err != nil {
		return nil, fmt.Errorf("session-persistence: encoding event %d (%s): %w", ev.Seq, ev.Type, err)
	}
	return json.Marshal(wireEvent{
		Type: ev.Type, Seq: ev.Seq, Time: ev.Time, Data: raw,
		Ignorable: ev.Ignorable, SourceEventSeqs: ev.SourceEventSeqs, SurfaceOp: ev.SurfaceOp,
	})
}

func decodeEvent(line []byte) (session.Event, error) {
	var w wireEvent
	if err := json.Unmarshal(line, &w); err != nil {
		return session.Event{}, err
	}
	data, err := decodeEventData(w.Type, w.Data)
	if err != nil {
		return session.Event{}, fmt.Errorf("session-persistence: decoding event %d (%s): %w", w.Seq, w.Type, err)
	}
	return session.Event{
		Type: w.Type, Seq: w.Seq, Time: w.Time, Data: data,
		Ignorable: w.Ignorable, SourceEventSeqs: w.SourceEventSeqs, SurfaceOp: w.SurfaceOp,
	}, nil
}

// decodeEventData reconstructs the concrete Go type core/agentloop (and
// plan/planmode) actually stores in Event.Data for each event type it
// produces — mirroring core/session.DeriveEventMessage's own switch, which
// is the reason this precision matters: it type-asserts Data directly
// (session.Message for user/message, a map with a "message" key holding a
// session.Message for assistant/message and tool/result) and silently
// treats a mismatch as "no message," not an error. An event type this
// package doesn't recognize (a future or third-party plugin's own event)
// decodes into whatever plain JSON shape encoding/json produces — best
// -effort, since this package can't know a type it's never seen the
// producer's Go struct for.
func decodeEventData(eventType string, raw json.RawMessage) (any, error) {
	switch eventType {
	case "turn/start":
		var v int
		err := json.Unmarshal(raw, &v)
		return v, err

	case "user/message":
		var m session.Message
		err := json.Unmarshal(raw, &m)
		return m, err

	case "assistant/message", "tool/result":
		var w struct {
			Turn      int             `json:"turn"`
			Step      int             `json:"step"`
			Message   session.Message `json:"message"`
			Error     bool            `json:"error,omitempty"`
			Recovered bool            `json:"recovered,omitempty"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		out := map[string]any{"turn": w.Turn, "step": w.Step, "message": w.Message}
		if w.Error {
			out["error"] = true
		}
		if w.Recovered {
			out["recovered"] = true
		}
		return out, nil

	case "assistant/chunk":
		var w struct {
			Turn  int                  `json:"turn"`
			Step  int                  `json:"step"`
			Chunk session.ContentBlock `json:"chunk"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		return map[string]any{"turn": w.Turn, "step": w.Step, "chunk": w.Chunk}, nil

	case "tool/call":
		var w struct {
			Turn int                  `json:"turn"`
			Step int                  `json:"step"`
			Call session.ContentBlock `json:"call"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		return map[string]any{"turn": w.Turn, "step": w.Step, "call": w.Call}, nil

	case "step/start", "step/end":
		var w struct {
			Turn int `json:"turn"`
			Step int `json:"step"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		return map[string]any{"turn": w.Turn, "step": w.Step}, nil

	case "turn/end":
		var w struct {
			Turn   int    `json:"turn"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, err
		}
		return map[string]any{"turn": w.Turn, "reason": w.Reason}, nil

	default:
		var v any
		err := json.Unmarshal(raw, &v)
		return v, err
	}
}

// Package session is a Go port of packages/core/session from DeepSeek
// Harness: the append-only, event-sourced session log, its surface
// projection (the ordered view deriveMessages walks), and an in-memory
// store with fork support. Message history is DERIVED from the log, never
// stored separately — the same invariant this codebase has followed since
// core/agentloop, now owned here instead of duplicated in that package.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package session

import "fmt"

// SessionId identifies one session in a Store.
type SessionId string

// FormatVersion is stamped into every newly created Header. This port has
// no persistence backend to enforce it against on load — see README.md —
// but new sessions still carry it, so a future backend has something to check.
const FormatVersion = 0

// ---- Conversation vocabulary ----------------------------------------------
//
// Moved here from core/agentloop (which now imports these via type aliases)
// to match the TS dependency direction: agent-loop depends on session, not
// the reverse. See core/agentloop's README for the alias list.

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockReasoning  BlockKind = "reasoning" // chain-of-thought, e.g. DeepSeek thinking mode
	BlockToolCall   BlockKind = "tool-call"
	BlockToolResult BlockKind = "tool-result"
)

type ContentBlock struct {
	Kind     BlockKind
	Text     string
	CallID   string
	ToolName string
	Args     map[string]any
}

type Message struct {
	Role    Role
	Content []ContentBlock
}

// ---- Header ----------------------------------------------------------------

// Header is immutable creation metadata, kept outside the event log — a
// storage concern, not replayable conversation state. Mirrors TS's
// SessionHeader minus the fields this port's Store never produces
// (agentPreset — no deployment composition concept here).
type Header struct {
	Version         int
	ID              SessionId
	CreatedAt       int64 // Unix epoch milliseconds
	CWD             string
	ParentSession   SessionId // "" unless forked
	SeedLength      int       // leading events inherited through a seed
	Origin          string    // "" or "subagent"
	DelegationDepth int
}

// ---- Surface operations -----------------------------------------------------

// SurfaceOp is how a surface-eligible event enters the ordered surface:
// either appended to the tail, or replacing an existing contiguous range
// (by seq) with this one event — the mechanism a compaction-style producer
// uses to shadow earlier messages without deleting them from the log.
type SurfaceOp struct {
	Replace bool
	Start   int // inclusive seq, only meaningful when Replace
	End     int // inclusive seq, only meaningful when Replace
}

// AppendOp is the ordinary "add to the tail" surface operation.
func AppendOp() SurfaceOp { return SurfaceOp{} }

// ReplaceOp shadows the surface nodes from start through end (inclusive,
// both must currently be on the surface) with the event carrying this op.
func ReplaceOp(start, end int) SurfaceOp { return SurfaceOp{Replace: true, Start: start, End: end} }

// SurfaceEligible reports whether eventType may carry a SurfaceOp — the
// three message-producing event types. Every other type must not.
func SurfaceEligible(eventType string) bool {
	switch eventType {
	case "user/message", "assistant/message", "tool/result":
		return true
	}
	return false
}

// ---- Event envelope ---------------------------------------------------------

// Event is one immutable entry in the session log. SourceEventSeqs and
// SurfaceOp are only meaningful (and only ever set) on SurfaceEligible
// types — see the Session.Append docs for the requirement.
type Event struct {
	Type            string
	Seq             int
	Time            int64 // Unix epoch milliseconds
	Data            any
	Ignorable       bool
	SourceEventSeqs []int      // nil = absent
	SurfaceOp       *SurfaceOp // nil = absent (non-surface event, or malformed)
}

func (e Event) String() string {
	return fmt.Sprintf("Event{seq=%d type=%q}", e.Seq, e.Type)
}

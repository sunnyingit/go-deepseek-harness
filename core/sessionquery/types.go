// Package sessionquery is a Go port of the read/filter/trace surface from
// packages/session-query/session-query (DeepSeek Harness): exact-match
// listing, metadata filtering, and lineage tracing over a *session.Store —
// everything that package's ctx.sessionQuery contract defines except its
// two full-text search methods, which the TS original leaves to a separate
// concrete backend (session-query-sqlite) and this port does the same
// (core/sessionquery/sqlitesearch). What's cut relative to the TS original
// is documented in README.md — chiefly everything about ctx.sessionPersistence
// reconciliation, since this codebase has no persistence layer to reconcile
// with (core/session.Store is in-memory only).
package sessionquery

import "go-harness/core/session"

// Code is a stable, switchable error classification.
type Code string

const (
	CodeSessionNotFound Code = "SESSION_QUERY_SESSION_NOT_FOUND"
	CodeInvalidFilter   Code = "SESSION_QUERY_INVALID_FILTER"
	CodeInvalidSurface  Code = "SESSION_QUERY_INVALID_SURFACE"
	CodeInvalidLineage  Code = "SESSION_QUERY_INVALID_LINEAGE"
	CodeEventNotFound   Code = "SESSION_QUERY_EVENT_NOT_FOUND"
)

// Error is this package's error type — Code is stable and safe to branch
// on, Message is human-readable detail.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// SessionRecord is a session's listable metadata — its header plus a
// couple of fields cheap to derive without replaying semantics, the same
// shape ListSessions/FilterSessions/TraceSession return.
type SessionRecord struct {
	Header        session.Header
	EventCount    int
	LastEventTime int64 // 0 if the session has no events yet
}

// SessionResultFilter narrows FilterSessions. Every non-nil field is an
// AND'd predicate; a field holding a slice matches if any element matches
// (OR within the field, matching the TS original's own filter-array
// semantics) — an empty non-nil slice therefore matches nothing.
type SessionResultFilter struct {
	IDs           []session.SessionId
	CWD           *string
	CreatedAfter  *int64 // inclusive, Unix epoch milliseconds
	CreatedBefore *int64 // inclusive, Unix epoch milliseconds
	// ParentSession filters by lineage; a pointer to "" matches only root
	// sessions (no parent).
	ParentSession *session.SessionId
}

// SurfaceClass classifies one logged event against a session's current
// surface: "current" is on the surface DeriveMessages walks right now,
// "shadowed" was surface-eligible but has since been replaced away
// (compacted), "log-only" was never surface-eligible at all (a boundary,
// chunk, or other non-message record).
type SurfaceClass string

const (
	SurfaceCurrent  SurfaceClass = "current"
	SurfaceShadowed SurfaceClass = "shadowed"
	SurfaceLogOnly  SurfaceClass = "log-only"
)

// ClassifiedEvent is one ListEvents result: the raw log event plus its
// surface classification.
type ClassifiedEvent struct {
	Event   session.Event
	Surface SurfaceClass
}

// SessionEventResultFilter narrows FilterEvents. Every non-nil field is an
// AND'd predicate, same OR-within-a-slice-field convention as
// SessionResultFilter. Text is a literal, case-insensitive substring match
// against the event's derived message text (see extractEventText) — not a
// full-text/token query; that distinction is the whole reason
// searchSessions/searchEvents exist as separate, FTS-backed methods.
type SessionEventResultFilter struct {
	SeqAfter   *int   // inclusive
	SeqBefore  *int   // inclusive
	TimeAfter  *int64 // inclusive, Unix epoch milliseconds
	TimeBefore *int64 // inclusive, Unix epoch milliseconds
	Types      []string
	Surface    []SurfaceClass
	Text       *string
}

// ReadEventRequest is ReadEvent's argument: the target event plus how much
// raw log context around it to include.
type ReadEventRequest struct {
	SessionID session.SessionId
	Seq       int
	Before    int // raw events before Seq to include, capped at Engine's ReadWindowMax
	After     int // raw events after Seq to include, capped at Engine's ReadWindowMax
}

// EventWindow is ReadEvent's result: the target event plus its bounded
// surrounding raw-log window, in log order.
type EventWindow struct {
	Header session.Header
	Target session.Event
	Before []session.Event
	After  []session.Event
}

// LineageNode is one entry in TraceSession's ancestor chain or descendant
// tree.
type LineageNode struct {
	Header   session.Header
	Children []LineageNode
}

// LineageTrace is TraceSession's result: the direct-parent-outward
// ancestor chain (index 0 is the immediate parent) and the full descendant
// tree rooted at the traced session. Complete is false when an ancestor's
// parent id no longer resolves to a live session (its own session has been
// discarded — this port has no persistence to recover it from).
type LineageTrace struct {
	Ancestors   []session.Header
	Complete    bool
	Descendants []LineageNode
}

// EventTrace is TraceEvent's result: the event's own source-event links
// (already on session.Event.SourceEventSeqs) plus the chain of positional
// replacements that shadowed it, if any, ending at whichever event
// currently holds its surface position.
type EventTrace struct {
	Header           session.Header
	Event            session.Event
	SourceEventSeqs  []int
	ReplacementChain []session.Event // empty if Event is itself still current
}

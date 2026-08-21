package sessionquery

import (
	"fmt"
	"sort"
	"strings"

	"go-harness/core/session"
	"go-harness/session/sessionpersistence"
)

// DefaultReadWindowMax mirrors the TS original's own default for ReadEvent's
// Before/After bound.
const DefaultReadWindowMax = 50

// Engine is the read/filter/trace surface over one *session.Store, with an
// optional durable fallback for a session that isn't currently live — this
// port's equivalent of ctx.sessionQuery's inherited (non-full-text)
// methods. See README.md for what's cut relative to the TS original.
type Engine struct {
	store         *session.Store
	readWindowMax int
	persistence   *sessionpersistence.Store
}

// New builds an Engine over store. readWindowMax bounds ReadEvent's
// Before/After (DefaultReadWindowMax if <= 0). Every read method falls
// back to persistence (if attached via WithPersistence) only for a
// session id that isn't currently live in store — a live copy is always
// authoritative.
func New(store *session.Store, readWindowMax int) *Engine {
	if readWindowMax <= 0 {
		readWindowMax = DefaultReadWindowMax
	}
	return &Engine{store: store, readWindowMax: readWindowMax}
}

// WithPersistence attaches p as the durable fallback every read method
// consults for a session id not currently live in Engine's Store — nil is
// the zero-value Engine's own behavior (in-memory only), so this is opt
// -in, not a breaking change to an Engine built before persistence
// existed in this codebase.
func (e *Engine) WithPersistence(p *sessionpersistence.Store) *Engine {
	e.persistence = p
	return e
}

// getSession is the live-then-persisted lookup every other method in this
// package builds on: ReadSession wraps it with a typed not-found error,
// ListSessions/allHeaders and TraceSession's ancestor walk use it directly
// since they already have their own not-found handling.
func (e *Engine) getSession(id session.SessionId) (*session.Session, bool) {
	if s, ok := e.store.Get(id); ok {
		return s, true
	}
	if e.persistence != nil {
		return e.loadPersisted(id)
	}
	return nil, false
}

// loadPersisted finds id among Engine's persisted sessions and loads it in
// full. id alone doesn't say which cwd directory it lives under, so this
// scans snapshot metadata (cheap: header-only reads, see
// sessionpersistence.Store.ListSnapshots) rather than requiring every
// caller to already know the cwd sessionpersistence.Store.Load needs.
func (e *Engine) loadPersisted(id session.SessionId) (*session.Session, bool) {
	snaps, err := e.persistence.ListSnapshots()
	if err != nil {
		return nil, false
	}
	for _, snap := range snaps {
		if snap.ID != id {
			continue
		}
		s, err := e.persistence.LoadPath(snap.Path)
		if err != nil {
			return nil, false
		}
		return s, true
	}
	return nil, false
}

// allHeaders returns every session header this Engine can see: live
// sessions first, then persisted-but-not-live ones (header-only reads, no
// full log load) — the corpus ListSessions and descendantTree's lineage
// scan both walk. A live session's own header always wins over a
// persisted copy of the same id.
func (e *Engine) allHeaders() []session.Header {
	live := e.store.List()
	seen := make(map[session.SessionId]bool, len(live))
	out := make([]session.Header, 0, len(live))
	for _, s := range live {
		h := s.Header()
		out = append(out, h)
		seen[h.ID] = true
	}
	if e.persistence != nil {
		if headers, err := e.persistence.List(); err == nil {
			for _, h := range headers {
				if seen[h.ID] {
					continue
				}
				out = append(out, h)
				seen[h.ID] = true
			}
		}
	}
	return out
}

// ListSessions returns every session's metadata this Engine can see —
// live, plus persisted-but-not-live once WithPersistence is attached —
// newest-created first. A persisted-only entry's EventCount/LastEventTime
// are left at zero: computing them would mean fully loading every such
// session's log just to list it, defeating the point of a cheap listing
// call. Call ReadSession for a specific id to get the real counts.
func (e *Engine) ListSessions() []SessionRecord {
	headers := e.allHeaders()
	out := make([]SessionRecord, 0, len(headers))
	for _, h := range headers {
		if s, ok := e.store.Get(h.ID); ok {
			out = append(out, recordOf(s))
			continue
		}
		out = append(out, SessionRecord{Header: h})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Header.CreatedAt > out[j].Header.CreatedAt })
	return out
}

func recordOf(s *session.Session) SessionRecord {
	events := s.Events()
	rec := SessionRecord{Header: s.Header(), EventCount: len(events)}
	if len(events) > 0 {
		rec.LastEventTime = events[len(events)-1].Time
	}
	return rec
}

// ReadSession returns the session registered under id — live if Engine's
// Store has it, otherwise loaded from persistence if attached. Unlike the
// TS original's defensively-cloned read, a live result is the same live
// *session.Session the Store holds — this port has exactly one in-memory
// copy of any live session, and every other consumer in this codebase
// (e.g. web/handlers.go's ag.Session()) already works with that same live
// pointer rather than a clone. A persisted result is a freshly-loaded
// session no other code holds a reference to, so mutating it (e.g. via
// Append) has no effect on anything until it's Sync'd back.
func (e *Engine) ReadSession(id session.SessionId) (*session.Session, error) {
	if s, ok := e.getSession(id); ok {
		return s, nil
	}
	return nil, &Error{Code: CodeSessionNotFound, Message: fmt.Sprintf("no such session %q", id)}
}

// FilterSessions applies filter to the same corpus ListSessions returns.
func (e *Engine) FilterSessions(filter SessionResultFilter) []SessionRecord {
	all := e.ListSessions()
	out := make([]SessionRecord, 0, len(all))
	for _, rec := range all {
		if matchesSessionFilter(rec, filter) {
			out = append(out, rec)
		}
	}
	return out
}

func matchesSessionFilter(rec SessionRecord, f SessionResultFilter) bool {
	if f.IDs != nil {
		if !containsID(f.IDs, rec.Header.ID) {
			return false
		}
	}
	if f.CWD != nil && rec.Header.CWD != *f.CWD {
		return false
	}
	if f.CreatedAfter != nil && rec.Header.CreatedAt < *f.CreatedAfter {
		return false
	}
	if f.CreatedBefore != nil && rec.Header.CreatedAt > *f.CreatedBefore {
		return false
	}
	if f.ParentSession != nil && rec.Header.ParentSession != *f.ParentSession {
		return false
	}
	return true
}

func containsID(ids []session.SessionId, id session.SessionId) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ListEvents returns every event in id's log, in seq order, each
// classified against the session's current surface.
func (e *Engine) ListEvents(id session.SessionId) ([]ClassifiedEvent, error) {
	s, err := e.ReadSession(id)
	if err != nil {
		return nil, err
	}
	events := s.Events()
	current := seqSet(s.SurfaceSeqs())

	out := make([]ClassifiedEvent, len(events))
	for i, ev := range events {
		out[i] = ClassifiedEvent{Event: ev, Surface: classify(ev, current)}
	}
	return out, nil
}

func seqSet(seqs []int) map[int]bool {
	set := make(map[int]bool, len(seqs))
	for _, s := range seqs {
		set[s] = true
	}
	return set
}

func classify(ev session.Event, current map[int]bool) SurfaceClass {
	if !session.SurfaceEligible(ev.Type) {
		return SurfaceLogOnly
	}
	if current[ev.Seq] {
		return SurfaceCurrent
	}
	return SurfaceShadowed
}

// FilterEvents extracts id's log and applies filter, seq-ascending. Text is
// a literal, case-insensitive substring scan (see extractEventText) — not
// a full-text query.
func (e *Engine) FilterEvents(id session.SessionId, filter SessionEventResultFilter) ([]ClassifiedEvent, error) {
	all, err := e.ListEvents(id)
	if err != nil {
		return nil, err
	}
	out := make([]ClassifiedEvent, 0, len(all))
	for _, ce := range all {
		if matchesEventFilter(ce, filter) {
			out = append(out, ce)
		}
	}
	return out, nil
}

func matchesEventFilter(ce ClassifiedEvent, f SessionEventResultFilter) bool {
	ev := ce.Event
	if f.SeqAfter != nil && ev.Seq < *f.SeqAfter {
		return false
	}
	if f.SeqBefore != nil && ev.Seq > *f.SeqBefore {
		return false
	}
	if f.TimeAfter != nil && ev.Time < *f.TimeAfter {
		return false
	}
	if f.TimeBefore != nil && ev.Time > *f.TimeBefore {
		return false
	}
	if f.Types != nil && !containsString(f.Types, ev.Type) {
		return false
	}
	if f.Surface != nil && !containsSurface(f.Surface, ce.Surface) {
		return false
	}
	if f.Text != nil {
		text := extractEventText(ev)
		if !strings.Contains(strings.ToLower(text), strings.ToLower(*f.Text)) {
			return false
		}
	}
	return true
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func containsSurface(xs []SurfaceClass, x SurfaceClass) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// extractEventText is the first-party document projection FilterEvents'
// literal scan and the SQLite full-text index (core/sessionquery/sqlitesearch)
// both build on: an event's derived message content, flattened to plain
// text. Events with no derived message (chunks, boundaries, request
// headers) extract no text.
func extractEventText(ev session.Event) string {
	msg, ok := session.DeriveEventMessage(ev)
	if !ok {
		return ""
	}
	var b strings.Builder
	for i, block := range msg.Content {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch block.Kind {
		case session.BlockText, session.BlockReasoning, session.BlockToolResult:
			b.WriteString(block.Text)
		case session.BlockToolCall:
			b.WriteString(block.ToolName)
		}
	}
	return b.String()
}

// ReadSurface returns id's header and its current surface, folded into
// model-visible messages — a thin wrapper over Session.DeriveMessages,
// which core/session already maintains incrementally as events append.
func (e *Engine) ReadSurface(id session.SessionId) (session.Header, []session.Message, error) {
	s, err := e.ReadSession(id)
	if err != nil {
		return session.Header{}, nil, err
	}
	return s.Header(), s.DeriveMessages(), nil
}

// ReadEvent returns req's target event plus a bounded raw-log window
// around it, clamped to Engine's readWindowMax.
func (e *Engine) ReadEvent(req ReadEventRequest) (EventWindow, error) {
	s, err := e.ReadSession(req.SessionID)
	if err != nil {
		return EventWindow{}, err
	}
	events := s.Events()
	if req.Seq < 0 || req.Seq >= len(events) || events[req.Seq].Seq != req.Seq {
		return EventWindow{}, &Error{Code: CodeEventNotFound, Message: fmt.Sprintf("no event at seq %d in session %q", req.Seq, req.SessionID)}
	}
	before := clamp(req.Before, 0, e.readWindowMax)
	after := clamp(req.After, 0, e.readWindowMax)

	startIdx := req.Seq - before
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := req.Seq + after
	if endIdx >= len(events) {
		endIdx = len(events) - 1
	}

	return EventWindow{
		Header: s.Header(),
		Target: events[req.Seq],
		Before: append([]session.Event(nil), events[startIdx:req.Seq]...),
		After:  append([]session.Event(nil), events[req.Seq+1:endIdx+1]...),
	}, nil
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

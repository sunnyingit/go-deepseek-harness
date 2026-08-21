package tokenmeter

import "go-harness/core/session"

// Measurement is a detached snapshot of one session's heuristically-priced
// surface, plus a request envelope's structural overhead if one was
// supplied to Measure.
//
// This is a smaller shape than the TS original's TokenMeasurement: no
// Baseline (none/estimated/usage) and no SurfaceDeltaTokens, because both
// exist there only to support reusing a provider's own reported usage as a
// pricing anchor — this port's session log never records usage on an
// assistant/message event (see README.md), so every measurement is always
// a full heuristic reprice; there is no anchor to diff against, and the
// total can never go negative the way a signed delta could.
type Measurement struct {
	// LogRevision is the number of durable events this measurement replayed.
	LogRevision int `json:"logRevision"`
	// HeaderTokens is Header's structural overhead; 0 when Measure was
	// called with a nil Header.
	HeaderTokens int `json:"headerTokens"`
	// SurfaceTokens is the sum of every current surface node's price.
	SurfaceTokens int `json:"surfaceTokens"`
	// TotalTokens is HeaderTokens + SurfaceTokens.
	TotalTokens int `json:"totalTokens"`
	// Nodes is the current surface in positional head-to-tail order,
	// detached from any internal state.
	Nodes []SurfaceNode `json:"nodes"`
}

// Meter is the replay-aware token estimator. It holds no per-session state
// (unlike the TS original's WeakMap<Session, ReplayState> incremental
// fold — see README.md): Measure replays a session's full event log on
// every call, trading its O(surface)-per-call cost for a simpler,
// stateless, concurrency-free implementation appropriate for this port's
// in-memory, short-lived demo sessions.
type Meter struct{}

// New returns a Meter. It carries no configuration — like the TS original,
// the fixed heuristic has no settings.
func New() *Meter { return &Meter{} }

// EstimateMessage heuristically prices one model-visible message (instance
// face of the package-level EstimateMessage, matching the TS original's
// TokenMeter.estimateMessage over its module-level export).
func (m *Meter) EstimateMessage(msg session.Message) int { return EstimateMessage(msg) }

// Measure replays sess's complete durable log into a priced surface, prices
// header's structural overhead if provided, and returns one detached
// snapshot. Every surface-eligible event is folded through
// FoldSurfaceTokens in log order; a malformed surface (an unresolvable
// replace range) is log corruption and fails loud rather than skipping the
// event, matching FoldSurfaceTokens' own contract.
func (m *Meter) Measure(sess *session.Session, header *Header) (Measurement, error) {
	events := sess.Events()

	var nodes []SurfaceNode
	for _, ev := range events {
		if !session.SurfaceEligible(ev.Type) {
			continue
		}
		fold, err := FoldSurfaceTokens(nodes, ev)
		if err != nil {
			return Measurement{}, err
		}
		nodes = fold.Nodes
	}

	surfaceTokens := 0
	for _, n := range nodes {
		surfaceTokens += n.Tokens
	}
	headerTokens := EstimateHeader(header)

	return Measurement{
		LogRevision:   len(events),
		HeaderTokens:  headerTokens,
		SurfaceTokens: surfaceTokens,
		TotalTokens:   headerTokens + surfaceTokens,
		Nodes:         nodes,
	}, nil
}

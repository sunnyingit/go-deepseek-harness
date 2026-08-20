// Port of surface.ts: the ordered projection of message-producing events on
// top of the append-only log. The log is always the source of truth; the
// surface is what deriveMessages actually walks, so a `replace` can shadow
// earlier messages (compaction) without deleting their log records.
package session

import "fmt"

// DeriveEventMessage is the per-node projection rule: what LLM message (if
// any) one event produces. Session.DeriveMessages folds this over the
// surface; a caller reconstructing history from a log prefix folds the same
// rule to get the exact messages any request was built from.
func DeriveEventMessage(ev Event) (Message, bool) {
	switch ev.Type {
	case "user/message":
		msg, ok := ev.Data.(Message)
		return msg, ok
	case "assistant/message":
		wrapped, ok := ev.Data.(map[string]any)
		if !ok {
			return Message{}, false
		}
		msg, ok := wrapped["message"].(Message)
		// An empty-content assistant/message exists only to host a
		// max-tokens step's usage — it must not inject a content-less turn.
		if !ok || len(msg.Content) == 0 {
			return Message{}, false
		}
		return msg, true
	case "tool/result":
		wrapped, ok := ev.Data.(map[string]any)
		if !ok {
			return Message{}, false
		}
		msg, ok := wrapped["message"].(Message)
		return msg, ok
	default:
		// Non-surface event (boundary, chunk, log-only record): no message.
		return Message{}, false
	}
}

// surfaceFold is the mutable state a surface replay accumulates: the
// current ordered node seqs, and how many replacements have landed.
type surfaceFold struct {
	nodes             []int
	replaceGeneration int
}

type surfacePlanKind int

const (
	planNone    surfacePlanKind = iota // not a surface-eligible type at all
	planAppend                         // adds to the surface tail
	planReplace                        // shadows an existing contiguous range
)

// surfacePlan is one validated, not-yet-committed surface transition — the
// validate-then-commit split Session.Append relies on so a failed
// validation can never leave the surface (or the log) partially updated.
type surfacePlan struct {
	kind         surfacePlanKind
	seq          int
	start, end   int // only meaningful when kind == planReplace
	shadowedSeqs []int
}

// planSurfaceEvent validates one candidate event against the fold state
// accumulated so far and returns what committing it would do, without
// mutating state.
func planSurfaceEvent(state *surfaceFold, ev Event, log []Event) (surfacePlan, error) {
	if !SurfaceEligible(ev.Type) {
		if ev.SurfaceOp != nil {
			return surfacePlan{}, fmt.Errorf("session event %q is not surface-eligible and cannot carry a SurfaceOp", ev.Type)
		}
		if ev.SourceEventSeqs != nil {
			return surfacePlan{}, fmt.Errorf("session event %q is not surface-eligible and cannot carry SourceEventSeqs", ev.Type)
		}
		return surfacePlan{kind: planNone}, nil
	}
	if ev.SurfaceOp == nil {
		return surfacePlan{}, fmt.Errorf("session event %q is surface-eligible and requires a SurfaceOp", ev.Type)
	}

	if !ev.SurfaceOp.Replace {
		if err := assertProvenance(ev, nil); err != nil {
			return surfacePlan{}, err
		}
		return surfacePlan{kind: planAppend, seq: ev.Seq}, nil
	}

	op := *ev.SurfaceOp
	startIdx := indexOf(state.nodes, op.Start)
	if startIdx == -1 {
		return surfacePlan{}, fmt.Errorf("surface replace: start seq %d not found in surface", op.Start)
	}
	endIdx := indexOf(state.nodes, op.End)
	if endIdx == -1 {
		return surfacePlan{}, fmt.Errorf("surface replace: end seq %d not found in surface", op.End)
	}
	if startIdx > endIdx {
		return surfacePlan{}, fmt.Errorf("surface replace: start seq %d (index %d) is after end seq %d (index %d)", op.Start, startIdx, op.End, endIdx)
	}
	shadowed := append([]int(nil), state.nodes[startIdx:endIdx+1]...)
	if err := assertProvenance(ev, shadowed); err != nil {
		return surfacePlan{}, err
	}
	if err := assertToolResultRewrite(ev, shadowed, log); err != nil {
		return surfacePlan{}, err
	}
	return surfacePlan{kind: planReplace, seq: ev.Seq, start: op.Start, end: op.End, shadowedSeqs: shadowed}, nil
}

// commitSurfacePlan applies an already-validated plan to the fold state.
func commitSurfacePlan(state *surfaceFold, plan surfacePlan) {
	switch plan.kind {
	case planAppend:
		state.nodes = append(state.nodes, plan.seq)
	case planReplace:
		startIdx := indexOf(state.nodes, plan.start)
		endIdx := indexOf(state.nodes, plan.end)
		next := make([]int, 0, len(state.nodes)-(endIdx-startIdx))
		next = append(next, state.nodes[:startIdx]...)
		next = append(next, plan.seq)
		next = append(next, state.nodes[endIdx+1:]...)
		state.nodes = next
		state.replaceGeneration++
	}
}

func indexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// assertProvenance validates SourceEventSeqs: earlier-only, no duplicates,
// non-empty unless this is an assistant/message (which may cite a known
// empty provider stream), and — on a replace — every shadowed seq must be
// cited.
func assertProvenance(ev Event, shadowedSeqs []int) error {
	if ev.SourceEventSeqs != nil {
		if len(ev.SourceEventSeqs) == 0 && ev.Type != "assistant/message" {
			return fmt.Errorf("SourceEventSeqs must not be empty except on assistant/message")
		}
		seen := map[int]bool{}
		for _, s := range ev.SourceEventSeqs {
			if s >= ev.Seq {
				return fmt.Errorf("SourceEventSeqs must reference earlier events: %d >= current seq %d", s, ev.Seq)
			}
			if seen[s] {
				return fmt.Errorf("SourceEventSeqs must not contain duplicates")
			}
			seen[s] = true
		}
		for _, shadow := range shadowedSeqs {
			if !seen[shadow] {
				return fmt.Errorf("surface replace: SourceEventSeqs must include every shadowed surface node; missing %d", shadow)
			}
		}
		return nil
	}
	if len(shadowedSeqs) > 0 {
		return fmt.Errorf("surface replace: SourceEventSeqs must include every shadowed surface node; missing %v", shadowedSeqs)
	}
	return nil
}

// assertToolResultRewrite restricts a tool/result replacement to changing
// only its result content: exactly one shadowed node, and every other field
// of the message must stay identical. This is what makes a content-pruning
// compaction producer safe — it cannot rewrite which call a result answers,
// its error state, or anything else about the exchange.
func assertToolResultRewrite(ev Event, shadowedSeqs []int, log []Event) error {
	if ev.Type != "tool/result" {
		return nil
	}
	if len(shadowedSeqs) != 1 {
		return fmt.Errorf("tool/result surface replacement must rewrite exactly one current node")
	}
	original := log[shadowedSeqs[0]]
	if original.Type != "tool/result" {
		return fmt.Errorf("tool/result surface replacement must target a current tool/result")
	}
	origWrapped, _ := original.Data.(map[string]any)
	newWrapped, _ := ev.Data.(map[string]any)
	origMsg, _ := origWrapped["message"].(Message)
	newMsg, _ := newWrapped["message"].(Message)
	if origMsg.Role != newMsg.Role || len(origMsg.Content) != len(newMsg.Content) {
		return fmt.Errorf("tool/result surface replacement may change only content")
	}
	for i := range origMsg.Content {
		if !contentBlocksEqualExceptText(origMsg.Content[i], newMsg.Content[i]) {
			return fmt.Errorf("tool/result surface replacement may change only content")
		}
	}
	return nil
}

// contentBlocksEqualExceptText reports whether a and b are identical in
// every field except Text — the one field a tool/result content rewrite may
// change. ContentBlock isn't comparable with == (its Args map field makes
// the whole struct non-comparable in Go), so this compares field by field.
func contentBlocksEqualExceptText(a, b ContentBlock) bool {
	if a.Kind != b.Kind || a.CallID != b.CallID || a.ToolName != b.ToolName {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for k, v := range a.Args {
		if bv, ok := b.Args[k]; !ok || fmt.Sprint(v) != fmt.Sprint(bv) {
			return false
		}
	}
	return true
}

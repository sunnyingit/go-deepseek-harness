// Package tokenmeter is a Go port of packages/llm/token-meter from
// DeepSeek Harness: fixed-heuristic token pricing for a session's surface
// and a request envelope's structural overhead. What this port leaves out
// relative to the TS original — provider-usage-anchor reuse, durable
// request-header replay, session projections, the invariant companion, the
// Cordis service singleton — is documented in README.md.
package tokenmeter

import (
	"encoding/json"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

// charsPerToken is the fixed text-density estimate used until exact
// tokenization is needed.
const charsPerToken = 4

// blockOverhead is per-block structural overhead for JSON framing and type
// tags.
const blockOverhead = 4

// RoleOverhead is role-field framing overhead added to every priced message.
const RoleOverhead = 4

func ceilDiv(n, d int) int { return (n + d - 1) / d }

// Header is the token-priced subset of a request envelope — just the parts
// EstimateHeader actually prices. It intentionally excludes the
// model/provider/temperature routing fields the TS original's EpochHeader
// carries: nothing in this port durably logs a per-request envelope to
// replay one back (core/agentloop builds GenerateRequest inline and never
// appends it as a session event), so there is nothing to reconstruct a
// fuller Header from — see README.md.
type Header struct {
	System string
	Tools  []agentloop.ToolSchema
}

// EstimateContent prices content blocks recursively under the fixed
// density heuristic.
//
// tool-result differs from the TS original by construction, not by
// omission: this port's session.ContentBlock stores a tool result's
// content as flat Text (see core/session's DeriveEventMessage and every
// tool's own renderStringResult), not TS's nested content-block array — so
// there is no nested structure here to recurse into, and it prices the
// same way BlockText does.
func EstimateContent(blocks []session.ContentBlock) int {
	tokens := 0
	for _, b := range blocks {
		switch b.Kind {
		case session.BlockText, session.BlockReasoning, session.BlockToolResult:
			tokens += ceilDiv(len(b.Text), charsPerToken) + blockOverhead
		case session.BlockToolCall:
			argsJSON, _ := json.Marshal(b.Args)
			tokens += ceilDiv(len(b.ToolName), charsPerToken) + ceilDiv(len(argsJSON), charsPerToken) + blockOverhead
		default:
			// BlockKind is a plain string type, not a closed enum — an
			// unrecognized value still retains a conservative structural
			// JSON price under the fixed heuristic, matching the TS
			// original's ContentBlockMap default case.
			raw, _ := json.Marshal(b)
			tokens += blockOverhead + ceilDiv(len(raw), charsPerToken)
		}
	}
	return tokens
}

// EstimateMessage heuristically prices one model-visible message: its
// content plus role-framing overhead.
func EstimateMessage(msg session.Message) int {
	return EstimateContent(msg.Content) + RoleOverhead
}

// EstimateSystemTokens prices the system-prompt part of a request envelope;
// 0 when absent.
func EstimateSystemTokens(system string) int {
	if system == "" {
		return 0
	}
	return ceilDiv(len(system), charsPerToken) + RoleOverhead
}

// EstimateToolsTokens prices the tool-schema part of a request envelope; 0
// when absent or empty.
func EstimateToolsTokens(tools []agentloop.ToolSchema) int {
	if len(tools) == 0 {
		return 0
	}
	raw, _ := json.Marshal(tools)
	return ceilDiv(len(raw), charsPerToken) + blockOverhead
}

// EstimateHeader prices the complete non-surface request envelope; 0 when
// header is nil.
func EstimateHeader(header *Header) int {
	if header == nil {
		return 0
	}
	return EstimateSystemTokens(header.System) + EstimateToolsTokens(header.Tools)
}

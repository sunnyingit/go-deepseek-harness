// Port of translate.ts: assemble DeepSeek SSE payloads into agentloop
// StreamChunks. Unlike the TS original (which streams a live delta per
// content/reasoning/tool-call index for UI replay), this port accumulates
// each block internally and emits exactly one StreamChunk per finished
// block on [DONE] — agentloop.step() treats each received chunk.Delta as
// one already-complete content block, so finer-grained deltas would be
// wasted precision this port doesn't consume.
package deepseek

import (
	"encoding/json"
	"fmt"
	"strings"

	"go-harness/core/agentloop"
)

type openBlock struct {
	kind     agentloop.BlockKind
	text     strings.Builder
	callID   string
	toolName string
}

// blockAssembler is one stream's worth of state: a single text block, a
// single reasoning block, and one tool-call block per streamed index —
// mirroring translate.ts's textBlock/reasoningBlock/toolBlocks split — plus
// the arrival order needed to emit them as StreamChunks in the same order
// the model produced them.
type blockAssembler struct {
	order        []*openBlock
	text         *openBlock
	reasoning    *openBlock
	tools        map[int]*openBlock
	finishReason string
}

func newBlockAssembler() *blockAssembler {
	return &blockAssembler{tools: map[int]*openBlock{}}
}

// consume folds one wire chunk's choices into the accumulator. It never
// itself produces StreamChunks — see finish.
func (b *blockAssembler) consume(wc wireChunk) {
	for _, choice := range wc.Choices {
		if choice.FinishReason != "" {
			b.finishReason = choice.FinishReason
		}
		d := choice.Delta

		// Reasoning first: thinking mode interleaves it before text. The
		// empty-string first chunk (DeepSeek's documented quirk) must not
		// open a block — the length check below already excludes it.
		if len(d.ReasoningContent) > 0 {
			if b.reasoning == nil {
				b.reasoning = &openBlock{kind: agentloop.BlockReasoning}
				b.order = append(b.order, b.reasoning)
			}
			b.reasoning.text.WriteString(d.ReasoningContent)
		}

		if len(d.Content) > 0 {
			if b.text == nil {
				b.text = &openBlock{kind: agentloop.BlockText}
				b.order = append(b.order, b.text)
			}
			b.text.text.WriteString(d.Content)
		}

		for _, call := range d.ToolCalls {
			blk, ok := b.tools[call.Index]
			if !ok {
				blk = &openBlock{kind: agentloop.BlockToolCall}
				b.tools[call.Index] = blk
				b.order = append(b.order, blk)
			}
			if call.ID != "" {
				blk.callID = call.ID
			}
			if call.Function != nil {
				if call.Function.Name != "" {
					blk.toolName = call.Function.Name
				}
				blk.text.WriteString(call.Function.Arguments)
			}
		}
	}
}

// finish closes the stream at [DONE] and returns the finished blocks as
// StreamChunks, in the order they first opened. A "length" finish marks the
// last chunk StreamChunk.MaxTokens; an unrecognized finish reason
// (content_filter, insufficient_system_resource, …) or a "stop"/"tool_calls"
// finish with nothing ever opened becomes an error instead of a chunk list,
// mirroring translate.ts's degenerate-completion and unknown-reason handling.
func (b *blockAssembler) finish() ([]agentloop.StreamChunk, error) {
	reason := b.finishReason
	if reason == "" {
		reason = "stop"
	}
	switch reason {
	case "stop", "tool_calls":
		if len(b.order) == 0 {
			return nil, &Error{Code: CodeEmptyResponse, Message: "model returned a completed response with no content"}
		}
		return b.chunks(false), nil
	case "length":
		return b.chunks(true), nil
	default:
		return nil, &Error{Code: CodeModelStopped, Message: fmt.Sprintf("model stopped: %s", reason)}
	}
}

func (b *blockAssembler) chunks(maxTokens bool) []agentloop.StreamChunk {
	out := make([]agentloop.StreamChunk, len(b.order))
	for i, blk := range b.order {
		var cb agentloop.ContentBlock
		if blk.kind == agentloop.BlockToolCall {
			cb = agentloop.ContentBlock{
				Kind:     blk.kind,
				CallID:   blk.callID,
				ToolName: blk.toolName,
				Args:     parseArguments(blk.text.String()),
			}
		} else {
			cb = agentloop.ContentBlock{Kind: blk.kind, Text: blk.text.String()}
		}
		out[i] = agentloop.StreamChunk{Delta: cb}
	}
	if maxTokens && len(out) > 0 {
		out[len(out)-1].MaxTokens = true
	}
	return out
}

// parseArguments mirrors tool-calls.ts's parseArguments: preserve invalid
// JSON rather than failing the whole step over one malformed tool call.
// agentloop.ContentBlock.Args is typed as a map, so the fallback wraps the
// raw text under "_raw" instead of dropping it.
func parseArguments(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err == nil {
		return m
	}
	return map[string]any{"_raw": raw}
}

// mapUsage converts wire usage into disjoint input/cache-read token counts;
// DeepSeek's prompt_tokens INCLUDES cache hits, so this subtracts them out.
// Exported for callers that want usage without threading it through
// agentloop.StreamChunk, which has no usage field.
func mapUsage(u wireUsage) (inputTokens, outputTokens, cacheReadTokens, reasoningTokens int) {
	cacheRead := u.PromptTokensDetails.CachedTokens
	if cacheRead == 0 {
		cacheRead = u.PromptCacheHitTokens
	}
	return u.PromptTokens - cacheRead, u.CompletionTokens, cacheRead, u.CompletionTokensDetails.ReasoningTokens
}

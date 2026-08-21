// Port of translate.ts: fold DeepSeek SSE payloads into agentloop
// StreamChunks. Text and reasoning are emitted live (one delta per wire
// chunk) so a UI can paint tokens as they arrive. Tool-call arguments are
// still assembled internally and emitted once at [DONE] — partial JSON is
// not a valid tool call. agentloop.step() coalesces consecutive text/
// reasoning deltas into one block on the durable assistant/message, so the
// live granularity is a streaming concern, not a transcript shape change.
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

// consume folds one wire chunk's choices into the accumulator and returns
// live text/reasoning deltas for this payload (empty when the chunk only
// carried tool-call fragments or a finish reason). Tool calls stay buffered
// until finish — their arguments arrive as JSON fragments.
func (b *blockAssembler) consume(wc wireChunk) []agentloop.StreamChunk {
	var live []agentloop.StreamChunk
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
			live = append(live, agentloop.StreamChunk{Delta: agentloop.ContentBlock{
				Kind: agentloop.BlockReasoning, Text: d.ReasoningContent,
			}})
		}

		if len(d.Content) > 0 {
			if b.text == nil {
				b.text = &openBlock{kind: agentloop.BlockText}
				b.order = append(b.order, b.text)
			}
			b.text.text.WriteString(d.Content)
			live = append(live, agentloop.StreamChunk{Delta: agentloop.ContentBlock{
				Kind: agentloop.BlockText, Text: d.Content,
			}})
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
	return live
}

// finish closes the stream at [DONE]. Text and reasoning already went out
// live from consume; this returns only completed tool-call blocks (and a
// MaxTokens marker on a length finish). An unrecognized finish reason
// (content_filter, …) or a "stop"/"tool_calls" finish with nothing ever
// opened becomes an error instead of a chunk list.
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
		return b.toolChunks(false), nil
	case "length":
		return b.toolChunks(true), nil
	default:
		return nil, &Error{Code: CodeModelStopped, Message: fmt.Sprintf("model stopped: %s", reason)}
	}
}

func (b *blockAssembler) toolChunks(maxTokens bool) []agentloop.StreamChunk {
	var out []agentloop.StreamChunk
	for _, blk := range b.order {
		if blk.kind != agentloop.BlockToolCall {
			continue
		}
		out = append(out, agentloop.StreamChunk{Delta: agentloop.ContentBlock{
			Kind:     blk.kind,
			CallID:   blk.callID,
			ToolName: blk.toolName,
			Args:     parseArguments(blk.text.String()),
		}})
	}
	if maxTokens {
		if len(out) > 0 {
			out[len(out)-1].MaxTokens = true
		} else {
			out = append(out, agentloop.StreamChunk{MaxTokens: true})
		}
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

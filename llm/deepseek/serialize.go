// Port of serialize.ts: agentloop.GenerateRequest -> the DeepSeek wire
// request. User text is flattened; assistant text becomes content, tool
// calls become tool_calls, and tool results become separate role:"tool"
// messages. Reasoning is replayed as reasoning_content only on assistant
// turns that also carried tool calls, per DeepSeek's thinking-mode passback
// rule — dropped elsewhere to avoid paying for tokens the API ignores anyway.
package deepseek

import (
	"encoding/json"
	"fmt"
	"strings"

	"go-harness/core/agentloop"
)

func serialize(req agentloop.GenerateRequest, cfg Config) (*wireRequest, error) {
	var messages []wireMessage
	if req.System != "" {
		messages = append(messages, wireMessage{Role: "system", Content: req.System})
	}
	converted, err := serializeMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	messages = append(messages, converted...)

	var tools []wireTool
	for _, t := range req.Tools {
		tools = append(tools, wireTool{
			Type:     "function",
			Function: wireToolFuncSpec{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	wr := &wireRequest{
		Model:         req.Model,
		Messages:      messages,
		Stream:        true,
		StreamOptions: streamOptions{IncludeUsage: true},
		Tools:         tools,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		MaxTokens:     req.MaxTokens,
		Stop:          req.Stop,
	}

	thinking, effort, err := resolveThinking(req.ReasoningEffort, cfg.Thinking)
	if err != nil {
		return nil, err
	}
	if thinking != "" {
		wr.Thinking = &wireThinking{Type: thinking}
	}
	if effort != "" {
		wr.ReasoningEffort = effort
	}
	return wr, nil
}

// resolveThinking maps the adapter-neutral ReasoningEffort ("", "off",
// "low", "high", "max") plus a deployment lock ("", "disabled") onto the
// wire thinking/reasoning_effort pair. "off" is adapter-owned and never
// crosses the wire as reasoning_effort — it maps to thinking:disabled with
// no effort field, matching DeepSeek's documented vocabulary.
func resolveThinking(requested, deploymentDefault string) (thinking, effort string, err error) {
	switch requested {
	case "":
		if deploymentDefault == "disabled" {
			return "disabled", "", nil
		}
		return "", "", nil // omit both; the provider's own default applies
	case "off":
		return "disabled", "", nil
	case "low", "high", "max":
		if deploymentDefault == "disabled" {
			return "", "", &Error{Code: CodeUnsupportedReasoning, Message: fmt.Sprintf("deployment does not support reasoning effort %q", requested)}
		}
		return "enabled", requested, nil
	default:
		return "", "", &Error{Code: CodeUnsupportedReasoning, Message: fmt.Sprintf("DeepSeek does not support reasoning effort %q", requested)}
	}
}

func flattenText(blocks []agentloop.ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Kind == agentloop.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// serializeAssistant serializes one assistant message: text + reasoning +
// tool calls. Content is always sent, even "" — DeepSeek rejects an
// assistant message with neither content nor tool_calls.
func serializeAssistant(m agentloop.Message) wireMessage {
	text := flattenText(m.Content)
	var reasoning strings.Builder
	var calls []wireToolCall
	for _, b := range m.Content {
		switch b.Kind {
		case agentloop.BlockReasoning:
			reasoning.WriteString(b.Text)
		case agentloop.BlockToolCall:
			args, _ := json.Marshal(b.Args)
			calls = append(calls, wireToolCall{
				ID:       b.CallID,
				Type:     "function",
				Function: wireToolFunction{Name: b.ToolName, Arguments: string(args)},
			})
		}
	}
	wm := wireMessage{Role: "assistant", Content: text}
	if len(calls) > 0 {
		wm.ToolCalls = calls
		// Official passback rule: reasoning_content is REQUIRED on assistant
		// turns that carried tool calls in thinking mode, and ignored (so
		// dropped, to save tokens) on tool-call-free turns.
		if reasoning.Len() > 0 {
			wm.ReasoningContent = reasoning.String()
		}
	}
	return wm
}

// serializeMessages serializes the conversation. tool-result blocks become
// standalone {role: "tool"} messages. DeepSeek (and the OpenAI tool-call
// contract) require those to follow the assistant tool_calls immediately —
// a later user retry must not land between them. Results are therefore
// flushed right after the assistant turn that issued the call, even if the
// harness log has a user message in between; leftover results (no matching
// assistant call) still emit in place.
func serializeMessages(messages []agentloop.Message) ([]wireMessage, error) {
	resultsByID := collectToolResults(messages)
	emitted := map[string]bool{}
	var wire []wireMessage
	for _, m := range messages {
		switch m.Role {
		case agentloop.RoleAssistant:
			wire = append(wire, serializeAssistant(m))
			for _, b := range m.Content {
				if b.Kind != agentloop.BlockToolCall || b.CallID == "" {
					continue
				}
				content, ok := resultsByID[b.CallID]
				if !ok {
					content = fmt.Sprintf("Interrupted before this tool call's outcome was recorded (call: %s).", b.ToolName)
				}
				if content == "" {
					content = "(no output)"
				}
				wire = append(wire, wireMessage{Role: "tool", ToolCallID: b.CallID, Content: content})
				emitted[b.CallID] = true
			}
		case agentloop.RoleUser:
			var leftover []agentloop.ContentBlock
			hasToolResult := false
			for _, b := range m.Content {
				if b.Kind != agentloop.BlockToolResult {
					continue
				}
				hasToolResult = true
				if !emitted[b.CallID] {
					leftover = append(leftover, b)
				}
			}
			text := flattenText(m.Content)
			if text != "" || !hasToolResult {
				wire = append(wire, wireMessage{Role: "user", Content: text})
			}
			for _, r := range leftover {
				content := r.Text
				if content == "" {
					content = "(no output)" // empty tool output still needs SOME content on the wire
				}
				wire = append(wire, wireMessage{Role: "tool", ToolCallID: r.CallID, Content: content})
				emitted[r.CallID] = true
			}
		default:
			return nil, fmt.Errorf("deepseek: unsupported message role %q", m.Role)
		}
	}
	return wire, nil
}

func collectToolResults(messages []agentloop.Message) map[string]string {
	out := map[string]string{}
	for _, m := range messages {
		if m.Role != agentloop.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Kind == agentloop.BlockToolResult && b.CallID != "" {
				out[b.CallID] = b.Text
			}
		}
	}
	return out
}

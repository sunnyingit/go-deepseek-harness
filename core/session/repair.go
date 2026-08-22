package session

import "fmt"

// CloseUnansweredToolCalls appends a synthetic tool/result for every
// tool-call that has no matching result — both `tool/call` events and
// tool-call blocks inside `assistant/message`. An assistant turn that
// recorded tool_calls without results (max-tokens cut-off before execute,
// or a crash between persist and dispatch) would otherwise replay as a
// malformed request: chat-completion APIs require a tool message for
// every tool_call_id before any later user text.
func CloseUnansweredToolCalls(s *Session) int {
	events := s.Events()

	answered := map[string]bool{}
	for _, ev := range events {
		if ev.Type != "tool/result" {
			continue
		}
		for _, block := range messageBlocks(ev) {
			if block.Kind == BlockToolResult && block.CallID != "" {
				answered[block.CallID] = true
			}
		}
	}

	type pending struct {
		call       ContentBlock
		turn, step int
	}
	var open []pending
	seen := map[string]bool{}
	for _, ev := range events {
		turn, step := eventTurnStep(ev)
		switch ev.Type {
		case "tool/call":
			wrapped, ok := ev.Data.(map[string]any)
			if !ok {
				continue
			}
			call, ok := wrapped["call"].(ContentBlock)
			if !ok || call.CallID == "" || answered[call.CallID] || seen[call.CallID] {
				continue
			}
			seen[call.CallID] = true
			open = append(open, pending{call: call, turn: turn, step: step})
		case "assistant/message":
			for _, block := range messageBlocks(ev) {
				if block.Kind != BlockToolCall || block.CallID == "" || answered[block.CallID] || seen[block.CallID] {
					continue
				}
				seen[block.CallID] = true
				open = append(open, pending{call: block, turn: turn, step: step})
			}
		}
	}

	for _, p := range open {
		s.Append("tool/result", map[string]any{
			"turn": p.turn, "step": p.step,
			"message": Message{Role: RoleUser, Content: []ContentBlock{{
				Kind:   BlockToolResult,
				CallID: p.call.CallID,
				Text: fmt.Sprintf(
					"Interrupted before this tool call's outcome was recorded (call: %s). "+
						"Its side effects, if any, are unverified — check before retrying, "+
						"and only retry work that is read-only or safely idempotent.",
					p.call.ToolName),
			}}},
			"recovered": true,
		}, WithSurfaceOp(AppendOp()))
	}
	return len(open)
}

func messageBlocks(ev Event) []ContentBlock {
	wrapped, ok := ev.Data.(map[string]any)
	if !ok {
		return nil
	}
	msg, ok := wrapped["message"].(Message)
	if !ok {
		return nil
	}
	return msg.Content
}

func eventTurnStep(ev Event) (turn, step int) {
	wrapped, ok := ev.Data.(map[string]any)
	if !ok {
		return 0, 0
	}
	turn, _ = wrapped["turn"].(int)
	step, _ = wrapped["step"].(int)
	return turn, step
}

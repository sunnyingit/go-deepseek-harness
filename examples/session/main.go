// Demo: core/session's two capabilities core/agentloop's old ad-hoc session
// never had — fork and surface compaction — plus the open-turn guard that
// makes fork safe.
//
// No LLM involved: this package is pure log/projection plumbing, so the demo
// builds a session log by hand, the same shape core/agentloop would produce.
//
//	go run .
package main

import (
	"fmt"
	"strings"

	"go-harness/core/session"
)

func main() {
	store := session.NewStore()
	root, _ := store.Create("root", session.Header{CWD: "/work/trip-planner"})

	root.Append("turn/start", 1)
	root.Append("user/message", userMsg("What's the weather in Tokyo and Paris?"),
		session.WithSurfaceOp(session.AppendOp()))

	tokyoCall := toolCallBlock("c1", "get_weather", map[string]any{"city": "Tokyo"})
	root.Append("assistant/message", wrapAssistant(tokyoCall), session.WithSurfaceOp(session.AppendOp()))
	tokyoResult := root.Append("tool/result", wrapToolResult(toolResultBlock("c1", tokyoWeather)),
		session.WithSurfaceOp(session.AppendOp()))

	parisCall := toolCallBlock("c2", "get_weather", map[string]any{"city": "Paris"})
	root.Append("assistant/message", wrapAssistant(parisCall), session.WithSurfaceOp(session.AppendOp()))
	parisResult := root.Append("tool/result", wrapToolResult(toolResultBlock("c2", parisWeather)),
		session.WithSurfaceOp(session.AppendOp()))

	fmt.Println("=== 1. A fork attempted before turn/end is rejected ===")
	if _, err := store.Fork(root, -1, "premature"); err != nil {
		fmt.Println("Fork error (expected):", err)
	}
	fmt.Println()

	root.Append("assistant/message", wrapAssistant(textBlock("Tokyo: sunny, 24C. Paris: light rain, 16C.")),
		session.WithSurfaceOp(session.AppendOp()))
	root.Append("turn/end", map[string]any{"turn": 1, "reason": "completed"})

	fmt.Println("=== 2. Turn 1 transcript, before compaction ===")
	printTranscript(root)
	fmt.Printf("(%d surface messages, %d log events)\n\n", len(root.DeriveMessages()), len(root.Events()))

	fmt.Println("=== 3. Compacting both tool results in place ===")
	root.Append("tool/result", wrapToolResult(toolResultBlock("c1", "[pruned: see original at seq "+fmt.Sprint(tokyoResult.Seq)+"]")),
		session.WithSurfaceOp(session.ReplaceOp(tokyoResult.Seq, tokyoResult.Seq)),
		session.WithSourceEventSeqs(tokyoResult.Seq))
	root.Append("tool/result", wrapToolResult(toolResultBlock("c2", "[pruned: see original at seq "+fmt.Sprint(parisResult.Seq)+"]")),
		session.WithSurfaceOp(session.ReplaceOp(parisResult.Seq, parisResult.Seq)),
		session.WithSourceEventSeqs(parisResult.Seq))
	printTranscript(root)
	fmt.Printf("(%d surface messages, %d log events — same message count, more log events: nothing was deleted)\n\n",
		len(root.DeriveMessages()), len(root.Events()))

	fmt.Println("=== 4. Forking root now that turn 1 is closed ===")
	child, err := store.Fork(root, -1, "child")
	if err != nil {
		fmt.Println("Fork error:", err)
		return
	}
	h := child.Header()
	fmt.Printf("child %q: parent=%q seedLength=%d\n", child.ID(), h.ParentSession, h.SeedLength)
	fmt.Printf("child inherited the COMPACTED view: %d surface messages, matching root\n\n", len(child.DeriveMessages()))

	fmt.Println("=== 5. Root and child now diverge independently ===")
	child.Append("turn/start", 2)
	child.Append("user/message", userMsg("What about Berlin?"), session.WithSurfaceOp(session.AppendOp()))
	child.Append("assistant/message", wrapAssistant(textBlock("Berlin: overcast, 18C.")), session.WithSurfaceOp(session.AppendOp()))
	child.Append("turn/end", map[string]any{"turn": 2, "reason": "completed"})

	fmt.Printf("root:  %d messages, last turn %d\n", len(root.DeriveMessages()), root.LastTurn())
	fmt.Printf("child: %d messages, last turn %d\n", len(child.DeriveMessages()), child.LastTurn())
}

// ---- helpers mirroring what core/agentloop's turn()/step() would log -----

func userMsg(text string) session.Message {
	return session.Message{Role: session.RoleUser, Content: []session.ContentBlock{textBlock(text)}}
}

func textBlock(text string) session.ContentBlock {
	return session.ContentBlock{Kind: session.BlockText, Text: text}
}

func toolCallBlock(callID, name string, args map[string]any) session.ContentBlock {
	return session.ContentBlock{Kind: session.BlockToolCall, CallID: callID, ToolName: name, Args: args}
}

func toolResultBlock(callID, text string) session.ContentBlock {
	return session.ContentBlock{Kind: session.BlockToolResult, CallID: callID, Text: text}
}

func wrapAssistant(blocks ...session.ContentBlock) map[string]any {
	return map[string]any{"message": session.Message{Role: session.RoleAssistant, Content: blocks}}
}

func wrapToolResult(block session.ContentBlock) map[string]any {
	return map[string]any{"message": session.Message{Role: session.RoleUser, Content: []session.ContentBlock{block}}}
}

const tokyoWeather = "Tokyo forecast: sunny throughout the day, high 24C / low 17C, " +
	"5% chance of rain, wind 8km/h from the southeast, humidity 55%, UV index 6."
const parisWeather = "Paris forecast: light rain from mid-morning, high 16C / low 11C, " +
	"70% chance of rain, wind 14km/h from the west, humidity 82%, UV index 2."

func printTranscript(s *session.Session) {
	for _, m := range s.DeriveMessages() {
		for _, b := range m.Content {
			switch b.Kind {
			case session.BlockText:
				fmt.Printf("  [%s] %s\n", m.Role, b.Text)
			case session.BlockToolCall:
				fmt.Printf("  [%s] call %s(%v)\n", m.Role, b.ToolName, b.Args)
			case session.BlockToolResult:
				fmt.Printf("  [%s] result: %s\n", m.Role, truncate(b.Text, 70))
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "..."
}

// Demo: DeepSeek + the ReAct turn/step loop.
//
// Asks the model an arithmetic question it can't safely answer from memory,
// gives it a "calculate" tool, and prints the resulting Thought -> Action ->
// Observation trace as the agent works through it.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"go-harness/core/agentloop"
	"go-harness/llm/deepseek"
)

// calculatorTool is the one capability the model gets. Giving it something
// it must reach for (rather than answer directly) is what makes the ReAct
// loop actually run more than one step.
type calculatorTool struct{}

func (calculatorTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}

func (calculatorTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name:        "calculate",
		Description: "Evaluate a single arithmetic operation between two numbers.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a":  map[string]any{"type": "number", "description": "left operand"},
				"op": map[string]any{"type": "string", "enum": []string{"+", "-", "*", "/"}},
				"b":  map[string]any{"type": "number", "description": "right operand"},
			},
			"required": []string{"a", "op", "b"},
		},
	}}
}

func (calculatorTool) Execute(_ context.Context, _ *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	errResult := func(msg string) agentloop.ToolResult {
		return agentloop.ToolResult{Content: agentloop.ContentBlock{
			Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: "Error: " + msg,
		}}
	}
	a, aOK := call.Args["a"].(float64)
	b, bOK := call.Args["b"].(float64)
	op, opOK := call.Args["op"].(string)
	if !aOK || !bOK || !opOK {
		return errResult(fmt.Sprintf("bad arguments %v", call.Args))
	}

	var result float64
	switch op {
	case "+":
		result = a + b
	case "-":
		result = a - b
	case "*":
		result = a * b
	case "/":
		if b == 0 {
			return errResult("division by zero")
		}
		result = a / b
	default:
		return errResult(fmt.Sprintf("unknown operator %q", op))
	}
	return agentloop.ToolResult{Content: agentloop.ContentBlock{
		Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: strconv.FormatFloat(result, 'g', -1, 64),
	}}
}

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set DEEPSEEK_API_KEY to run this demo")
		os.Exit(1)
	}

	adapter, err := deepseek.New(deepseek.Config{
		APIKey:  apiKey,
		BaseURL: os.Getenv("DEEPSEEK_BASE_URL"), // optional; deepseek.DefaultBaseURL when empty
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "deepseek adapter:", err)
		os.Exit(1)
	}

	session := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(
		session,
		adapter,
		calculatorTool{},
		"You are a terse assistant. Always use the calculate tool for arithmetic instead of computing it yourself.",
		agentloop.Hooks{},
	).WithModel("deepseek-chat")

	question := "What is 123 times 456, and then add 789 to that result?"
	fmt.Println("User:", question)
	agent.Followup(agentloop.Message{
		Role:    agentloop.RoleUser,
		Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}},
	})
	agent.WhenIdle()

	printTranscript(session)
}

// printTranscript walks the session log and renders the ReAct trace: every
// step's Thought (assistant text/reasoning), Action (tool call), and
// Observation (tool result), grouped by turn.
func printTranscript(session agentloop.Session) {
	fmt.Println()
	for _, ev := range session.Events() {
		switch ev.Type {
		case "turn/start":
			fmt.Printf("=== Turn %d ===\n", ev.Data.(int))
		case "step/start":
			data := ev.Data.(map[string]any)
			fmt.Printf("--- Step %d ---\n", data["step"].(int))
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, block := range msg.Content {
				switch block.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", block.Text)
				case agentloop.BlockToolCall:
					fmt.Printf("Action:  %s(%s)\n", block.ToolName, formatArgs(block.Args))
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", msg.Content[0].Text)
		case "turn/end":
			fmt.Printf("=== Turn ended: %s ===\n\n", reasonString(ev.Data.(map[string]any)["reason"]))
		}
	}
}

func formatArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, args[k])
	}
	return strings.Join(parts, ", ")
}

func reasonString(reason any) string {
	if s, ok := reason.(agentloop.StepEndReason); ok {
		return string(s)
	}
	return fmt.Sprint(reason)
}

// Demo: plan mode with a real DeepSeek model, planning a trip.
//
// The model researches a destination with a few simulated travel tools
// (weather, budget, attractions), then proposes a day-by-day itinerary
// through exit_plan_mode. You review it for real on the terminal — reject
// with feedback and it revises, approve and it wraps up.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"go-harness/core/agentloop"
	"go-harness/llm/deepseek"
	"go-harness/plan/planmode"
)

// ---- simulated travel tools -------------------------------------------
//
// None of this is real travel data — each tool is a small deterministic
// lookup so the demo needs no travel APIs of its own. The point is giving a
// *real* model something to actually call before it's allowed to plan, not
// the data's accuracy.

type weatherTool struct{}

func (weatherTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeParallel
}

func (weatherTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name:        "check_weather",
		Description: "Look up typical weather for a destination in a given month.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"destination": map[string]any{"type": "string"},
				"month":       map[string]any{"type": "string", "description": `e.g. "April"`},
			},
			"required": []string{"destination", "month"},
		},
	}}
}

func (weatherTool) Execute(_ context.Context, _ *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	destination, _ := call.Args["destination"].(string)
	month, _ := call.Args["month"].(string)
	profiles := map[string]string{
		"japan":    "mild, 12-19C, occasional rain - pack a light jacket and a compact umbrella",
		"tokyo":    "mild, 12-19C, occasional rain - pack a light jacket and a compact umbrella",
		"iceland":  "cold and windy, 2-8C, frequent rain/sleet - pack thermal layers and a waterproof shell",
		"thailand": "hot and humid, 28-34C, brief afternoon showers - pack light breathable clothing",
	}
	text := fmt.Sprintf("%s in %s: temperate, 15-22C, low chance of rain (default estimate - no local profile for this destination)", destination, month)
	for name, profile := range profiles {
		if strings.Contains(strings.ToLower(destination), name) {
			text = fmt.Sprintf("%s in %s: %s", destination, month, profile)
			break
		}
	}
	return agentloop.ToolResult{Content: agentloop.ContentBlock{Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: text}}
}

type budgetTool struct{}

func (budgetTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeParallel
}

func (budgetTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name:        "estimate_budget",
		Description: "Rough per-day cost estimate in USD for a destination at a given comfort level.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"destination": map[string]any{"type": "string"},
				"style":       map[string]any{"type": "string", "enum": []string{"budget", "mid-range", "luxury"}},
			},
			"required": []string{"destination", "style"},
		},
	}}
}

func (budgetTool) Execute(_ context.Context, _ *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	style, _ := call.Args["style"].(string)
	perDay, ok := map[string]int{"budget": 80, "mid-range": 180, "luxury": 400}[style]
	if !ok {
		perDay = 150
	}
	text := fmt.Sprintf("estimated %s cost: about $%d/day per person (lodging + food + local transit, excluding flights)", style, perDay)
	return agentloop.ToolResult{Content: agentloop.ContentBlock{Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: text}}
}

type attractionsTool struct{}

func (attractionsTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeParallel
}

func (attractionsTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name:        "search_attractions",
		Description: "List a few notable attractions for a destination.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"destination": map[string]any{"type": "string"},
			},
			"required": []string{"destination"},
		},
	}}
}

func (attractionsTool) Execute(_ context.Context, _ *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	destination, _ := call.Args["destination"].(string)
	catalog := map[string][]string{
		"japan":   {"Fushimi Inari Shrine (Kyoto)", "Shibuya Crossing (Tokyo)", "Arashiyama Bamboo Grove (Kyoto)", "Mount Fuji viewpoint (Hakone)", "Dotonbori food street (Osaka)"},
		"tokyo":   {"Shibuya Crossing", "Senso-ji Temple", "teamLab Planets", "Tsukiji Outer Market"},
		"iceland": {"Blue Lagoon", "Golden Circle route", "Jokulsarlon glacier lagoon", "Reykjavik old town"},
	}
	text := "no catalog entry for this destination - use general knowledge"
	for name, list := range catalog {
		if strings.Contains(strings.ToLower(destination), name) {
			text = strings.Join(list, "; ")
			break
		}
	}
	return agentloop.ToolResult{Content: agentloop.ContentBlock{Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: text}}
}

// ---- human review ---------------------------------------------------------

// interactiveReview reads from a *bufio.Reader the caller owns and reuses
// across calls — a fresh reader per call would silently discard whatever it
// had already buffered past the first line.
func interactiveReview(stdin *bufio.Reader) planmode.ReviewFunc {
	return func(_ context.Context, plan string) (approved bool, feedback string) {
		fmt.Println()
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println(" Itinerary review")
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println(plan)
		fmt.Println(strings.Repeat("─", 60))
		fmt.Print("Approve this itinerary? [y/N], or type feedback to request changes: ")

		line, err := stdin.ReadString('\n')
		if err != nil {
			// No more input to read. Looping back to ask again would just
			// spin forever re-asking a question nothing can ever answer.
			fmt.Println()
			fmt.Println("(no more input on stdin - stopping)")
			os.Exit(0)
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "y", "yes":
			return true, ""
		case "", "n", "no":
			return false, "not quite right, please reconsider"
		default:
			return false, line
		}
	}
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

	controller, err := planmode.New(
		"You are in planning mode. Research the destination using check_weather, " +
			"estimate_budget, and search_attractions before presenting a complete " +
			"day-by-day itinerary through exit_plan_mode. Ground the plan in what " +
			"those tools returned instead of guessing.",
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "planmode.New:", err)
		os.Exit(1)
	}

	tools := planmode.NewMultiTool().
		Add(weatherTool{}).
		Add(budgetTool{}).
		Add(attractionsTool{}).
		Add(&planmode.ExitTool{
			Controller: controller,
			Review:     interactiveReview(bufio.NewReader(os.Stdin)),
		})

	session := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(
		session, adapter, tools,
		"You are a meticulous travel planning assistant.",
		agentloop.Hooks{PreStep: controller.PreStepHook()},
	).WithModel("deepseek-chat")

	request := "Plan me a 4-day trip to Japan in April. Mid-range budget. I like history and food."
	fmt.Println("User: /plan " + request)
	reply, steer := controller.Command(agent, request)
	fmt.Println("System:", reply)
	if steer != "" {
		agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: steer}}})
	}
	waitIdle(agent)

	printTranscript(session)

	active, pending := controller.Get(agent)
	fmt.Printf("\nFinal plan-mode state: active=%v pending=%v\n", active, pending != nil)
}

func waitIdle(agent *agentloop.Agent) {
	done := make(chan struct{})
	go func() { agent.WhenIdle(); close(done) }()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
		fmt.Fprintln(os.Stderr, "agent never went idle")
		os.Exit(1)
	}
}

// printTranscript renders the session log: Thought/Action/Observation per
// step, plus the plan/mode transitions, grouped by turn.
func printTranscript(session agentloop.Session) {
	fmt.Println()
	fmt.Println("=== Transcript ===")
	for _, ev := range session.Events() {
		switch ev.Type {
		case "turn/start":
			fmt.Printf("--- Turn %d ---\n", ev.Data.(int))
		case "plan/mode":
			fmt.Printf("[plan mode -> %v]\n", ev.Data.(map[string]any)["active"])
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, b := range msg.Content {
				switch b.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", b.Text)
				case agentloop.BlockToolCall:
					fmt.Printf("Action:  %s(%s)\n", b.ToolName, formatArgs(b.Args))
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", msg.Content[0].Text)
		case "turn/end":
			fmt.Printf("=== Turn ended: %v ===\n", ev.Data.(map[string]any)["reason"])
		}
	}
}

// formatArgs renders tool-call arguments for the one-line Action entry.
// A long value (the exit_plan_mode "plan" argument especially — full text
// already appears in the review prompt and the transcript's later
// Observation) is truncated so one call doesn't blow the transcript into a
// multi-line wall of text.
func formatArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%s", k, truncate(fmt.Sprint(args[k]), 60))
	}
	return strings.Join(parts, ", ")
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

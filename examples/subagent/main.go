// Demo: core/agent's Registry and initiator scope, with a real DeepSeek
// model on both sides of a delegation.
//
// A coordinator agent gets a question that needs deep technical detail; its
// one tool, delegate_to_specialist, creates a specialist child agent through
// the registry (owned by the coordinator), asks it the technical
// sub-question, and folds the answer back into the coordinator's own reply.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"os"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/llm/deepseek"
)

// delegateTool is the coordinator's one capability: hand a focused technical
// question to a fresh specialist agent and get back its answer. Creating
// the specialist through registry (rather than a bare agentloop.NewAgent
// call) is what makes it show up in Roots()/List()/IsOwnedBy() afterward.
type delegateTool struct {
	registry *coreagent.Registry
	seq      int
}

func (t *delegateTool) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}

func (t *delegateTool) Schemas() []agentloop.ToolSchema {
	return []agentloop.ToolSchema{{
		Name:        "delegate_to_specialist",
		Description: "Delegate a focused technical sub-question to a specialist agent and get back their detailed answer.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic":    map[string]any{"type": "string", "description": "short label for the sub-task"},
				"question": map[string]any{"type": "string", "description": "the exact question to ask the specialist"},
			},
			"required": []string{"topic", "question"},
		},
	}}
}

func (t *delegateTool) Execute(ctx context.Context, root *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	fail := func(msg string) agentloop.ToolResult {
		return agentloop.ToolResult{Content: agentloop.ContentBlock{Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: "Error: " + msg}}
	}

	topic, _ := call.Args["topic"].(string)
	question, _ := call.Args["question"].(string)
	if topic == "" || question == "" {
		return fail("topic and question are required")
	}

	// agentloop's own driver context starts fresh from context.Background()
	// (Followup/Steer/Inject don't even take a ctx to thread one through),
	// so the ctx reaching Execute never carries an initiator this package
	// set upstream — `root`, an explicit parameter, is the reliable way
	// this tool knows its caller. WithInitiator here marks the scope for
	// this call's OWN nested work instead: the round trip below reads it
	// back purely within this function's local ctx, demonstrating the
	// mechanism without overclaiming an agentloop integration that doesn't
	// exist. See core/agent's README "no ctx integration" note.
	ctx = coreagent.WithInitiator(ctx, coreagent.Agent(root))
	if got, ok := coreagent.CurrentInitiator(ctx); ok && got == coreagent.Agent(root) {
		fmt.Println("  [initiator scope: this call correctly resolves back to the coordinator]")
	}

	t.seq++
	childID := fmt.Sprintf("specialist-%d", t.seq)
	handle, err := t.registry.Create(childID, "root")
	if err != nil {
		return fail("could not create specialist: " + err.Error())
	}
	child := handle.Agent

	fmt.Printf("  [delegating %q to %s]\n", topic, childID)
	child.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}}})
	child.WhenIdle()

	answer := lastAssistantText(child.Session())
	return agentloop.ToolResult{Content: agentloop.ContentBlock{
		Kind: agentloop.BlockToolResult, CallID: call.CallID,
		Text: fmt.Sprintf("[%s's answer] %s", childID, answer),
	}}
}

func lastAssistantText(session agentloop.Session) string {
	var text string
	for _, ev := range session.Events() {
		if ev.Type != "assistant/message" {
			continue
		}
		msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
		var chunk string
		for _, b := range msg.Content {
			if b.Kind == agentloop.BlockText {
				chunk += b.Text
			}
		}
		if chunk != "" {
			text = chunk
		}
	}
	return text
}

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set DEEPSEEK_API_KEY to run this demo")
		os.Exit(1)
	}
	baseURL := os.Getenv("DEEPSEEK_BASE_URL")

	newAdapter := func() (*deepseek.Adapter, error) {
		return deepseek.New(deepseek.Config{APIKey: apiKey, BaseURL: baseURL})
	}

	registry := coreagent.NewRegistry()
	// The factory builds plain, tool-less specialist agents — it's only
	// ever used for children the delegate tool creates on demand, never for
	// the root itself.
	registry.SetFactory(func(id string) (coreagent.Agent, error) {
		adapter, err := newAdapter()
		if err != nil {
			return nil, err
		}
		session := agentloop.NewMemorySession()
		specialist := agentloop.NewAgent(
			session, adapter, noTools{},
			"You are a specialist. Give a precise, technically detailed answer in 3-4 sentences.",
			agentloop.Hooks{},
		).WithModel("deepseek-chat")
		return specialist, nil
	})

	rootAdapter, err := newAdapter()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deepseek adapter:", err)
		os.Exit(1)
	}
	rootSession := agentloop.NewMemorySession()
	root := agentloop.NewAgent(
		rootSession, rootAdapter, &delegateTool{registry: registry},
		"You are a coordinator. For any question needing deep technical detail, call "+
			"delegate_to_specialist to get an expert answer, then write a short summary "+
			"for a non-technical stakeholder that incorporates it.",
		agentloop.Hooks{},
	).WithModel("deepseek-chat")
	if _, err := registry.Register("root", root, ""); err != nil {
		fmt.Fprintln(os.Stderr, "register root:", err)
		os.Exit(1)
	}

	question := "Explain how the Raft consensus algorithm elects a leader, then summarize it for a non-technical stakeholder."
	fmt.Println("User:", question)

	root.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}}})
	root.WhenIdle()

	fmt.Println()
	fmt.Println("=== Coordinator transcript ===")
	printTranscript(rootSession)

	fmt.Println("=== Registry state ===")
	fmt.Println("All agents:", registry.List())
	fmt.Println("Roots:     ", registry.Roots())
	for _, id := range registry.List() {
		if id == "root" {
			continue
		}
		fmt.Printf("IsOwnedBy(%q, \"root\") = %v\n", id, registry.IsOwnedBy(id, "root"))
	}
}

func printTranscript(session agentloop.Session) {
	for _, ev := range session.Events() {
		switch ev.Type {
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, b := range msg.Content {
				switch b.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", b.Text)
				case agentloop.BlockToolCall:
					fmt.Printf("Action:  %s\n", b.ToolName)
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", msg.Content[0].Text)
		}
	}
	fmt.Println()
}

// noTools is a ToolExecutor with nothing registered, for the specialist
// agents: they answer directly, no delegation of their own.
type noTools struct{}

func (noTools) ExecutionMode(agentloop.ContentBlock) agentloop.ExecutionMode {
	return agentloop.ModeExclusive
}
func (noTools) Execute(context.Context, *agentloop.Agent, agentloop.ContentBlock) agentloop.ToolResult {
	return agentloop.ToolResult{}
}
func (noTools) Schemas() []agentloop.ToolSchema { return nil }

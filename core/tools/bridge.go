package tools

import (
	"context"

	"go-harness/core/agentloop"
)

// Bridge adapts a Registry to satisfy agentloop.ToolExecutor, so an Agent
// built on core/agentloop can dispatch every tool call through this
// package's full pipeline (guards, pre/post-execute hooks, timeouts,
// finalizeContent) instead of a bare hand-rolled ToolExecutor.
type Bridge struct{ Registry *Registry }

var _ agentloop.ToolExecutor = Bridge{}

func (b Bridge) ExecutionMode(call agentloop.ContentBlock) agentloop.ExecutionMode {
	if b.Registry.ExecutionMode(ExecutionInput{Name: call.ToolName, Arguments: call.Args}) == ModeParallel {
		return agentloop.ModeParallel
	}
	return agentloop.ModeExclusive
}

func (b Bridge) Schemas() []agentloop.ToolSchema {
	schemas := b.Registry.Schemas()
	out := make([]agentloop.ToolSchema, len(schemas))
	for i, s := range schemas {
		out[i] = agentloop.ToolSchema{Name: s.Name, Description: s.Description, Parameters: s.Parameters}
	}
	return out
}

// Execute ignores agent (core/tools has no agent-scoping concept — see
// README.md) and reduces the pipeline's full ExecutionResult down to the
// single ContentBlock + ConcludesTurn flag agentloop.ToolResult carries: the
// first content block on success or failure alike, tagged with the call's
// CallID (a Registry-constructed error already carries it; a tool's own
// Render might not, since it never sees the call id).
func (b Bridge) Execute(ctx context.Context, _ *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	result := b.Registry.Execute(ctx, ExecutionInput{CallID: call.CallID, Name: call.ToolName, Arguments: call.Args})

	content := agentloop.ContentBlock{Kind: agentloop.BlockToolResult, CallID: call.CallID}
	if len(result.Content) > 0 {
		content = result.Content[0]
		content.CallID = call.CallID
	}
	return agentloop.ToolResult{Content: content, ConcludesTurn: result.ConcludesTurn}
}

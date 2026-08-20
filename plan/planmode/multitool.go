package planmode

import (
	"context"

	"go-harness/core/agentloop"
)

// MultiTool composes several ToolExecutors into one, dispatching by the tool
// name the model actually called. agentloop.NewAgent takes exactly one
// ToolExecutor, so combining ExitTool with any other tool (a calculator, a
// file reader, …) goes through this rather than a bespoke registry — not
// part of the TS package, but needed to use ExitTool for anything beyond a
// single-tool demo.
type MultiTool struct {
	tools   map[string]agentloop.ToolExecutor
	schemas []agentloop.ToolSchema
}

func NewMultiTool() *MultiTool {
	return &MultiTool{tools: map[string]agentloop.ToolExecutor{}}
}

// Add registers every schema exec.Schemas() declares under exec, so calls to
// any of those names route to it. Panics on a duplicate name — a
// programming error to catch at wiring time, not a runtime condition to
// recover from.
func (m *MultiTool) Add(exec agentloop.ToolExecutor) *MultiTool {
	for _, schema := range exec.Schemas() {
		if _, exists := m.tools[schema.Name]; exists {
			panic("planmode: duplicate tool name " + schema.Name)
		}
		m.tools[schema.Name] = exec
		m.schemas = append(m.schemas, schema)
	}
	return m
}

func (m *MultiTool) Schemas() []agentloop.ToolSchema { return m.schemas }

func (m *MultiTool) ExecutionMode(call agentloop.ContentBlock) agentloop.ExecutionMode {
	if exec, ok := m.tools[call.ToolName]; ok {
		return exec.ExecutionMode(call)
	}
	return agentloop.ModeExclusive // unknown name: fail fast and alone in Execute, not racing in a pool
}

func (m *MultiTool) Execute(ctx context.Context, agent *agentloop.Agent, call agentloop.ContentBlock) agentloop.ToolResult {
	exec, ok := m.tools[call.ToolName]
	if !ok {
		return agentloop.ToolResult{Content: agentloop.ContentBlock{
			Kind: agentloop.BlockToolResult, CallID: call.CallID, Text: "Error: unknown tool " + call.ToolName,
		}}
	}
	return exec.Execute(ctx, agent, call)
}

var _ agentloop.ToolExecutor = (*MultiTool)(nil)

// Port of tool-calls.ts, the tool-call scheduler. Exclusive calls form a
// one-at-a-time barrier; parallel calls share a bounded rolling pool.
// Results commit in model order regardless of dispatch order, and an abort
// mid-execution records synthetic results for calls that never claimed a
// pool slot, so every model-issued call keeps a result in the log.
package agentloop

import (
	"context"
	"fmt"
	"sync"

	"go-harness/core/session"
)

// ExecutionMode mirrors tool-calls.ts's classification: exclusive calls form
// a barrier (run alone, one at a time), parallel calls share a bounded pool.
type ExecutionMode string

const (
	ModeExclusive ExecutionMode = "exclusive"
	ModeParallel  ExecutionMode = "parallel"
)

// ToolResult is a tool call's outcome. ConcludesTurn mirrors
// ToolExecutionResult.concludesTurn: when true, the turn ends right after
// this step even though a tool was called, instead of feeding the result
// back for another model round.
type ToolResult struct {
	Content       ContentBlock
	ConcludesTurn bool
}

type ToolExecutor interface {
	ExecutionMode(call ContentBlock) ExecutionMode
	// Execute receives the calling Agent (mirroring the TS tool contract's
	// exec.agent) so a session-state tool (e.g. planmode.ExitTool) can fold
	// and extend durable state via agent.Session() without a bespoke seam.
	Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult
	// Schemas returns the model-facing tool declarations this executor
	// serves; step() puts them on every GenerateRequest.Tools.
	Schemas() []ToolSchema
}

// executeToolCalls is the port of tool-calls.ts's executeToolCalls: it walks
// the model-ordered call list, grouping consecutive parallel-mode calls into
// one bounded-pool batch and running exclusive-mode calls alone, one at a
// time. It returns true when any committed result set ConcludesTurn.
func (a *Agent) executeToolCalls(ctx context.Context, turn, stepNum int, calls []ContentBlock) bool {
	concluded := false
	next := 0
	for next < len(calls) {
		first := calls[next]
		group := []ContentBlock{first}
		if a.tools.ExecutionMode(first) == ModeParallel {
			for i := next + 1; i < len(calls) && a.tools.ExecutionMode(calls[i]) == ModeParallel; i++ {
				group = append(group, calls[i])
			}
		}

		groupConcluded, aborted, consumed := a.runGroup(ctx, turn, stepNum, group)
		concluded = concluded || groupConcluded
		next += consumed
		if aborted {
			for _, call := range calls[next:] {
				a.appendSkippedToolCall(turn, stepNum, call)
			}
			return concluded
		}
	}
	return concluded
}

// runGroup runs one exclusive barrier (a group of 1) or one parallel pool,
// bounded by maxParallelToolCalls concurrent dispatches. tool/call is logged
// the moment a call actually claims a pool slot; tool/result is committed in
// model order even though dispatches can settle out of order.
func (a *Agent) runGroup(ctx context.Context, turn, stepNum int, group []ContentBlock) (concluded, aborted bool, started int) {
	type outcome struct {
		result ToolResult
		ready  bool
	}
	slots := make([]outcome, len(group))
	sem := make(chan struct{}, max(1, a.maxParallelToolCalls))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, call := range group {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{} // blocks here once the pool is full, so a call is only logged as it truly starts
		a.session.Append("tool/call", map[string]any{"turn": turn, "step": stepNum, "call": call})
		started++
		wg.Add(1)
		go func(i int, call ContentBlock) {
			defer wg.Done()
			defer func() { <-sem }()
			result := a.safeExecute(ctx, a, call)
			mu.Lock()
			slots[i] = outcome{result: result, ready: true}
			mu.Unlock()
		}(i, call)
	}
	wg.Wait()

	committed := 0
	for committed < started {
		slot := slots[committed]
		if !slot.ready {
			break
		}
		a.session.Append("tool/result", map[string]any{
			"turn": turn, "step": stepNum,
			"message": Message{Role: RoleUser, Content: []ContentBlock{slot.result.Content}},
		}, session.WithSurfaceOp(session.AppendOp()))
		if slot.result.ConcludesTurn {
			concluded = true
		}
		committed++
	}
	return concluded, ctx.Err() != nil, started
}

// safeExecute runs Execute inside its own recover boundary. A panicking tool
// runs in a goroutine this method's caller spawned — a recover() anywhere
// else, even one frame up in the same call, is in a *different* goroutine
// and cannot catch it, so this exists specifically to turn a tool bug into
// an ordinary error result instead of taking down the whole process.
func (a *Agent) safeExecute(ctx context.Context, agent *Agent, call ContentBlock) (result ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			result = ToolResult{Content: ContentBlock{
				Kind:   BlockToolResult,
				CallID: call.CallID,
				Text:   fmt.Sprintf("Error: tool %q panicked: %v", call.ToolName, r),
			}}
		}
	}()
	return a.tools.Execute(ctx, agent, call)
}

// appendSkippedToolCall records the durable call/result pair for a
// model-issued call that never got a pool slot before the step aborted, so
// replaying the log still finds a result for every call the model made.
func (a *Agent) appendSkippedToolCall(turn, stepNum int, call ContentBlock) {
	a.session.Append("tool/call", map[string]any{"turn": turn, "step": stepNum, "call": call})
	a.session.Append("tool/result", map[string]any{
		"turn": turn, "step": stepNum,
		"message": Message{Role: RoleUser, Content: []ContentBlock{
			{Kind: BlockToolResult, CallID: call.CallID, Text: "Error: tool call aborted before dispatch"},
		}},
		"error": true,
	}, session.WithSurfaceOp(session.AppendOp()))
}

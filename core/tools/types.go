// Package tools is a Go port of packages/core/tools (dsh-tools) from
// DeepSeek Harness: a tool registry and execution pipeline. A tool
// registers a schema, an output declaration, and an Execute function; every
// call then runs through pre-execute policy, monotonic guards, dispatch
// (with cooperative timeout and panic containment), post-execute policy,
// and a definition-owned content finalizer, before a result observer is
// notified of the frozen outcome.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package tools

import (
	"context"

	"go-harness/core/session"
)

// Token is a Registry-assigned identity for one call, useful only for
// pointer-equality correlation — never for anything else. Go has no
// equivalent to TS's branded Symbol; a private pointer plays the same role.
type Token struct{ _ byte }

func newToken() *Token { return &Token{} }

// ExecutionInput is the caller-supplied description of one tool call.
type ExecutionInput struct {
	CallID    string
	Name      string
	Arguments map[string]any
}

// Execution is one pending call inside the registry pipeline: the caller's
// input plus the registry-assigned Token.
type Execution struct {
	ExecutionInput
	Token *Token
}

// RunContext is handed to a tool implementation once the registry has
// accepted an Execution. DeferContext attaches an extra message (e.g. one
// carrying an image) that rides alongside the eventual result without being
// part of it; ConcludeTurn mirrors core/agentloop's ToolResult.ConcludesTurn
// — both let a tool body reach outside its own return value.
type RunContext struct {
	Execution
	Ctx context.Context

	deferred      []session.Message
	concludesTurn bool
}

func (rc *RunContext) DeferContext(msg session.Message) { rc.deferred = append(rc.deferred, msg) }
func (rc *RunContext) ConcludeTurn()                    { rc.concludesTurn = true }

// Failure is a structured tool error: Message is what the model sees
// (rendered as "Error: <message>"); Info carries an optional machine
// -readable code (e.g. {"code": "ABORTED"}) for a caller that wants to
// branch on cancellation vs. an ordinary tool failure.
type Failure struct {
	Message string
	Info    map[string]any
}

// ExecutionResult is the discriminated pipeline outcome. Value is only
// meaningful when !IsError; Error is only set when IsError.
type ExecutionResult struct {
	IsError            bool
	Value              any
	Error              *Failure
	Content            []session.ContentBlock
	Meta               any
	AdditionalContexts []session.Message
	ConcludesTurn      bool
}

// PreDecision is tools/pre-execute's outcome: Deny materializes an error
// with Reason before the call ever dispatches; the zero value allows it.
//
// TS's third `ask` variant (approval-service integration) is dropped: this
// port has no approval service, and TS's own documented fallback for a
// missing one is immediate denial — so `ask` would add nothing here that
// Deny doesn't already cover directly.
type PreDecision struct {
	Deny   bool
	Reason string
}

// PostDecision is tools/post-execute's outcome. At most one of
// (Content-replacement via HasContent) or (Value-replacement via HasValue)
// may be set — never both, and Value only when the dispatched result was
// itself successful. Block converts the call into a failure carrying
// Feedback as the model-facing content. The zero value accepts the
// dispatched result unchanged.
type PostDecision struct {
	Block    bool
	Feedback []session.ContentBlock

	HasContent bool
	Content    []session.ContentBlock
	HasValue   bool
	Value      any

	AdditionalContexts []session.Message
}

// Guard is a monotonic synchronous check run after the pre-execute chain:
// a non-empty return denies the call. A guard can never un-deny a call an
// earlier guard, or pre-execute, already denied.
type Guard func(exec Execution) (reason string)

// PreExecuteHook and PostExecuteHook form waterfall chains, mirroring
// Cordis's `tools/pre-execute` / `tools/post-execute` events: each
// registered hook may decide outright or defer to next (the next hook, or
// the default decision after the last one).
type PreExecuteHook func(ctx context.Context, exec Execution, next func() PreDecision) PreDecision
type PostExecuteHook func(ctx context.Context, exec Execution, result ExecutionResult, next func() PostDecision) PostDecision

// ResultObserver is notified with the frozen final outcome of every call.
// An observer that panics is contained — mirroring TS's "unfailing"
// `tools/result` emit — so a broken listener can never change what Execute
// returns to its caller.
type ResultObserver func(exec Execution, result ExecutionResult)

// ExecutionMode mirrors core/agentloop's own vocabulary (exclusive vs.
// bounded-parallel dispatch) so Bridge can hand it straight to the loop.
type ExecutionMode string

const (
	ModeExclusive ExecutionMode = "exclusive"
	ModeParallel  ExecutionMode = "parallel"
)

// ToolSchema is the model-facing view of one registered tool: name,
// description, and the wire JSON Schema for its parameters.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

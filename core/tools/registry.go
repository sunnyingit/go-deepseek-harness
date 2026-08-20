package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go-harness/core/session"
)

// Output is a tool's mandatory canonical-result declaration: Schema
// describes the JSON value Execute returns, Render projects (args, value)
// into model-facing content, and PresentationMeta optionally attaches
// structured metadata alongside it.
type Output struct {
	Schema           ValueSchema
	Render           func(args map[string]any, value any) []session.ContentBlock
	PresentationMeta func(args map[string]any, value any) any
}

// ToolDefinition is one registered tool: schema, output declaration,
// execution, and the optional callbacks the TS original calls
// isConcurrencySafe / finalizeContent.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  ParameterSchema
	Output      Output

	// TimeoutMs, when positive, bounds one dispatch: the context passed to
	// Execute is cancelled after this many milliseconds. Zero means no
	// timeout. TS's timeoutMs is declarative only ("enforcement requires a
	// separate timeout-policy wrapper") because AbortSignal has no built-in
	// timer; Go's context.WithTimeout makes real enforcement nearly free, so
	// this port actually enforces it instead of leaving it as documentation.
	TimeoutMs int

	// IsConcurrencySafe classifies one call for ExecutionMode; nil (or a
	// function that doesn't return exactly true) means always exclusive —
	// fail-closed, matching the TS original.
	IsConcurrencySafe func(args map[string]any) bool

	Execute func(ctx context.Context, args map[string]any, run *RunContext) (any, error)

	// FinalizeContent runs exactly once for every normalized result
	// (including a bypassed unknown-tool/invalid-args failure) and may
	// replace only Content — never Value, Error, or any other field. A nil
	// return leaves the result's Content unchanged.
	FinalizeContent func(exec Execution, result ExecutionResult) []session.ContentBlock
}

// ArgsError reports every argument violation ValidateArgs found.
type ArgsError struct{ Violations []string }

func (e *ArgsError) Error() string {
	return "invalid arguments: " + strings.Join(e.Violations, "; ")
}

// Registry holds tool definitions and the pipeline hooks (guards,
// pre/post-execute, result observers) every Execute call runs through.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition

	nextID      int
	guards      []idEntry[Guard]
	preExecute  []idEntry[PreExecuteHook]
	postExecute []idEntry[PostExecuteHook]
	onResult    []idEntry[ResultObserver]
}

type idEntry[T any] struct {
	id int
	fn T
}

func NewRegistry() *Registry { return &Registry{tools: map[string]ToolDefinition{}} }

// Register adds def, rejecting an empty name, a missing output declaration,
// a missing Execute, a negative timeout, or a name already registered.
// The returned func unregisters it.
func (r *Registry) Register(def ToolDefinition) (func(), error) {
	if def.Name == "" {
		return nil, fmt.Errorf("tools: tool name must not be empty")
	}
	if def.Output.Render == nil {
		return nil, fmt.Errorf("tools: tool %q must declare Output.Render", def.Name)
	}
	if def.Execute == nil {
		return nil, fmt.Errorf("tools: tool %q must declare Execute", def.Name)
	}
	if def.TimeoutMs < 0 {
		return nil, fmt.Errorf("tools: tool %q timeout must not be negative, got %d", def.Name, def.TimeoutMs)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; exists {
		return nil, fmt.Errorf("tools: %q is already registered", def.Name)
	}
	r.tools[def.Name] = def
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.tools, def.Name)
	}, nil
}

func (r *Registry) Get(name string) (ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}

// Schemas returns every registered tool's model-facing schema, sorted by
// name for deterministic wire output (Go map iteration order is random;
// TS's insertion-ordered object doesn't need this, but this port does).
func (r *Registry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ToolSchema, 0, len(names))
	for _, name := range names {
		def := r.tools[name]
		out = append(out, ToolSchema{Name: def.Name, Description: def.Description, Parameters: def.Parameters.ToJSONSchema()})
	}
	return out
}

// ExecutionMode classifies a pending call: parallel only when the named
// tool declares IsConcurrencySafe and it returns exactly true for these
// arguments (a panic or any other return is treated as exclusive — fail
// closed, matching the TS original's try/catch).
func (r *Registry) ExecutionMode(input ExecutionInput) ExecutionMode {
	def, ok := r.Get(input.Name)
	if !ok || def.IsConcurrencySafe == nil {
		return ModeExclusive
	}
	if safeIsConcurrencySafe(def, input.Arguments) {
		return ModeParallel
	}
	return ModeExclusive
}

func safeIsConcurrencySafe(def ToolDefinition, args map[string]any) (safe bool) {
	defer func() {
		if recover() != nil {
			safe = false
		}
	}()
	return def.IsConcurrencySafe(args)
}

// Guard registers a monotonic denial check; the returned func unregisters it.
func (r *Registry) Guard(g Guard) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.guards = append(r.guards, idEntry[Guard]{id, g})
	return func() { r.removeGuard(id) }
}

func (r *Registry) removeGuard(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.guards = removeByID(r.guards, id)
}

// PreExecute registers a pre-execute waterfall hook; the returned func
// unregisters it. Hooks run in registration order, each free to defer to
// the next via the `next` callback it receives.
func (r *Registry) PreExecute(hook PreExecuteHook) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.preExecute = append(r.preExecute, idEntry[PreExecuteHook]{id, hook})
	return func() { r.removePreExecute(id) }
}

func (r *Registry) removePreExecute(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preExecute = removeByID(r.preExecute, id)
}

// PostExecute registers a post-execute waterfall hook; the returned func
// unregisters it.
func (r *Registry) PostExecute(hook PostExecuteHook) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.postExecute = append(r.postExecute, idEntry[PostExecuteHook]{id, hook})
	return func() { r.removePostExecute(id) }
}

func (r *Registry) removePostExecute(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.postExecute = removeByID(r.postExecute, id)
}

// OnResult registers a result observer; the returned func unregisters it.
func (r *Registry) OnResult(obs ResultObserver) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.onResult = append(r.onResult, idEntry[ResultObserver]{id, obs})
	return func() { r.removeOnResult(id) }
}

func (r *Registry) removeOnResult(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onResult = removeByID(r.onResult, id)
}

func removeByID[T any](entries []idEntry[T], id int) []idEntry[T] {
	out := entries[:0:0]
	for _, e := range entries {
		if e.id != id {
			out = append(out, e)
		}
	}
	return out
}

// Execute runs one call through the full pipeline: argument validation,
// pre-execute policy, guards, dispatch (with timeout and panic
// containment), post-execute policy, the definition's finalizeContent, and
// finally the result observers. Every branch returns a materialized
// ExecutionResult — Execute itself never returns an error.
//
// Two failures bypass pre-execute AND post-execute entirely (an unknown
// tool, or arguments that fail validation) since policy never got a chance
// to run over something the registry itself already rejects; every other
// outcome — a guard denial, a dispatched result, a pre-dispatch abort —
// still passes through post-execute, which can accept, replace, or block it.
func (r *Registry) Execute(ctx context.Context, input ExecutionInput) ExecutionResult {
	def, ok := r.Get(input.Name)
	if !ok {
		exec := Execution{ExecutionInput: input, Token: newToken()}
		return r.finalizeOnly(exec, unknownToolResult(input.CallID, input.Name), nil)
	}

	exec := Execution{ExecutionInput: input, Token: newToken()}
	if violations := ValidateArgs(def.Parameters, input.Arguments); len(violations) > 0 {
		return r.finalizeOnly(exec, argsErrorResult(input.CallID, violations), &def)
	}

	if ctx.Err() != nil {
		return r.finalizeOnly(exec, abortedBeforeDispatchResult(input.CallID), &def)
	}

	decision := r.runPreExecute(ctx, exec)
	denialReason := decision.Reason
	if !decision.Deny {
		denialReason = r.runGuards(exec)
	}

	var result ExecutionResult
	switch {
	case denialReason != "":
		result = denialResult(input.CallID, denialReason)
	case ctx.Err() != nil:
		result = abortedBeforeDispatchResult(input.CallID)
	default:
		result = r.dispatch(ctx, exec, def)
		if ctx.Err() != nil && !result.IsError {
			result = abortedResult(input.CallID, result)
		}
	}

	return r.finalizeWithPost(ctx, exec, result, def)
}

func (r *Registry) runPreExecute(ctx context.Context, exec Execution) PreDecision {
	r.mu.RLock()
	hooks := snapshot(r.preExecute)
	r.mu.RUnlock()

	var run func(i int) PreDecision
	run = func(i int) PreDecision {
		if i >= len(hooks) {
			return PreDecision{}
		}
		return hooks[i](ctx, exec, func() PreDecision { return run(i + 1) })
	}
	return run(0)
}

func (r *Registry) runGuards(exec Execution) string {
	r.mu.RLock()
	guards := snapshot(r.guards)
	r.mu.RUnlock()
	for _, g := range guards {
		if reason := g(exec); reason != "" {
			return reason
		}
	}
	return ""
}

// dispatch runs the tool body: a positive TimeoutMs bounds the context, and
// a panic inside Execute is recovered into an ordinary error result instead
// of crashing whatever goroutine called Execute.
func (r *Registry) dispatch(ctx context.Context, exec Execution, def ToolDefinition) ExecutionResult {
	dctx := ctx
	if def.TimeoutMs > 0 {
		var cancel context.CancelFunc
		dctx, cancel = context.WithTimeout(ctx, time.Duration(def.TimeoutMs)*time.Millisecond)
		defer cancel()
	}
	run := &RunContext{Execution: exec, Ctx: dctx}
	value, err := safeExecute(dctx, def, run)
	if err != nil {
		return errorResult(exec.CallID, err.Error())
	}
	return r.createSuccessResult(exec, def, run, value)
}

func safeExecute(ctx context.Context, def ToolDefinition, run *RunContext) (value any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("tool %q panicked: %v", def.Name, rec)
		}
	}()
	return def.Execute(ctx, run.Arguments, run)
}

func (r *Registry) createSuccessResult(exec Execution, def ToolDefinition, run *RunContext, value any) ExecutionResult {
	if violations := validateValue(def.Output.Schema, value, "output"); len(violations) > 0 {
		return errorResult(exec.CallID, fmt.Sprintf("tool %q returned an invalid output value: %s", def.Name, strings.Join(violations, "; ")))
	}
	content := def.Output.Render(exec.Arguments, value)
	var meta any
	if def.Output.PresentationMeta != nil {
		meta = def.Output.PresentationMeta(exec.Arguments, value)
	}
	return ExecutionResult{
		IsError:            false,
		Value:              value,
		Content:            content,
		Meta:               meta,
		AdditionalContexts: run.deferred,
		ConcludesTurn:      run.concludesTurn,
	}
}

// finalizeWithPost runs the post-execute chain over result, applies the
// same post-dispatch cancellation supersession Execute already applied
// around dispatch, then finalizes.
func (r *Registry) finalizeWithPost(ctx context.Context, exec Execution, result ExecutionResult, def ToolDefinition) ExecutionResult {
	postResult := r.runPostExecute(ctx, exec, result, def)
	if ctx.Err() != nil && !postResult.IsError {
		postResult = abortedResult(exec.CallID, postResult)
	}
	return r.finalizeOnly(exec, postResult, &def)
}

func (r *Registry) runPostExecute(ctx context.Context, exec Execution, result ExecutionResult, def ToolDefinition) ExecutionResult {
	r.mu.RLock()
	hooks := snapshot(r.postExecute)
	r.mu.RUnlock()

	var run func(i int) PostDecision
	run = func(i int) PostDecision {
		if i >= len(hooks) {
			return PostDecision{}
		}
		return hooks[i](ctx, exec, result, func() PostDecision { return run(i + 1) })
	}
	decision := run(0)

	if decision.Block {
		return ExecutionResult{
			IsError:            true,
			Error:              &Failure{Message: feedbackMessage(decision.Feedback)},
			Content:            decision.Feedback,
			AdditionalContexts: decision.AdditionalContexts,
		}
	}

	contexts := append(append([]session.Message(nil), result.AdditionalContexts...), decision.AdditionalContexts...)

	if decision.HasValue {
		if result.IsError {
			return errorResult(exec.CallID, "tools/post-execute cannot replace the value of a failed result")
		}
		replaced := r.createSuccessResult(exec, def, &RunContext{Execution: exec}, decision.Value)
		replaced.AdditionalContexts = contexts
		return replaced
	}

	out := result
	if decision.HasContent {
		out.Content = decision.Content
	}
	out.AdditionalContexts = contexts
	return out
}

func (r *Registry) finalizeOnly(exec Execution, result ExecutionResult, def *ToolDefinition) ExecutionResult {
	final := result
	if def != nil && def.FinalizeContent != nil {
		final = applyFinalizeContent(exec, final, *def)
	}
	r.notifyResult(exec, final)
	return final
}

func applyFinalizeContent(exec Execution, result ExecutionResult, def ToolDefinition) (final ExecutionResult) {
	final = result
	defer func() {
		if rec := recover(); rec != nil {
			final = errorResult(exec.CallID, fmt.Sprintf("finalizeContent for tool %q panicked: %v", exec.Name, rec))
		}
	}()
	if content := def.FinalizeContent(exec, result); content != nil {
		final = result
		final.Content = content
	}
	return final
}

func (r *Registry) notifyResult(exec Execution, result ExecutionResult) {
	r.mu.RLock()
	observers := snapshot(r.onResult)
	r.mu.RUnlock()
	for _, obs := range observers {
		notifyOne(obs, exec, result)
	}
}

// notifyOne isolates one observer's panic to itself — "unfailing"
// notification, matching the TS original: a broken listener can never
// change what Execute returns, and can't stop the remaining observers.
func notifyOne(obs ResultObserver, exec Execution, result ExecutionResult) {
	defer func() { recover() }()
	obs(exec, result)
}

func snapshot[T any](entries []idEntry[T]) []T {
	out := make([]T, len(entries))
	for i, e := range entries {
		out[i] = e.fn
	}
	return out
}

func feedbackMessage(content []session.ContentBlock) string {
	for _, b := range content {
		if b.Kind == session.BlockText && b.Text != "" {
			return b.Text
		}
	}
	return "tool call blocked"
}

func unknownToolResult(callID, name string) ExecutionResult {
	msg := fmt.Sprintf("unknown tool %q", name)
	return ExecutionResult{IsError: true, Error: &Failure{Message: msg, Info: map[string]any{"code": "UNKNOWN_TOOL"}}, Content: errorContent(callID, msg)}
}

func argsErrorResult(callID string, violations []string) ExecutionResult {
	msg := "invalid arguments: " + strings.Join(violations, "; ")
	return ExecutionResult{IsError: true, Error: &Failure{Message: msg, Info: map[string]any{"code": "INVALID_ARGS", "violations": violations}}, Content: errorContent(callID, msg)}
}

func denialResult(callID, reason string) ExecutionResult {
	return ExecutionResult{IsError: true, Error: &Failure{Message: reason}, Content: errorContent(callID, reason)}
}

func abortedBeforeDispatchResult(callID string) ExecutionResult {
	const msg = "tool call aborted before dispatch"
	return ExecutionResult{IsError: true, Error: &Failure{Message: msg, Info: map[string]any{"code": "ABORTED_BEFORE_DISPATCH"}}, Content: errorContent(callID, msg)}
}

func abortedResult(callID string, prior ExecutionResult) ExecutionResult {
	const msg = "tool call aborted"
	return ExecutionResult{
		IsError:            true,
		Error:              &Failure{Message: msg, Info: map[string]any{"code": "ABORTED"}},
		Content:            errorContent(callID, msg),
		AdditionalContexts: prior.AdditionalContexts,
	}
}

func errorResult(callID, msg string) ExecutionResult {
	return ExecutionResult{IsError: true, Error: &Failure{Message: msg}, Content: errorContent(callID, msg)}
}

func errorContent(callID, msg string) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, CallID: callID, Text: "Error: " + msg}}
}

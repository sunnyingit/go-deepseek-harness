package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-harness/core/session"
)

// echoDef is a minimal, always-succeeding tool: returns its "text" argument
// verbatim as both Value and rendered Content.
func echoDef() ToolDefinition {
	return ToolDefinition{
		Name:        "echo",
		Description: "echoes text back",
		Parameters: ParameterSchema{
			"text": {ValueSchema: ValueSchema{Kind: KindString}, Required: true},
		},
		Output: Output{
			Schema: ValueSchema{Kind: KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		Execute: func(_ context.Context, args map[string]any, _ *RunContext) (any, error) {
			return args["text"].(string), nil
		},
	}
}

func mustRegister(t *testing.T, r *Registry, def ToolDefinition) func() {
	t.Helper()
	dispose, err := r.Register(def)
	if err != nil {
		t.Fatalf("Register(%q): %v", def.Name, err)
	}
	return dispose
}

// ---- Register ---------------------------------------------------------

func TestRegisterRejectsEmptyName(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.Name = ""
	if _, err := r.Register(def); err == nil {
		t.Fatal("want an error for an empty name")
	}
}

func TestRegisterRejectsMissingRender(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.Output.Render = nil
	if _, err := r.Register(def); err == nil {
		t.Fatal("want an error for a missing Output.Render")
	}
}

func TestRegisterRejectsMissingExecute(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.Execute = nil
	if _, err := r.Register(def); err == nil {
		t.Fatal("want an error for a missing Execute")
	}
}

func TestRegisterRejectsNegativeTimeout(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.TimeoutMs = -1
	if _, err := r.Register(def); err == nil {
		t.Fatal("want an error for a negative timeout")
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	if _, err := r.Register(echoDef()); err == nil {
		t.Fatal("want an error registering the same name twice")
	}
}

func TestRegisterDisposeUnregisters(t *testing.T) {
	r := NewRegistry()
	dispose := mustRegister(t, r, echoDef())
	dispose()
	if _, ok := r.Get("echo"); ok {
		t.Fatal("tool should be gone after dispose")
	}
	// And the name is free again.
	mustRegister(t, r, echoDef())
}

// ---- Execute: happy path, unknown tool, invalid args -------------------

func TestExecuteHappyPath(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())

	result := r.Execute(context.Background(), ExecutionInput{CallID: "c1", Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.IsError {
		t.Fatalf("result = %+v, want success", result)
	}
	if result.Value != "hi" || result.Content[0].Text != "hi" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	r := NewRegistry()
	result := r.Execute(context.Background(), ExecutionInput{CallID: "c1", Name: "nope"})
	if !result.IsError || result.Error.Info["code"] != "UNKNOWN_TOOL" {
		t.Fatalf("result = %+v", result)
	}
	if result.Content[0].CallID != "c1" {
		t.Fatalf("content = %+v, want CallID threaded through", result.Content[0])
	}
}

func TestExecuteInvalidArgs(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	result := r.Execute(context.Background(), ExecutionInput{CallID: "c1", Name: "echo", Arguments: map[string]any{}})
	if !result.IsError || result.Error.Info["code"] != "INVALID_ARGS" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteUnknownToolAndInvalidArgsBypassPreExecuteAndGuards(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	preCalls, guardCalls := 0, 0
	r.PreExecute(func(_ context.Context, _ Execution, next func() PreDecision) PreDecision {
		preCalls++
		return next()
	})
	r.Guard(func(Execution) string {
		guardCalls++
		return ""
	})

	r.Execute(context.Background(), ExecutionInput{Name: "missing"})
	r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{}})
	if preCalls != 0 || guardCalls != 0 {
		t.Fatalf("preCalls=%d guardCalls=%d, want 0/0 — unknown tool and invalid args must bypass policy entirely", preCalls, guardCalls)
	}

	r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if preCalls != 1 || guardCalls != 1 {
		t.Fatalf("preCalls=%d guardCalls=%d, want 1/1 for a valid call", preCalls, guardCalls)
	}
}

// ---- Guards -------------------------------------------------------------

func TestGuardDenies(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.Guard(func(exec Execution) string {
		if exec.Name == "echo" {
			return "echo is disabled"
		}
		return ""
	})
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError || result.Error.Message != "echo is disabled" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGuardDoesNotRunAfterPreExecuteDeny(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.PreExecute(func(_ context.Context, _ Execution, _ func() PreDecision) PreDecision {
		return PreDecision{Deny: true, Reason: "denied by policy"}
	})
	guardCalled := false
	r.Guard(func(Execution) string {
		guardCalled = true
		return ""
	})

	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError || result.Error.Message != "denied by policy" {
		t.Fatalf("result = %+v", result)
	}
	if guardCalled {
		t.Fatal("a guard must not run once pre-execute has already denied — monotonic, can't un-deny")
	}
}

func TestGuardDenialStillReachesPostExecute(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.Guard(func(Execution) string { return "denied" })
	postSawDenial := false
	r.PostExecute(func(_ context.Context, _ Execution, result ExecutionResult, next func() PostDecision) PostDecision {
		if result.IsError && result.Error.Message == "denied" {
			postSawDenial = true
		}
		return next()
	})
	r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !postSawDenial {
		t.Fatal("a guard denial should still run through post-execute — only unknown-tool/invalid-args bypass it entirely")
	}
}

// ---- PreExecute waterfall -------------------------------------------------

func TestPreExecuteChainOrderAndShortCircuit(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	var order []string
	r.PreExecute(func(_ context.Context, _ Execution, next func() PreDecision) PreDecision {
		order = append(order, "first")
		return next()
	})
	r.PreExecute(func(_ context.Context, _ Execution, next func() PreDecision) PreDecision {
		order = append(order, "second")
		return PreDecision{Deny: true, Reason: "stop here"}
	})
	r.PreExecute(func(_ context.Context, _ Execution, next func() PreDecision) PreDecision {
		order = append(order, "third")
		return next()
	})

	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError || result.Error.Message != "stop here" {
		t.Fatalf("result = %+v", result)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v, want [first second] — the third hook must never run once the second denies", order)
	}
}

// ---- PostExecute ----------------------------------------------------------

func TestPostExecuteDefaultAcceptsUnchanged(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.IsError || result.Value != "hi" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPostExecuteBlockConvertsSuccessToFailure(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	feedback := []session.ContentBlock{{Kind: session.BlockText, Text: "not allowed to say that"}}
	r.PostExecute(func(_ context.Context, _ Execution, _ ExecutionResult, _ func() PostDecision) PostDecision {
		return PostDecision{Block: true, Feedback: feedback}
	})
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError || result.Error.Message != "not allowed to say that" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPostExecuteContentReplacement(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.PostExecute(func(_ context.Context, _ Execution, _ ExecutionResult, _ func() PostDecision) PostDecision {
		return PostDecision{HasContent: true, Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "[redacted]"}}}
	})
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.IsError || result.Content[0].Text != "[redacted]" || result.Value != "hi" {
		t.Fatalf("result = %+v, want content replaced but value untouched", result)
	}
}

func TestPostExecuteValueReplacementRerenders(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.PostExecute(func(_ context.Context, _ Execution, _ ExecutionResult, _ func() PostDecision) PostDecision {
		return PostDecision{HasValue: true, Value: "replaced"}
	})
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.IsError || result.Value != "replaced" || result.Content[0].Text != "replaced" {
		t.Fatalf("result = %+v, want the replacement value re-rendered", result)
	}
}

func TestPostExecuteValueReplacementOnFailureIsRejected(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	r.Guard(func(Execution) string { return "denied" })
	r.PostExecute(func(_ context.Context, _ Execution, _ ExecutionResult, _ func() PostDecision) PostDecision {
		return PostDecision{HasValue: true, Value: "should not work"}
	})
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError {
		t.Fatalf("result = %+v, want an error — post-execute cannot inject a value onto a failed result", result)
	}
}

// ---- FinalizeContent --------------------------------------------------

func TestFinalizeContentRunsOnEveryResultIncludingBypassed(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	calls := 0
	def.FinalizeContent = func(exec Execution, result ExecutionResult) []session.ContentBlock {
		calls++
		return []session.ContentBlock{{Kind: session.BlockToolResult, Text: "finalized: " + result.Content[0].Text}}
	}
	mustRegister(t, r, def)

	ok := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if ok.Content[0].Text != "finalized: hi" {
		t.Fatalf("ok.Content = %+v", ok.Content)
	}
	bad := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{}})
	if calls != 2 || bad.Content[0].Text[:11] != "finalized: " {
		t.Fatalf("calls=%d bad.Content=%+v, want finalizeContent applied even to the bypassed invalid-args result", calls, bad.Content)
	}
}

func TestFinalizeContentNilReturnLeavesContentUnchanged(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.FinalizeContent = func(Execution, ExecutionResult) []session.ContentBlock { return nil }
	mustRegister(t, r, def)
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.Content[0].Text != "hi" {
		t.Fatalf("content = %+v, want unchanged", result.Content)
	}
}

func TestFinalizeContentPanicBecomesErrorResult(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.FinalizeContent = func(Execution, ExecutionResult) []session.ContentBlock { panic("boom") }
	mustRegister(t, r, def)
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError {
		t.Fatalf("result = %+v, want a panicking finalizeContent contained as an error", result)
	}
}

// ---- Cancellation -----------------------------------------------------

func TestExecuteAbortedBeforeDispatch(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := r.Execute(ctx, ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError || result.Error.Info["code"] != "ABORTED_BEFORE_DISPATCH" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteAbortedDuringBodySupersedesSuccess(t *testing.T) {
	r := NewRegistry()
	started := make(chan struct{})
	def := ToolDefinition{
		Name: "slow",
		Output: Output{
			Schema: ValueSchema{Kind: KindString},
			Render: func(_ map[string]any, v any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: v.(string)}}
			},
		},
		Execute: func(ctx context.Context, _ map[string]any, _ *RunContext) (any, error) {
			close(started)
			<-ctx.Done() // observes cancellation, still returns a normal success
			return "done anyway", nil
		},
	}
	mustRegister(t, r, def)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ExecutionResult, 1)
	go func() { resultCh <- r.Execute(ctx, ExecutionInput{Name: "slow"}) }()
	<-started
	cancel()
	result := <-resultCh

	if !result.IsError || result.Error.Info["code"] != "ABORTED" {
		t.Fatalf("result = %+v, want a successful-but-cancelled body superseded by ABORTED", result)
	}
}

func TestExecuteToolOwnErrorSurvivesCancellation(t *testing.T) {
	r := NewRegistry()
	started := make(chan struct{})
	def := echoDef()
	def.Execute = func(ctx context.Context, _ map[string]any, _ *RunContext) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, errors.New("tool-specific failure")
	}
	mustRegister(t, r, def)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ExecutionResult, 1)
	go func() {
		resultCh <- r.Execute(ctx, ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	}()
	<-started
	cancel()
	result := <-resultCh
	if !result.IsError || result.Error.Message != "tool-specific failure" {
		t.Fatalf("result = %+v, want the tool's own error preserved, not overwritten by ABORTED", result)
	}
}

// ---- Timeout ------------------------------------------------------------

func TestTimeoutMsBoundsTheDispatchContext(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.TimeoutMs = 20
	def.Execute = func(ctx context.Context, _ map[string]any, _ *RunContext) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	mustRegister(t, r, def)

	start := time.Now()
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v, want the TimeoutMs deadline to fire quickly", elapsed)
	}
	if !result.IsError || result.Error.Message != context.DeadlineExceeded.Error() {
		t.Fatalf("result = %+v", result)
	}
}

// ---- Panic containment --------------------------------------------------

func TestExecutePanicInsideToolIsContained(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.Execute = func(context.Context, map[string]any, *RunContext) (any, error) {
		panic("bug inside the tool")
	}
	mustRegister(t, r, def)
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError {
		t.Fatalf("result = %+v, want the panic contained as an error", result)
	}
}

// ---- DeferContext / ConcludeTurn -----------------------------------------

func TestRunContextDeferContextAndConcludeTurn(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	extra := session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{Kind: session.BlockText, Text: "fyi"}}}
	def.Execute = func(_ context.Context, args map[string]any, run *RunContext) (any, error) {
		run.DeferContext(extra)
		run.ConcludeTurn()
		return args["text"].(string), nil
	}
	mustRegister(t, r, def)
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.ConcludesTurn {
		t.Fatal("want ConcludesTurn propagated from RunContext.ConcludeTurn()")
	}
	if len(result.AdditionalContexts) != 1 || result.AdditionalContexts[0].Content[0].Text != "fyi" {
		t.Fatalf("AdditionalContexts = %+v", result.AdditionalContexts)
	}
}

// ---- Output validation --------------------------------------------------

func TestExecuteRejectsInvalidOutputValue(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.Execute = func(context.Context, map[string]any, *RunContext) (any, error) {
		return 5.0, nil // declared output schema is string
	}
	mustRegister(t, r, def)
	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if !result.IsError {
		t.Fatalf("result = %+v, want an error — the tool violated its own output schema", result)
	}
}

// ---- ExecutionMode --------------------------------------------------------

func TestExecutionModeUnknownToolIsExclusive(t *testing.T) {
	r := NewRegistry()
	if r.ExecutionMode(ExecutionInput{Name: "nope"}) != ModeExclusive {
		t.Fatal("want exclusive for an unknown tool")
	}
}

func TestExecutionModeNoClassifierIsExclusive(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())
	if r.ExecutionMode(ExecutionInput{Name: "echo"}) != ModeExclusive {
		t.Fatal("want exclusive when IsConcurrencySafe is nil")
	}
}

func TestExecutionModeTrueIsParallel(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.IsConcurrencySafe = func(map[string]any) bool { return true }
	mustRegister(t, r, def)
	if r.ExecutionMode(ExecutionInput{Name: "echo"}) != ModeParallel {
		t.Fatal("want parallel when IsConcurrencySafe returns true")
	}
}

func TestExecutionModePanicIsExclusive(t *testing.T) {
	r := NewRegistry()
	def := echoDef()
	def.IsConcurrencySafe = func(map[string]any) bool { panic("bug") }
	mustRegister(t, r, def)
	if r.ExecutionMode(ExecutionInput{Name: "echo"}) != ModeExclusive {
		t.Fatal("want exclusive (fail-closed) when the classifier panics")
	}
}

// ---- Schemas --------------------------------------------------------------

func TestSchemasAreSortedByName(t *testing.T) {
	r := NewRegistry()
	zDef := echoDef()
	zDef.Name = "zzz"
	mustRegister(t, r, zDef)
	mustRegister(t, r, echoDef()) // "echo"
	schemas := r.Schemas()
	if len(schemas) != 2 || schemas[0].Name != "echo" || schemas[1].Name != "zzz" {
		t.Fatalf("schemas = %+v", schemas)
	}
}

// ---- OnResult -----------------------------------------------------------

func TestOnResultObserverSeesFinalOutcomeAndPanicIsContained(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, echoDef())

	var seen ExecutionResult
	r.OnResult(func(_ Execution, result ExecutionResult) { panic("broken observer") })
	r.OnResult(func(_ Execution, result ExecutionResult) { seen = result })

	result := r.Execute(context.Background(), ExecutionInput{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if result.IsError {
		t.Fatalf("result = %+v, a panicking observer must not affect Execute's return", result)
	}
	if seen.Value != "hi" {
		t.Fatalf("seen = %+v, want the second observer still notified after the first panicked", seen)
	}
}

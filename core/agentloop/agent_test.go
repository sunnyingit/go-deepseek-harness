package agentloop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-harness/core/session"
)

// ---- test doubles ---------------------------------------------------------

type scriptedLLM struct {
	mu        sync.Mutex
	responses [][]StreamChunk
	calls     int
	onCall    func(i int)
}

func (s *scriptedLLM) Stream(ctx context.Context, req GenerateRequest) (<-chan StreamChunk, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	s.mu.Unlock()
	if i >= len(s.responses) {
		return nil, fmt.Errorf("scriptedLLM: no response scripted for call %d", i)
	}
	if s.onCall != nil {
		s.onCall(i)
	}
	ch := make(chan StreamChunk, len(s.responses[i]))
	for _, c := range s.responses[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// blockingLLM signals `started` then hangs until either the context is
// cancelled or `block` is closed, simulating an in-flight model call.
type blockingLLM struct {
	started chan struct{}
	block   chan struct{}
}

func (b *blockingLLM) Stream(ctx context.Context, req GenerateRequest) (<-chan StreamChunk, error) {
	close(b.started)
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-b.block:
		ch := make(chan StreamChunk)
		close(ch)
		return ch, nil
	}
}

// panickingTool simulates a buggy tool implementation, to verify a panic
// inside a tool's own goroutine is contained instead of crashing the process.
type panickingTool struct{}

func (panickingTool) ExecutionMode(call ContentBlock) ExecutionMode { return ModeExclusive }
func (panickingTool) Schemas() []ToolSchema                         { return nil }

func (panickingTool) Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult {
	panic("bug inside a tool implementation")
}

// panickingLLM simulates a buggy adapter, to verify a panic directly in
// turn()'s own synchronous chain (not a spawned goroutine) is recovered too.
type panickingLLM struct{}

func (panickingLLM) Stream(ctx context.Context, req GenerateRequest) (<-chan StreamChunk, error) {
	panic("bug inside the LLM adapter")
}

type echoTool struct{}

func (echoTool) ExecutionMode(call ContentBlock) ExecutionMode { return ModeExclusive }
func (echoTool) Schemas() []ToolSchema                         { return nil }

func (echoTool) Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult {
	return ToolResult{Content: ContentBlock{Kind: BlockToolResult, CallID: call.CallID, Text: "ok:" + call.ToolName}}
}

// trackingParallelTool records the peak number of concurrently in-flight
// calls, to verify the bounded pool actually overlaps and respects its cap.
type trackingParallelTool struct {
	sleep       time.Duration
	current     int32
	maxObserved int32
}

func (t *trackingParallelTool) ExecutionMode(call ContentBlock) ExecutionMode { return ModeParallel }
func (t *trackingParallelTool) Schemas() []ToolSchema                         { return nil }

func (t *trackingParallelTool) Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult {
	n := atomic.AddInt32(&t.current, 1)
	for {
		m := atomic.LoadInt32(&t.maxObserved)
		if n <= m || atomic.CompareAndSwapInt32(&t.maxObserved, m, n) {
			break
		}
	}
	time.Sleep(t.sleep)
	atomic.AddInt32(&t.current, -1)
	return ToolResult{Content: ContentBlock{Kind: BlockToolResult, CallID: call.CallID, Text: "ok"}}
}

type concludingTool struct{}

func (concludingTool) ExecutionMode(call ContentBlock) ExecutionMode { return ModeExclusive }
func (concludingTool) Schemas() []ToolSchema                         { return nil }

func (concludingTool) Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult {
	return ToolResult{Content: ContentBlock{Kind: BlockToolResult, CallID: call.CallID, Text: "ok"}, ConcludesTurn: true}
}

// cancelingExclusiveTool cancels the owning agent from inside its first
// Execute call, simulating an abort that arrives mid tool-execution.
type cancelingExclusiveTool struct {
	onFirstCall func()
	executed    int
}

func (t *cancelingExclusiveTool) ExecutionMode(call ContentBlock) ExecutionMode { return ModeExclusive }
func (t *cancelingExclusiveTool) Schemas() []ToolSchema                         { return nil }

func (t *cancelingExclusiveTool) Execute(ctx context.Context, agent *Agent, call ContentBlock) ToolResult {
	t.executed++
	if t.executed == 1 && t.onFirstCall != nil {
		t.onFirstCall()
	}
	return ToolResult{Content: ContentBlock{Kind: BlockToolResult, CallID: call.CallID, Text: "ok"}}
}

func userMsg(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{{Kind: BlockText, Text: text}}}
}

// ---- assertion helpers ------------------------------------------------

func eventTypes(s Session) []string {
	var out []string
	for _, ev := range s.Events() {
		out = append(out, ev.Type)
	}
	return out
}

func assertSequence(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event sequence length = %d, want %d\n got:  %v\n want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q\n got:  %v\n want: %v", i, got[i], want[i], got, want)
		}
	}
}

func assertHasType(t *testing.T, s Session, typ string) {
	t.Helper()
	for _, ev := range s.Events() {
		if ev.Type == typ {
			return
		}
	}
	t.Fatalf("expected an event of type %q, found none", typ)
}

func countEventType(s Session, typ string) int {
	n := 0
	for _, ev := range s.Events() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

type stepMark struct{ turn, step int }

func stepStarts(s Session) []stepMark {
	var out []stepMark
	for _, ev := range s.Events() {
		if ev.Type != "step/start" {
			continue
		}
		data := ev.Data.(map[string]any)
		out = append(out, stepMark{turn: data["turn"].(int), step: data["step"].(int)})
	}
	return out
}

func turnEndReason(t *testing.T, s Session, turn int) string {
	t.Helper()
	for _, ev := range s.Events() {
		if ev.Type != "turn/end" {
			continue
		}
		data, ok := ev.Data.(map[string]any)
		if !ok {
			continue
		}
		if tn, _ := data["turn"].(int); tn != turn {
			continue
		}
		switch r := data["reason"].(type) {
		case string:
			return r
		case StepEndReason:
			return string(r)
		}
	}
	t.Fatalf("no turn/end event found for turn %d", turn)
	return ""
}

func turnEndErrorMessage(t *testing.T, s Session, turn int) string {
	t.Helper()
	for _, ev := range s.Events() {
		if ev.Type != "turn/end" {
			continue
		}
		data, ok := ev.Data.(map[string]any)
		if !ok {
			continue
		}
		if tn, _ := data["turn"].(int); tn != turn {
			continue
		}
		msg, _ := data["error"].(string)
		return msg
	}
	t.Fatalf("no turn/end event found for turn %d", turn)
	return ""
}

// ---- tests ----------------------------------------------------------------

func TestSingleStepCompletion(t *testing.T) {
	session := NewMemorySession()
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "hi there"}}},
	}}
	agent := NewAgent(session, llm, echoTool{}, "system prompt", Hooks{})

	agent.Followup(userMsg("hello"))
	agent.WhenIdle()

	if got := agent.Status(); got != "idle" {
		t.Fatalf("status = %q, want idle", got)
	}
	if llm.calls != 1 {
		t.Fatalf("llm called %d times, want 1 (no tool call -> one step)", llm.calls)
	}
	assertSequence(t, eventTypes(session), []string{
		"turn/start", "step/start", "user/message", "assistant/chunk", "assistant/message", "step/end", "turn/end",
	})
	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed", reason)
	}
}

func TestLiveTextDeltasCoalesceOnAssistantMessage(t *testing.T) {
	session := NewMemorySession()
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{
			{Delta: ContentBlock{Kind: BlockText, Text: "hel"}},
			{Delta: ContentBlock{Kind: BlockText, Text: "lo"}},
			{Delta: ContentBlock{Kind: BlockText, Text: " there"}},
		},
	}}
	agent := NewAgent(session, llm, echoTool{}, "sys", Hooks{})
	agent.Followup(userMsg("hi"))
	agent.WhenIdle()

	var chunks int
	for _, ev := range session.Events() {
		if ev.Type == "assistant/chunk" {
			chunks++
		}
	}
	if chunks != 3 {
		t.Fatalf("assistant/chunk count = %d, want 3 live deltas", chunks)
	}

	var got string
	var textBlocks int
	for _, m := range session.DeriveMessages() {
		if m.Role != RoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Kind == BlockText {
				textBlocks++
				got += b.Text
			}
		}
	}
	if textBlocks != 1 || got != "hello there" {
		t.Fatalf("assistant message text blocks = %d %q, want 1 coalesced %q", textBlocks, got, "hello there")
	}
}

func TestToolCallLoopsAcrossStepsWithinOneTurn(t *testing.T) {
	session := NewMemorySession()
	call := ContentBlock{Kind: BlockToolCall, CallID: "c1", ToolName: "read"}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "let me check"}}, {Delta: call}},
		{{Delta: ContentBlock{Kind: BlockText, Text: "done"}}},
	}}
	agent := NewAgent(session, llm, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("do the thing"))
	agent.WhenIdle()

	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2 (tool call must trigger a second step)", llm.calls)
	}
	steps := stepStarts(session)
	if len(steps) != 2 {
		t.Fatalf("got %d step/start events, want 2", len(steps))
	}
	if steps[0].turn != 1 || steps[1].turn != 1 {
		t.Fatalf("tool-call continuation opened a new turn instead of a new step: %+v", steps)
	}
	if steps[0].step != 1 || steps[1].step != 2 {
		t.Fatalf("unexpected step numbering: %+v", steps)
	}
	assertHasType(t, session, "tool/call")
	assertHasType(t, session, "tool/result")
	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed", reason)
	}

	// The tool observation itself must round-trip into derived history, not
	// just leave a log entry — this is what the second step's model call
	// actually reads.
	foundObservation := false
	for _, m := range session.DeriveMessages() {
		for _, b := range m.Content {
			if b.Kind == BlockToolResult && b.CallID == "c1" && b.Text == "ok:read" {
				foundObservation = true
			}
		}
	}
	if !foundObservation {
		t.Fatalf("derived messages never carried the tool/result observation: %+v", session.DeriveMessages())
	}
}

func TestMaxTokensWithToolCallStillExecutes(t *testing.T) {
	sess := NewMemorySession()
	call := ContentBlock{Kind: BlockToolCall, CallID: "c-write", ToolName: "write"}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "writing"}}, {Delta: call, MaxTokens: true}},
		{{Delta: ContentBlock{Kind: BlockText, Text: "done"}}},
	}}
	agent := NewAgent(sess, llm, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("write a page"))
	agent.WhenIdle()

	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2 (max-tokens tool call must still execute and continue)", llm.calls)
	}
	assertHasType(t, sess, "tool/call")
	assertHasType(t, sess, "tool/result")
	found := false
	for _, m := range sess.DeriveMessages() {
		for _, b := range m.Content {
			if b.Kind == BlockToolResult && b.CallID == "c-write" && b.Text == "ok:write" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("tool result missing after a max-tokens tool-call step: %+v", sess.DeriveMessages())
	}
	if reason := turnEndReason(t, sess, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed (tools ran, next step finished)", reason)
	}
}

func TestFollowupClosesUnansweredToolCallsBeforeUserText(t *testing.T) {
	sess := NewMemorySession()
	sess.Append("assistant/message", map[string]any{
		"turn": 1, "step": 1,
		"message": Message{Role: RoleAssistant, Content: []ContentBlock{
			{Kind: BlockToolCall, CallID: "dangling", ToolName: "write"},
		}},
	}, session.WithSurfaceOp(session.AppendOp()))

	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "ok from a repaired history"}}},
	}}
	agent := NewAgent(sess, llm, echoTool{}, "sys", Hooks{})
	agent.Followup(userMsg("retry"))
	agent.WhenIdle()

	if llm.calls != 1 {
		t.Fatalf("llm called %d times, want 1", llm.calls)
	}
	var sawResult, sawRetry bool
	for _, m := range sess.DeriveMessages() {
		for _, b := range m.Content {
			if b.Kind == BlockToolResult && b.CallID == "dangling" {
				sawResult = true
			}
			if b.Kind == BlockText && b.Text == "retry" {
				sawRetry = true
			}
		}
	}
	if !sawResult || !sawRetry {
		t.Fatalf("repaired history missing result or retry: result=%v retry=%v messages=%+v", sawResult, sawRetry, sess.DeriveMessages())
	}
}

func TestMaxTokensStaysStickyAcrossACompletedContinuation(t *testing.T) {
	session := NewMemorySession()
	var agent *Agent
	llm := &scriptedLLM{
		responses: [][]StreamChunk{
			{{Delta: ContentBlock{Kind: BlockText, Text: "cut off"}, MaxTokens: true}},
			{{Delta: ContentBlock{Kind: BlockText, Text: "continued answer"}}},
		},
	}
	llm.onCall = func(i int) {
		if i == 0 {
			// Simulate an auto-continue policy queuing a follow-up before step 1 returns.
			agent.Inject(userMsg("continue"))
		}
	}
	agent = NewAgent(session, llm, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("long task"))
	agent.WhenIdle()

	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2", llm.calls)
	}
	if reason := turnEndReason(t, session, 1); reason != "max-tokens" {
		t.Fatalf("turn/end reason = %q, want max-tokens to stay sticky despite step 2 completing normally", reason)
	}
}

func TestPreStepRejectClosesTurnWithoutAStep(t *testing.T) {
	session := NewMemorySession()
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "should not run"}}},
	}}
	hooks := Hooks{PreStep: func(ctx context.Context, agent *Agent, turn, step int, messages []Message) (bool, []Message) {
		return false, nil
	}}
	agent := NewAgent(session, llm, echoTool{}, "sys", hooks)

	agent.Followup(userMsg("hello"))
	agent.WhenIdle()

	if llm.calls != 0 {
		t.Fatalf("llm called %d times, want 0 (pre-step rejected before any model call)", llm.calls)
	}
	if reason := turnEndReason(t, session, 1); reason != "blocked" {
		t.Fatalf("turn/end reason = %q, want blocked", reason)
	}
}

// erroringThenSucceedingLLM fails its Stream call synchronously (the shape
// llm/deepseek's adapter actually returns for HTTP-level failures — rate
// limits, 5xx, transport errors, all surface as Stream's own error return,
// not a mid-stream chunk) for the first `failures` calls, then succeeds.
type erroringThenSucceedingLLM struct {
	failures  int
	calls     int
	responses [][]StreamChunk
}

func (e *erroringThenSucceedingLLM) Stream(ctx context.Context, req GenerateRequest) (<-chan StreamChunk, error) {
	i := e.calls
	e.calls++
	if i < e.failures {
		return nil, fmt.Errorf("simulated synchronous request failure %d", i)
	}
	ch := make(chan StreamChunk, len(e.responses[i-e.failures]))
	for _, c := range e.responses[i-e.failures] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// TestRequestErrorHookFiresForSynchronousStreamError guards against a real
// gap this suite didn't previously cover: Stream's own synchronous error
// return (as opposed to a StreamChunk.Err mid-stream) used to skip
// hooks.RequestError entirely and fail the step outright, so a retry policy
// wired via that hook would never see the errors that matter most.
func TestRequestErrorHookFiresForSynchronousStreamError(t *testing.T) {
	session := NewMemorySession()
	llm := &erroringThenSucceedingLLM{
		failures: 2,
		responses: [][]StreamChunk{
			{{Delta: ContentBlock{Kind: BlockText, Text: "recovered"}}},
		},
	}
	var seenErrors []error
	hooks := Hooks{RequestError: func(ctx context.Context, turn, step int, err error) bool {
		seenErrors = append(seenErrors, err)
		return true // always retry in this test
	}}
	agent := NewAgent(session, llm, echoTool{}, "sys", hooks)

	agent.Followup(userMsg("hello"))
	agent.WhenIdle()

	if len(seenErrors) != 2 {
		t.Fatalf("RequestError hook fired %d times, want 2 (one per synchronous failure)", len(seenErrors))
	}
	if llm.calls != 3 {
		t.Fatalf("llm called %d times, want 3 (2 failures + 1 success)", llm.calls)
	}
	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed", reason)
	}
}

func TestCancelAbortsInFlightTurn(t *testing.T) {
	session := NewMemorySession()
	llm := &blockingLLM{started: make(chan struct{}), block: make(chan struct{})}
	agent := NewAgent(session, llm, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("hang"))
	<-llm.started
	if got := agent.Status(); got != "running" {
		t.Fatalf("status while streaming = %q, want running", got)
	}
	agent.Cancel(CancelCause{Kind: "user", Reason: "user pressed stop"}, false)
	close(llm.block) // let the goroutine unwind instead of leaking it
	agent.WhenIdle()

	if got := agent.Status(); got != "idle" {
		t.Fatalf("status after cancel = %q, want idle", got)
	}
	if reason := turnEndReason(t, session, 1); reason != "aborted" {
		t.Fatalf("turn/end reason = %q, want aborted", reason)
	}
}

// TestTurnEndedFiresOnEveryExitPath is the regression test for the gap
// web/server.go's syncPersistence used to have: relying on whichever HTTP
// handler happened to still be polling when a turn went idle meant a
// client that disconnected mid-turn (or a turn that errored/was cancelled)
// could finish without its durable side effects ever running. TurnEnded is
// the fix — it must fire regardless of *why* the turn ended, unlike
// TurnStopping, which only covers the clean-completion path.
func TestTurnEndedFiresOnEveryExitPath(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		sess := NewMemorySession()
		llm := &scriptedLLM{responses: [][]StreamChunk{{{Delta: ContentBlock{Kind: BlockText, Text: "hi"}}}}}
		var got []string
		hooks := Hooks{TurnEnded: func(_ context.Context, turn int, reason string) {
			got = append(got, reason)
		}}
		agent := NewAgent(sess, llm, echoTool{}, "sys", hooks)
		agent.Followup(userMsg("hello"))
		agent.WhenIdle()
		if len(got) != 1 || got[0] != "completed" {
			t.Fatalf("TurnEnded calls = %v, want [completed]", got)
		}
	})

	t.Run("blocked", func(t *testing.T) {
		sess := NewMemorySession()
		llm := &scriptedLLM{responses: [][]StreamChunk{{{Delta: ContentBlock{Kind: BlockText, Text: "should not run"}}}}}
		var got []string
		hooks := Hooks{
			PreStep:   func(context.Context, *Agent, int, int, []Message) (bool, []Message) { return false, nil },
			TurnEnded: func(_ context.Context, turn int, reason string) { got = append(got, reason) },
		}
		agent := NewAgent(sess, llm, echoTool{}, "sys", hooks)
		agent.Followup(userMsg("hello"))
		agent.WhenIdle()
		if len(got) != 1 || got[0] != "blocked" {
			t.Fatalf("TurnEnded calls = %v, want [blocked]", got)
		}
	})

	t.Run("aborted by Cancel", func(t *testing.T) {
		sess := NewMemorySession()
		llm := &blockingLLM{started: make(chan struct{}), block: make(chan struct{})}
		var got []string
		hooks := Hooks{TurnEnded: func(_ context.Context, turn int, reason string) { got = append(got, reason) }}
		agent := NewAgent(sess, llm, echoTool{}, "sys", hooks)

		agent.Followup(userMsg("hang"))
		<-llm.started
		agent.Cancel(CancelCause{Kind: "user", Reason: "stop"}, false)
		close(llm.block)
		agent.WhenIdle()

		if len(got) != 1 || got[0] != "aborted" {
			t.Fatalf("TurnEnded calls = %v, want [aborted]", got)
		}
	})

	t.Run("error after RequestError gives up", func(t *testing.T) {
		sess := NewMemorySession()
		llm := &erroringThenSucceedingLLM{failures: 1, responses: [][]StreamChunk{{{Delta: ContentBlock{Kind: BlockText, Text: "unreached"}}}}}
		var got []string
		hooks := Hooks{
			RequestError: func(context.Context, int, int, error) bool { return false }, // never retry
			TurnEnded:    func(_ context.Context, turn int, reason string) { got = append(got, reason) },
		}
		agent := NewAgent(sess, llm, echoTool{}, "sys", hooks)
		agent.Followup(userMsg("hello"))
		agent.WhenIdle()
		if len(got) != 1 || got[0] != "error" {
			t.Fatalf("TurnEnded calls = %v, want [error]", got)
		}
	})

	t.Run("panic recovered", func(t *testing.T) {
		sess := NewMemorySession()
		var got []string
		hooks := Hooks{TurnEnded: func(_ context.Context, turn int, reason string) { got = append(got, reason) }}
		agent := NewAgent(sess, panickingLLM{}, echoTool{}, "sys", hooks)
		agent.Followup(userMsg("hello"))
		agent.WhenIdle()
		if len(got) != 1 || got[0] != "error" {
			t.Fatalf("TurnEnded calls = %v, want [error]", got)
		}
	})
}

// TestTurnEndCarriesTheActualErrorText is the regression test for the other
// half of the same gap TestTurnEndedFiresOnEveryExitPath covers: knowing a
// turn ended in "error" isn't enough for a client to explain what actually
// went wrong — kick()'s own `cont, _ := a.turn(ctx)` throws the real error
// away, so without turn/end recording it, the specific failure (e.g. a
// model request that ran out of retries) was lost the moment the turn
// closed, even though the log otherwise durably records everything else
// about it.
func TestTurnEndCarriesTheActualErrorText(t *testing.T) {
	sess := NewMemorySession()
	llm := &erroringThenSucceedingLLM{failures: 1, responses: [][]StreamChunk{{{Delta: ContentBlock{Kind: BlockText, Text: "unreached"}}}}}
	hooks := Hooks{RequestError: func(context.Context, int, int, error) bool { return false }} // never retry
	agent := NewAgent(sess, llm, echoTool{}, "sys", hooks)

	agent.Followup(userMsg("hello"))
	agent.WhenIdle()

	if reason := turnEndReason(t, sess, 1); reason != "error" {
		t.Fatalf("turn/end reason = %q, want error", reason)
	}
	msg := turnEndErrorMessage(t, sess, 1)
	if msg == "" {
		t.Fatal("turn/end has no error text, want the underlying failure message")
	}
	if !strings.Contains(msg, "simulated synchronous request failure") {
		t.Fatalf("turn/end error = %q, want it to contain the underlying LLM error", msg)
	}
}

func TestParallelToolCallsRunConcurrentlyBoundedByPool(t *testing.T) {
	session := NewMemorySession()
	toolCalls := []ContentBlock{
		{Kind: BlockToolCall, CallID: "a", ToolName: "fetch"},
		{Kind: BlockToolCall, CallID: "b", ToolName: "fetch"},
		{Kind: BlockToolCall, CallID: "c", ToolName: "fetch"},
	}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: toolCalls[0]}, {Delta: toolCalls[1]}, {Delta: toolCalls[2]}},
		{{Delta: ContentBlock{Kind: BlockText, Text: "done"}}},
	}}
	tool := &trackingParallelTool{sleep: 40 * time.Millisecond}
	agent := NewAgent(session, llm, tool, "sys", Hooks{}).WithMaxParallelToolCalls(2)

	start := time.Now()
	agent.Followup(userMsg("fetch three things"))
	agent.WhenIdle()
	elapsed := time.Since(start)

	if max := atomic.LoadInt32(&tool.maxObserved); max != 2 {
		t.Fatalf("max concurrent calls observed = %d, want exactly 2 (pool of 2, 3 calls)", max)
	}
	if elapsed >= 3*tool.sleep {
		t.Fatalf("elapsed %v looks fully sequential (>= 3x40ms), want overlap from the bounded pool", elapsed)
	}
	if got := countEventType(session, "tool/call"); got != 3 {
		t.Fatalf("got %d tool/call events, want 3", got)
	}
	if got := countEventType(session, "tool/result"); got != 3 {
		t.Fatalf("got %d tool/result events, want 3", got)
	}
}

func TestConcludesTurnEndsTurnDespiteToolCall(t *testing.T) {
	session := NewMemorySession()
	call := ContentBlock{Kind: BlockToolCall, CallID: "c1", ToolName: "exit_plan_mode"}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "let's exit plan mode"}}, {Delta: call}},
	}}
	agent := NewAgent(session, llm, concludingTool{}, "sys", Hooks{})

	agent.Followup(userMsg("go ahead"))
	agent.WhenIdle()

	if llm.calls != 1 {
		t.Fatalf("llm called %d times, want 1 (a concludesTurn result must skip the continuation step)", llm.calls)
	}
	assertHasType(t, session, "tool/call")
	assertHasType(t, session, "tool/result")
	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed", reason)
	}
}

func TestAbortDuringToolExecutionSkipsUnstartedCalls(t *testing.T) {
	session := NewMemorySession()
	toolCalls := []ContentBlock{
		{Kind: BlockToolCall, CallID: "a", ToolName: "slow"},
		{Kind: BlockToolCall, CallID: "b", ToolName: "slow"},
		{Kind: BlockToolCall, CallID: "c", ToolName: "slow"},
	}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: toolCalls[0]}, {Delta: toolCalls[1]}, {Delta: toolCalls[2]}},
	}}
	var agent *Agent
	tool := &cancelingExclusiveTool{
		onFirstCall: func() { agent.Cancel(CancelCause{Kind: "user", Reason: "stop"}, false) },
	}
	agent = NewAgent(session, llm, tool, "sys", Hooks{})

	agent.Followup(userMsg("run three slow steps"))
	agent.WhenIdle()

	if tool.executed != 1 {
		t.Fatalf("tool actually executed %d times, want 1 (b and c must be skipped, not run)", tool.executed)
	}
	// Every model-issued call still needs a call/result pair in the log —
	// b and c get a synthetic "aborted before dispatch" result — so replay
	// never finds an orphaned tool/call with no matching tool/result.
	if got := countEventType(session, "tool/call"); got != 3 {
		t.Fatalf("got %d tool/call events, want 3 (1 real + 2 synthetic)", got)
	}
	if got := countEventType(session, "tool/result"); got != 3 {
		t.Fatalf("got %d tool/result events, want 3 (1 real + 2 synthetic)", got)
	}
	if reason := turnEndReason(t, session, 1); reason != "aborted" {
		t.Fatalf("turn/end reason = %q, want aborted", reason)
	}
}

func TestPanicInsideToolIsContainedAsAnErrorResult(t *testing.T) {
	session := NewMemorySession()
	call := ContentBlock{Kind: BlockToolCall, CallID: "c1", ToolName: "buggy"}
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "calling the buggy tool"}}, {Delta: call}},
		{{Delta: ContentBlock{Kind: BlockText, Text: "that tool is broken, giving up"}}},
	}}
	agent := NewAgent(session, llm, panickingTool{}, "sys", Hooks{})

	agent.Followup(userMsg("go"))
	agent.WhenIdle()

	if got := agent.Status(); got != "idle" {
		t.Fatalf("status = %q, want idle (a tool panic must not crash the driver)", got)
	}
	if llm.calls != 2 {
		t.Fatalf("llm called %d times, want 2 (the model must still see the panic as a tool error and get to respond)", llm.calls)
	}
	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn/end reason = %q, want completed", reason)
	}
}

func TestPanicInTurnSynchronousChainIsRecovered(t *testing.T) {
	session := NewMemorySession()
	agent := NewAgent(session, panickingLLM{}, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("go"))
	agent.WhenIdle()

	if got := agent.Status(); got != "idle" {
		t.Fatalf("status = %q, want idle (a panic inside turn()'s own chain must not crash the driver)", got)
	}
	if reason := turnEndReason(t, session, 1); reason != "error" {
		t.Fatalf("turn/end reason = %q, want error", reason)
	}
}

func TestFollowupAfterIdleOpensASecondTurn(t *testing.T) {
	session := NewMemorySession()
	llm := &scriptedLLM{responses: [][]StreamChunk{
		{{Delta: ContentBlock{Kind: BlockText, Text: "first"}}},
		{{Delta: ContentBlock{Kind: BlockText, Text: "second"}}},
	}}
	agent := NewAgent(session, llm, echoTool{}, "sys", Hooks{})

	agent.Followup(userMsg("one"))
	agent.WhenIdle()
	agent.Followup(userMsg("two"))
	agent.WhenIdle()

	if reason := turnEndReason(t, session, 1); reason != "completed" {
		t.Fatalf("turn 1 reason = %q, want completed", reason)
	}
	if reason := turnEndReason(t, session, 2); reason != "completed" {
		t.Fatalf("turn 2 reason = %q, want completed", reason)
	}
	msgs := session.DeriveMessages()
	if len(msgs) != 4 { // user/one, assistant/first, user/two, assistant/second
		t.Fatalf("derived %d messages, want 4: %+v", len(msgs), msgs)
	}
}

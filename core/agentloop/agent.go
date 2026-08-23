// Package agentloop is a faithful port of the ReAct-style turn/step driver
// (packages/core/agent-loop/src/agent.ts) from DeepSeek Harness.
package agentloop

import (
	"context"
	"fmt"
	"sync"

	"go-harness/core/session"
)

// ---- Conversation vocabulary -------------------------------------------
//
// Owned by core/session now, matching the TS dependency direction
// (agent-loop depends on session, not the reverse). Aliased here so every
// existing call site (this package, its consumers, its tests) keeps
// compiling untouched.

type Role = session.Role

const (
	RoleUser      = session.RoleUser
	RoleAssistant = session.RoleAssistant
)

type BlockKind = session.BlockKind

const (
	BlockText       = session.BlockText
	BlockReasoning  = session.BlockReasoning // chain-of-thought, e.g. DeepSeek thinking mode
	BlockToolCall   = session.BlockToolCall
	BlockToolResult = session.BlockToolResult
)

type ContentBlock = session.ContentBlock

type Message = session.Message

func toolCallsIn(blocks []ContentBlock) []ContentBlock {
	var calls []ContentBlock
	for _, b := range blocks {
		if b.Kind == BlockToolCall {
			calls = append(calls, b)
		}
	}
	return calls
}

// coalesceBlocks merges consecutive text (and consecutive reasoning) deltas
// into one block each. Live streaming emits many tiny assistant/chunk
// events; the durable assistant/message still wants one text block, which
// is what DeriveMessages / serialize replay. Tool-call blocks are never
// merged — each call is its own Action.
func coalesceBlocks(blocks []ContentBlock) []ContentBlock {
	if len(blocks) < 2 {
		return blocks
	}
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if n := len(out); n > 0 && (b.Kind == BlockText || b.Kind == BlockReasoning) && out[n-1].Kind == b.Kind {
			out[n-1].Text += b.Text
			continue
		}
		out = append(out, b)
	}
	return out
}

func isEmptyTextish(b ContentBlock) bool {
	return (b.Kind == BlockText || b.Kind == BlockReasoning) && b.Text == ""
}

// ---- Session log: the single source of truth ---------------------------
//
// Also owned by core/session: its Session type is the real event-sourced
// log with surface projection (append/replace); this package no longer
// keeps its own ad-hoc memorySession. NewMemorySession is kept as a
// zero-touch constructor for existing callers.

type SessionEvent = session.Event

type Session = *session.Session

// NewMemorySession returns a fresh, detached, in-memory session with an
// auto-assigned id — the same "just give me somewhere to log" convenience
// the old ad-hoc memorySession offered call sites that don't need a Store.
func NewMemorySession() Session { return session.New("", session.Header{}) }

// ---- LLM and tool seams --------------------------------------------------

type StreamChunk struct {
	Delta     ContentBlock
	MaxTokens bool
	Err       error
}

// ToolSchema is one model-facing tool declaration: name, description, and a
// JSON Schema `parameters` object, exactly as sent on the wire.
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// GenerateRequest is provider-agnostic; a concrete LLM adapter (e.g.
// llm/deepseek) maps it onto its own wire format. ReasoningEffort is an
// adapter-defined vocabulary ("off"/"low"/"high"/"max" for DeepSeek); an
// adapter that doesn't recognize a value should reject it rather than
// silently ignore it.
type GenerateRequest struct {
	Model           string
	System          string
	Messages        []Message
	Tools           []ToolSchema
	MaxTokens       int
	Temperature     *float64
	TopP            *float64
	Stop            []string
	ReasoningEffort string
}

type LLM interface {
	Stream(ctx context.Context, req GenerateRequest) (<-chan StreamChunk, error)
}

// ToolExecutor and the ExecutionMode/ToolResult vocabulary it uses live in
// tool_calls.go, alongside the scheduler that calls them.

// ---- Inbox: the two ordered pending-message queues ----------------------

type InboxTarget int

const (
	TargetNextTurn InboxTarget = iota
	TargetNextStep
)

type Inbox struct {
	mu       sync.Mutex
	nextTurn []Message
	nextStep []Message
}

func (ib *Inbox) push(target InboxTarget, msg Message) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if target == TargetNextTurn {
		ib.nextTurn = append(ib.nextTurn, msg)
	} else {
		ib.nextStep = append(ib.nextStep, msg)
	}
}

// claim removes the proposed step batch: all next-step input, plus one
// next-turn message when claiming a turn boundary.
func (ib *Inbox) claim(target InboxTarget) []Message {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	claimed := ib.nextStep
	ib.nextStep = nil
	if target == TargetNextTurn && len(ib.nextTurn) > 0 {
		claimed = append(claimed, ib.nextTurn[0])
		ib.nextTurn = ib.nextTurn[1:]
	}
	return claimed
}

func (ib *Inbox) hasPending() bool {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return len(ib.nextTurn) > 0 || len(ib.nextStep) > 0
}

func (ib *Inbox) hasNextStepPending() bool {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return len(ib.nextStep) > 0
}

func (ib *Inbox) clear() {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.nextTurn, ib.nextStep = nil, nil
}

// ---- Cancellation cause, mirroring AgentCancelCause ----------------------

type CancelCause struct {
	Kind   string // "user" | "parent" | "hook" | "disposed"
	Reason string
}

func (c CancelCause) Error() string { return c.Kind + ": " + c.Reason }

// ---- Extension points, mirroring the four waterfall/serial hooks --------

type Hooks struct {
	// PreStep may rewrite or reject the claimed batch before it enters a step.
	// It receives the owning Agent (not just its messages) so a session-state
	// plugin can fold and extend durable state via agent.Session().
	PreStep func(ctx context.Context, agent *Agent, turn, step int, messages []Message) (enter bool, rewritten []Message)
	// RequestError runs after a failed model call; returning true retries in place.
	RequestError func(ctx context.Context, turn, step int, err error) (retry bool)
	// TurnStopping is the terminal, non-retryable checkpoint before a turn closes.
	TurnStopping func(ctx context.Context, turn int)
	// TurnEnded fires exactly once per turn, from the same deferred cleanup
	// that guarantees turn/end always gets appended — unlike TurnStopping,
	// this runs on every exit path without exception: normal completion,
	// max-tokens, a rejected PreStep, an aborted (cancelled) turn, a
	// request error that gave up, and a recovered panic all reach it. A
	// caller whose durable side effects must not depend on a client still
	// being connected (e.g. flushing a session to persistent storage —
	// see web/server.go's syncPersistence wiring) belongs here, not in
	// TurnStopping, which a cancelled or errored turn skips entirely.
	TurnEnded func(ctx context.Context, turn int, reason string)
}

// ---- Phase state machine -------------------------------------------------

type phaseKind int

const (
	phaseIdle phaseKind = iota
	phaseMaintenance
	phaseRunning
)

type phase struct {
	kind          phaseKind
	ctx           context.Context
	cancel        context.CancelCauseFunc
	turn, step    int
	wakeRequested bool
}

// ---- Agent ----------------------------------------------------------------

type StepEndReason string

const (
	stepContinue  StepEndReason = "" // like TS `null`: keep looping within the turn
	StepCompleted StepEndReason = "completed"
	StepMaxTokens StepEndReason = "max-tokens"
)

type Agent struct {
	mu    sync.Mutex
	phase phase

	session Session
	inbox   *Inbox
	llm     LLM
	tools   ToolExecutor
	system  string
	hooks   Hooks

	model           string
	maxTokens       int
	temperature     *float64
	topP            *float64
	reasoningEffort string

	maxParallelToolCalls int
	activityDone         chan struct{}
}

func NewAgent(sess Session, llm LLM, tools ToolExecutor, system string, hooks Hooks) *Agent {
	done := make(chan struct{})
	close(done)
	return &Agent{
		session:              sess,
		inbox:                &Inbox{},
		llm:                  llm,
		tools:                tools,
		system:               system,
		hooks:                hooks,
		maxParallelToolCalls: DefaultMaxParallelToolCalls,
		phase:                phase{kind: phaseIdle, turn: sess.LastTurn()},
		activityDone:         done,
	}
}

// WithMaxParallelToolCalls overrides the bounded pool size for the parallel
// execution mode (default DefaultMaxParallelToolCalls). Call before the
// first Followup/Steer/Inject.
func (a *Agent) WithMaxParallelToolCalls(n int) *Agent {
	a.maxParallelToolCalls = n
	return a
}

// WithModel sets the model id passed as GenerateRequest.Model on every
// request this Agent issues. Call before the first Followup/Steer/Inject.
func (a *Agent) WithModel(model string) *Agent {
	a.model = model
	return a
}

// WithMaxTokens sets GenerateRequest.MaxTokens on every request. Zero (the
// default) omits a cap, leaving the adapter's own default in effect.
func (a *Agent) WithMaxTokens(n int) *Agent {
	a.maxTokens = n
	return a
}

// WithTemperature sets GenerateRequest.Temperature on every request.
func (a *Agent) WithTemperature(t float64) *Agent {
	a.temperature = &t
	return a
}

// WithTopP sets GenerateRequest.TopP on every request.
func (a *Agent) WithTopP(p float64) *Agent {
	a.topP = &p
	return a
}

// WithReasoningEffort sets GenerateRequest.ReasoningEffort on every request;
// the vocabulary is adapter-defined (DeepSeek: "off"/"low"/"high"/"max").
func (a *Agent) WithReasoningEffort(effort string) *Agent {
	a.reasoningEffort = effort
	return a
}

// Session returns this Agent's session log — the read/append surface a
// session-state plugin (e.g. planmode.Controller) needs to fold durable
// state and extend it, without agentloop knowing that plugin exists.
func (a *Agent) Session() Session { return a.session }

// SystemPrompt returns the system prompt this Agent sends with every
// request — the same string a caller wanting to price a hypothetical next
// request's structural overhead (e.g. llm/tokenmeter.Header.System) needs,
// without duplicating whatever constant buildAgent constructed it from.
func (a *Agent) SystemPrompt() string { return a.system }

// ToolSchemas returns the tool declarations this Agent sends with every
// request — the other half of llm/tokenmeter.Header, alongside SystemPrompt.
func (a *Agent) ToolSchemas() []ToolSchema { return a.tools.Schemas() }

func (a *Agent) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.phase.kind == phaseRunning {
		return "running"
	}
	return "idle"
}

// send is the one primitive; Followup/Steer/Inject are fixed presets over it.
func (a *Agent) send(msg Message, target InboxTarget, wakeup bool) {
	a.mu.Lock()
	wakingAfterAbort := wakeup && a.phase.kind != phaseIdle && a.phase.ctx.Err() != nil
	resolved := target
	if wakingAfterAbort {
		resolved = TargetNextTurn
	}
	a.mu.Unlock()

	a.inbox.push(resolved, msg)
	if wakeup {
		a.wakeDriver(wakingAfterAbort)
	}
}

func (a *Agent) Followup(msg Message) { a.send(msg, TargetNextTurn, true) }
func (a *Agent) Steer(msg Message)    { a.send(msg, TargetNextStep, true) }
func (a *Agent) Inject(msg Message)   { a.send(msg, TargetNextStep, false) }

func (a *Agent) Cancel(cause CancelCause, keepInbox bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !keepInbox {
		a.inbox.clear()
		a.phase.wakeRequested = false
	}
	if a.phase.kind != phaseIdle {
		a.phase.cancel(cause)
	}
}

func (a *Agent) WhenIdle() {
	for {
		a.mu.Lock()
		ch := a.activityDone
		a.mu.Unlock()
		<-ch
		a.mu.Lock()
		settled := ch == a.activityDone
		a.mu.Unlock()
		if settled {
			return
		}
	}
}

// wakeDriver starts kick() from idle, or latches the wake for replay once
// the in-flight maintenance task or aborted activity converges.
func (a *Agent) wakeDriver(afterAbort bool) {
	a.mu.Lock()
	if a.phase.kind != phaseIdle {
		if cause, ok := context.Cause(a.phase.ctx).(CancelCause); !ok || cause.Kind != "disposed" {
			if a.phase.kind == phaseMaintenance || afterAbort {
				a.phase.wakeRequested = true
			}
		}
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	a.activityDone = make(chan struct{})
	a.phase = phase{kind: phaseRunning, ctx: ctx, cancel: cancel, turn: a.phase.turn}
	a.mu.Unlock()

	go a.kick(ctx)
}

// kick drives consecutive turns until the inbox is drained; this is the
// outermost loop, equivalent to `while (await this.turn()) {}`.
//
// turn() already recovers panics from its own synchronous body and from
// safeExecute for tool goroutines, so both should reach here as ordinary
// errors, not panics. This recover is a last-resort backstop: without it, an
// unforeseen panic anywhere in this call chain would skip straight past the
// cleanup below, leaving activityDone permanently unclosed and any WhenIdle()
// caller blocked forever — on top of crashing the whole process, which an
// unrecovered goroutine panic always does in Go regardless of what called it.
func (a *Agent) kick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			a.mu.Lock()
			a.phase = phase{kind: phaseIdle, turn: a.phase.turn}
			close(a.activityDone)
			a.mu.Unlock()
		}
	}()
	for {
		cont, _ := a.turn(ctx)
		if !cont {
			break
		}
	}
	a.mu.Lock()
	wake := a.phase.wakeRequested
	a.phase = phase{kind: phaseIdle, turn: a.phase.turn}
	close(a.activityDone)
	a.mu.Unlock()
	if wake && a.inbox.hasPending() {
		a.wakeDriver(false)
	}
}

// turn opens one durable turn and loops its steps until nothing is owed.
// This is the outer half of the ReAct loop: it decides *whether* to keep
// going, not *what* to do inside one model exchange.
//
// Named returns plus one deferred cleanup guarantee two things regardless of
// which of the several return points below fires, or whether the body
// panics instead of returning at all: exactly one turn/end gets appended for
// the turn/start already written, and a panic becomes an ordinary `err`
// instead of an unrecovered crash (recover() only works within the panicking
// goroutine's own stack, and turn() runs in kick()'s goroutine — this is what
// actually catches a bug in this file's own code, as opposed to
// safeExecute's recover in tool_calls.go, which covers tool-goroutine bugs).
func (a *Agent) turn(ctx context.Context) (cont bool, err error) {
	a.mu.Lock()
	turnNum := a.phase.turn + 1
	a.phase.turn = turnNum
	a.mu.Unlock()

	a.session.Append("turn/start", turnNum)

	var reason any
	defer func() {
		if r := recover(); r != nil {
			reason, err, cont = "error", fmt.Errorf("agent: turn %d panicked: %v", turnNum, r), false
		}
		endData := map[string]any{"turn": turnNum, "reason": reason}
		if err != nil {
			// Durable, not just an in-memory return value: err is otherwise
			// discarded by kick()'s own `cont, _ := a.turn(ctx)` (its caller
			// only cares whether to keep looping), so without this the
			// specific reason a turn failed — a model request that ran out of
			// retries, a tool that errored — would never reach anything
			// reading the session log after the fact, including a client
			// watching live via GET /sessions/{id}/stream.
			endData["error"] = err.Error()
		}
		a.session.Append("turn/end", endData)
		if a.hooks.TurnEnded != nil {
			a.hooks.TurnEnded(ctx, turnNum, reasonString(reason))
		}
	}()

	var turnEnd StepEndReason
	target := TargetNextTurn

	for {
		if e := ctx.Err(); e != nil {
			reason, err = "aborted", e
			return false, e
		}

		a.mu.Lock()
		stepNum := a.phase.step + 1
		a.mu.Unlock()

		// Close dangling tool_calls before this step's user text lands.
		// Retry/Followup would otherwise insert a user message between an
		// assistant tool_calls turn and its (missing) tool results — every
		// chat-completion API rejects that shape.
		session.CloseUnansweredToolCalls(a.session)

		claimed := a.inbox.claim(target)
		if a.hooks.PreStep != nil {
			enter, rewritten := a.hooks.PreStep(ctx, a, turnNum, stepNum, claimed)
			if !enter {
				reason = "blocked"
				return false, nil
			}
			claimed = rewritten
		}

		if stepNum == 1 && len(claimed) == 0 {
			reason = "completed"
			return false, nil
		}

		a.session.Append("step/start", map[string]any{"turn": turnNum, "step": stepNum})
		a.mu.Lock()
		a.phase.step = stepNum
		a.mu.Unlock()
		for _, m := range claimed {
			a.session.Append("user/message", m, session.WithSurfaceOp(session.AppendOp()))
		}

		result, stepErr := a.step(ctx, turnNum, stepNum)
		a.session.Append("step/end", map[string]any{"turn": turnNum, "step": stepNum})
		if stepErr != nil {
			reason = "error"
			if ctx.Err() != nil {
				reason = "aborted"
			}
			return false, stepErr
		}
		// max-tokens is sticky: a later completed step must not downgrade it.
		if turnEnd != StepMaxTokens {
			turnEnd = result
		}

		if turnEnd != stepContinue && !a.inbox.hasNextStepPending() {
			if a.hooks.TurnStopping != nil {
				a.hooks.TurnStopping(ctx, turnNum)
			}
			if !a.inbox.hasNextStepPending() {
				break
			}
		}
		target = TargetNextStep
	}

	reason = turnEnd
	if !a.inbox.hasPending() {
		return false, nil
	}
	a.mu.Lock()
	a.phase.step = 0
	a.mu.Unlock()
	return true, nil
}

// reasonString normalizes turn/end's reason field (a StepEndReason, a plain
// string like "blocked"/"aborted"/"error", or nil) to a string for
// Hooks.TurnEnded — the same duck-typing turnEndReason's own test helper
// already treats as equivalent, ported to production code instead of a
// _test.go file so a real hook doesn't have to redo it.
func reasonString(reason any) string {
	switch r := reason.(type) {
	case nil:
		return ""
	case string:
		return r
	case StepEndReason:
		return string(r)
	default:
		return fmt.Sprint(r)
	}
}

// step is the inner ReAct cycle: Thought (assistant text) + Action
// (tool-call blocks) go out, Observation (tool results) come back as
// tool/result log events — DeriveMessages() picks them up on the next
// request, no inbox involved. It loops on `while(true)` only to retry a
// failed request; the tool round-trip itself returns to turn().
func (a *Agent) step(ctx context.Context, turn, stepNum int) (StepEndReason, error) {
	for {
		req := GenerateRequest{
			Model:           a.model,
			System:          a.system,
			Messages:        a.session.DeriveMessages(),
			Tools:           a.tools.Schemas(),
			MaxTokens:       a.maxTokens,
			Temperature:     a.temperature,
			TopP:            a.topP,
			ReasoningEffort: a.reasoningEffort,
		}
		stream, err := a.llm.Stream(ctx, req)
		if err != nil {
			if a.hooks.RequestError != nil && a.hooks.RequestError(ctx, turn, stepNum, err) {
				continue // retry in place, same step
			}
			return stepContinue, err
		}

		var blocks []ContentBlock
		maxTokens := false
		var streamErr error
		for chunk := range stream {
			if chunk.Err != nil {
				streamErr = chunk.Err
				break
			}
			if chunk.MaxTokens {
				maxTokens = true
			}
			// A MaxTokens marker may ride on an empty delta (the adapter
			// already streamed the text live). Don't persist a blank block.
			if chunk.Delta.Kind == "" || isEmptyTextish(chunk.Delta) {
				continue
			}
			a.session.Append("assistant/chunk", map[string]any{"turn": turn, "step": stepNum, "chunk": chunk.Delta})
			blocks = append(blocks, chunk.Delta)
		}

		if streamErr != nil {
			if a.hooks.RequestError != nil && a.hooks.RequestError(ctx, turn, stepNum, streamErr) {
				continue // retry in place, same step
			}
			return stepContinue, streamErr
		}

		msg := Message{Role: RoleAssistant, Content: coalesceBlocks(blocks)}
		a.session.Append("assistant/message", map[string]any{"turn": turn, "step": stepNum, "message": msg},
			session.WithSurfaceOp(session.AppendOp()))

		// A length finish must not skip Action: the model often completes a
		// write/bash tool call and only then hits the output cap. Returning
		// here leaves unanswered tool_calls on the surface and poisons every
		// later request. Execute first; only treat max-tokens as terminal
		// when there was nothing to run.
		calls := toolCallsIn(blocks)
		if len(calls) == 0 {
			if maxTokens {
				return StepMaxTokens, nil
			}
			return StepCompleted, nil // no Action -> model gave a final answer
		}

		concluded := a.executeToolCalls(ctx, turn, stepNum, calls)
		if concluded {
			return StepCompleted, nil // a result opted out of another round, even though a tool ran
		}
		return stepContinue, nil // Observation logged; turn() opens the next step, DeriveMessages() picks it up
	}
}

// executeToolCalls, runGroup, and appendSkippedToolCall — the tool-call
// scheduler this method hands off to — live in tool_calls.go.

// Package planmode is a Go port of packages/plan/plan-mode from DeepSeek
// Harness: plan mode as logged, per-session collaboration state rather than
// a generic mode registry. While active, a deployment-owned guidance message
// goes out with each turn's first step, and exit_plan_mode (see exit_tool.go)
// presents a completed plan for human review before leaving. Sandbox mode
// and approval policy are separate concerns — this package guides, it does
// not enforce.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package planmode

import (
	"context"
	"errors"
	"sync"

	"go-harness/core/agentloop"
)

var errEmptySection = errors.New("planmode: section must be non-empty")

// Outcome reports what Set actually did.
type Outcome string

const (
	// Committed means the change was appended to the session log immediately
	// (the agent was idle — no turn was open to interrupt).
	Committed Outcome = "committed"
	// Queued means the change is pending: it will commit at the next
	// accepted pre-step, because a turn is currently open.
	Queued Outcome = "queued"
	// Cancelled means a previously queued selection was reverted before it
	// ever committed — the logged state already matched the new target.
	Cancelled Outcome = "cancelled"
	// Noop means the requested state already matches the logged-or-pending
	// state; nothing changed.
	Noop Outcome = "noop"
)

type intent struct {
	active  bool
	narrate bool
}

// Controller is the plan-mode service: ctx.planMode's equivalent. One
// Controller can serve any number of Agents/sessions — state is keyed per
// Session, mirroring the TS WeakMap<Session, ...> (a plain map here; Go has
// no built-in weak-map GC story, so a long-lived Controller retains an entry
// per session that ever had a pending selection until Forget is called).
type Controller struct {
	mu      sync.Mutex
	pending map[agentloop.Session]intent

	// Section is the deployment-owned guidance injected while plan mode is
	// active. Required, non-empty — mirrors PlanModeConfig.section.
	Section string
}

// New validates config and returns a ready Controller. section must be
// non-empty, matching TS's resolveConfig.
func New(section string) (*Controller, error) {
	if section == "" {
		return nil, errEmptySection
	}
	return &Controller{pending: map[agentloop.Session]intent{}, Section: section}, nil
}

// foldPlanMode is the port of foldPlanMode(events): the last plan/mode event
// wins; a log with none folds to inactive.
func foldPlanMode(events []agentloop.SessionEvent) bool {
	active := false
	for _, ev := range events {
		if ev.Type == "plan/mode" {
			active = ev.Data.(map[string]any)["active"].(bool)
		}
	}
	return active
}

// hasOpenTurn is the port of hasOpenTurn(events): whether the log holds a
// turn/start with no matching turn/end yet — the same "is this agent
// actually idle" signal Set uses to decide committed vs. queued.
func hasOpenTurn(events []agentloop.SessionEvent) bool {
	open := false
	for _, ev := range events {
		switch ev.Type {
		case "turn/start":
			open = true
		case "turn/end":
			open = false
		}
	}
	return open
}

// Get reads the logged plan state plus any selection awaiting the next
// accepted pre-step. pending is nil when no selection is outstanding.
func (c *Controller) Get(agent *agentloop.Agent) (active bool, pending *bool) {
	session := agent.Session()
	active = foldPlanMode(session.Events())
	c.mu.Lock()
	in, ok := c.pending[session]
	c.mu.Unlock()
	if !ok {
		return active, nil
	}
	p := in.active
	return active, &p
}

// Set selects whether plan mode should be active, mirroring
// PlanModeController.set: between turns it commits immediately (no pre-step
// will run before the next prompt); during an open turn it queues until the
// next accepted pre-step, so a mid-flight step's context never changes out
// from under it. Repeated selection of the current or already-pending state
// is a Noop.
func (c *Controller) Set(agent *agentloop.Agent, active bool) Outcome {
	session := agent.Session()
	events := session.Events()
	logged := foldPlanMode(events)

	c.mu.Lock()
	in, hasIntent := c.pending[session]
	c.mu.Unlock()
	target := logged
	if hasIntent {
		target = in.active
	}
	if active == target {
		return Noop
	}

	if hasOpenTurn(events) {
		if logged == active {
			// The new target already matches what's logged — clear any queued
			// selection outright rather than leaving a pending entry that
			// merely happens to equal the logged state (Get should report no
			// pending selection, not a same-valued one).
			c.mu.Lock()
			delete(c.pending, session)
			c.mu.Unlock()
			return Cancelled
		}
		c.mu.Lock()
		c.pending[session] = intent{active: active, narrate: true}
		c.mu.Unlock()
		return Queued
	}

	// No open turn: commit now. hasIntent can still be true here (a queued
	// selection made during a turn that has since closed) — either way the
	// fresh target already reflects it, so this compares against `logged`.
	if active == logged {
		c.mu.Lock()
		delete(c.pending, session)
		c.mu.Unlock()
		return Cancelled
	}
	session.Append("plan/mode", map[string]any{"active": active})
	c.mu.Lock()
	delete(c.pending, session)
	c.mu.Unlock()
	agent.Inject(c.narration(active))
	return Committed
}

// Forget drops any pending selection for session, e.g. once its Agent is
// disposed — the closest this port gets to the TS WeakMap's automatic GC.
func (c *Controller) Forget(session agentloop.Session) {
	c.mu.Lock()
	delete(c.pending, session)
	c.mu.Unlock()
}

// queueExit is ExitTool's path to the same pending mechanism Set uses,
// narrate=false because the tool's own result already tells the model it
// left plan mode — a separate injected notice would say the same thing twice.
func (c *Controller) queueExit(session agentloop.Session) {
	c.mu.Lock()
	c.pending[session] = intent{active: false, narrate: false}
	c.mu.Unlock()
}

// PreStepHook returns an agentloop.Hooks.PreStep function: it applies any
// pending selection (every accepted pre-step is a valid commit boundary, not
// just a turn's first step), then — only on a turn's first step — prepends
// the active guidance message.
//
// The first-step-only guard is this port's answer to not having a real
// multi-section system prompt: the TS original recomputes its "plan:policy"
// section fresh for every request without logging it, so it costs no history
// tokens; here the guidance can only be delivered as a logged message, so
// repeating it every step of a multi-step turn would flood the transcript.
// Logging it once, at the turn's first step, keeps it in derived history for
// the rest of that turn without duplication.
func (c *Controller) PreStepHook() func(ctx context.Context, agent *agentloop.Agent, turn, step int, messages []agentloop.Message) (bool, []agentloop.Message) {
	return func(_ context.Context, agent *agentloop.Agent, _, step int, messages []agentloop.Message) (bool, []agentloop.Message) {
		session := agent.Session()

		c.mu.Lock()
		in, hasIntent := c.pending[session]
		c.mu.Unlock()
		if hasIntent {
			if in.active != foldPlanMode(session.Events()) {
				session.Append("plan/mode", map[string]any{"active": in.active})
			}
			c.mu.Lock()
			delete(c.pending, session)
			c.mu.Unlock()
			if in.narrate {
				messages = append(messages, c.narration(in.active))
			}
		}

		if step == 1 && foldPlanMode(session.Events()) {
			messages = append([]agentloop.Message{c.policyMessage()}, messages...)
		}
		return true, messages
	}
}

func (c *Controller) policyMessage() agentloop.Message {
	return agentloop.Message{
		Role:    agentloop.RoleUser,
		Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: c.Section}},
	}
}

// narration is a one-sentence notice the model sees when the user (not the
// exit tool) switches modes, simplified from the TS original's
// planModeAtLastHeader check: this port has no request/header log to compare
// against, so it always narrates a direct Set()-driven switch rather than
// only when the model was told something different last time.
func (c *Controller) narration(target bool) agentloop.Message {
	text := "The user switched this session back to the default mode."
	if target {
		text = "The user switched this session to plan mode."
	}
	return agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: text}}}
}

// Command is the port of the /plan [message] handler: bare "" (or
// whitespace) selects plan mode; the exact argument "off" selects inactive;
// anything else selects plan mode and returns the trimmed message for the
// caller to Steer() in as the next step's ordinary input (mirroring the TS
// handler's agent.steer() call — kept as a caller responsibility here so
// this package never assumes how a host wires its command loop).
func (c *Controller) Command(agent *agentloop.Agent, rawArg string) (reply string, steer string) {
	arg := rawArg
	for len(arg) > 0 && (arg[0] == ' ' || arg[0] == '\t') {
		arg = arg[1:]
	}
	for len(arg) > 0 && (arg[len(arg)-1] == ' ' || arg[len(arg)-1] == '\t') {
		arg = arg[:len(arg)-1]
	}

	if arg == "off" {
		switch c.Set(agent, false) {
		case Committed:
			return "Plan mode off.", ""
		case Queued:
			return "Leaving plan mode (applies from the next step).", ""
		case Cancelled:
			return "Plan mode entry cancelled.", ""
		default: // Noop
			if active, _ := c.Get(agent); active {
				return "Leaving plan mode (applies from the next step).", ""
			}
			return "Plan mode is already inactive.", ""
		}
	}

	outcome := c.Set(agent, true)
	reply = "Entering plan mode (applies from the next step). Use /plan off to leave."
	if outcome == Committed {
		reply = "Plan mode on. Use /plan off to leave."
	}
	return reply, arg
}

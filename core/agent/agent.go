// Package agent is a Go port of packages/core/agent from DeepSeek Harness:
// the Agent interface, a live-agent registry with creator ownership, and a
// process-local "initiator" scope — all with zero dependency on the concrete
// driver (core/agentloop), so the loop stays swappable.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package agent

import "go-harness/core/agentloop"

// Agent is the handle every plugin (a tool, a registry consumer, an
// orchestrator) programs against, mirroring the TS Agent interface's live
// surface. *agentloop.Agent satisfies it; a different concrete driver could
// too, without this package or its callers ever importing agentloop's
// driver internals beyond the seams it already exposes.
//
// The TS interface also carries `id`, `options`, `ctx` (an agent-scoped DI
// container), and a public inbox read/mutate surface. This port omits them:
// identity is the Registry's business (see registry.go) rather than the
// Agent's own, and agentloop has no scoped-DI or public-inbox equivalent to
// expose — see README.md.
type Agent interface {
	Status() string
	Session() agentloop.Session
	Followup(agentloop.Message)
	Steer(agentloop.Message)
	Inject(agentloop.Message)
	Cancel(agentloop.CancelCause, bool)
	WhenIdle()
}

var _ Agent = (*agentloop.Agent)(nil)

// Handle is the pair Create/Resume return: the disposer is a consumer
// capability, mirroring the TS AgentHandle — a caller holding only a bare
// Agent (e.g. from Registry.Get) cannot tear it down.
type Handle struct {
	Agent   Agent
	Dispose func()
}

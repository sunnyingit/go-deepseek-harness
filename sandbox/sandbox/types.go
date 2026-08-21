// Package sandbox is a Go port of packages/sandbox/sandbox (dsh-sandbox)
// from DeepSeek Harness: the platform-agnostic vocabulary a sandbox
// confinement backend is built against — the mode/policy types, the
// Provider contract, the writable-roots derivation, and the escalation
// state machine a denied call uses to ask for a wider mode.
//
// This package defines the contract; it confines nothing itself. See
// go-harness/sandbox/local for a real implementation.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package sandbox

import "fmt"

// Mode is the file-effect confinement level a command runs under.
type Mode string

const (
	// ReadOnly permits reads everywhere but writes only to essential sinks
	// (e.g. /dev/null) — no workspace or temp write access.
	ReadOnly Mode = "read-only"
	// WorkspaceWrite additionally permits writes under the workspace root
	// and the platform's temp directories.
	WorkspaceWrite Mode = "workspace-write"
	// DangerFullAccess bypasses confinement entirely. A Provider must never
	// be asked to Confine a DangerFullAccess policy — see Policy.
	DangerFullAccess Mode = "danger-full-access"
)

// Confined reports whether m is a mode a Provider can actually confine
// (i.e. not DangerFullAccess, which means "don't sandbox this at all").
func (m Mode) Confined() bool { return m == ReadOnly || m == WorkspaceWrite }

// ExecutionPolicy is the caller's intended policy for one command, before
// it's known whether that policy needs confining at all: Mode governs file
// writes, Network governs outbound networking — two independent axes, not
// escalation targets of each other (see EscalationTargets, which is
// Mode-only; a caller wanting network access sets Network directly on the
// Policy NewPolicy returns, e.g. via its own approval flow — see
// shell/toolbash's "network" tool parameter).
type ExecutionPolicy struct {
	Mode          Mode
	WorkspaceRoot string
	SessionID     string // "" when the call isn't tied to a session
	// Network grants outbound network access when true. Defaults to false —
	// every backend denies networking unless a Policy explicitly asks for
	// it (see sandbox/local's README for what each runner actually does
	// with this).
	Network bool
}

// Policy is an ExecutionPolicy already known to need confinement — its Mode
// is never DangerFullAccess. NewPolicy is the only way to construct one, so
// a Provider.Confine implementation never has to re-check this itself.
type Policy struct {
	ExecutionPolicy
}

// NewPolicy validates mode and returns a Policy a Provider can confine, with
// Network defaulting to false — set p.Network = true on the result to grant
// it. DangerFullAccess is rejected here, not silently downgraded — a caller
// that reaches this constructor with DangerFullAccess has a bug: that mode
// means "call the command directly, no Provider involved at all."
func NewPolicy(mode Mode, workspaceRoot, sessionID string) (Policy, error) {
	if !mode.Confined() {
		return Policy{}, fmt.Errorf("sandbox: %q is not a confinable mode (call the command directly instead)", mode)
	}
	return Policy{ExecutionPolicy{Mode: mode, WorkspaceRoot: workspaceRoot, SessionID: sessionID}}, nil
}

// Enforcement reports how completely a Provider actually confined the
// command it wrapped. Complete is false when the backend had to leave some
// gap open (e.g. a syscall class it can't restrict) — Notes explains which.
type Enforcement struct {
	Complete bool
	Notes    []string
}

// RunnerFailureRule lets a caller recognize a confined process's own
// failure (wrong exit code, a particular stderr fragment) as *the sandbox
// denying something* rather than an ordinary bug in the confined program.
type RunnerFailureRule struct {
	// Pattern is matched against the confined process's stderr.
	Pattern string
	// Meaning is a short, human-readable explanation surfaced when Pattern matches.
	Meaning string
}

// ConfinedArgv is a Provider's rewrite of one argv: Argv is what to spawn
// instead of the caller's original argv (the runner, its profile/flags, a
// separator, then the original command); Enforcement and the rule/signature
// lists let the caller interpret what happened after it runs.
type ConfinedArgv struct {
	Argv               []string
	Enforcement        Enforcement
	DenialSignatures   []string
	RunnerFailureRules []RunnerFailureRule
}

// UnavailableError is returned by Confine when no backend can confine this
// platform/policy at all. Providers must fail closed with this rather than
// ever returning the caller's argv unwrapped — "silent unconfined
// passthrough is forbidden."
type UnavailableError struct {
	Reason string
}

func (e *UnavailableError) Error() string { return "sandbox: unavailable: " + e.Reason }

// Provider confines one command's argv under policy, returning what to spawn
// instead of the original argv. It never runs anything itself.
type Provider interface {
	Confine(argv []string, policy Policy) (ConfinedArgv, error)
}

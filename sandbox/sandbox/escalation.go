// Port of escalation.ts: the state machine a denied call under a narrow
// mode uses to ask a human, once, to retry under a wider one — read-only ->
// workspace-write -> danger-full-access, never the reverse, and never
// automatic.
package sandbox

import (
	"context"
	"errors"
	"fmt"
)

// EscalationOutcome is an approver's answer to one escalation request.
type EscalationOutcome string

const (
	AllowedOnce EscalationOutcome = "allowed-once"
	Rejected    EscalationOutcome = "rejected"
	Cancelled   EscalationOutcome = "cancelled"
	Unavailable EscalationOutcome = "unavailable"
)

// EscalationTargets are the only modes a request may ever name — no mode
// escalates to ReadOnly, and DangerFullAccess never needs escalating to
// something wider than itself.
var EscalationTargets = []Mode{WorkspaceWrite, DangerFullAccess}

// widerModes maps a denied mode to the modes that would actually grant more
// than it does — used to validate a request's target is a real widening,
// not a lateral or narrowing move.
var widerModes = map[Mode][]Mode{
	ReadOnly:       {WorkspaceWrite, DangerFullAccess},
	WorkspaceWrite: {DangerFullAccess},
}

// EscalationRequest describes one attempt: a call denied under
// EffectiveMode wants to retry Subject once under RequestedMode, with
// Justification for whoever approves it.
type EscalationRequest struct {
	RequestedMode Mode
	Justification string
	EffectiveMode Mode
	Subject       string
}

// EscalationApprover is the human-in-the-loop boundary — this port's whole
// answer to TS's userQuestions approval service: one blocking call in place
// of a separate multi-provider interaction service, the same simplification
// plan/planmode.ReviewFunc already makes for plan-mode approval.
//
// TS's request also carries a generic caller-supplied `agent` value; this
// port drops it — nothing here needs to identify the calling agent beyond
// toolName/callID, and adding a type parameter for a field no caller in
// this codebase would populate isn't worth the API complexity.
type EscalationApprover func(ctx context.Context, toolName, callID, reason string) EscalationOutcome

// ValidateEscalationArgs checks a model-supplied escalation request's raw
// arguments before ApproveEscalation runs: requestedMode must name a real
// escalation target, and justification must be non-empty.
func ValidateEscalationArgs(requestedMode Mode, justification string) error {
	if justification == "" {
		return errors.New("sandbox: escalation requires a non-empty justification")
	}
	for _, m := range EscalationTargets {
		if m == requestedMode {
			return nil
		}
	}
	return fmt.Errorf("sandbox: %q is not an escalation target (want one of %v)", requestedMode, EscalationTargets)
}

// SandboxDenialMarker is the fixed text a tool result's content embeds when
// a call was denied under mode — a signal, matched literally by whatever
// renders the result, that this failure came from confinement and not the
// underlying command.
func SandboxDenialMarker(mode Mode) string {
	return fmt.Sprintf("[sandbox:%s-denied]", mode)
}

// EscalationHintMarker is embedded alongside SandboxDenialMarker to name
// what exactly could be retried with wider permissions.
func EscalationHintMarker(subject string) string {
	return fmt.Sprintf("[sandbox:escalate:%s]", subject)
}

// EscalationDeniedError is returned by ApproveEscalation whenever the retry
// must not happen: an explicit Rejected/Cancelled outcome, or Unavailable —
// which is also what a nil approver resolves to, matching the TS original's
// documented fallback ("missing approval support turns ask into denial").
type EscalationDeniedError struct{ Outcome EscalationOutcome }

func (e *EscalationDeniedError) Error() string { return "sandbox: escalation " + string(e.Outcome) }

// ApproveEscalation validates request against effectiveMode (it must name a
// mode strictly wider than the one that was denied), then asks approver.
// Returns the mode a retry should run under on AllowedOnce, or an error
// otherwise — *EscalationDeniedError for a real answer, a plain error for a
// malformed request.
func ApproveEscalation(ctx context.Context, request EscalationRequest, approver EscalationApprover, toolName, callID string) (Mode, error) {
	if err := ValidateEscalationArgs(request.RequestedMode, request.Justification); err != nil {
		return "", err
	}
	allowed := false
	for _, m := range widerModes[request.EffectiveMode] {
		if m == request.RequestedMode {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("sandbox: %q does not widen %q", request.RequestedMode, request.EffectiveMode)
	}

	if approver == nil {
		return "", &EscalationDeniedError{Outcome: Unavailable}
	}
	outcome := approver(ctx, toolName, callID, request.Justification)
	if outcome != AllowedOnce {
		return "", &EscalationDeniedError{Outcome: outcome}
	}
	return request.RequestedMode, nil
}

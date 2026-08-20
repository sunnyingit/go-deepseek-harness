package sandbox

import (
	"context"
	"errors"
	"testing"
)

func TestValidateEscalationArgsRequiresJustification(t *testing.T) {
	if err := ValidateEscalationArgs(WorkspaceWrite, ""); err == nil {
		t.Fatal("want an error for an empty justification")
	}
}

func TestValidateEscalationArgsRejectsNonTargetMode(t *testing.T) {
	if err := ValidateEscalationArgs(ReadOnly, "because"); err == nil {
		t.Fatal("want an error — read-only is never an escalation target")
	}
}

func TestValidateEscalationArgsAcceptsRealTargets(t *testing.T) {
	for _, mode := range EscalationTargets {
		if err := ValidateEscalationArgs(mode, "because"); err != nil {
			t.Fatalf("ValidateEscalationArgs(%s): %v", mode, err)
		}
	}
}

func TestApproveEscalationNilApproverIsUnavailable(t *testing.T) {
	req := EscalationRequest{RequestedMode: WorkspaceWrite, Justification: "need it", EffectiveMode: ReadOnly, Subject: "write config.yaml"}
	_, err := ApproveEscalation(context.Background(), req, nil, "write_file", "c1")
	var denied *EscalationDeniedError
	if !errors.As(err, &denied) || denied.Outcome != Unavailable {
		t.Fatalf("err = %v, want EscalationDeniedError{Unavailable}", err)
	}
}

func TestApproveEscalationAllowedOnceReturnsRequestedMode(t *testing.T) {
	req := EscalationRequest{RequestedMode: WorkspaceWrite, Justification: "need it", EffectiveMode: ReadOnly, Subject: "write config.yaml"}
	approver := EscalationApprover(func(context.Context, string, string, string) EscalationOutcome { return AllowedOnce })
	mode, err := ApproveEscalation(context.Background(), req, approver, "write_file", "c1")
	if err != nil || mode != WorkspaceWrite {
		t.Fatalf("mode=%v err=%v, want WorkspaceWrite, nil", mode, err)
	}
}

func TestApproveEscalationRejectedAndCancelled(t *testing.T) {
	for _, outcome := range []EscalationOutcome{Rejected, Cancelled} {
		req := EscalationRequest{RequestedMode: WorkspaceWrite, Justification: "need it", EffectiveMode: ReadOnly}
		approver := EscalationApprover(func(context.Context, string, string, string) EscalationOutcome { return outcome })
		_, err := ApproveEscalation(context.Background(), req, approver, "write_file", "c1")
		var denied *EscalationDeniedError
		if !errors.As(err, &denied) || denied.Outcome != outcome {
			t.Fatalf("outcome=%s: err = %v, want EscalationDeniedError{%s}", outcome, err, outcome)
		}
	}
}

func TestApproveEscalationRejectsNonWideningRequest(t *testing.T) {
	// Requesting read-only from an already-workspace-write call is not a
	// widening at all — must be rejected before the approver is even asked.
	called := false
	approver := EscalationApprover(func(context.Context, string, string, string) EscalationOutcome {
		called = true
		return AllowedOnce
	})
	req := EscalationRequest{RequestedMode: ReadOnly, Justification: "because", EffectiveMode: WorkspaceWrite}
	if _, err := ApproveEscalation(context.Background(), req, approver, "t", "c1"); err == nil {
		t.Fatal("want an error — read-only does not widen workspace-write")
	}
	if called {
		t.Fatal("approver must not be consulted for a malformed (non-widening) request")
	}
}

func TestSandboxDenialMarkerAndEscalationHintMarker(t *testing.T) {
	if got := SandboxDenialMarker(ReadOnly); got != "[sandbox:read-only-denied]" {
		t.Fatalf("marker = %q", got)
	}
	if got := EscalationHintMarker("write_file"); got != "[sandbox:escalate:write_file]" {
		t.Fatalf("marker = %q", got)
	}
}

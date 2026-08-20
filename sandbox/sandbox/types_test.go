package sandbox

import "testing"

func TestModeConfined(t *testing.T) {
	cases := map[Mode]bool{
		ReadOnly:         true,
		WorkspaceWrite:   true,
		DangerFullAccess: false,
	}
	for mode, want := range cases {
		if got := mode.Confined(); got != want {
			t.Errorf("%s.Confined() = %v, want %v", mode, got, want)
		}
	}
}

func TestNewPolicyRejectsDangerFullAccess(t *testing.T) {
	if _, err := NewPolicy(DangerFullAccess, "/ws", ""); err == nil {
		t.Fatal("want an error constructing a Policy with DangerFullAccess — that mode never reaches a Provider")
	}
}

func TestNewPolicyAcceptsConfinedModes(t *testing.T) {
	for _, mode := range []Mode{ReadOnly, WorkspaceWrite} {
		p, err := NewPolicy(mode, "/ws", "sess-1")
		if err != nil {
			t.Fatalf("NewPolicy(%s): %v", mode, err)
		}
		if p.Mode != mode || p.WorkspaceRoot != "/ws" || p.SessionID != "sess-1" {
			t.Fatalf("policy = %+v", p)
		}
	}
}

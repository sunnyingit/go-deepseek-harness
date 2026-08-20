package local

import (
	"errors"
	"testing"

	"go-harness/sandbox/sandbox"
)

// fakeRunner is a test double letting provider_test.go exercise selection
// and caching without depending on any real platform binary.
type fakeRunner struct {
	runnerName   string
	isAvailable  bool
	availCalls   int
	confineCalls int
}

func (f *fakeRunner) name() string { return f.runnerName }
func (f *fakeRunner) available() bool {
	f.availCalls++
	return f.isAvailable
}
func (f *fakeRunner) confine(argv []string, _ sandbox.Policy) (sandbox.ConfinedArgv, error) {
	f.confineCalls++
	return sandbox.ConfinedArgv{Argv: append([]string{"wrapped-by-" + f.runnerName}, argv...)}, nil
}

func mustPolicy(t *testing.T, mode sandbox.Mode, workspaceRoot string) sandbox.Policy {
	t.Helper()
	p, err := sandbox.NewPolicy(mode, workspaceRoot, "")
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return p
}

func TestProviderSelectsFirstAvailableCandidate(t *testing.T) {
	unavailable := &fakeRunner{runnerName: "a", isAvailable: false}
	available := &fakeRunner{runnerName: "b", isAvailable: true}
	p := &Provider{candidates: []runner{unavailable, available}}

	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	result, err := p.Confine([]string{"echo", "hi"}, policy)
	if err != nil {
		t.Fatalf("Confine: %v", err)
	}
	if result.Argv[0] != "wrapped-by-b" {
		t.Fatalf("argv = %v, want the available candidate to win", result.Argv)
	}
	if unavailable.confineCalls != 0 || available.confineCalls != 1 {
		t.Fatalf("unavailable.confineCalls=%d available.confineCalls=%d", unavailable.confineCalls, available.confineCalls)
	}
}

func TestProviderCachesTheResolvedRunner(t *testing.T) {
	r := &fakeRunner{runnerName: "a", isAvailable: true}
	p := &Provider{candidates: []runner{r}}
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")

	if _, err := p.Confine([]string{"echo"}, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Confine([]string{"echo"}, policy); err != nil {
		t.Fatal(err)
	}
	if r.availCalls != 1 {
		t.Fatalf("available() called %d times, want exactly 1 — selection must be cached", r.availCalls)
	}
	if r.confineCalls != 2 {
		t.Fatalf("confine() called %d times, want 2 — every call still dispatches", r.confineCalls)
	}
}

func TestProviderFailsClosedWhenNoCandidateIsAvailable(t *testing.T) {
	p := &Provider{candidates: []runner{&fakeRunner{runnerName: "a", isAvailable: false}}}
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	_, err := p.Confine([]string{"echo"}, policy)
	var unavail *sandbox.UnavailableError
	if !errors.As(err, &unavail) {
		t.Fatalf("err = %v, want *sandbox.UnavailableError", err)
	}
}

func TestProviderFailsClosedWithNoCandidatesAtAll(t *testing.T) {
	p := &Provider{candidates: nil} // the "no runner for this OS at all" case (e.g. Windows)
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	_, err := p.Confine([]string{"echo"}, policy)
	var unavail *sandbox.UnavailableError
	if !errors.As(err, &unavail) {
		t.Fatalf("err = %v, want *sandbox.UnavailableError", err)
	}
}

func TestProviderUnavailabilityIsAlsoCached(t *testing.T) {
	r := &fakeRunner{runnerName: "a", isAvailable: false}
	p := &Provider{candidates: []runner{r}}
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")

	p.Confine([]string{"echo"}, policy)
	p.Confine([]string{"echo"}, policy)
	if r.availCalls != 1 {
		t.Fatalf("available() called %d times, want exactly 1 — even a negative resolution is cached", r.availCalls)
	}
}

func TestNewPicksAPlatformDefaultCandidateList(t *testing.T) {
	// Just confirms New() doesn't panic and returns a usable Provider —
	// whether it can actually confine depends on this machine's OS/binaries.
	if New() == nil {
		t.Fatal("New() should return a non-nil Provider")
	}
}

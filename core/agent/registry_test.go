package agent

import (
	"errors"
	"sort"
	"testing"

	"go-harness/core/agentloop"
)

// stubAgent is a minimal Agent for registry/initiator tests that don't need
// a real running loop — just an identity to register and compare.
type stubAgent struct{ name string }

func (s *stubAgent) Status() string                     { return "idle" }
func (s *stubAgent) Session() agentloop.Session         { return nil }
func (s *stubAgent) Followup(agentloop.Message)         {}
func (s *stubAgent) Steer(agentloop.Message)            {}
func (s *stubAgent) Inject(agentloop.Message)           {}
func (s *stubAgent) Cancel(agentloop.CancelCause, bool) {}
func (s *stubAgent) WhenIdle()                          {}

var _ Agent = (*stubAgent)(nil)

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	a := &stubAgent{name: "a"}

	if _, err := r.Register("a", a, ""); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("a")
	if !ok || got != Agent(a) {
		t.Fatalf("Get = (%v, %v), want (a, true)", got, ok)
	}
}

func TestRegisterDuplicateIDFails(t *testing.T) {
	r := NewRegistry()
	r.Register("a", &stubAgent{}, "")

	_, err := r.Register("a", &stubAgent{}, "")
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
}

func TestDisposeRemovesEntry(t *testing.T) {
	r := NewRegistry()
	dispose, _ := r.Register("a", &stubAgent{}, "")
	dispose()

	if _, ok := r.Get("a"); ok {
		t.Fatal("agent still registered after dispose")
	}
}

func TestStaleDisposerCannotRemoveAReplacementEntry(t *testing.T) {
	r := NewRegistry()
	first := &stubAgent{name: "first"}
	disposeFirst, _ := r.Register("a", first, "")
	disposeFirst()

	second := &stubAgent{name: "second"}
	r.Register("a", second, "")

	// The stale disposer from the first registration must not remove the
	// second entry that happens to share the same id.
	disposeFirst()

	got, ok := r.Get("a")
	if !ok || got != Agent(second) {
		t.Fatalf("Get = (%v, %v), want the second agent still registered", got, ok)
	}
}

func TestRootsOnlyReturnsUnownedAgents(t *testing.T) {
	r := NewRegistry()
	r.Register("root", &stubAgent{}, "")
	r.Register("child", &stubAgent{}, "root")

	roots := r.Roots()
	if len(roots) != 1 || roots[0] != "root" {
		t.Fatalf("Roots = %v, want [root]", roots)
	}
}

func TestIsOwnedBy(t *testing.T) {
	r := NewRegistry()
	r.Register("root", &stubAgent{}, "")
	r.Register("child", &stubAgent{}, "root")

	if !r.IsOwnedBy("child", "root") {
		t.Fatal("want child owned by root")
	}
	if r.IsOwnedBy("child", "someone-else") {
		t.Fatal("want child NOT owned by an unrelated id")
	}
	if r.IsOwnedBy("root", "root") {
		t.Fatal("a root must not be its own owner")
	}
}

func TestListReturnsAllIDs(t *testing.T) {
	r := NewRegistry()
	r.Register("root", &stubAgent{}, "")
	r.Register("child", &stubAgent{}, "root")

	ids := r.List()
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "child" || ids[1] != "root" {
		t.Fatalf("List = %v, want [child root]", ids)
	}
}

func TestCreateUsesFactoryAndRegisters(t *testing.T) {
	r := NewRegistry()
	r.SetFactory(func(id string) (Agent, error) { return &stubAgent{name: id}, nil })

	handle, err := r.Create("root", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, ok := r.Get("root"); !ok || got != handle.Agent {
		t.Fatalf("Get after Create = (%v, %v)", got, ok)
	}

	handle.Dispose()
	if _, ok := r.Get("root"); ok {
		t.Fatal("agent still registered after handle.Dispose()")
	}
}

func TestCreateWithoutFactoryFails(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Create("root", ""); !errors.Is(err, ErrNoFactory) {
		t.Fatalf("err = %v, want ErrNoFactory", err)
	}
}

func TestCreateDuplicateIDDoesNotLeakTheConstructedAgent(t *testing.T) {
	r := NewRegistry()
	r.Register("root", &stubAgent{}, "")
	constructed := 0
	r.SetFactory(func(id string) (Agent, error) {
		constructed++
		return &stubAgent{name: id}, nil
	})

	if _, err := r.Create("root", ""); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
	if constructed != 1 {
		t.Fatalf("factory called %d times, want 1 (construction still happens; only registration fails)", constructed)
	}
}

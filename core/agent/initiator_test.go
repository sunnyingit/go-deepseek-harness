package agent

import (
	"context"
	"errors"
	"testing"
)

func TestCurrentInitiatorAbsentByDefault(t *testing.T) {
	if _, ok := CurrentInitiator(context.Background()); ok {
		t.Fatal("want no initiator on a bare context")
	}
}

func TestRequireInitiatorErrorsWhenAbsent(t *testing.T) {
	_, err := RequireInitiator(context.Background())
	if !errors.Is(err, ErrNoInitiator) {
		t.Fatalf("err = %v, want ErrNoInitiator", err)
	}
}

func TestWithInitiatorSetsIt(t *testing.T) {
	a := &stubAgent{name: "a"}
	ctx := WithInitiator(context.Background(), a)

	got, ok := CurrentInitiator(ctx)
	if !ok || got != Agent(a) {
		t.Fatalf("CurrentInitiator = (%v, %v), want (a, true)", got, ok)
	}
	if got, err := RequireInitiator(ctx); err != nil || got != Agent(a) {
		t.Fatalf("RequireInitiator = (%v, %v)", got, err)
	}
}

func TestWithoutInitiatorHidesAnInheritedOne(t *testing.T) {
	a := &stubAgent{name: "a"}
	ctx := WithInitiator(context.Background(), a)
	hidden := WithoutInitiator(ctx)

	if _, ok := CurrentInitiator(hidden); ok {
		t.Fatal("want no initiator visible under WithoutInitiator")
	}
	// The original context is untouched by deriving a hidden child from it.
	if got, ok := CurrentInitiator(ctx); !ok || got != Agent(a) {
		t.Fatalf("original ctx initiator = (%v, %v), want (a, true) unaffected", got, ok)
	}
}

func TestNestedWithInitiatorOverridesWithoutMutatingTheParent(t *testing.T) {
	parent := &stubAgent{name: "parent"}
	child := &stubAgent{name: "child"}

	parentCtx := WithInitiator(context.Background(), parent)
	childCtx := WithInitiator(parentCtx, child)

	if got, _ := CurrentInitiator(childCtx); got != Agent(child) {
		t.Fatalf("childCtx initiator = %v, want child", got)
	}
	if got, _ := CurrentInitiator(parentCtx); got != Agent(parent) {
		t.Fatalf("parentCtx initiator = %v, want parent — deriving childCtx must not mutate it", got)
	}
}

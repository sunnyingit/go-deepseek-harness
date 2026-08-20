package agent

import (
	"context"
	"errors"
)

// ErrNoInitiator is returned by RequireInitiator when none is set.
var ErrNoInitiator = errors.New("agent: no initiating agent is active")

type initiatorKey struct{}

// initiatorValue distinguishes "never set" (ctx.Value returns nil, the zero
// value) from "explicitly hidden" (WithoutInitiator) — both must read back
// as "no initiator", but only the latter should block a value inherited
// from a parent context from leaking back through.
type initiatorValue struct {
	agent Agent
	set   bool
}

// WithInitiator derives a context in which CurrentInitiator/RequireInitiator
// return agent. This is this port's answer to the TS AsyncLocalStorage-based
// initiator scope: Go has no ambient goroutine-local storage, so the
// idiomatic substitute is a context.Context value the caller threads
// explicitly — every function on the resulting call path takes ctx and
// passes it on, in place of TS's implicit propagation through the async
// chain. See README.md.
func WithInitiator(ctx context.Context, a Agent) context.Context {
	return context.WithValue(ctx, initiatorKey{}, initiatorValue{agent: a, set: true})
}

// WithoutInitiator derives a context that hides any initiator inherited
// from ctx, for unrelated process-local work that must not appear to run on
// behalf of whichever agent happens to be ambient.
func WithoutInitiator(ctx context.Context) context.Context {
	return context.WithValue(ctx, initiatorKey{}, initiatorValue{set: true})
}

// CurrentInitiator reads the inherited initiator without requiring one.
func CurrentInitiator(ctx context.Context) (Agent, bool) {
	v, _ := ctx.Value(initiatorKey{}).(initiatorValue)
	if !v.set || v.agent == nil {
		return nil, false
	}
	return v.agent, true
}

// RequireInitiator reads the inherited initiator or returns ErrNoInitiator.
func RequireInitiator(ctx context.Context) (Agent, error) {
	a, ok := CurrentInitiator(ctx)
	if !ok {
		return nil, ErrNoInitiator
	}
	return a, nil
}

// Package llmretry is a Go port of packages/llm/llm-retry from DeepSeek
// Harness: a provider-routed retry policy for core/agentloop's
// Hooks.RequestError extension point. What this port deliberately leaves
// out relative to the TS original — durable session retry events, retryId
// chains, the Cordis waterfall's multi-listener composition, the separate
// invariant companion — is documented in README.md.
package llmretry

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Mode selects a policy's retry-count behavior.
type Mode string

const (
	// ModeNormal retries only errors whose classified code is in
	// RetryableCodes, up to MaxRetries times.
	ModeNormal Mode = "normal"
	// ModeAlways retries every request failure, unboundedly, until the
	// step's context is cancelled — MaxRetries is ignored.
	ModeAlways Mode = "always"
)

// Backoff bounds the delay between attempts: doubling from InitialDelay up
// to MaxDelay, with symmetric jitter of +/- JitterRatio (0..1) applied on
// top of the exponential value before it is capped again.
type Backoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	JitterRatio  float64
}

// Policy configures one Hook.
type Policy struct {
	Mode Mode
	// RetryableCodes gates ModeNormal only — ModeAlways retries every
	// failure regardless of its classified code.
	RetryableCodes []string
	// MaxRetries bounds ModeNormal only: the number of retry attempts
	// allowed (so MaxRetries requests total is MaxRetries+1).
	MaxRetries int
	Backoff    Backoff
}

// DefaultRetryableCodes mirrors llm-retry's normal-mode default: the
// failure classes an adapter classifies as safely repeatable.
// EMPTY_RESPONSE is a degenerate provider completion that produced no
// durable content, so repeating it is safe the same way the other four
// transient-failure classes are. These strings match llm/deepseek's own
// Code vocabulary (see its RetryCode method).
var DefaultRetryableCodes = []string{"EMPTY_RESPONSE", "RATE_LIMIT", "SERVER", "TIMEOUT", "TRANSPORT"}

// NormalPolicy is llm-retry's normal-mode default: five retries, 500ms..10s
// bounded exponential backoff, 10% jitter, over DefaultRetryableCodes.
func NormalPolicy() Policy {
	return Policy{
		Mode:           ModeNormal,
		RetryableCodes: DefaultRetryableCodes,
		MaxRetries:     5,
		Backoff:        Backoff{InitialDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second, JitterRatio: 0.1},
	}
}

// AlwaysPolicy retries every request failure unboundedly under backoff,
// until the step's context is cancelled (turn cancellation, agent
// disposal). The caller is responsible for the cost/latency that implies —
// see README.md.
func AlwaysPolicy(backoff Backoff) Policy {
	return Policy{Mode: ModeAlways, Backoff: backoff}
}

// classifiable is implemented by an adapter's error type to expose a stable
// retry code and any provider-suggested delay, so this package classifies
// failures without depending on any one provider package. *llm/deepseek.Error
// implements it via RetryCode/ProviderRetryDelay; an error that doesn't
// implement it classifies as code "" (retryable only under ModeAlways).
type classifiable interface {
	RetryCode() string
	ProviderRetryDelay() time.Duration
}

// Internals overrides Hook's non-deterministic behavior for tests.
type Internals struct {
	// Random returns a sample in [0, 1); defaults to rand.Float64.
	Random func() float64
	// Sleep waits d, or returns false if ctx is done first; defaults to a
	// real timer raced against ctx.Done().
	Sleep func(ctx context.Context, d time.Duration) bool
}

// Hook builds a core/agentloop Hooks.RequestError implementation from
// policy. The returned func tracks retry counts per (turn, step) across
// calls, so build one Hook per Agent — core/agentloop already calls it from
// a single step() goroutine at a time, but the counts aren't meaningful
// shared across agents/sessions.
func Hook(policy Policy, internals ...Internals) func(ctx context.Context, turn, step int, err error) bool {
	var in Internals
	if len(internals) > 0 {
		in = internals[0]
	}
	random := in.Random
	if random == nil {
		random = rand.Float64
	}
	sleep := in.Sleep
	if sleep == nil {
		sleep = realSleep
	}

	var mu sync.Mutex
	counts := map[[2]int]int{}

	return func(ctx context.Context, turn, step int, err error) bool {
		code, providerDelay := classify(err)

		if policy.Mode == ModeNormal && !containsCode(policy.RetryableCodes, code) {
			return false
		}

		key := [2]int{turn, step}
		mu.Lock()
		counts[key]++
		retry := counts[key]
		mu.Unlock()

		if policy.Mode == ModeNormal && retry > policy.MaxRetries {
			return false
		}

		delay, ok := resolveDelay(policy, providerDelay, retry, random)
		if !ok {
			return false // provider delay exceeded MaxDelay under ModeNormal — give up rather than wait past it
		}
		return sleep(ctx, delay)
	}
}

func classify(err error) (code string, providerDelay time.Duration) {
	if ce, ok := err.(classifiable); ok {
		return ce.RetryCode(), ce.ProviderRetryDelay()
	}
	return "", 0
}

func containsCode(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// resolveDelay picks the wait before the next attempt: an in-bounds
// provider-suggested delay wins outright (no jitter added — the provider
// already knows best, matching llm-retry's own rule); one that exceeds
// Backoff.MaxDelay gives up under ModeNormal (ok=false) or falls back to
// the local backoff under ModeAlways, so that instruction can't stall
// unbounded retries forever; no suggestion at all always uses the local
// backoff.
func resolveDelay(policy Policy, providerDelay time.Duration, retry int, random func() float64) (delay time.Duration, ok bool) {
	if providerDelay > 0 {
		if providerDelay > policy.Backoff.MaxDelay {
			if policy.Mode == ModeNormal {
				return 0, false
			}
			return localDelay(policy.Backoff, retry, random), true
		}
		return providerDelay, true
	}
	return localDelay(policy.Backoff, retry, random), true
}

// localDelay is bounded exponential backoff — InitialDelay * 2^(retry-1),
// capped at MaxDelay — with symmetric jitter of +/- JitterRatio applied on
// top and capped again, matching llm-retry's own formula exactly. Computed
// in float64 (like the TS original's numbers) rather than integer duration
// arithmetic so a large retry count saturates to +Inf instead of
// overflowing int64 and wrapping negative.
func localDelay(b Backoff, retry int, random func() float64) time.Duration {
	exponent := retry - 1
	if exponent < 0 {
		exponent = 0
	}
	if exponent > 1024 { // matches llm-retry's own cap; unreachable in practice
		exponent = 1024
	}
	exponential := float64(b.InitialDelay) * math.Pow(2, float64(exponent))
	maxDelay := float64(b.MaxDelay)
	if exponential > maxDelay {
		exponential = maxDelay
	}
	jitter := 1 - b.JitterRatio + 2*b.JitterRatio*random()
	delay := exponential * jitter
	if delay > maxDelay {
		delay = maxDelay
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

func realSleep(ctx context.Context, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

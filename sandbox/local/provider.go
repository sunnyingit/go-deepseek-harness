// Package local is a Go port of packages/sandbox/sandbox-local
// (dsh-sandbox-local) from DeepSeek Harness: a sandbox.Provider that
// selects and caches one platform runner — macOS uses Seatbelt
// (sandbox-exec), Linux prefers bwrap, every other platform (including
// Windows) fails closed.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package local

import (
	"fmt"
	"runtime"
	"sync"

	"go-harness/sandbox/sandbox"
)

// runner is what one platform backend must provide: build the confined
// argv for a policy, and report whether it can actually run on this
// machine (the binary is on PATH, the platform matches).
type runner interface {
	name() string
	available() bool
	confine(argv []string, policy sandbox.Policy) (sandbox.ConfinedArgv, error)
}

// Provider selects and caches one platform runner on first use. Unsupported
// platforms, and platforms whose runner binary isn't installed, fail closed
// with *sandbox.UnavailableError — never a silent unconfined passthrough.
type Provider struct {
	mu         sync.Mutex
	resolved   bool
	active     runner // nil once resolved if nothing on this machine can confine
	candidates []runner
}

var _ sandbox.Provider = (*Provider)(nil)

// New returns a Provider with the default platform-ordered candidate list.
func New() *Provider {
	return &Provider{candidates: defaultCandidates()}
}

func defaultCandidates() []runner {
	switch runtime.GOOS {
	case "darwin":
		return []runner{newSeatbeltRunner()}
	case "linux":
		return []runner{newBwrapRunner()}
	default:
		// Windows's real backend needs win32 ACL/token FFI this port
		// doesn't carry (see README.md) — no candidate means Confine fails
		// closed below, exactly the documented "unsupported platforms fail
		// closed" behavior, with no special-casing needed here.
		return nil
	}
}

// Confine resolves (and caches) the active runner, then delegates.
func (p *Provider) Confine(argv []string, policy sandbox.Policy) (sandbox.ConfinedArgv, error) {
	r, err := p.resolve()
	if err != nil {
		return sandbox.ConfinedArgv{}, err
	}
	return r.confine(argv, policy)
}

// resolve picks the first available candidate once and reuses it — "selects
// and caches one platform runner." A machine's runner availability can't
// change mid-process (the binary doesn't get installed or the OS doesn't
// change), so re-probing on every call would only add cost, never accuracy.
func (p *Provider) resolve() (runner, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resolved {
		if p.active == nil {
			return nil, unavailable()
		}
		return p.active, nil
	}
	p.resolved = true
	for _, c := range p.candidates {
		if c.available() {
			p.active = c
			return c, nil
		}
	}
	return nil, unavailable()
}

func unavailable() error {
	return &sandbox.UnavailableError{Reason: fmt.Sprintf("no sandbox runner available on %s", runtime.GOOS)}
}

package agent

import (
	"fmt"
	"sync"
)

// ErrDuplicateID is returned by Register/Create when id is already live.
var ErrDuplicateID = fmt.Errorf("agent: id already registered")

// ErrNoFactory is returned by Create when no factory is set.
var ErrNoFactory = fmt.Errorf("agent: no factory registered")

// Factory constructs a fresh Agent for the given id. A concrete driver
// package (like agentloop) supplies one by wrapping its own constructor;
// mirrors the TS AgentFactory seam that keeps creation off the Agent
// interface itself.
type Factory func(id string) (Agent, error)

type entry struct {
	agent Agent
	owner string // "" marks a root — created with no owning parent
}

// Registry tracks live agents by an externally supplied id, plus creator
// ownership, mirroring ctx.agents (packages/core/agent). It has no Cordis
// fiber to tie disposal to, so callers call the Dispose/unregister func
// returned by Register/Create explicitly instead of relying on ambient
// teardown — see README.md.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]entry
	factory Factory
}

func NewRegistry() *Registry {
	return &Registry{entries: map[string]entry{}}
}

// Register records an already-constructed agent under id, optionally owned
// by ownerID ("" for a root). The returned func is the disposer capability:
// only whoever called Register can unregister this exact entry — a later
// caller that registered a different agent under the same id (after this
// one was already removed) is never accidentally torn down by a stale
// disposer, mirroring the TS registry's stale-capability guard.
func (r *Registry) Register(id string, a Agent, ownerID string) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[id]; exists {
		return nil, fmt.Errorf("%w: %q", ErrDuplicateID, id)
	}
	r.entries[id] = entry{agent: a, owner: ownerID}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if cur, ok := r.entries[id]; ok && cur.agent == a {
			delete(r.entries, id)
		}
	}, nil
}

// SetFactory registers the creation factory Create uses.
func (r *Registry) SetFactory(f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factory = f
}

// Create builds a fresh agent through the registered Factory and registers
// it under id, owned by ownerID ("" for a root). It rolls the construction
// back (never registers) if registration fails, e.g. on a duplicate id.
func (r *Registry) Create(id string, ownerID string) (Handle, error) {
	r.mu.RLock()
	factory := r.factory
	r.mu.RUnlock()
	if factory == nil {
		return Handle{}, ErrNoFactory
	}
	a, err := factory(id)
	if err != nil {
		return Handle{}, err
	}
	dispose, err := r.Register(id, a, ownerID)
	if err != nil {
		return Handle{}, err
	}
	return Handle{Agent: a, Dispose: dispose}, nil
}

// Get returns the live agent registered under id, if any.
func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e.agent, ok
}

// List returns every live agent id, in no particular order.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	return ids
}

// Roots returns the ids of live agents created with no owning parent.
func (r *Registry) Roots() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var ids []string
	for id, e := range r.entries {
		if e.owner == "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// IsOwnedBy reports whether the live entry at id was registered with
// exactly ownerID as its owner — runtime ownership, independent of any
// durable lineage the sessions themselves might separately record.
func (r *Registry) IsOwnedBy(id, ownerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return ok && e.owner == ownerID
}

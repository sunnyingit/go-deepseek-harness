package skill

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// runtimeProviderName is reserved: RegisterProvider rejects a provider
// using it, and Register always stamps it onto embedded skills.
const runtimeProviderName = "runtime"

// runtimeRank is the fixed Rank embedded skills sort at — a real provider's
// own candidates can still outrank (or be outranked by) them; nothing about
// "runtime" gives an embedded skill automatic priority.
const runtimeRank = 250

// defaultMaxCacheEntries bounds Get's per-CWD candidate cache. TS's default
// is 128; this port keeps the same number without reimplementing real LRU
// eviction — see README.md.
const defaultMaxCacheEntries = 128

var kebabCasePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validateName(name string) error {
	if !kebabCasePattern.MatchString(name) {
		return fmt.Errorf("skill: %q is not a valid skill name (want lowercase kebab-case, e.g. \"my-skill\")", name)
	}
	return nil
}

type providerEntry struct {
	provider Provider
	order    int
	cancel   context.CancelFunc
}

// indexedCandidate is one candidate plus what collect's sort/dedup needs;
// isRuntime distinguishes an embedded skill from a real provider's
// candidate without relying on Rank/order values that a provider could
// coincidentally also produce.
type indexedCandidate struct {
	candidate     Candidate
	providerOrder int
	localOrder    int
	isRuntime     bool
}

// Registry discovers and loads skills across registered providers plus
// embedded "runtime" skills — the collect/dedup/winning-candidate algorithm
// ported from index.ts, minus the Cordis scope/layer system: this port has
// one global registry, not a per-agent-scope chain shadowed by nearness.
// See README.md.
type Registry struct {
	mu              sync.Mutex
	nextOrder       int
	providers       []*providerEntry
	runtime         map[string]Definition
	cache           map[string][]indexedCandidate
	maxCacheEntries int

	changeMu        sync.Mutex
	changeObservers []func()
}

func NewRegistry() *Registry {
	return &Registry{
		runtime:         map[string]Definition{},
		cache:           map[string][]indexedCandidate{},
		maxCacheEntries: defaultMaxCacheEntries,
	}
}

// Register adds an embedded "runtime" skill. A duplicate name is tolerated,
// not an error — it's a no-op that returns a disposer doing nothing,
// matching the TS original ("duplicates log warning" rather than fail);
// this port has no logger to warn through, so silence is the whole
// difference.
func (r *Registry) Register(reg Registration) (func(), error) {
	if err := validateName(reg.Name); err != nil {
		return nil, err
	}
	if reg.Description == "" {
		return nil, fmt.Errorf("skill: %q must have a non-empty description", reg.Name)
	}
	policy := InvocationPolicy{ModelInvocable: true, UserInvocable: true}
	if reg.Invocation != nil {
		policy = *reg.Invocation
	}

	r.mu.Lock()
	if _, exists := r.runtime[reg.Name]; exists {
		r.mu.Unlock()
		return func() {}, nil
	}
	r.runtime[reg.Name] = Definition{
		Summary: Summary{
			Name: reg.Name, Description: reg.Description, WhenToUse: reg.WhenToUse,
			Invocation: policy, Provider: runtimeProviderName, ResourceBase: reg.ResourceBase,
		},
		Content: reg.Content, Path: reg.Path, Metadata: reg.Metadata,
	}
	r.invalidateCacheLocked()
	r.mu.Unlock()
	r.notifyChange()

	return func() {
		r.mu.Lock()
		delete(r.runtime, reg.Name)
		r.invalidateCacheLocked()
		r.mu.Unlock()
		r.notifyChange()
	}, nil
}

// RegisterProvider runs factory synchronously (mirroring the TS original's
// synchronous-during-apply construction) and registers the Provider it
// returns. A duplicate provider name — including the reserved "runtime" —
// is rejected outright, unlike Register's tolerant duplicate-skill handling.
func (r *Registry) RegisterProvider(factory func(Control) (Provider, error)) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	control := Control{Ctx: ctx, Invalidate: func() {
		r.mu.Lock()
		r.invalidateCacheLocked()
		r.mu.Unlock()
	}}

	provider, err := factory(control)
	if err != nil {
		cancel()
		return nil, err
	}
	if provider.Name() == runtimeProviderName {
		cancel()
		return nil, fmt.Errorf("skill: provider name %q is reserved", runtimeProviderName)
	}

	r.mu.Lock()
	for _, pe := range r.providers {
		if pe.provider.Name() == provider.Name() {
			r.mu.Unlock()
			cancel()
			return nil, fmt.Errorf("skill: provider %q is already registered", provider.Name())
		}
	}
	r.nextOrder++
	entry := &providerEntry{provider: provider, order: r.nextOrder, cancel: cancel}
	r.providers = append(r.providers, entry)
	r.invalidateCacheLocked()
	r.mu.Unlock()
	r.notifyChange()

	return func() {
		r.mu.Lock()
		for i, pe := range r.providers {
			if pe == entry {
				r.providers = append(r.providers[:i:i], r.providers[i+1:]...)
				break
			}
		}
		r.invalidateCacheLocked()
		r.mu.Unlock()
		cancel()
		r.notifyChange()
	}, nil
}

// OnChange registers an observer notified after any Register/RegisterProvider
// call or disposal — an unfiltered "something changed" signal, mirroring
// the `skills/change` event: it carries no diff, callers refetch Snapshot
// themselves. The returned func unregisters it.
func (r *Registry) OnChange(fn func()) func() {
	r.changeMu.Lock()
	defer r.changeMu.Unlock()
	r.changeObservers = append(r.changeObservers, fn)
	idx := len(r.changeObservers) - 1
	return func() {
		r.changeMu.Lock()
		defer r.changeMu.Unlock()
		r.changeObservers[idx] = nil
	}
}

func (r *Registry) notifyChange() {
	r.changeMu.Lock()
	observers := append([]func(){}, r.changeObservers...)
	r.changeMu.Unlock()
	for _, obs := range observers {
		if obs == nil {
			continue
		}
		notifyOneChange(obs)
	}
}

func notifyOneChange(obs func()) {
	defer func() { recover() }()
	obs()
}

// invalidateCacheLocked clears the candidate cache. Must be called with
// r.mu held.
func (r *Registry) invalidateCacheLocked() {
	r.cache = map[string][]indexedCandidate{}
}

// collect runs discovery fresh: every runtime skill plus every registered
// provider's List, sorted by (Rank, provider registration order,
// within-provider order) and deduplicated by name (first occurrence wins —
// see the package doc for why runtime's fixed Rank doesn't mean it always
// wins). Never reads or writes the cache; Snapshot/List always call this
// directly so their result is always fresh, matching the TS original.
func (r *Registry) collect(ctx context.Context, opts LookupOptions) ([]indexedCandidate, bool, error) {
	var all []indexedCandidate

	r.mu.Lock()
	names := make([]string, 0, len(r.runtime))
	for name := range r.runtime {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def := r.runtime[name]
		all = append(all, indexedCandidate{
			candidate:     Candidate{Summary: def.Summary, Rank: runtimeRank, Locator: def.Name, Path: def.Path, Metadata: def.Metadata},
			providerOrder: -1, localOrder: 0, isRuntime: true,
		})
	}
	providers := append([]*providerEntry{}, r.providers...)
	r.mu.Unlock()

	complete := true
	for _, pe := range providers {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		obs, err := pe.provider.List(ctx, opts)
		if err != nil {
			complete = false
			continue
		}
		if !obs.Complete {
			complete = false
		}
		for i, c := range obs.Candidates {
			// The registry stamps Provider authoritatively from the
			// provider that actually produced this candidate — the same
			// treatment runtime skills already get (Provider:
			// runtimeProviderName) — rather than trusting each provider to
			// self-report its own name correctly on every candidate.
			c.Provider = pe.provider.Name()
			all = append(all, indexedCandidate{candidate: c, providerOrder: pe.order, localOrder: i})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].candidate.Rank != all[j].candidate.Rank {
			return all[i].candidate.Rank < all[j].candidate.Rank
		}
		if all[i].providerOrder != all[j].providerOrder {
			return all[i].providerOrder < all[j].providerOrder
		}
		return all[i].localOrder < all[j].localOrder
	})

	seen := map[string]bool{}
	winners := make([]indexedCandidate, 0, len(all))
	for _, ic := range all {
		if seen[ic.candidate.Name] {
			continue
		}
		seen[ic.candidate.Name] = true
		winners = append(winners, ic)
	}
	return winners, complete, nil
}

// cachedCollect is collect with a cache in front, keyed by CWD: Get uses
// this (resolving which provider owns a name shouldn't force a fresh
// discovery on every single load), Snapshot/List don't. A result is cached
// only when complete — an incomplete discovery must not poison a later
// call into thinking it already knows the full picture.
func (r *Registry) cachedCollect(ctx context.Context, opts LookupOptions) ([]indexedCandidate, bool, error) {
	r.mu.Lock()
	if cached, ok := r.cache[opts.CWD]; ok {
		r.mu.Unlock()
		return cached, true, nil
	}
	r.mu.Unlock()

	winners, complete, err := r.collect(ctx, opts)
	if err != nil {
		return nil, false, err
	}
	if complete {
		r.mu.Lock()
		if len(r.cache) < r.maxCacheEntries {
			r.cache[opts.CWD] = winners
		}
		r.mu.Unlock()
	}
	return winners, complete, nil
}

// Snapshot returns every winning skill visible right now, plus whether
// every provider actually completed discovery. Always fresh — never served
// from cache.
func (r *Registry) Snapshot(ctx context.Context, opts LookupOptions) (Snapshot, error) {
	winners, complete, err := r.collect(ctx, opts)
	if err != nil {
		return Snapshot{}, err
	}
	summaries := make([]Summary, len(winners))
	for i, w := range winners {
		summaries[i] = w.candidate.Summary
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return Snapshot{Skills: summaries, Complete: complete}, nil
}

// List is Snapshot's Skills field alone — the common case when a caller
// doesn't need the completeness flag.
func (r *Registry) List(ctx context.Context, opts LookupOptions) ([]Summary, error) {
	snap, err := r.Snapshot(ctx, opts)
	if err != nil {
		return nil, err
	}
	return snap.Skills, nil
}

// Get loads name's full Definition: the winning candidate's own provider
// (or the stored Definition directly, for a runtime skill) supplies the
// body. Returns (nil, nil) — not an error — when name has no winning
// candidate, or when the provider's returned Definition doesn't actually
// match the candidate it was asked for (a stale result; Get invalidates the
// cache so the next call re-discovers instead of returning it).
func (r *Registry) Get(ctx context.Context, name string, opts LookupOptions) (*Definition, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	winners, _, err := r.cachedCollect(ctx, opts)
	if err != nil {
		return nil, err
	}

	var winner *indexedCandidate
	for i := range winners {
		if winners[i].candidate.Name == name {
			winner = &winners[i]
			break
		}
	}
	if winner == nil {
		return nil, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if winner.isRuntime {
		r.mu.Lock()
		def, ok := r.runtime[name]
		r.mu.Unlock()
		if !ok {
			return nil, nil
		}
		return &def, nil
	}

	provider := r.providerByOrder(winner.providerOrder)
	if provider == nil {
		return nil, nil
	}
	def, err := provider.Get(ctx, winner.candidate, opts)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, nil
	}
	if def.Name != name {
		r.mu.Lock()
		r.invalidateCacheLocked()
		r.mu.Unlock()
		return nil, nil
	}
	return def, nil
}

func (r *Registry) providerByOrder(order int) Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pe := range r.providers {
		if pe.order == order {
			return pe.provider
		}
	}
	return nil
}

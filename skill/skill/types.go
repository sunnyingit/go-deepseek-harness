// Package skill is a Go port of packages/skill/skill (dsh-skill) from
// DeepSeek Harness: a registry for discovering and loading "skills" —
// named, providered instruction bundles an agent or a user can invoke —
// through a pluggable Provider model, plus embedded "runtime" skills
// registered directly in code.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package skill

import "context"

// InvocationPolicy controls who may invoke a skill: a model deciding for
// itself, a user issuing an explicit command, both, or (rare) neither —
// e.g. a skill still being staged.
type InvocationPolicy struct {
	ModelInvocable bool
	UserInvocable  bool
}

// ResourceBaseKind is ResourceBase's discriminant.
type ResourceBaseKind string

const (
	ResourceDirectory ResourceBaseKind = "directory"
	ResourceURL       ResourceBaseKind = "url"
	ResourceOpaque    ResourceBaseKind = "opaque"
)

// ResourceBase tells a consumer how to resolve relative resources a skill's
// body mentions: a filesystem directory, a base URL, or a free-text
// description when neither structured form applies.
type ResourceBase struct {
	Kind        ResourceBaseKind
	Path        string // Kind == ResourceDirectory
	URL         string // Kind == ResourceURL
	Description string // Kind == ResourceOpaque
}

// Summary is what discovery returns: enough to decide whether a skill is
// worth loading, not its full instructions.
type Summary struct {
	Name        string
	Description string
	WhenToUse   string
	Invocation  InvocationPolicy
	// Provider is stamped by the Registry itself when a candidate is
	// collected — "runtime" for an embedded skill, else the Provider.Name()
	// that produced it — never trusted from the Candidate a Provider.List
	// call returns, so a provider can't misreport (or forget to set) whose
	// candidate this is.
	Provider     string
	ResourceBase *ResourceBase
}

// Candidate is one provider's offer for a name: a Summary plus what the
// registry needs to break a same-name tie (Rank, then registration order,
// then within-provider order — see collect() in registry.go) and to later
// ask the same provider to load the full Definition (Locator).
type Candidate struct {
	Summary
	Rank     int
	Locator  any
	Path     string
	Metadata map[string]any
}

// Definition is a skill's full, loaded form: a Summary plus the verbatim
// instruction body a consumer renders (see RenderSkillContent).
type Definition struct {
	Summary
	Content  string
	Path     string
	Metadata map[string]any
}

// Registration is what Register accepts for an embedded "runtime" skill: a
// Definition without Invocation/Provider, since the registry fills both in
// (Provider is always "runtime"; Invocation defaults to
// {ModelInvocable: true, UserInvocable: true} when nil).
type Registration struct {
	Name         string
	Description  string
	WhenToUse    string
	Content      string
	ResourceBase *ResourceBase
	Path         string
	Metadata     map[string]any
	Invocation   *InvocationPolicy
}

// LookupOptions scopes one discovery/load call. TS's SkillLookupOptions
// also carries a per-call AbortSignal; this port uses ctx.Context for that
// instead, passed alongside opts rather than inside it — Go convention.
type LookupOptions struct {
	CWD string
}

// Snapshot is one point-in-time discovery result.
type Snapshot struct {
	Skills []Summary
	// Complete is false when any provider's List call errored or reported
	// incomplete discovery — a caller should retain its last-good Snapshot
	// rather than treat this one as authoritative.
	Complete bool
}

// ProviderObservation is what Provider.List returns: candidates plus
// whether discovery actually finished. TS allows a provider to return a
// bare array (implying complete) or this observation shape; this port
// always uses the observation shape — a provider that's always complete
// just sets Complete: true, one less type for a Go caller to switch on.
type ProviderObservation struct {
	Candidates []Candidate
	Complete   bool
}

// Control is handed to a provider factory by RegisterProvider: Ctx cancels
// when the provider is unregistered, and Invalidate clears the registry's
// cached candidate collection while this provider is still registered —
// there is no TTL; a provider that knows its own results changed must call
// this itself.
type Control struct {
	Ctx        context.Context
	Invalidate func()
}

// Provider discovers and loads skills from one source (local files, HTTP,
// embedded data, ...).
type Provider interface {
	Name() string
	List(ctx context.Context, opts LookupOptions) (ProviderObservation, error)
	// Get loads the full Definition for candidate, previously returned by
	// this same provider's List. Providers are not asked to cache this —
	// the registry re-calls Get on every Registry.Get, not just the first.
	Get(ctx context.Context, candidate Candidate, opts LookupOptions) (*Definition, error)
}

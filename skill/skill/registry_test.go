package skill

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeProvider is a test double whose List/Get behavior a test controls
// directly, and which counts List calls to verify caching behavior.
type fakeProvider struct {
	mu         sync.Mutex
	nameStr    string
	candidates []Candidate
	complete   bool
	listErr    error
	listCalls  int
	getFunc    func(Candidate) (*Definition, error)
}

func (f *fakeProvider) Name() string { return f.nameStr }

func (f *fakeProvider) List(ctx context.Context, opts LookupOptions) (ProviderObservation, error) {
	f.mu.Lock()
	f.listCalls++
	f.mu.Unlock()
	if f.listErr != nil {
		return ProviderObservation{}, f.listErr
	}
	return ProviderObservation{Candidates: f.candidates, Complete: f.complete}, nil
}

func (f *fakeProvider) Get(ctx context.Context, candidate Candidate, opts LookupOptions) (*Definition, error) {
	if f.getFunc != nil {
		return f.getFunc(candidate)
	}
	return &Definition{Summary: candidate.Summary, Content: "body of " + candidate.Name}, nil
}

func (f *fakeProvider) listCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls
}

func registerFake(t *testing.T, r *Registry, p *fakeProvider) func() {
	t.Helper()
	dispose, err := r.RegisterProvider(func(Control) (Provider, error) { return p, nil })
	if err != nil {
		t.Fatalf("RegisterProvider(%s): %v", p.nameStr, err)
	}
	return dispose
}

func basicRegistration(name string) Registration {
	return Registration{Name: name, Description: "desc of " + name, Content: "content of " + name}
}

// ---- Register (embedded runtime skills) -----------------------------------

func TestRegisterValidatesKebabCaseName(t *testing.T) {
	r := NewRegistry()
	for _, bad := range []string{"MySkill", "my_skill", "my skill", "-my-skill", "my-skill-", ""} {
		if _, err := r.Register(basicRegistration(bad)); err == nil {
			t.Errorf("Register(%q) should have failed kebab-case validation", bad)
		}
	}
}

func TestRegisterRequiresDescription(t *testing.T) {
	r := NewRegistry()
	reg := basicRegistration("my-skill")
	reg.Description = ""
	if _, err := r.Register(reg); err == nil {
		t.Fatal("want an error for an empty description")
	}
}

func TestRegisterDefaultInvocationPolicy(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Register(basicRegistration("my-skill")); err != nil {
		t.Fatal(err)
	}
	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("Get: def=%v err=%v", def, err)
	}
	if !def.Invocation.ModelInvocable || !def.Invocation.UserInvocable {
		t.Fatalf("policy = %+v, want both true by default", def.Invocation)
	}
	if def.Provider != runtimeProviderName {
		t.Fatalf("provider = %q, want %q", def.Provider, runtimeProviderName)
	}
}

func TestRegisterDuplicateNameIsANoop(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Register(basicRegistration("my-skill")); err != nil {
		t.Fatal(err)
	}
	reg2 := basicRegistration("my-skill")
	reg2.Content = "second registration, should be ignored"
	dispose, err := r.Register(reg2)
	if err != nil {
		t.Fatalf("duplicate Register should not error: %v", err)
	}
	dispose() // must not panic and must not remove the FIRST registration

	def, _ := r.Get(context.Background(), "my-skill", LookupOptions{})
	if def == nil || def.Content != "content of my-skill" {
		t.Fatalf("def = %+v, want the first registration to have won and survived the second's dispose", def)
	}
}

func TestRegisterDisposeRemoves(t *testing.T) {
	r := NewRegistry()
	dispose, err := r.Register(basicRegistration("my-skill"))
	if err != nil {
		t.Fatal(err)
	}
	dispose()
	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil || def != nil {
		t.Fatalf("def = %v, err = %v, want nil, nil after dispose", def, err)
	}
}

// ---- RegisterProvider -------------------------------------------------

func TestRegisterProviderRejectsReservedName(t *testing.T) {
	r := NewRegistry()
	_, err := r.RegisterProvider(func(Control) (Provider, error) {
		return &fakeProvider{nameStr: "runtime"}, nil
	})
	if err == nil {
		t.Fatal("want an error registering a provider named \"runtime\"")
	}
}

func TestRegisterProviderRejectsDuplicateName(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "files", complete: true})
	_, err := r.RegisterProvider(func(Control) (Provider, error) {
		return &fakeProvider{nameStr: "files"}, nil
	})
	if err == nil {
		t.Fatal("want an error registering a duplicate provider name")
	}
}

func TestRegisterProviderPropagatesFactoryError(t *testing.T) {
	r := NewRegistry()
	_, err := r.RegisterProvider(func(Control) (Provider, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("want the factory's error propagated")
	}
}

func TestRegisterProviderControlCtxCancelsOnDispose(t *testing.T) {
	r := NewRegistry()
	var ctx context.Context
	dispose, err := r.RegisterProvider(func(c Control) (Provider, error) {
		ctx = c.Ctx
		return &fakeProvider{nameStr: "files", complete: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("ctx should not be cancelled before dispose")
	}
	dispose()
	if ctx.Err() == nil {
		t.Fatal("ctx should be cancelled after dispose")
	}
}

// ---- collect / List / Snapshot ordering ------------------------------------

func TestListMergesRuntimeAndProviderSortedByName(t *testing.T) {
	r := NewRegistry()
	r.Register(basicRegistration("zzz-skill"))
	registerFake(t, r, &fakeProvider{
		nameStr:  "files",
		complete: true,
		candidates: []Candidate{
			{Summary: Summary{Name: "aaa-skill", Description: "d"}, Rank: 100},
		},
	})
	skills, err := r.List(context.Background(), LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0].Name != "aaa-skill" || skills[1].Name != "zzz-skill" {
		t.Fatalf("skills = %+v, want sorted [aaa-skill zzz-skill]", skills)
	}
}

func TestListLowerRankWinsOverRuntimeSameName(t *testing.T) {
	r := NewRegistry()
	r.Register(basicRegistration("shared-name")) // runtime rank is fixed at 250
	registerFake(t, r, &fakeProvider{
		nameStr:  "files",
		complete: true,
		candidates: []Candidate{
			{Summary: Summary{Name: "shared-name", Description: "from provider"}, Rank: 100},
		},
	})
	def, err := r.Get(context.Background(), "shared-name", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v", def, err)
	}
	if def.Provider != "files" {
		t.Fatalf("provider = %q, want the lower-rank (100) provider candidate to win over runtime (250)", def.Provider)
	}
}

func TestListProviderRegistrationOrderTiebreak(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "first", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "shared", Description: "d"}, Rank: 100},
	}})
	registerFake(t, r, &fakeProvider{nameStr: "second", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "shared", Description: "d"}, Rank: 100},
	}})
	def, err := r.Get(context.Background(), "shared", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v", def, err)
	}
	if def.Provider != "first" {
		t.Fatalf("provider = %q, want the first-registered provider to win a same-rank tie", def.Provider)
	}
}

func TestListWithinProviderLocalOrderTiebreak(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "files", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "dup", Description: "first"}, Rank: 100},
		{Summary: Summary{Name: "dup", Description: "second"}, Rank: 100},
	}})
	skills, err := r.List(context.Background(), LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Description != "first" {
		t.Fatalf("skills = %+v, want the first same-provider candidate to win", skills)
	}
}

// ---- Snapshot completeness --------------------------------------------

func TestSnapshotCompleteFalseOnProviderError(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "files", listErr: errors.New("network down")})
	snap, err := r.Snapshot(context.Background(), LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Complete {
		t.Fatal("want Complete=false when a provider's List call errors")
	}
}

func TestSnapshotCompleteFalseOnIncompleteObservation(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "files", complete: false, candidates: []Candidate{
		{Summary: Summary{Name: "partial", Description: "d"}, Rank: 100},
	}})
	snap, err := r.Snapshot(context.Background(), LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Complete {
		t.Fatal("want Complete=false when a provider reports incomplete discovery")
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("skills = %+v, want the partial candidates still surfaced", snap.Skills)
	}
}

func TestSnapshotIsAlwaysFresh(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{nameStr: "files", complete: true}
	registerFake(t, r, fp)

	snap1, _ := r.Snapshot(context.Background(), LookupOptions{})
	if len(snap1.Skills) != 0 {
		t.Fatalf("snap1 = %+v, want empty", snap1)
	}

	fp.mu.Lock()
	fp.candidates = []Candidate{{Summary: Summary{Name: "new-skill", Description: "d"}, Rank: 100}}
	fp.mu.Unlock()

	snap2, _ := r.Snapshot(context.Background(), LookupOptions{})
	if len(snap2.Skills) != 1 || snap2.Skills[0].Name != "new-skill" {
		t.Fatalf("snap2 = %+v, want the newly added candidate reflected immediately (Snapshot is never cached)", snap2)
	}
}

// ---- Get --------------------------------------------------------------

func TestGetUnknownNameReturnsNilNilNoError(t *testing.T) {
	r := NewRegistry()
	def, err := r.Get(context.Background(), "does-not-exist", LookupOptions{})
	if err != nil || def != nil {
		t.Fatalf("def=%v err=%v, want nil, nil", def, err)
	}
}

func TestGetInvalidNameIsAnError(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Get(context.Background(), "Not Kebab Case", LookupOptions{}); err == nil {
		t.Fatal("want an error for an invalid name")
	}
}

func TestGetDispatchesToTheWinningProvider(t *testing.T) {
	r := NewRegistry()
	registerFake(t, r, &fakeProvider{nameStr: "files", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "my-skill", Description: "d"}, Rank: 100, Locator: "loc-1"},
	}})
	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v", def, err)
	}
	if def.Content != "body of my-skill" {
		t.Fatalf("content = %q", def.Content)
	}
}

func TestGetRejectsNameMismatchAndInvalidatesCache(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{nameStr: "files", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "my-skill", Description: "d"}, Rank: 100},
	}}
	fp.getFunc = func(c Candidate) (*Definition, error) {
		return &Definition{Summary: Summary{Name: "wrong-name"}}, nil // stale/buggy provider
	}
	registerFake(t, r, fp)

	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if def != nil {
		t.Fatalf("def = %+v, want nil when the loaded Definition's name doesn't match the candidate", def)
	}
}

func TestGetCachesCandidateCollectionAcrossCalls(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{nameStr: "files", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "my-skill", Description: "d"}, Rank: 100},
	}}
	registerFake(t, r, fp)

	if _, err := r.Get(context.Background(), "my-skill", LookupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(context.Background(), "my-skill", LookupOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := fp.listCallCount(); got != 1 {
		t.Fatalf("provider.List called %d times, want 1 — Get should cache the candidate collection", got)
	}
}

func TestGetCacheDoesNotAffectSnapshotFreshness(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{nameStr: "files", complete: true, candidates: []Candidate{
		{Summary: Summary{Name: "my-skill", Description: "d"}, Rank: 100},
	}}
	registerFake(t, r, fp)

	r.Get(context.Background(), "my-skill", LookupOptions{}) // populates the Get-only cache

	fp.mu.Lock()
	fp.candidates = append(fp.candidates, Candidate{Summary: Summary{Name: "another", Description: "d"}, Rank: 100})
	fp.mu.Unlock()

	snap, _ := r.Snapshot(context.Background(), LookupOptions{})
	if len(snap.Skills) != 2 {
		t.Fatalf("skills = %+v, want Snapshot unaffected by Get's internal cache", snap.Skills)
	}
}

func TestRegisterInvalidatesGetCache(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{nameStr: "files", complete: true}
	registerFake(t, r, fp)

	r.Get(context.Background(), "my-skill", LookupOptions{}) // populates cache with an empty result

	r.Register(basicRegistration("my-skill"))

	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v, want the newly registered skill visible — Register must invalidate Get's cache", def, err)
	}
}

func TestControlInvalidateClearsGetCache(t *testing.T) {
	r := NewRegistry()
	var invalidate func()
	fp := &fakeProvider{nameStr: "files", complete: true}
	r.RegisterProvider(func(c Control) (Provider, error) {
		invalidate = c.Invalidate
		return fp, nil
	})

	r.Get(context.Background(), "my-skill", LookupOptions{}) // caches "not found"

	fp.mu.Lock()
	fp.candidates = []Candidate{{Summary: Summary{Name: "my-skill", Description: "d"}, Rank: 100}}
	fp.mu.Unlock()
	invalidate()

	def, err := r.Get(context.Background(), "my-skill", LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v, want control.Invalidate() to force rediscovery", def, err)
	}
}

// ---- OnChange -----------------------------------------------------------

func TestOnChangeFiresOnRegisterAndDispose(t *testing.T) {
	r := NewRegistry()
	calls := 0
	r.OnChange(func() { calls++ })

	dispose, _ := r.Register(basicRegistration("my-skill"))
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 after Register", calls)
	}
	dispose()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 after dispose", calls)
	}
}

func TestOnChangePanicIsContained(t *testing.T) {
	r := NewRegistry()
	r.OnChange(func() { panic("broken observer") })
	seen := false
	r.OnChange(func() { seen = true })

	if _, err := r.Register(basicRegistration("my-skill")); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("a panicking observer must not stop later observers from being notified")
	}
}

func TestOnChangeDisposeStopsNotifications(t *testing.T) {
	r := NewRegistry()
	calls := 0
	dispose := r.OnChange(func() { calls++ })
	dispose()

	r.Register(basicRegistration("my-skill"))
	if calls != 0 {
		t.Fatalf("calls = %d, want 0 after disposing the observer", calls)
	}
}

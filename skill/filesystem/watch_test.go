package filesystem

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go-harness/skill/skill"
)

func TestPollInvalidateFiresRepeatedlyUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	control := skill.Control{Ctx: ctx, Invalidate: func() { atomic.AddInt32(&calls, 1) }}

	done := make(chan struct{})
	go func() {
		pollInvalidate(control, 10*time.Millisecond)
		close(done)
	}()

	time.Sleep(55 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pollInvalidate did not stop after context cancellation")
	}

	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("Invalidate called %d times, want at least 2 in 55ms at a 10ms interval", got)
	}
}

func TestPollInvalidateStopsImmediatelyOnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	control := skill.Control{Ctx: ctx, Invalidate: func() { t.Fatal("must not be called") }}

	done := make(chan struct{})
	go func() {
		pollInvalidate(control, time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pollInvalidate did not return promptly for an already-cancelled context")
	}
}

func TestRegisterWithoutPollingDoesNotInvalidateAutomatically(t *testing.T) {
	root := t.TempDir()
	reg := skill.NewRegistry()
	dispose, err := Register(reg, "files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dispose()

	// No skill on disk yet.
	def, _ := reg.Get(context.Background(), "x", skill.LookupOptions{})
	if def != nil {
		t.Fatalf("def = %+v, want nil before the file exists", def)
	}

	writeFile(t, root+"/x.md", "---\nname: x\ndescription: d\n---\nbody")

	// List/Snapshot are always fresh regardless of polling, so the new
	// skill is visible immediately through List even with pollInterval: 0.
	skills, _ := reg.List(context.Background(), skill.LookupOptions{})
	if len(skills) != 1 {
		t.Fatalf("skills = %+v, want the new skill visible via List (always fresh)", skills)
	}
}

func TestRegisterWithPollingEventuallyInvalidatesGetCache(t *testing.T) {
	root := t.TempDir()
	reg := skill.NewRegistry()
	dispose, err := Register(reg, "files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}}, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer dispose()

	// Populate Get's candidate cache with an empty result.
	if _, err := reg.Get(context.Background(), "x", skill.LookupOptions{}); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root+"/x.md", "---\nname: x\ndescription: d\n---\nbody")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		def, err := reg.Get(context.Background(), "x", skill.LookupOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if def != nil {
			return // polling invalidated the stale cache and Get found it
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Get never reflected the new file even after polling should have invalidated the cache")
}

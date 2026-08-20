package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got := CanonicalPath(link)
	want := CanonicalPath(real) // both should resolve to the same underlying path
	if got != want {
		t.Fatalf("CanonicalPath(link) = %q, want it to match CanonicalPath(real) = %q", got, want)
	}
	if got == link {
		t.Fatal("CanonicalPath did not resolve the symlink at all")
	}
}

func TestCanonicalPathFallsBackWhenUnresolvable(t *testing.T) {
	missing := "/definitely/does/not/exist/anywhere"
	if got := CanonicalPath(missing); got != missing {
		t.Fatalf("CanonicalPath(missing) = %q, want the original path back unchanged", got)
	}
}

func TestWritableRootsReadOnlyIsEmpty(t *testing.T) {
	roots := WritableRoots(ExecutionPolicy{Mode: ReadOnly, WorkspaceRoot: "/ws"})
	if len(roots) != 0 {
		t.Fatalf("roots = %v, want none under read-only", roots)
	}
}

func TestWritableRootsWorkspaceWriteIncludesWorkspaceAndTemp(t *testing.T) {
	dir := t.TempDir()
	roots := WritableRoots(ExecutionPolicy{Mode: WorkspaceWrite, WorkspaceRoot: dir})
	if len(roots) < 1 {
		t.Fatalf("roots = %v, want at least the workspace root", roots)
	}
	found := false
	for _, r := range roots {
		if r == CanonicalPath(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("roots = %v, want the canonical workspace root among them", roots)
	}
}

func TestWritableRootsDeduplicatesWhenWorkspaceIsTemp(t *testing.T) {
	tmp := os.TempDir()
	roots := WritableRoots(ExecutionPolicy{Mode: WorkspaceWrite, WorkspaceRoot: tmp})
	seen := map[string]int{}
	for _, r := range roots {
		seen[r]++
	}
	for path, count := range seen {
		if count > 1 {
			t.Fatalf("root %q appeared %d times, want deduplicated", path, count)
		}
	}
}

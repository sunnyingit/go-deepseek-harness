package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootWalksUpToGit(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(base, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectRoot(nested); got != base {
		t.Fatalf("projectRoot(%q) = %q, want %q", nested, got, base)
	}
}

func TestProjectRootFallsBackToCwdWithoutGit(t *testing.T) {
	// t.TempDir() sits under the system temp root, which (on most CI/dev
	// machines) has no .git ancestor — if it did, this test would need a
	// different isolation strategy.
	dir := t.TempDir()
	if got := projectRoot(dir); got != dir {
		t.Fatalf("projectRoot(%q) = %q, want unchanged (no .git ancestor found)", dir, got)
	}
}

func TestRootsRankOrderAndPaths(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{IncludeDefaultRoots: true, DshHome: "/dsh-home", AgentsHome: "/agents-home", CustomSkillDirs: []string{"/custom"}}
	roots := cfg.roots(base)

	want := []rootSpec{
		{path: filepath.Join(base, ".dsh", "skills"), rank: RankProjectDsh},
		{path: filepath.Join(base, ".agents", "skills"), rank: RankProjectAgents},
		{path: "/custom", rank: RankCustom},
		{path: filepath.Join("/dsh-home", "skills"), rank: RankUserDsh},
		{path: filepath.Join("/agents-home", "skills"), rank: RankUserAgents},
	}
	if len(roots) != len(want) {
		t.Fatalf("roots = %+v, want %+v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots[%d] = %+v, want %+v", i, roots[i], want[i])
		}
	}
}

func TestRootsIncludeDefaultRootsFalseOnlyCustom(t *testing.T) {
	cfg := Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{"/custom"}}
	roots := cfg.roots("/some/cwd")
	if len(roots) != 1 || roots[0].path != "/custom" {
		t.Fatalf("roots = %+v, want only the custom dir", roots)
	}
}

func TestRootsEmptyCWDOmitsProjectRoots(t *testing.T) {
	cfg := Config{IncludeDefaultRoots: true}
	roots := cfg.roots("")
	for _, r := range roots {
		if r.rank == RankProjectDsh || r.rank == RankProjectAgents {
			t.Fatalf("roots = %+v, want no project-relative roots for an empty cwd", roots)
		}
	}
}

func TestDshHomeResolutionPrecedence(t *testing.T) {
	cfg := Config{DshHome: "/explicit"}
	if got := cfg.resolvedDshHome(); got != "/explicit" {
		t.Fatalf("resolvedDshHome() = %q, want the explicit override to win", got)
	}

	t.Setenv("DSH_HOME", "/from-env")
	cfg = Config{}
	if got := cfg.resolvedDshHome(); got != "/from-env" {
		t.Fatalf("resolvedDshHome() = %q, want the env var when no explicit override is set", got)
	}
}

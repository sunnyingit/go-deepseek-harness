package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-harness/skill/skill"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findCandidate(candidates []skill.Candidate, name string) (skill.Candidate, bool) {
	for _, c := range candidates {
		if c.Name == name {
			return c, true
		}
	}
	return skill.Candidate{}, false
}

func TestListDirectoryBundleSkill(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "my-skill", "SKILL.md"),
		"---\nname: my-skill\ndescription: a bundled skill\n---\ninstructions here")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}})
	obs, err := p.List(context.Background(), skill.LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := findCandidate(obs.Candidates, "my-skill")
	if !ok {
		t.Fatalf("candidates = %+v", obs.Candidates)
	}
	if c.ResourceBase.Kind != skill.ResourceDirectory || c.ResourceBase.Path != filepath.Join(root, "my-skill") {
		t.Fatalf("resourceBase = %+v, want the skill's own bundle directory", c.ResourceBase)
	}
}

func TestListFlatMarkdownSkill(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "my-skill.md"),
		"---\nname: my-skill\ndescription: a flat skill\n---\ninstructions here")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}})
	obs, err := p.List(context.Background(), skill.LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := findCandidate(obs.Candidates, "my-skill")
	if !ok {
		t.Fatalf("candidates = %+v", obs.Candidates)
	}
	if c.ResourceBase.Kind != skill.ResourceDirectory || c.ResourceBase.Path != root {
		t.Fatalf("resourceBase = %+v, want the shared root directory", c.ResourceBase)
	}
}

func TestListMissingRootIsEmptyNotError(t *testing.T) {
	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{"/definitely/does/not/exist"}})
	obs, err := p.List(context.Background(), skill.LookupOptions{})
	if err != nil {
		t.Fatalf("List should not error on a missing root: %v", err)
	}
	if !obs.Complete {
		t.Fatal("a missing root is valid empty state, not incompleteness")
	}
	if len(obs.Candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", obs.Candidates)
	}
}

func TestListMalformedFileSkippedNotFatal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "broken.md"), "not frontmatter at all")
	writeFile(t, filepath.Join(root, "good.md"), "---\nname: good\ndescription: works\n---\nbody")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}})
	obs, err := p.List(context.Background(), skill.LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Complete {
		t.Fatal("a malformed individual file must not mark discovery incomplete")
	}
	if _, ok := findCandidate(obs.Candidates, "good"); !ok {
		t.Fatalf("candidates = %+v, want the good file still discovered", obs.Candidates)
	}
	if len(obs.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want the broken file silently skipped", obs.Candidates)
	}
}

func TestListRankReflectsRootOrder(t *testing.T) {
	custom := t.TempDir()
	writeFile(t, filepath.Join(custom, "x.md"), "---\nname: x\ndescription: from custom\n---\nbody")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{custom}})
	obs, _ := p.List(context.Background(), skill.LookupOptions{})
	c, ok := findCandidate(obs.Candidates, "x")
	if !ok || c.Rank != RankCustom {
		t.Fatalf("candidate = %+v, want Rank=%d", c, RankCustom)
	}
}

func TestGetRereadsCurrentContentAfterEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.md")
	writeFile(t, path, "---\nname: x\ndescription: v1\n---\noriginal body")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}})
	obs, _ := p.List(context.Background(), skill.LookupOptions{})
	c, _ := findCandidate(obs.Candidates, "x")

	writeFile(t, path, "---\nname: x\ndescription: v2\n---\nedited body")

	def, err := p.Get(context.Background(), c, skill.LookupOptions{})
	if err != nil || def == nil {
		t.Fatalf("def=%v err=%v", def, err)
	}
	if def.Description != "v2" || def.Content != "edited body" {
		t.Fatalf("def = %+v, want the edit reflected without a fresh List", def)
	}
}

func TestGetMissingFileReturnsNilNil(t *testing.T) {
	p := New("files", Config{})
	c := skill.Candidate{Summary: skill.Summary{Name: "x"}, Locator: "/does/not/exist.md"}
	def, err := p.Get(context.Background(), c, skill.LookupOptions{})
	if err != nil || def != nil {
		t.Fatalf("def=%v err=%v, want nil, nil", def, err)
	}
}

func TestGetRenameIsCaughtByTheRegistryLayer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.md")
	writeFile(t, path, "---\nname: x\ndescription: d\n---\nbody")

	reg := skill.NewRegistry()
	if _, err := reg.RegisterProvider(func(skill.Control) (skill.Provider, error) {
		return New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}}), nil
	}); err != nil {
		t.Fatal(err)
	}

	// Rename the frontmatter's own name after discovery would have seen "x".
	writeFile(t, path, "---\nname: renamed\ndescription: d\n---\nbody")

	def, err := reg.Get(context.Background(), "x", skill.LookupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if def != nil {
		t.Fatalf("def = %+v, want nil — the registry must reject a stale name mismatch", def)
	}
}

func TestGetInvocationPolicyPropagates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.md"), "---\nname: x\ndescription: d\nuser-invocable: false\n---\nbody")

	p := New("files", Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{root}})
	obs, _ := p.List(context.Background(), skill.LookupOptions{})
	c, _ := findCandidate(obs.Candidates, "x")
	def, _ := p.Get(context.Background(), c, skill.LookupOptions{})
	if def.Invocation.UserInvocable {
		t.Fatalf("def.Invocation = %+v", def.Invocation)
	}
}

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-harness/skill/skill"
)

func TestBundledSkillsDirEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_SKILLS_DIR", dir)
	got := bundledSkillsDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != abs {
		t.Fatalf("bundledSkillsDir = %q, want %q", got, abs)
	}
}

func TestBundledSkillsDirMissingEnv(t *testing.T) {
	t.Setenv("HARNESS_SKILLS_DIR", filepath.Join(t.TempDir(), "no-such-dir"))
	if bundledSkillsDir() == os.Getenv("HARNESS_SKILLS_DIR") {
		t.Fatal("missing env path should not win")
	}
}

func TestRenderLoadedSkillAddsChineseUserFacingNote(t *testing.T) {
	got := renderLoadedSkill(skill.Definition{
		Summary: skill.Summary{Name: "demo", Provider: "files"},
		Content: "Do the thing in English.",
	})
	if !strings.Contains(got, "Do the thing in English.") {
		t.Fatal("skill body should be kept")
	}
	if !strings.Contains(got, "简体中文") {
		t.Fatalf("want a Chinese user-facing note, got:\n%s", got)
	}
}

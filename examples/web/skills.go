package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	coretools "go-harness/core/tools"
	skillfs "go-harness/skill/filesystem"
	"go-harness/skill/skill"
)

// bootstrapSkills wires one embedded runtime skill plus a real
// skill/filesystem.Provider pointed at skillsDir, writing a sample SKILL.md
// bundle into it so a fresh server has something to discover immediately.
// Drop more *.md or <name>/SKILL.md bundles into skillsDir at any time —
// List/Snapshot are always fresh (see skill.Registry's README), so a new
// file is visible on the very next /skills call, no restart needed.
func bootstrapSkills(skillsDir string) (*skill.Registry, error) {
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, err
	}
	if err := writeSampleSkillBundle(skillsDir); err != nil {
		return nil, err
	}

	registry := skill.NewRegistry()
	if _, err := registry.Register(skill.Registration{
		Name:        "explain-jargon",
		Description: "Explain a technical term in one plain-English sentence, no jargon back.",
		Content:     "When asked to explain a term, respond with exactly one sentence, in plain English, with no jargon of its own.",
	}); err != nil {
		return nil, fmt.Errorf("registering runtime skill: %w", err)
	}
	if _, err := skillfs.Register(registry, "files",
		skillfs.Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{skillsDir}}, 0); err != nil {
		return nil, fmt.Errorf("registering filesystem skill provider: %w", err)
	}
	return registry, nil
}

func writeSampleSkillBundle(skillsDir string) error {
	bundleDir := filepath.Join(skillsDir, "commit-message")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return err
	}
	skillFile := filepath.Join(bundleDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err == nil {
		return nil // don't clobber an operator's edits across restarts
	}
	content := `---
name: commit-message
description: "Write a Conventional Commits message: <type>(<scope>): <subject>"
---
Rules:
- type is one of: feat, fix, docs, refactor, test, chore
- scope is optional, in parentheses, lowercase
- subject is imperative mood, present tense, no trailing period, under 72 characters
- add a body only if the change needs explaining beyond the subject line
`
	return os.WriteFile(skillFile, []byte(content), 0o644)
}

func listSkillsTool(reg *skill.Registry) coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        "list_skills",
		Description: "List available skills (name and description) you can load with load_skill.",
		Parameters:  coretools.ParameterSchema{},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		Execute: func(ctx context.Context, _ map[string]any, _ *coretools.RunContext) (any, error) {
			skills, err := reg.List(ctx, skill.LookupOptions{})
			if err != nil {
				return nil, err
			}
			sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
			var lines []string
			for _, s := range skills {
				if !skill.IsModelInvocable(s) {
					continue
				}
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", s.Name, s.Provider, s.Description))
			}
			if len(lines) == 0 {
				return "(no skills available)", nil
			}
			return strings.Join(lines, "\n"), nil
		},
	}
}

func loadSkillTool(reg *skill.Registry) coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        "load_skill",
		Description: "Load a skill's full instructions by name (see list_skills).",
		Parameters: coretools.ParameterSchema{
			"name": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "the skill's name"}, Required: true},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			name, _ := args["name"].(string)
			def, err := reg.Get(ctx, name, skill.LookupOptions{})
			if err != nil {
				return nil, err
			}
			if def == nil {
				return nil, fmt.Errorf("no such skill %q", name)
			}
			if !skill.IsModelInvocable(def.Summary) {
				return nil, fmt.Errorf("skill %q is not model-invocable", name)
			}
			return skill.RenderSkillContent(*def), nil
		},
	}
}

func skillDTOs(reg *skill.Registry) ([]skillDTO, error) {
	skills, err := reg.List(context.Background(), skill.LookupOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	out := make([]skillDTO, len(skills))
	for i, s := range skills {
		out[i] = skillDTO{
			Name: s.Name, Description: s.Description, Provider: s.Provider,
			ModelInvocable: s.Invocation.ModelInvocable, UserInvocable: s.Invocation.UserInvocable,
		}
	}
	return out, nil
}

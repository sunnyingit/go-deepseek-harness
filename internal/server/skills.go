package server

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

// bootstrapSkills wires one embedded runtime skill (explain-jargon) plus a
// skill/filesystem.Provider pointed at the shipped web/skills catalog and
// skillsDir (for extra bundles dropped at runtime). List/Snapshot are always
// fresh (see skill.Registry's README), so a new file is visible on the next
// /skills call, no restart needed.
func bootstrapSkills(skillsDir string) (*skill.Registry, error) {
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, err
	}

	dirs := []string{}
	if bundled := bundledSkillsDir(); bundled != "" && bundled != skillsDir {
		dirs = append(dirs, bundled)
	}
	dirs = append(dirs, skillsDir)

	registry := skill.NewRegistry()
	if _, err := registry.Register(skill.Registration{
		Name:        "explain-jargon",
		Description: "Explain a technical term in one plain-English sentence, no jargon back.",
		Content:     "When asked to explain a term, respond with exactly one sentence, in plain English, with no jargon of its own.",
	}); err != nil {
		return nil, fmt.Errorf("registering runtime skill: %w", err)
	}
	if _, err := skillfs.Register(registry, "files",
		skillfs.Config{IncludeDefaultRoots: false, CustomSkillDirs: dirs}, 0); err != nil {
		return nil, fmt.Errorf("registering filesystem skill provider: %w", err)
	}
	return registry, nil
}

// bundledSkillsDir is the SKILL.md catalog shipped with this package
// (web/skills when running from the web/ directory). Empty if we can't find it.
func bundledSkillsDir() string {
	var candidates []string
	if d := os.Getenv("HARNESS_SKILLS_DIR"); d != "" {
		candidates = append(candidates, d)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "skills"), filepath.Join(wd, "web", "skills"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "skills"), filepath.Join(dir, "web", "skills"))
	}
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err != nil || !st.IsDir() {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			return c
		}
		return abs
	}
	return ""
}

func listSkillsTool(reg *skill.Registry) coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        "list_skills",
		Description: "列出可用技能（名称和简介）。需要按技能执行任务时先调用本工具，再用 load_skill 加载。",
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
		Description: "按名称加载技能的完整说明（见 list_skills）。加载后对用户的说明必须用简体中文。",
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
			return renderLoadedSkill(*def), nil
		},
	}
}

const skillUserFacingLanguage = `

对用户可见的回复（进度、说明、总结、最终答案）必须使用简体中文。上面的技能正文可能是英文：按步骤执行即可，不要把执行过程用英文讲给用户听。
`

func renderLoadedSkill(def skill.Definition) string {
	return skill.RenderSkillContent(def) + skillUserFacingLanguage
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

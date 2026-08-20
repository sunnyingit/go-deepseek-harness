// Demo: a real DeepSeek + ReAct loop with two tools — list_skills and
// load_skill — backed by skill.Registry: one embedded "runtime" skill
// registered directly in code, and one loaded from a real
// skill/filesystem.Provider (a SKILL.md directory bundle, with genuine YAML
// frontmatter — name/description/metadata, parsed by gopkg.in/yaml.v3, not
// a hand-rolled stand-in), mixed into a single catalog. The model discovers
// what's available, loads the one it needs, and follows its instructions.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go-harness/core/agentloop"
	coretools "go-harness/core/tools"
	"go-harness/llm/deepseek"
	skillfs "go-harness/skill/filesystem"
	"go-harness/skill/skill"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set DEEPSEEK_API_KEY to run this demo")
		os.Exit(1)
	}

	skillsDir, err := os.MkdirTemp("", "go-harness-skill-demo-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(skillsDir)
	writeCommitMessageSkillBundle(skillsDir)

	registry := skill.NewRegistry()

	if _, err := registry.Register(skill.Registration{
		Name:        "explain-jargon",
		Description: "Explain a technical term in one plain-English sentence, no jargon back.",
		Content:     "When asked to explain a term, respond with exactly one sentence, in plain English, with no jargon of its own.",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "registering runtime skill:", err)
		os.Exit(1)
	}

	// No polling (0): List/Snapshot are always fresh regardless, and this
	// demo never edits skill files after startup — see skill/filesystem's
	// README for what the poll interval is actually for.
	if _, err := skillfs.Register(registry, "files",
		skillfs.Config{IncludeDefaultRoots: false, CustomSkillDirs: []string{skillsDir}}, 0); err != nil {
		fmt.Fprintln(os.Stderr, "registering filesystem provider:", err)
		os.Exit(1)
	}

	toolRegistry := coretools.NewRegistry()
	must(toolRegistry.Register(listSkillsTool(registry)))
	must(toolRegistry.Register(loadSkillTool(registry)))

	adapter, err := deepseek.New(deepseek.Config{APIKey: apiKey, BaseURL: os.Getenv("DEEPSEEK_BASE_URL")})
	if err != nil {
		fmt.Fprintln(os.Stderr, "deepseek adapter:", err)
		os.Exit(1)
	}

	sess := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(
		sess,
		adapter,
		coretools.Bridge{Registry: toolRegistry},
		"You have list_skills and load_skill available. Before doing a task a skill might help with, "+
			"call list_skills to see what's available, then load_skill to pull in the one that applies.",
		agentloop.Hooks{},
	).WithModel("deepseek-chat")

	question := "I staged a change that fixes a null pointer crash in the login handler. Write the commit message for it."
	fmt.Println("User:", question)
	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}}})
	agent.WhenIdle()

	printTranscript(sess)
}

// writeCommitMessageSkillBundle writes a real SKILL.md directory bundle —
// skill/filesystem's other supported layout is a flat <name>.md, but a
// bundle is the more interesting case to demo since its resourceBase is
// the skill's own directory, not the shared root (see skill/filesystem's
// README for why the two layouts resolve resources differently).
func writeCommitMessageSkillBundle(skillsDir string) {
	bundleDir := filepath.Join(skillsDir, "commit-message")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	content := `---
name: commit-message
description: "Write a Conventional Commits message: <type>(<scope>): <subject>"
metadata:
  format: conventional-commits
---
Rules:
- type is one of: feat, fix, docs, refactor, test, chore
- scope is optional, in parentheses, lowercase
- subject is imperative mood, present tense, no trailing period, under 72 characters
- add a body only if the change needs explaining beyond the subject line
`
	if err := os.WriteFile(filepath.Join(bundleDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func listSkillsTool(reg *skill.Registry) coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        "list_skills",
		Description: "List available skills (name and description) you can load with load_skill.",
		Parameters:  coretools.ParameterSchema{},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderString,
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
			Render: renderString,
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

func renderString(_ map[string]any, value any) []agentloop.ContentBlock {
	return []agentloop.ContentBlock{{Kind: agentloop.BlockToolResult, Text: value.(string)}}
}

func printTranscript(sess agentloop.Session) {
	fmt.Println()
	for _, ev := range sess.Events() {
		switch ev.Type {
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, b := range msg.Content {
				switch b.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", b.Text)
				case agentloop.BlockToolCall:
					fmt.Printf("Action:  %s(%v)\n", b.ToolName, b.Args)
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", strings.ReplaceAll(msg.Content[0].Text, "\n", "\n             "))
		}
	}
}

func must(_ func(), err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		os.Exit(1)
	}
}

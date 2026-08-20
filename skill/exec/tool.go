// Package exec closes a gap between skill/skill and sandbox/local: a loaded
// skill's own instructions may reference a script bundled alongside it
// (e.g. "run scripts/generate.py to build the report" — a convention this
// port's skill/filesystem inherits directly from the real SKILL.md/scripts
// /references/assets layout), but nothing before this package let a model
// actually *run* that script — only load the text telling it the script
// exists. run_skill_script closes it: given a skill name and a path
// relative to that skill's own resource directory, it confines and runs
// the script through the exact same sandbox.Provider + tools/python.Env
// run_python already uses, so a skill's bundled code gets the same
// pre-installed packages and the same write confinement as model-authored
// code — reads are unrestricted either way (see sandbox/sandbox's README),
// so a skill's own scripts/references/assets are always readable; only
// writing outside the granted workspace is what the sandbox actually stops.
//
// This is not a port of any one deepseek-harness package — it's this Go
// module's own answer to a capability the TS original's tool-skill package
// alludes to but that package wasn't in scope for this port.
package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-harness/core/session"
	"go-harness/core/tools"
	"go-harness/sandbox/sandbox"
	"go-harness/skill/skill"
	"go-harness/tools/python"
)

// RunScriptToolOptions configures NewRunScriptTool.
type RunScriptToolOptions struct {
	// Skills resolves a skill name to its Definition (and, from that, the
	// directory a relative script path is resolved against). Required.
	Skills *skill.Registry
	// Env is the provisioned interpreter scripts run against — the same
	// one run_python uses, so a skill's script sees the same installed
	// packages. Required.
	Env *python.Env
	// Provider confines the actual script execution. Required.
	Provider sandbox.Provider
	// WorkspaceRoot is the directory a script may write inside. Required —
	// scripts themselves are read from a skill's own (read-only) directory,
	// never from here, but a script that writes output needs somewhere
	// granted.
	WorkspaceRoot string
	// Mode is the confinement mode scripts run under. Defaults to
	// WorkspaceWrite, matching run_python.
	Mode sandbox.Mode
	// Timeout bounds one script run. Defaults to 30s.
	Timeout time.Duration
}

// NewRunScriptTool builds the run_skill_script tool.
func NewRunScriptTool(opts RunScriptToolOptions) (tools.ToolDefinition, error) {
	if opts.Skills == nil {
		return tools.ToolDefinition{}, fmt.Errorf("skillexec: RunScriptToolOptions.Skills is required")
	}
	if opts.Env == nil {
		return tools.ToolDefinition{}, fmt.Errorf("skillexec: RunScriptToolOptions.Env is required")
	}
	if opts.Provider == nil {
		return tools.ToolDefinition{}, fmt.Errorf("skillexec: RunScriptToolOptions.Provider is required")
	}
	if opts.WorkspaceRoot == "" {
		return tools.ToolDefinition{}, fmt.Errorf("skillexec: RunScriptToolOptions.WorkspaceRoot is required")
	}
	mode := opts.Mode
	if mode == "" {
		mode = sandbox.WorkspaceWrite
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return tools.ToolDefinition{
		Name: "run_skill_script",
		Description: "Run a Python script bundled with a loaded skill. `script` is a path relative to " +
			"the skill's base directory (see load_skill's resourceBase hint) — e.g. \"scripts/generate.py\". " +
			"Runs in the same sandboxed interpreter as run_python, with the same packages installed.",
		Parameters: tools.ParameterSchema{
			"skill":  {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "the skill's name, as returned by list_skills"}, Required: true},
			"script": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "path to the script, relative to the skill's base directory"}, Required: true},
		},
		Output: tools.Output{
			Schema: tools.ValueSchema{Kind: tools.KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		TimeoutMs: int(timeout.Milliseconds()),
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			skillName, _ := args["skill"].(string)
			scriptRel, _ := args["script"].(string)
			return runSkillScript(ctx, opts, mode, skillName, scriptRel)
		},
	}, nil
}

func runSkillScript(ctx context.Context, opts RunScriptToolOptions, mode sandbox.Mode, skillName, scriptRel string) (string, error) {
	def, err := opts.Skills.Get(ctx, skillName, skill.LookupOptions{})
	if err != nil {
		return "", err
	}
	if def == nil {
		return "", fmt.Errorf("no such skill %q", skillName)
	}
	if !skill.IsModelInvocable(def.Summary) {
		return "", fmt.Errorf("skill %q is not model-invocable", skillName)
	}
	if def.ResourceBase == nil || def.ResourceBase.Kind != skill.ResourceDirectory {
		return "", fmt.Errorf("skill %q has no directory of runnable scripts", skillName)
	}

	scriptPath, err := resolveWithinBase(def.ResourceBase.Path, scriptRel)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(scriptPath); err != nil || info.IsDir() {
		return "", fmt.Errorf("script %q not found under skill %q", scriptRel, skillName)
	}

	policy, err := sandbox.NewPolicy(mode, opts.WorkspaceRoot, "")
	if err != nil {
		return "", err
	}
	confined, err := opts.Provider.Confine([]string{opts.Env.PythonPath, scriptPath}, policy)
	if err != nil {
		return "", err
	}

	output, err := python.RunConfined(ctx, confined.Argv, opts.WorkspaceRoot)
	if err != nil {
		return "", fmt.Errorf("script %s", err)
	}
	return output, nil
}

// resolveWithinBase joins base and rel, rejecting a result that would
// escape base — a skill's script reference must stay inside that skill's
// own directory. This is a tidiness/correctness check, not a security
// boundary by itself: sandbox reads are unrestricted regardless (see
// sandbox/sandbox's README), so nothing here is what actually stops a
// script from reading elsewhere on disk — only from being *invoked* via a
// name that claims to be "this skill's own script" when it isn't.
func resolveWithinBase(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("script path must be relative to the skill's base directory, got %q", rel)
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	joinedAbs, err := filepath.Abs(filepath.Join(baseAbs, rel))
	if err != nil {
		return "", err
	}
	if joinedAbs != baseAbs && !strings.HasPrefix(joinedAbs, baseAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("script path %q escapes the skill's base directory", rel)
	}
	return joinedAbs, nil
}

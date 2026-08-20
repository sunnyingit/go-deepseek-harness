// Server wires together every package built in this module into one
// process: core/session (Store), core/agentloop (the driver), core/agent
// (the live-agent registry), core/tools + tools/python (execution),
// sandbox/local (confinement), skill/skill + skill/filesystem (the skill
// catalog), plan/planmode (plan-mode collaboration state), and
// llm/deepseek (the model). One HTTP session maps to one core/session.Session
// plus one core/agentloop.Agent, registered in a shared core/agent.Registry.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/core/session"
	coretools "go-harness/core/tools"
	"go-harness/llm/deepseek"
	"go-harness/plan/planmode"
	"go-harness/sandbox/local"
	"go-harness/skill/skill"
	"go-harness/tools/python"
)

const systemPrompt = `You are a helpful assistant with three kinds of tools:
- run_python: a sandboxed Python interpreter pre-installed with document-processing
  libraries — use it for anything involving Word/PowerPoint/Excel/PDF files or
  general computation.
- list_skills / load_skill: a catalog of reusable instruction sets. Before a task a
  skill might help with, call list_skills, then load_skill to pull in the one that
  applies, then follow its instructions.
- exit_plan_mode: only relevant while plan mode is active (see the plan-mode
  endpoint) — present your complete plan for approval before carrying it out.`

// Server holds every shared, process-lifetime dependency. Everything
// session-specific (the session log, the agent, its own workspace
// directory) is built fresh per session in buildAgent.
type Server struct {
	store          *session.Store
	agents         *coreagent.Registry
	skills         *skill.Registry
	planController *planmode.Controller
	sandboxProv    *local.Provider
	pythonEnv      *python.Env
	llm            *deepseek.Adapter
	model          string
	dataDir        string
}

func newServer(ctx context.Context, cfg config) (*Server, error) {
	llm, err := deepseek.New(deepseek.Config{APIKey: cfg.apiKey, BaseURL: cfg.baseURL})
	if err != nil {
		return nil, fmt.Errorf("deepseek adapter: %w", err)
	}

	skills, err := bootstrapSkills(filepath.Join(cfg.dataDir, "skills"))
	if err != nil {
		return nil, fmt.Errorf("bootstrapping skills: %w", err)
	}

	planController, err := planmode.New(
		"Explore before presenting the complete plan. Use exit_plan_mode once it's ready for review.")
	if err != nil {
		return nil, fmt.Errorf("plan-mode controller: %w", err)
	}

	fmt.Println("provisioning the Python environment (first run downloads packages, this can take a while)...")
	env, err := python.EnsureEnv(ctx, python.EnvOptions{CacheDir: filepath.Join(cfg.dataDir, "python-venv")})
	if err != nil {
		return nil, fmt.Errorf("provisioning python: %w", err)
	}
	fmt.Println("python environment ready:", env.Root)

	return &Server{
		store:          session.NewStore(),
		agents:         coreagent.NewRegistry(),
		skills:         skills,
		planController: planController,
		sandboxProv:    local.New(),
		pythonEnv:      env,
		llm:            llm,
		model:          cfg.model,
		dataDir:        cfg.dataDir,
	}, nil
}

// createSession starts a brand new session (no parent) and its agent.
func (s *Server) createSession(cwd string) (*session.Session, *agentloop.Agent, error) {
	sess, err := s.store.Create("", session.Header{CWD: cwd})
	if err != nil {
		return nil, nil, err
	}
	ag, err := s.buildAgent(sess)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.agents.Register(string(sess.ID()), ag, ""); err != nil {
		return nil, nil, err
	}
	return sess, ag, nil
}

// forkSession branches parentID at boundary (-1 for "right now") into a new
// live session + agent, owned by the parent in the agent registry.
func (s *Server) forkSession(parentID string, boundary int) (*session.Session, *agentloop.Agent, error) {
	parent, ok := s.agents.Get(parentID)
	if !ok {
		return nil, nil, fmt.Errorf("no such session %q", parentID)
	}
	child, err := s.store.Fork(parent.Session(), boundary, "")
	if err != nil {
		return nil, nil, err
	}
	ag, err := s.buildAgent(child)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.agents.Register(string(child.ID()), ag, parentID); err != nil {
		return nil, nil, err
	}
	return child, ag, nil
}

// buildAgent gives sess its own workspace directory and tool set (run_python
// needs a WorkspaceRoot baked in at construction, so tools — and the
// run_python definition specifically — are built fresh per session, not
// shared) and wires plan mode's PreStepHook so guidance/narration/exit
// commit at the right boundary automatically.
func (s *Server) buildAgent(sess *session.Session) (*agentloop.Agent, error) {
	workspaceRoot := filepath.Join(s.dataDir, "sessions", string(sess.ID()), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return nil, err
	}

	pythonTool, err := python.NewRunTool(python.RunToolOptions{
		Env: s.pythonEnv, Provider: s.sandboxProv, WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		return nil, err
	}

	toolRegistry := coretools.NewRegistry()
	if _, err := toolRegistry.Register(pythonTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(listSkillsTool(s.skills)); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(loadSkillTool(s.skills)); err != nil {
		return nil, err
	}

	// autoApproveReview approves every plan immediately: this is a headless
	// HTTP API with no human sitting in front of a terminal to ask, unlike
	// examples/planmode's interactive demo. A real deployment would instead
	// hold the call open (or return "pending" and let a separate endpoint
	// resolve it) — out of scope for stringing the packages together here.
	exitTool := &planmode.ExitTool{Controller: s.planController, Review: autoApproveReview}
	multiTool := planmode.NewMultiTool().Add(coretools.Bridge{Registry: toolRegistry}).Add(exitTool)

	return agentloop.NewAgent(sess, s.llm, multiTool, systemPrompt,
		agentloop.Hooks{PreStep: s.planController.PreStepHook()}).WithModel(s.model), nil
}

func autoApproveReview(_ context.Context, _ string) (bool, string) { return true, "" }

func renderStringResult(_ map[string]any, value any) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
}

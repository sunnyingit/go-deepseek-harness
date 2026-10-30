// Server wires together every package built in this module into one
// process: core/session (Store), core/agentloop (the driver), core/agent
// (the live-agent registry), core/tools + tools/python (execution),
// sandbox/local (confinement), shell/bashsandbox + shell/toolbash (a general
// sandboxed bash executor and its model-facing tool), fs/fssandbox +
// fs/toolfs (sandboxed file read/write/edit), skill/skill +
// skill/filesystem (the skill catalog), plan/planmode (plan-mode
// collaboration state), and llm/deepseek (the model). One HTTP session maps
// to one core/session.Session plus one core/agentloop.Agent, registered in
// a shared core/agent.Registry.
package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/core/session"
	"go-harness/core/systemprompt"
	coretools "go-harness/core/tools"
	"go-harness/fs/fssandbox"
	"go-harness/fs/toolfs"
	"go-harness/internal/appconfig"
	"go-harness/llm/deepseek"
	"go-harness/llm/llmretry"
	"go-harness/plan/planmode"
	"go-harness/sandbox/local"
	"go-harness/sandbox/sandbox"
	"go-harness/session/sessionpersistence"
	"go-harness/shell/bashsandbox"
	"go-harness/shell/toolbash"
	"go-harness/skill/skill"
	"go-harness/tools/python"
)

// buildSystemPrompt assembles this session's system prompt via
// core/systemprompt instead of one hand-maintained string constant:
// harness identity + a persona line (itself a template, referencing
// {{model}}/{{cwd}} — the two variables registered below) + one ordered
// section per tool group. A system_prompt set in config.yaml/
// agent.system_prompt is fed in as a Complete section rather than
// returned verbatim — that's the exact mapping systemprompt.Section
// .Complete exists for ("this exact text becomes the whole prompt,
// nothing else contributed applies"), so a deployment override and this
// function's own default assembly aren't two separate code paths that
// could drift out of sync, and the override can itself still reference
// {{model}}/{{cwd}} if it wants to.
func (s *Server) buildSystemPrompt(cwd string) (string, error) {
	b := systemprompt.New()
	if override := strings.TrimSpace(s.cfg.SystemPrompt); override != "" {
		b.Section(systemprompt.Section{Name: "deployment:override", Text: override, Complete: true})
	} else {
		b.WithHarnessIdentity(false)
		b.WithPersona("你是有用的助手，当前模型为 {{model}}，工作目录为 {{cwd}}。对用户一律使用简体中文（用户改用其他语言时再跟随用户）：包括进度说明、步骤总结和最终回答。技能正文可能是英文，按技能执行即可，不要用英文向用户复述过程或内心独白。你有六类工具：")
		b.Section(systemprompt.Section{
			Name: "locale", Order: systemprompt.OrderDeploymentPersona + 1,
			Text: "对用户可见的文字必须是简体中文。工具名、代码、路径、命令可以保持英文。不要输出 English chain-of-thought、\"I'll start by…\"、\"Let me check…\" 这类英文旁白。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:fs", Order: systemprompt.OrderToolGuidanceMin,
			Text: "- read / write / edit：文件读写，无沙箱限制，任意目录可读可写。简单读改文件优先用这些，而不是 bash / run_python。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:run_python", Order: systemprompt.OrderToolGuidanceMin + 1,
			Text: "- run_python：预装了文档处理库的 Python。适合 Word / PPT / Excel / PDF，或内联代码计算。可读写任意路径，包括会话工作区。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:bash", Order: systemprompt.OrderToolGuidanceMin + 2,
			Text: "- bash / bash_job_result：完整文件系统和网络的 shell。跑技能自带脚本时，command 填脚本路径（如 python3 scripts/generate.py），workdir 设为 load_skill 给出的技能目录。run_in_background 会立刻返回 job_id，再用 bash_job_result 轮询。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:skills", Order: systemprompt.OrderToolGuidanceMin + 3,
			Text: "- list_skills / load_skill：可复用的技能说明。任务可能用到技能时，先 list_skills，再 load_skill 加载后严格按其步骤做。对用户说明进度时用中文。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:display", Order: systemprompt.OrderToolGuidanceMin + 4,
			Text: "- show_table / show_chart / show_code / show_suggestion / show_image / show_error / show_progress：展示工具。答案适合用表格、图、代码块、建议、图片、错误或进度条时，调用对应工具；工具结果只是已展示的确认，之后继续用中文说话。",
		})
		b.Section(systemprompt.Section{
			Name: "html_in_chat", Order: systemprompt.OrderToolGuidanceMin + 5,
			Text: "- 可交互 HTML：把最终 .html 写到当前会话工作区（默认 cwd）。对话页会自动用 iframe 嵌入。介绍页、生平、时间线、关系图等单页：直接写一个自包含 HTML，不要跑 Vite/Parcel 初始化和打包。D3 图用技能自带的 vendor/d3.v7.min.js 经 inline-d3.py 内联，不要 CDN、不要现场下载、不要为验收开 Chrome。不要把成品只写在技能目录，不要用截图代替页面。单文件必须自包含：不要链 Google Fonts / CDN；favicon 用 data URI 内联 SVG（不要引用 /favicon.svg）。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:plan_mode", Order: systemprompt.OrderToolGuidanceMin + 6,
			Text: "- exit_plan_mode：仅在计划模式开启时使用。先给出完整计划供确认，再动手执行。",
		})
		b.Section(systemprompt.Section{
			Name: "tool:elicit", Order: systemprompt.OrderToolGuidanceMin + 7,
			Text: "- ask_user_question：请求在真正拿不准、瞎猜会导致返工时才用——给 2-4 个具体选项，用户会选一个、自己打字回答，或者跳过。常规确认或有合理默认值时不要用，优先直接推进；这个调用会卡住整轮对话直到用户回应，同一时间只问一个问题。",
		})
	}
	b.Variable("model", s.cfg.Model)
	b.Variable("cwd", cwd)

	assembly, err := b.Assemble()
	if err != nil {
		return "", fmt.Errorf("assembling system prompt: %w", err)
	}
	return systemprompt.RenderPrompt(assembly)
}

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
	cfg            appconfig.Config
	configPath     string
	cfgMu          sync.Mutex
	// answers backs ask_user_question (see elicit.go) — process-lifetime and
	// shared across sessions like sandboxProv/pythonEnv above, since it's
	// just a lookup table keyed by (sessionID, callID), not itself
	// session-scoped state.
	answers *answerRegistry
	// persistence is nil-able by construction (Open can't fail short of a
	// filesystem error, so it's always set in practice) — kept as a plain
	// field rather than assumed non-nil so a future caller could construct
	// a Server without it (e.g. a unit test) without every persistence
	// -touching method needing its own separate nil-Store special case.
	persistence *sessionpersistence.Store
}

func New(ctx context.Context, cfg appconfig.Config) (*Server, error) {
	llm, err := deepseek.New(deepseek.Config{
		APIKey:              cfg.APIKey,
		BaseURL:             cfg.BaseURL,
		Thinking:            cfg.Thinking,
		StreamIdleTimeout:   cfg.StreamIdleTimeout,
		ConnectTimeout:      cfg.ConnectTimeout,
		TLSHandshakeTimeout: cfg.TLSHandshakeTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("deepseek adapter: %w", err)
	}

	skills, err := bootstrapSkills(filepath.Join(cfg.DataDir, "skills"))
	if err != nil {
		return nil, fmt.Errorf("bootstrapping skills: %w", err)
	}

	planController, err := planmode.New(
		"Explore before presenting the complete plan. Use exit_plan_mode once it's ready for review.")
	if err != nil {
		return nil, fmt.Errorf("plan-mode controller: %w", err)
	}

	// persistence lives under its own subdirectory, separate from session
	// workspaces (dataDir/sessions/<id>/workspace) — session/sessionpersistence
	// owns its own <root>/<cwd>/<id>/session.jsonl layout underneath it.
	persistence, err := sessionpersistence.Open(filepath.Join(cfg.DataDir, "session-logs"))
	if err != nil {
		return nil, fmt.Errorf("opening session persistence: %w", err)
	}

	fmt.Println("checking the Python environment...")
	exposeBundledNodeModules()
	env, err := python.EnsureEnv(ctx, cfg.PythonEnvOptions())
	if err != nil {
		return nil, fmt.Errorf("provisioning python: %w", err)
	}
	fmt.Println("python environment ready:", env.Root)

	server := &Server{
		store:          session.NewStore(),
		agents:         coreagent.NewRegistry(),
		skills:         skills,
		planController: planController,
		sandboxProv:    local.New(),
		pythonEnv:      env,
		llm:            llm,
		cfg:            cfg,
		persistence:    persistence,
		answers:        newAnswerRegistry(),
	}

	restored, err := server.restorePersistedSessions()
	if err != nil {
		return nil, fmt.Errorf("restoring persisted sessions: %w", err)
	}
	if restored > 0 {
		fmt.Println("restored", restored, "session(s) from", filepath.Join(cfg.DataDir, "session-logs"))
	}

	return server, nil
}

// restorePersistedSessions loads every session found under persistence's
// root back into store and rebuilds a live agent for each, so a restarted
// process picks up exactly where a prior run left off — GET /sessions and
// POST /sessions/{id}/messages work on a restored session precisely like
// one created this run. A session whose file is corrupt beyond what
// sessionpersistence.Store.Load already recovers from is skipped (logged,
// not fatal) rather than blocking every other session from coming back.
func (s *Server) restorePersistedSessions() (int, error) {
	if s.persistence == nil {
		return 0, nil
	}
	snaps, err := s.persistence.ListSnapshots()
	if err != nil {
		return 0, err
	}
	restored := 0
	for _, snap := range snaps {
		sess, err := s.persistence.LoadPath(snap.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping unrecoverable session log %s: %v\n", snap.Path, err)
			continue
		}
		if err := s.store.Adopt(sess); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping duplicate session id from %s: %v\n", snap.Path, err)
			continue
		}
		ag, err := s.buildAgent(sess)
		if err != nil {
			return restored, fmt.Errorf("rebuilding agent for restored session %q: %w", sess.ID(), err)
		}
		if _, err := s.agents.Register(string(sess.ID()), ag, string(sess.Header().ParentSession)); err != nil {
			return restored, fmt.Errorf("registering restored session %q: %w", sess.ID(), err)
		}
		restored++
	}
	return restored, nil
}

// syncPersistence durably appends whatever id's session has accumulated
// since the last sync — called after a turn completes (see handlers.go).
// Errors are logged, not surfaced to the HTTP caller: a save failure
// shouldn't turn an otherwise-successful chat turn into a failed request,
// and the next successful sync naturally catches up on everything a
// failed one missed (Sync always sends every event past the last durable
// one, not just the latest turn's).
func (s *Server) syncPersistence(sess *session.Session) {
	if s.persistence == nil {
		return
	}
	if err := s.persistence.Sync(sess); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to persist session %q: %v\n", sess.ID(), err)
	}
}

// createSession starts a brand new session (no parent) and its agent.
func (s *Server) createSession(cwd string) (*session.Session, *agentloop.Agent, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = strings.TrimSpace(s.cfg.WorkspaceDir)
	}
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
	if err := os.MkdirAll(s.sessionDataDir(string(sess.ID())), 0o755); err != nil {
		return nil, err
	}
	workspaceRoot := s.workspaceRoot(string(sess.ID()))
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return nil, err
	}

	var approver sandbox.EscalationApprover
	if s.cfg.AutoApproveEscalation {
		approver = autoApproveEscalation
	}
	pythonTool, err := python.NewRunTool(python.RunToolOptions{
		Env: s.pythonEnv, Provider: s.sandboxProv, WorkspaceRoot: workspaceRoot,
		Mode: s.cfg.PythonMode, Timeout: s.cfg.PythonRunTimeout,
		DefaultNetwork: s.cfg.PythonNetwork, Approver: approver,
	})
	if err != nil {
		return nil, err
	}
	bashTool, bashJobResultTool, err := toolbash.NewTools(toolbash.ToolOptions{
		Executor:       bashsandbox.New(s.sandboxProv),
		WorkspaceRoot:  workspaceRoot,
		DefaultMode:    s.cfg.BashMode,
		DefaultNetwork: s.cfg.BashNetwork,
		Timeout:        s.cfg.BashTimeout,
		Approver:       approver,
	})
	if err != nil {
		return nil, err
	}
	readTool, writeTool, editTool, err := toolfs.NewTools(toolfs.ToolOptions{
		FS: fssandbox.New(workspaceRoot, s.cfg.FSMode), CWD: workspaceRoot,
	})
	if err != nil {
		return nil, err
	}

	toolRegistry := coretools.NewRegistry()
	if _, err := toolRegistry.Register(pythonTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(bashTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(bashJobResultTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(readTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(writeTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(editTool); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(listSkillsTool(s.skills)); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(loadSkillTool(s.skills)); err != nil {
		return nil, err
	}
	for _, def := range displayTools() {
		if _, err := toolRegistry.Register(def); err != nil {
			return nil, err
		}
	}
	if _, err := toolRegistry.Register(newAskUserQuestionTool(s.answers, string(sess.ID()))); err != nil {
		return nil, err
	}

	exitTool := &planmode.ExitTool{Controller: s.planController, Review: s.reviewPlan}
	multiTool := planmode.NewMultiTool().Add(coretools.Bridge{Registry: toolRegistry}).Add(exitTool)

	cwd := sess.Header().CWD
	if cwd == "" {
		cwd = workspaceRoot
	}
	prompt, err := s.buildSystemPrompt(cwd)
	if err != nil {
		return nil, err
	}

	// llmretry.Hook is stateful (per-(turn,step) retry counters) — built
	// fresh per agent here for the same reason the tools above are: shared
	// state across sessions would let one session's retries count against
	// another's turn/step numbers.
	ag := agentloop.NewAgent(sess, s.llm, multiTool, prompt, agentloop.Hooks{
		PreStep:      s.planController.PreStepHook(),
		RequestError: llmretry.Hook(s.cfg.Retry),
		// TurnEnded fires on every turn exit — completed, aborted (a
		// client's "stop" or a lost connection), or errored — unlike
		// relying on whichever HTTP handler happens to still be watching
		// when the turn goes idle (GET /sessions/{id}/stream only syncs
		// from inside its own poll loop, which a disconnected client never
		// reaches again unless it reconnects). Wiring the sync here instead
		// makes it unconditional: a turn that finishes with nobody
		// connected still lands on disk.
		TurnEnded: func(_ context.Context, _ int, _ string) { s.syncPersistence(sess) },
	}).WithModel(s.cfg.Model)
	if s.cfg.MaxTokens > 0 {
		ag = ag.WithMaxTokens(s.cfg.MaxTokens)
	}
	if s.cfg.Temperature != nil {
		ag = ag.WithTemperature(*s.cfg.Temperature)
	}
	if s.cfg.TopP != nil {
		ag = ag.WithTopP(*s.cfg.TopP)
	}
	if s.cfg.ReasoningEffort != "" {
		ag = ag.WithReasoningEffort(s.cfg.ReasoningEffort)
	}
	if s.cfg.MaxParallelToolCalls > 0 {
		ag = ag.WithMaxParallelToolCalls(s.cfg.MaxParallelToolCalls)
	}
	return ag, nil
}

func (s *Server) sessionDataDir(sessionID string) string {
	return filepath.Join(s.cfg.DataDir, "sessions", sessionID)
}

func (s *Server) sessionSandboxDir(sessionID string) string {
	return filepath.Join(s.sessionDataDir(sessionID), "workspace")
}

// workspaceRoot is where the agent reads and writes. When the user has
// configured workspace_dir, every session uses that folder so generated
// files land in the personal-space working directory. Otherwise each
// session keeps its own sandbox under data_dir.
func (s *Server) workspaceRoot(sessionID string) string {
	if root := strings.TrimSpace(s.cfg.WorkspaceDir); root != "" {
		return filepath.Clean(root)
	}
	return s.sessionSandboxDir(sessionID)
}

func (s *Server) reviewPlan(_ context.Context, _ string) (bool, string) {
	if s.cfg.AutoApprovePlan {
		return true, ""
	}
	return false, "plan review is disabled (agent.auto_approve_plan: false)"
}

func autoApproveEscalation(_ context.Context, _, _, _ string) sandbox.EscalationOutcome {
	return sandbox.AllowedOnce
}

func renderStringResult(_ map[string]any, value any) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
}

// SetConfigPath records which config.yaml the process loaded, so the
// settings endpoints know where to read and write it. Unset (the zero value)
// means settings can be viewed but not saved.
func (s *Server) SetConfigPath(path string) { s.configPath = path }

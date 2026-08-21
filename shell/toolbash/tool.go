// Package toolbash is a Go port of packages/shell/tool-bash (dsh-tool-bash)
// from DeepSeek Harness: the model-facing `bash` tool schema
// (command/workdir/run_in_background/sandbox_permissions/justification)
// and a companion bash_job_result tool for retrieving a background job's
// settled result — the TS original's own separate job-output retrieval
// path.
//
// This package imports go-harness/shell/bashsandbox for the executor;
// nothing imports back.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package toolbash

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-harness/core/session"
	"go-harness/core/tools"
	"go-harness/sandbox/sandbox"
	"go-harness/shell/bashsandbox"
)

// ToolOptions configures NewTools.
type ToolOptions struct {
	// Executor runs every command. Required.
	Executor *bashsandbox.Executor
	// WorkspaceRoot is the confinement policy's write-granted root for
	// every call. Required.
	WorkspaceRoot string
	// DefaultMode is the mode a call runs under absent a granted
	// escalation. Defaults to ReadOnly, matching the TS original.
	DefaultMode sandbox.Mode
	// DefaultNetwork, when true, grants outbound network on every call
	// without a network/justification pair. Defaults to false.
	DefaultNetwork bool
	// Approver, if set, lets a call request a wider mode for itself via
	// sandbox_permissions/justification — the same per-call escalation the
	// TS original wires through ctx.approval. A nil Approver (the default)
	// means every escalation request resolves to Unavailable — the same
	// documented fallback sandbox.ApproveEscalation itself has.
	Approver sandbox.EscalationApprover
	// Timeout bounds one command. Defaults to 30s.
	Timeout time.Duration
}

// jobStore tracks background jobs by an auto-assigned id, so a later
// bash_job_result call can retrieve one's settled result — this port's
// stand-in for whatever durable job registry the real deployment's shell
// layer keeps; here it's just an in-memory map scoped to one ToolOptions.
type jobStore struct {
	mu      sync.Mutex
	counter int
	jobs    map[string]*bashsandbox.Job
}

func (s *jobStore) add(j *bashsandbox.Job) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	id := "job-" + strconv.Itoa(s.counter)
	s.jobs[id] = j
	return id
}

func (s *jobStore) get(id string) (*bashsandbox.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

// NewTools builds the bash tool and its bash_job_result companion, sharing
// one background-job registry between them. Register both — bash's
// run_in_background output is useless without somewhere to redeem the job
// id it returns.
func NewTools(opts ToolOptions) (bashTool, jobTool tools.ToolDefinition, err error) {
	if opts.Executor == nil {
		return tools.ToolDefinition{}, tools.ToolDefinition{}, fmt.Errorf("toolbash: ToolOptions.Executor is required")
	}
	if opts.WorkspaceRoot == "" {
		return tools.ToolDefinition{}, tools.ToolDefinition{}, fmt.Errorf("toolbash: ToolOptions.WorkspaceRoot is required")
	}
	defaultMode := opts.DefaultMode
	if defaultMode == "" {
		defaultMode = sandbox.ReadOnly
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	store := &jobStore{jobs: map[string]*bashsandbox.Job{}}

	bashDesc := "Run a shell command via `bash -c`. No state (like `cd`) persists between calls — pass " +
		"`workdir` instead. Runs read-only and network-less by default; a denied command may be retried " +
		"once with sandbox_permissions (the narrowest wider mode that suffices) and/or network, plus " +
		"justification for whichever is set. Set run_in_background to get a job id back immediately instead " +
		"of waiting; retrieve its result with bash_job_result."
	if defaultMode == sandbox.DangerFullAccess {
		bashDesc = "Run a shell command via `bash -c`. No state (like `cd`) persists between calls — pass " +
			"`workdir` instead. The sandbox is fully open: every directory is readable and writable, and " +
			"outbound network is allowed. Set run_in_background to get a job id back immediately instead " +
			"of waiting; retrieve its result with bash_job_result."
	}
	workdirDesc := "working directory; defaults to the session workspace. Point it elsewhere (e.g. a " +
		"loaded skill's base directory) to resolve relative paths against that directory instead — " +
		"write access is still governed by the workspace root regardless of workdir"
	if defaultMode == sandbox.DangerFullAccess {
		workdirDesc = "working directory; defaults to the session workspace. Point it elsewhere (e.g. a " +
			"loaded skill's base directory) to resolve relative paths against that directory instead"
	}
	bashTool = tools.ToolDefinition{
		Name:        "bash",
		Description: bashDesc,
		Parameters: tools.ParameterSchema{
			"command": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "the command to run"}, Required: true},
			"workdir": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: workdirDesc}},
			"run_in_background": {ValueSchema: tools.ValueSchema{Kind: tools.KindBoolean,
				Description: "return a job id immediately instead of waiting for completion"}},
			"sandbox_permissions": {ValueSchema: tools.ValueSchema{
				Kind: tools.KindString, Enum: []any{string(sandbox.WorkspaceWrite), string(sandbox.DangerFullAccess)},
				Description: "request a wider mode for this call only",
			}},
			"network": {ValueSchema: tools.ValueSchema{Kind: tools.KindBoolean,
				Description: "request outbound network access for this call only (e.g. curl, pip/npm install, git clone) — independent of sandbox_permissions, doesn't widen file-write access"}},
			"justification": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "required alongside sandbox_permissions and/or network"}},
		},
		Output:    tools.Output{Schema: tools.ValueSchema{Kind: tools.KindString}, Render: renderBashValue},
		TimeoutMs: int(timeout.Milliseconds()),
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			return executeBash(ctx, opts, store, defaultMode, timeout, args)
		},
	}

	jobTool = tools.ToolDefinition{
		Name:        "bash_job_result",
		Description: "Retrieve a background bash job's status and, once settled, its output. Pass the job id bash returned when run_in_background was set.",
		Parameters: tools.ParameterSchema{
			"job_id": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "the job id returned by bash"}, Required: true},
		},
		Output: tools.Output{Schema: tools.ValueSchema{Kind: tools.KindString}, Render: renderBashValue},
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			return jobResult(store, args)
		},
	}

	return bashTool, jobTool, nil
}

func executeBash(ctx context.Context, opts ToolOptions, store *jobStore, defaultMode sandbox.Mode, timeout time.Duration, args map[string]any) (string, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}
	workdir, _ := args["workdir"].(string)
	background, _ := args["run_in_background"].(bool)

	mode, err := resolveMode(ctx, opts, defaultMode, args)
	if err != nil {
		return "", err
	}
	network, err := resolveNetwork(ctx, opts, args)
	if err != nil {
		return "", err
	}
	runOpts := bashsandbox.RunOptions{Command: command, WorkspaceRoot: opts.WorkspaceRoot, Workdir: workdir, Mode: mode, Network: network, Timeout: timeout}

	if background {
		job := opts.Executor.RunBackground(context.WithoutCancel(ctx), runOpts)
		id := store.add(job)
		return fmt.Sprintf("started in background, job_id: %s", id), nil
	}

	result, err := opts.Executor.Run(ctx, runOpts)
	if err != nil {
		return "", err
	}
	return renderRunResult(result), nil
}

// resolveMode honors a sandbox_permissions/justification pair via the same
// escalation state machine sandbox.ApproveEscalation defines — this is the
// per-call widening TS's tool-bash wires through ctx.approval; the executor
// itself (Run) never negotiates permissions, only this tool layer does.
func resolveMode(ctx context.Context, opts ToolOptions, defaultMode sandbox.Mode, args map[string]any) (sandbox.Mode, error) {
	requested, _ := args["sandbox_permissions"].(string)
	if requested == "" || defaultMode == sandbox.DangerFullAccess {
		return defaultMode, nil
	}
	justification, _ := args["justification"].(string)
	req := sandbox.EscalationRequest{
		RequestedMode: sandbox.Mode(requested),
		Justification: justification,
		EffectiveMode: defaultMode,
		Subject:       "this bash command",
	}
	widened, err := sandbox.ApproveEscalation(ctx, req, opts.Approver, "bash", "")
	if err != nil {
		return "", fmt.Errorf("escalation not granted: %w", err)
	}
	return widened, nil
}

// resolveNetwork honors a network/justification pair via the same
// human-in-the-loop approver resolveMode uses (opts.Approver) — network is
// a second, independent axis from Mode (see sandbox.ExecutionPolicy.Network),
// so this goes through the approver directly rather than
// sandbox.ApproveEscalation, which specifically validates one Mode is
// strictly wider than another and has nothing to say about networking.
func resolveNetwork(ctx context.Context, opts ToolOptions, args map[string]any) (bool, error) {
	if opts.DefaultNetwork {
		return true, nil
	}
	requested, _ := args["network"].(bool)
	if !requested {
		return false, nil
	}
	justification, _ := args["justification"].(string)
	if justification == "" {
		return false, fmt.Errorf("network requires a non-empty justification")
	}
	if opts.Approver == nil {
		return false, fmt.Errorf("escalation not granted: %w", &sandbox.EscalationDeniedError{Outcome: sandbox.Unavailable})
	}
	outcome := opts.Approver(ctx, "bash", "", justification)
	if outcome != sandbox.AllowedOnce {
		return false, fmt.Errorf("escalation not granted: %w", &sandbox.EscalationDeniedError{Outcome: outcome})
	}
	return true, nil
}

func jobResult(store *jobStore, args map[string]any) (string, error) {
	id, _ := args["job_id"].(string)
	job, ok := store.get(id)
	if !ok {
		return "", fmt.Errorf("no such job %q", id)
	}
	if job.Status() == bashsandbox.JobRunning {
		return "still running", nil
	}
	result, err := job.Wait(context.Background())
	if err != nil {
		return "", err
	}
	return renderRunResult(result), nil
}

func renderRunResult(r bashsandbox.RunResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit code: %d\n", r.ExitCode)
	if r.Sandbox.Denied {
		b.WriteString(sandbox.SandboxDenialMarker(r.Sandbox.Mode) + "\n")
	}
	if r.Stdout != "" {
		b.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if r.Stdout != "" {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n" + r.Stderr)
	}
	return strings.TrimSpace(b.String())
}

func renderBashValue(_ map[string]any, value any) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
}

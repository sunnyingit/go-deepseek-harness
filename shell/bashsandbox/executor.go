// Package bashsandbox is a Go port of packages/shell/bash-sandbox (dsh-bash
// -sandbox) from DeepSeek Harness: a sandboxed bash-command executor. Every
// command is confined by handing a sandbox.Provider the exact
// []string{"bash", "-c", command} argv and spawning whatever it returns —
// this package only handles bash orchestration (process supervision,
// timeout, output capture, denial classification); a Provider (see
// sandbox/local) picks the actual confinement backend and rewrites argv.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package bashsandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go-harness/sandbox/sandbox"
)

const maxOutputBytes = 64 * 1024
const defaultTimeout = 30 * time.Second

// RunOptions configures one command execution.
type RunOptions struct {
	// Command is executed as `bash -c <Command>`.
	Command string
	// WorkspaceRoot is the confinement policy's write-granted root. Not
	// necessarily where the command actually runs — see Workdir.
	WorkspaceRoot string
	// Workdir is the command's actual working directory (cmd.Dir).
	// Defaults to WorkspaceRoot. Pointing it elsewhere — e.g. a skill's own
	// bundle directory, so a command's relative paths resolve against the
	// skill's files without needing $(dirname "$0") tricks — only changes
	// path resolution; sandbox write access is still governed by
	// WorkspaceRoot's own granted roots (sandbox.WritableRoots) regardless
	// of Workdir, so a command writing a relative path outside
	// WorkspaceRoot is still denied under WorkspaceWrite.
	Workdir string
	// Mode is the confinement mode. Defaults to ReadOnly, matching the TS
	// original's own default.
	Mode sandbox.Mode
	// Network grants this run outbound network access when true — a second,
	// independent axis from Mode (see sandbox.ExecutionPolicy.Network).
	// Defaults to false: no run gets networking unless explicitly asked for.
	Network bool
	// Timeout bounds one run. Defaults to 30s.
	Timeout time.Duration
}

// SandboxOutcome reports what happened w.r.t. confinement for one run —
// mirrors the TS original's `sandbox: {...}` result field.
type SandboxOutcome struct {
	Mode sandbox.Mode
	// Network reports whether this run had outbound network access —
	// always true under DangerFullAccess (nothing sandboxes it), otherwise
	// whatever RunOptions.Network asked for.
	Network bool
	// Denied is a conservative, best-effort classification: stderr
	// contained one of the confining Provider's own DenialSignatures. A
	// command can still fail for its own unrelated reasons; this is a
	// hint, not a certainty.
	Denied bool
	// Enforcement reports the confining backend's own completeness claim
	// (see sandbox.Enforcement) — zero value under DangerFullAccess, where
	// no Provider is ever consulted.
	Enforcement sandbox.Enforcement
	// RunnerFailed is true when stderr matched one of the Provider's own
	// RunnerFailureRules — the sandbox infrastructure itself broke, as
	// opposed to it correctly denying something.
	RunnerFailed bool
}

// RunResult is one command's outcome, foreground or settled background.
type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Sandbox  SandboxOutcome
}

// Executor runs bash commands confined through a sandbox.Provider.
type Executor struct {
	Provider sandbox.Provider
}

func New(provider sandbox.Provider) *Executor { return &Executor{Provider: provider} }

// Run executes one command to completion (or Timeout/ctx cancellation). A
// non-zero exit is reported via RunResult.ExitCode, not err — err is
// reserved for infrastructure failure: the sandbox unavailable (as
// *sandbox.UnavailableError, retrievable via errors.As), the shell binary
// missing, or the run exceeding Timeout (as context.DeadlineExceeded,
// retrievable via errors.Is). This mirrors the TS original's "deny-only at
// the seam": Run reports what happened, it never negotiates permissions —
// a caller wanting to retry under a wider mode after a denial calls Run
// again with a different opts.Mode (see sandbox.ApproveEscalation for the
// state machine that decides whether it's allowed to, and toolbash.NewTools
// for where that's actually wired to a model-facing parameter).
func (e *Executor) Run(ctx context.Context, opts RunOptions) (RunResult, error) {
	mode := opts.Mode
	if mode == "" {
		mode = sandbox.ReadOnly
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	workdir := opts.Workdir
	if workdir == "" {
		workdir = opts.WorkspaceRoot
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := []string{"bash", "-c", opts.Command}

	// danger-full-access bypasses confinement entirely — the provider is
	// never consulted, matching the TS original exactly. Real network
	// access either way: nothing sandboxes this process at all.
	if mode == sandbox.DangerFullAccess {
		return e.spawn(runCtx, argv, workdir, SandboxOutcome{Mode: mode, Network: true}, nil)
	}

	policy, err := sandbox.NewPolicy(mode, opts.WorkspaceRoot, "")
	if err != nil {
		return RunResult{}, err
	}
	policy.Network = opts.Network
	confined, err := e.Provider.Confine(argv, policy)
	if err != nil {
		return RunResult{}, fmt.Errorf("bashsandbox: %w", err)
	}

	return e.spawn(runCtx, confined.Argv, workdir, SandboxOutcome{Mode: mode, Network: opts.Network, Enforcement: confined.Enforcement}, &confined)
}

func (e *Executor) spawn(ctx context.Context, argv []string, dir string, outcome SandboxOutcome, confined *sandbox.ConfinedArgv) (RunResult, error) {
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxOutputBytes, maxOutputBytes

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := sandbox.RunSupervised(ctx, cmd)
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	if confined != nil {
		outcome.Denied = containsAny(stderr.String(), confined.DenialSignatures)
		outcome.RunnerFailed = matchesAnyRule(stderr.String(), confined.RunnerFailureRules)
	}

	result := RunResult{ExitCode: exitCode, Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String()), Sandbox: outcome}

	if runErr != nil {
		if ctx.Err() != nil {
			return result, ctx.Err() // context.DeadlineExceeded (or Canceled), unwrapped
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return result, nil // a non-zero exit is reported via ExitCode, not an error
		}
		return result, fmt.Errorf("bashsandbox: %w", runErr) // e.g. the shell binary itself is missing
	}
	return result, nil
}

func containsAny(s string, signatures []string) bool {
	for _, sig := range signatures {
		if sig != "" && strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

func matchesAnyRule(s string, rules []sandbox.RunnerFailureRule) bool {
	for _, r := range rules {
		if r.Pattern != "" && strings.Contains(s, r.Pattern) {
			return true
		}
	}
	return false
}

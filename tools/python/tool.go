package python

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go-harness/core/session"
	"go-harness/core/tools"
	"go-harness/sandbox/sandbox"
)

// maxOutputBytes caps how much of a script's combined stdout/stderr this
// tool holds in memory and hands back to the model — a runaway print loop
// must not exhaust the process, and the model gains nothing from megabytes
// of output it can't usefully read anyway.
const maxOutputBytes = 64 * 1024

// RunToolOptions configures NewRunTool.
type RunToolOptions struct {
	// Env is the provisioned interpreter scripts run against. Required —
	// build one with EnsureEnv first.
	Env *Env
	// Provider confines the actual script execution. Required.
	Provider sandbox.Provider
	// WorkspaceRoot is the directory scripts may write inside, and where
	// the model-supplied script itself is staged. Required.
	WorkspaceRoot string
	// Mode is the confinement mode scripts run under. Defaults to
	// WorkspaceWrite — a document-processing tool that can't write its
	// output isn't useful; ReadOnly is valid for a script that only reads
	// and reports.
	Mode sandbox.Mode
	// Timeout bounds one script run, enforced by ToolDefinition.TimeoutMs
	// (see core/tools/registry.go's dispatch). Defaults to 30s.
	Timeout time.Duration
}

// NewRunTool builds the run_python tool: it stages the model-supplied code
// as a script file under WorkspaceRoot, confines `<venv python> <script>`
// through Provider, runs it, and returns combined stdout+stderr. A non-zero
// exit or a timeout becomes an ordinary tool failure the model sees like
// any other tool error — never a Go panic or a hung process, since
// exec.CommandContext is bound by the same context TimeoutMs cancels.
func NewRunTool(opts RunToolOptions) (tools.ToolDefinition, error) {
	if opts.Env == nil {
		return tools.ToolDefinition{}, fmt.Errorf("python: RunToolOptions.Env is required")
	}
	if opts.Provider == nil {
		return tools.ToolDefinition{}, fmt.Errorf("python: RunToolOptions.Provider is required")
	}
	if opts.WorkspaceRoot == "" {
		return tools.ToolDefinition{}, fmt.Errorf("python: RunToolOptions.WorkspaceRoot is required")
	}
	mode := opts.Mode
	if mode == "" {
		mode = sandbox.WorkspaceWrite
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	desc := fmt.Sprintf(
		"Run a Python script in a sandboxed interpreter pre-installed with: %s. "+
			"Use it to read or produce Word/PowerPoint/Excel/PDF documents and other files under the workspace. "+
			"Print anything you want returned to yourself — stdout and stderr are both captured.",
		strings.Join(opts.Env.Packages, ", "),
	)
	if mode == sandbox.DangerFullAccess {
		desc = fmt.Sprintf(
			"Run a Python script in an interpreter pre-installed with: %s. "+
				"The sandbox is fully open: every directory is readable and writable. "+
				"Use it to read or produce Word/PowerPoint/Excel/PDF documents. "+
				"Print anything you want returned to yourself — stdout and stderr are both captured.",
			strings.Join(opts.Env.Packages, ", "),
		)
	}

	return tools.ToolDefinition{
		Name:        "run_python",
		Description: desc,
		Parameters: tools.ParameterSchema{
			"code": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "the complete Python script to run"}, Required: true},
		},
		Output: tools.Output{
			Schema: tools.ValueSchema{Kind: tools.KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		TimeoutMs: int(timeout.Milliseconds()),
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			code, _ := args["code"].(string)
			return runScript(ctx, opts, mode, code)
		},
	}, nil
}

func runScript(ctx context.Context, opts RunToolOptions, mode sandbox.Mode, code string) (string, error) {
	scriptFile, err := os.CreateTemp(opts.WorkspaceRoot, "run-*.py")
	if err != nil {
		return "", fmt.Errorf("staging script: %w", err)
	}
	defer os.Remove(scriptFile.Name())
	if _, err := scriptFile.WriteString(code); err != nil {
		scriptFile.Close()
		return "", fmt.Errorf("staging script: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return "", fmt.Errorf("staging script: %w", err)
	}

	argv := []string{opts.Env.PythonPath, scriptFile.Name()}
	if mode != sandbox.DangerFullAccess {
		policy, err := sandbox.NewPolicy(mode, opts.WorkspaceRoot, "")
		if err != nil {
			return "", err
		}
		confined, err := opts.Provider.Confine(argv, policy)
		if err != nil {
			return "", err
		}
		argv = confined.Argv
	}

	output, err := runConfined(ctx, argv, opts.WorkspaceRoot)
	if err != nil {
		return "", fmt.Errorf("script %s", err)
	}
	return output, nil
}

// runConfined runs a sandbox.Provider.Confine-wrapped argv with dir as its
// working directory, capturing up to maxOutputBytes of combined
// stdout+stderr, and distinguishing a context-cancelled run ("timed out")
// from any other non-zero exit ("failed"). The actual process supervision
// (starting argv, killing its whole process group on ctx cancellation —
// necessary because argv[0] is a sandbox wrapper that can fork an
// unsupervised grandchild) lives in sandbox.RunSupervised, shared with
// shell/bashsandbox's executor.
func runConfined(ctx context.Context, argv []string, dir string) (output string, err error) {
	out := &limitedBuffer{max: maxOutputBytes}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	runErr := sandbox.RunSupervised(ctx, cmd)
	captured := strings.TrimSpace(out.String())
	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out: %s", captured)
		}
		return "", fmt.Errorf("failed: %v: %s", runErr, captured)
	}
	return captured, nil
}

// limitedBuffer caps how many bytes it retains — an io.Writer, so it can be
// used directly as an exec.Cmd's Stdout/Stderr.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil // report success — the process must not see a write error over this
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *limitedBuffer) String() string {
	if w.truncated {
		return w.buf.String() + "\n...[output truncated]"
	}
	return w.buf.String()
}

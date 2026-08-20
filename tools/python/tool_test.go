package python

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go-harness/core/tools"
	"go-harness/sandbox/local"
	"go-harness/sandbox/sandbox"
)

func systemPythonEnv(t *testing.T) *Env {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	return &Env{Root: filepath.Dir(path), PythonPath: path, Packages: []string{"(system interpreter, no venv)"}}
}

func skipUnlessSandboxAvailable(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no sandbox backend on this platform")
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Skip("sandbox-exec not on PATH")
		}
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err != nil {
			t.Skip("bwrap not on PATH")
		}
	}
}

// runViaRegistry registers def and dispatches through a real
// tools.Registry — the same path core/agentloop's Bridge uses in
// production, and critically the only path that actually applies
// ToolDefinition.TimeoutMs (Registry.dispatch wraps the context; calling
// def.Execute directly, bypassing the Registry, would silently skip that
// enforcement — the mistake that first hid TestRunToolTimeout's hang below).
func runViaRegistry(t *testing.T, def tools.ToolDefinition, code string) (tools.ExecutionResult, error) {
	t.Helper()
	r := tools.NewRegistry()
	if _, err := r.Register(def); err != nil {
		return tools.ExecutionResult{}, err
	}
	result := r.Execute(context.Background(), tools.ExecutionInput{CallID: "c1", Name: def.Name, Arguments: map[string]any{"code": code}})
	return result, nil
}

func TestNewRunToolValidatesRequiredFields(t *testing.T) {
	env := &Env{PythonPath: "/usr/bin/python3"}
	provider := local.New()
	workspace := t.TempDir()

	if _, err := NewRunTool(RunToolOptions{Provider: provider, WorkspaceRoot: workspace}); err == nil {
		t.Fatal("want an error for a missing Env")
	}
	if _, err := NewRunTool(RunToolOptions{Env: env, WorkspaceRoot: workspace}); err == nil {
		t.Fatal("want an error for a missing Provider")
	}
	if _, err := NewRunTool(RunToolOptions{Env: env, Provider: provider}); err == nil {
		t.Fatal("want an error for a missing WorkspaceRoot")
	}
}

func TestRunToolExecutesScriptAndCapturesStdout(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	workspace := t.TempDir()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result, err := runViaRegistry(t, def, "print('hello from sandboxed python')")
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if result.Value.(string) != "hello from sandboxed python" {
		t.Fatalf("value = %q", result.Value)
	}
}

func TestRunToolWorkspaceWriteAllowsFileCreation(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	workspace := t.TempDir()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: workspace, Mode: sandbox.WorkspaceWrite})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	script := `
with open("output.txt", "w") as f:
    f.write("written by the sandboxed script")
print("done")
`
	result, err := runViaRegistry(t, def, script)
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}

	// The script has no cwd override, so it resolves "output.txt" against
	// whatever directory the confined process actually started in — assert
	// against the workspace, which is where run_python stages the script.
	data, err := os.ReadFile(filepath.Join(workspace, "output.txt"))
	if err != nil {
		t.Fatalf("expected output.txt in the workspace: %v", err)
	}
	if string(data) != "written by the sandboxed script" {
		t.Fatalf("output.txt content = %q", data)
	}
}

func TestRunToolReadOnlyDeniesFileWrite(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	workspace := t.TempDir()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: workspace, Mode: sandbox.ReadOnly})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	script := `
with open("should-not-exist.txt", "w") as f:
    f.write("this must be denied")
`
	result, err := runViaRegistry(t, def, script)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("want an error — read-only must deny the write")
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "should-not-exist.txt")); statErr == nil {
		t.Fatal("the denied write must not have happened")
	}
}

func TestRunToolTimeout(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	workspace := t.TempDir()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: workspace, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	start := time.Now()
	result, err := runViaRegistry(t, def, "import time; time.sleep(60)")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("want a timeout error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v, want the 300ms timeout to actually bound it", elapsed)
	}
}

func TestRunToolCapsOutputSize(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	workspace := t.TempDir()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result, err := runViaRegistry(t, def, `print("x" * 1_000_000)`)
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	out := result.Value.(string)
	if len(out) > maxOutputBytes+100 {
		t.Fatalf("output length %d, want it bounded near maxOutputBytes=%d", len(out), maxOutputBytes)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("output = %q, want a truncation marker", out[:min(200, len(out))])
	}
}

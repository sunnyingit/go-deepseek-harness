package python

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	return runViaRegistryArgs(t, def, map[string]any{"code": code}), nil
}

func runViaRegistryArgs(t *testing.T, def tools.ToolDefinition, args map[string]any) tools.ExecutionResult {
	t.Helper()
	r := tools.NewRegistry()
	if _, err := r.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	return r.Execute(context.Background(), tools.ExecutionInput{CallID: "c1", Name: def.Name, Arguments: args})
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

func TestRunToolDangerFullAccessBypassesProvider(t *testing.T) {
	env := systemPythonEnv(t)
	workspace := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "unconfined.txt")

	def, err := NewRunTool(RunToolOptions{
		Env: env, Provider: panicProvider{t}, WorkspaceRoot: workspace, Mode: sandbox.DangerFullAccess,
	})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	script := fmt.Sprintf(`
with open(%q, "w") as f:
    f.write("ok")
print("done")
`, target)
	result, err := runViaRegistry(t, def, script)
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected write outside workspace: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("content = %q", data)
	}
}

type panicProvider struct{ t *testing.T }

func (p panicProvider) Confine([]string, sandbox.Policy) (sandbox.ConfinedArgv, error) {
	p.t.Fatal("Confine must never be called under danger-full-access")
	return sandbox.ConfinedArgv{}, nil
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

// urlopenScript builds a script that tries to reach url and prints "reached"
// or "blocked" depending on whether the request got through — used to probe
// whether policy.Network actually made it to the confined process, the same
// way shell/toolbash's own network tests probe with curl.
func urlopenScript(url string) string {
	return fmt.Sprintf(`
import urllib.request
try:
    urllib.request.urlopen(%q, timeout=3)
    print("reached")
except Exception:
    print("blocked")
`, url)
}

func TestRunToolWithoutNetworkRequestDeniesOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: t.TempDir(), Mode: sandbox.WorkspaceWrite})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result, err := runViaRegistry(t, def, urlopenScript(srv.URL))
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if !strings.Contains(result.Value.(string), "blocked") {
		t.Fatalf("output = %q, want the request denied by default", result.Value)
	}
}

func TestRunToolNetworkRequestWithoutApproverIsDenied(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: t.TempDir(), Mode: sandbox.WorkspaceWrite}) // no Approver
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result := runViaRegistryArgs(t, def, map[string]any{"code": "print('hi')", "network": true, "justification": "need it"})
	if !result.IsError {
		t.Fatal("want an error — no approver means a network request is always denied")
	}
}

func TestRunToolNetworkRequestWithoutJustificationIsDenied(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	approver := sandbox.EscalationApprover(func(context.Context, string, string, string) sandbox.EscalationOutcome {
		return sandbox.AllowedOnce
	})

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: t.TempDir(), Mode: sandbox.WorkspaceWrite, Approver: approver})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result := runViaRegistryArgs(t, def, map[string]any{"code": "print('hi')", "network": true})
	if !result.IsError {
		t.Fatal("want an error — network requires a justification even with an approver set")
	}
}

func TestRunToolNetworkApprovedAllowsOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	approver := sandbox.EscalationApprover(func(context.Context, string, string, string) sandbox.EscalationOutcome {
		return sandbox.AllowedOnce
	})

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: t.TempDir(), Mode: sandbox.WorkspaceWrite, Approver: approver})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result := runViaRegistryArgs(t, def, map[string]any{"code": urlopenScript(srv.URL), "network": true, "justification": "need it"})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Value.(string), "reached") {
		t.Fatalf("output = %q, want the network-approved request to succeed", result.Value)
	}
}

func TestRunToolDefaultNetworkGrantsAccessWithoutRequest(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	env := systemPythonEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	def, err := NewRunTool(RunToolOptions{Env: env, Provider: local.New(), WorkspaceRoot: t.TempDir(), Mode: sandbox.WorkspaceWrite, DefaultNetwork: true})
	if err != nil {
		t.Fatalf("NewRunTool: %v", err)
	}

	result, err := runViaRegistry(t, def, urlopenScript(srv.URL))
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if !strings.Contains(result.Value.(string), "reached") {
		t.Fatalf("output = %q, want DefaultNetwork to grant access with no per-call request", result.Value)
	}
}

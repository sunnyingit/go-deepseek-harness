package toolbash

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"go-harness/core/tools"
	"go-harness/sandbox/local"
	"go-harness/sandbox/sandbox"
	"go-harness/shell/bashsandbox"
)

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

func runViaRegistry(t *testing.T, defs []tools.ToolDefinition, name string, args map[string]any) tools.ExecutionResult {
	t.Helper()
	r := tools.NewRegistry()
	for _, def := range defs {
		if _, err := r.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return r.Execute(context.Background(), tools.ExecutionInput{CallID: "c1", Name: name, Arguments: args})
}

func TestNewToolsValidatesRequiredFields(t *testing.T) {
	e := bashsandbox.New(local.New())
	if _, _, err := NewTools(ToolOptions{WorkspaceRoot: t.TempDir()}); err == nil {
		t.Fatal("want an error for a missing Executor")
	}
	if _, _, err := NewTools(ToolOptions{Executor: e}); err == nil {
		t.Fatal("want an error for a missing WorkspaceRoot")
	}
}

func TestBashToolExecutesCommand(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{"command": "echo hi"})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	out := result.Value.(string)
	if !strings.Contains(out, "exit code: 0") || !strings.Contains(out, "hi") {
		t.Fatalf("output = %q", out)
	}
}

func TestBashToolNonZeroExitIsNotAToolError(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{"command": "exit 3"})
	if result.IsError {
		t.Fatalf("a non-zero exit must not be a tool error: %+v", result)
	}
	if !strings.Contains(result.Value.(string), "exit code: 3") {
		t.Fatalf("output = %q", result.Value)
	}
}

func TestBashToolEscalationWithoutApproverIsDenied(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()}) // no Approver
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "echo x > allowed.txt", "sandbox_permissions": string(sandbox.WorkspaceWrite), "justification": "need it",
	})
	if !result.IsError {
		t.Fatal("want an error — no approver means escalation is always Unavailable")
	}
}

func TestBashToolEscalationApprovedWidensMode(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	approver := sandbox.EscalationApprover(func(context.Context, string, string, string) sandbox.EscalationOutcome {
		return sandbox.AllowedOnce
	})
	workspace := t.TempDir()
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: workspace, Approver: approver})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "echo x > allowed.txt", "sandbox_permissions": string(sandbox.WorkspaceWrite), "justification": "need it",
	})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Value.(string), "exit code: 0") {
		t.Fatalf("output = %q, want the escalated write to succeed", result.Value)
	}
}

func TestBashToolNetworkRequestWithoutApproverIsDenied(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()}) // no Approver
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "echo hi", "network": true, "justification": "need it",
	})
	if !result.IsError {
		t.Fatal("want an error — no approver means a network request is always denied")
	}
}

func TestBashToolNetworkRequestWithoutJustificationIsDenied(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	approver := sandbox.EscalationApprover(func(context.Context, string, string, string) sandbox.EscalationOutcome {
		return sandbox.AllowedOnce
	})
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir(), Approver: approver})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "echo hi", "network": true,
	})
	if !result.IsError {
		t.Fatal("want an error — network requires a justification even with an approver set")
	}
}

func TestBashToolNetworkApprovedAllowsOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	e := bashsandbox.New(local.New())
	approver := sandbox.EscalationApprover(func(context.Context, string, string, string) sandbox.EscalationOutcome {
		return sandbox.AllowedOnce
	})
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir(), Approver: approver})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "curl -s -m 3 -o /dev/null -f -w '%{http_code}' " + srv.URL, "network": true, "justification": "need it",
	})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Value.(string), "exit code: 0") {
		t.Fatalf("output = %q, want the network-approved curl to succeed", result.Value)
	}
}

func TestBashToolWithoutNetworkRequestDeniesOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash", map[string]any{
		"command": "curl -s -m 3 -o /dev/null -f " + srv.URL,
	})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(result.Value.(string), "exit code: 0") {
		t.Fatal("want curl denied — bash without a network request must not have outbound access by default")
	}
}

func TestBashToolBackgroundJobRetrievableViaJobResultTool(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defs := []tools.ToolDefinition{bashTool, jobTool}

	start := runViaRegistry(t, defs, "bash", map[string]any{"command": "echo backgrounded", "run_in_background": true})
	if start.IsError {
		t.Fatalf("result = %+v", start)
	}
	started := start.Value.(string)
	if !strings.Contains(started, "job_id:") {
		t.Fatalf("output = %q, want a job_id", started)
	}
	jobID := strings.TrimSpace(strings.Split(strings.Split(started, "job_id: ")[1], " ")[0])

	var got string
	for i := 0; i < 50; i++ {
		poll := runViaRegistry(t, defs, "bash_job_result", map[string]any{"job_id": jobID})
		if poll.IsError {
			t.Fatalf("poll result = %+v", poll)
		}
		got = poll.Value.(string)
		if got != "still running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(got, "backgrounded") {
		t.Fatalf("final job result = %q", got)
	}
}

func TestBashJobResultUnknownJobIsAnError(t *testing.T) {
	e := bashsandbox.New(local.New())
	bashTool, jobTool, err := NewTools(ToolOptions{Executor: e, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	result := runViaRegistry(t, []tools.ToolDefinition{bashTool, jobTool}, "bash_job_result", map[string]any{"job_id": "no-such-job"})
	if !result.IsError {
		t.Fatal("want an error for an unknown job id")
	}
}

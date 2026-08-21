package bashsandbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"go-harness/sandbox/local"
	"go-harness/sandbox/sandbox"
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

func TestRunDefaultModeIsReadOnly(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	workspace := t.TempDir()

	result, err := e.Run(context.Background(), RunOptions{
		Command: "echo denied > should-not-exist.txt", WorkspaceRoot: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 {
		t.Fatal("want a non-zero exit — read-only must deny the write")
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "should-not-exist.txt")); statErr == nil {
		t.Fatal("the denied write must not have happened")
	}
}

func TestRunWorkspaceWriteAllowsWrite(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	workspace := t.TempDir()

	result, err := e.Run(context.Background(), RunOptions{
		Command: "echo hello > allowed.txt", WorkspaceRoot: workspace, Mode: sandbox.WorkspaceWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", result.ExitCode, result.Stderr)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "allowed.txt"))
	if err != nil || string(data) != "hello\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestRunCapturesNonZeroExitWithoutError(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	result, err := e.Run(context.Background(), RunOptions{Command: "exit 7", WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("a non-zero exit must not be a Go error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", result.ExitCode)
	}
}

func TestRunCapturesStdoutAndStderrSeparately(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	result, err := e.Run(context.Background(), RunOptions{
		Command: "echo to-stdout; echo to-stderr 1>&2", WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "to-stdout" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if result.Stderr != "to-stderr" {
		t.Fatalf("stderr = %q", result.Stderr)
	}
}

func TestRunNetworkFalseDeniesOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	e := New(local.New())
	result, err := e.Run(context.Background(), RunOptions{
		Command: "curl -s -m 3 -o /dev/null -f " + srv.URL, WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 {
		t.Fatal("want curl denied network access when RunOptions.Network is false (the default)")
	}
}

func TestRunNetworkTrueAllowsOutboundAccess(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	e := New(local.New())
	result, err := e.Run(context.Background(), RunOptions{
		Command: "curl -s -m 3 -o /dev/null -f " + srv.URL, WorkspaceRoot: t.TempDir(), Network: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q, want curl to succeed when RunOptions.Network is true", result.ExitCode, result.Stderr)
	}
	if !result.Sandbox.Network {
		t.Fatal("Sandbox.Network = false, want true")
	}
}

func TestRunDangerFullAccessBypassesProvider(t *testing.T) {
	e := New(panicProvider{t}) // must never be called
	result, err := e.Run(context.Background(), RunOptions{
		Command: "echo unconfined", WorkspaceRoot: t.TempDir(), Mode: sandbox.DangerFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "unconfined" {
		t.Fatalf("result = %+v", result)
	}
	if result.Sandbox.Mode != sandbox.DangerFullAccess {
		t.Fatalf("Sandbox.Mode = %q", result.Sandbox.Mode)
	}
	if !result.Sandbox.Network {
		t.Fatal("Sandbox.Network = false, want true — nothing sandboxes a danger-full-access run")
	}
}

type panicProvider struct{ t *testing.T }

func (p panicProvider) Confine([]string, sandbox.Policy) (sandbox.ConfinedArgv, error) {
	p.t.Fatal("Confine must never be called under danger-full-access")
	return sandbox.ConfinedArgv{}, nil
}

func TestRunTimeout(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	start := time.Now()
	_, err := e.Run(context.Background(), RunOptions{
		Command: "sleep 60", WorkspaceRoot: t.TempDir(), Timeout: 300 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %v, want the 300ms timeout to actually bound it", elapsed)
	}
}

func TestRunDeniedClassification(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	result, err := e.Run(context.Background(), RunOptions{
		Command: "echo denied > /should-not-exist-anywhere.txt", WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == 0 {
		t.Fatal("want a non-zero exit")
	}
	if !result.Sandbox.Denied {
		t.Fatalf("Sandbox.Denied = false, want true — stderr was %q", result.Stderr)
	}
}

func TestRunPropagatesUnavailableProvider(t *testing.T) {
	e := New(unavailableProvider{})
	_, err := e.Run(context.Background(), RunOptions{Command: "echo hi", WorkspaceRoot: t.TempDir()})
	var unavail *sandbox.UnavailableError
	if !errors.As(err, &unavail) {
		t.Fatalf("err = %v, want *sandbox.UnavailableError", err)
	}
}

type unavailableProvider struct{}

func (unavailableProvider) Confine([]string, sandbox.Policy) (sandbox.ConfinedArgv, error) {
	return sandbox.ConfinedArgv{}, &sandbox.UnavailableError{Reason: "test"}
}

package bashsandbox

import (
	"context"
	"testing"
	"time"

	"go-harness/sandbox/local"
)

func TestRunBackgroundReturnsImmediately(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	start := time.Now()
	job := e.RunBackground(context.Background(), RunOptions{Command: "sleep 1", WorkspaceRoot: t.TempDir()})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("RunBackground blocked for %v, want it to return immediately", elapsed)
	}
	if job.Status() != JobRunning {
		t.Fatalf("Status() = %q, want %q immediately after starting", job.Status(), JobRunning)
	}
}

func TestJobWaitBlocksUntilSettled(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	job := e.RunBackground(context.Background(), RunOptions{Command: "echo done", WorkspaceRoot: t.TempDir()})

	result, err := job.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "done" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if job.Status() != JobCompleted {
		t.Fatalf("Status() = %q after Wait returned, want %q", job.Status(), JobCompleted)
	}
}

func TestJobWaitRespectsItsOwnContext(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	job := e.RunBackground(context.Background(), RunOptions{Command: "sleep 5", WorkspaceRoot: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := job.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded — Wait's own ctx must not block on the job forever", err)
	}
}

func TestJobFailedStatusOnInfrastructureError(t *testing.T) {
	e := New(unavailableProvider{})
	job := e.RunBackground(context.Background(), RunOptions{Command: "echo hi", WorkspaceRoot: t.TempDir()})
	if _, err := job.Wait(context.Background()); err == nil {
		t.Fatal("want an error from an unavailable provider")
	}
	if job.Status() != JobFailed {
		t.Fatalf("Status() = %q, want %q", job.Status(), JobFailed)
	}
}

func TestMultipleWaitersSeeTheSameResult(t *testing.T) {
	skipUnlessSandboxAvailable(t)
	e := New(local.New())
	job := e.RunBackground(context.Background(), RunOptions{Command: "echo shared", WorkspaceRoot: t.TempDir()})

	done := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result, _ := job.Wait(context.Background())
			done <- result.Stdout
		}()
	}
	for i := 0; i < 2; i++ {
		if got := <-done; got != "shared" {
			t.Fatalf("waiter got %q, want %q", got, "shared")
		}
	}
}

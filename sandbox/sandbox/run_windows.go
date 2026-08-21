//go:build windows

package sandbox

import (
	"context"
	"os/exec"
)

// RunSupervised on Windows: best-effort, no process-group semantics — this
// port's Provider has no Windows runner and always fails closed (see
// sandbox/local's README), so a caller only ever reaches this with an
// unconfined cmd in practice; it exists so the package compiles.
func RunSupervised(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		<-done
		return ctx.Err()
	}
}

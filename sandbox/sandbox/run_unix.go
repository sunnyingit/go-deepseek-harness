//go:build !windows

package sandbox

import (
	"context"
	"os/exec"
	"syscall"
)

// RunSupervised starts cmd — its Stdout/Stderr/Dir already configured by
// the caller — in its own process group and waits for it, killing the
// whole group, not just the direct child, if ctx is cancelled first.
//
// This matters specifically when cmd's argv is a Provider.Confine-wrapped
// command: argv[0] is a *wrapper* (sandbox-exec, bwrap) that execs or forks
// the real program as a grandchild. exec.CommandContext's default
// cancellation only signals the direct child. Seatbelt's sandbox-exec
// re-execs in place, so the grandchild problem doesn't arise there, but
// bwrap forks and supervises its target — killing only the wrapper leaves
// the actual process (and whatever it's doing to the filesystem) running
// unbounded, and leaves Cmd.Wait() blocked forever on a stdout pipe an
// orphaned grandchild still holds open. Setpgid plus a negative-PID kill
// reaches every process in the group in one signal, wrapper included.
//
// This lives here, not in a specific tool package, because the grandchild
// problem is a property of sandbox wrapping itself — every caller that
// spawns a Provider.Confine result needs the same supervision, regardless
// of what it's actually running (tools/python's interpreter, shell
// /bashsandbox's arbitrary commands).
func RunSupervised(ctx context.Context, cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done // reap: Wait must still be called exactly once
		return ctx.Err()
	}
}

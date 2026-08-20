//go:build !windows

package python

import (
	"context"
	"io"
	"os/exec"
	"syscall"
)

// runConfined starts argv in its own process group and kills the whole
// group on ctx cancellation, not just the direct child.
//
// This matters specifically because argv's first element is a *wrapper*
// (sandbox-exec, bwrap) that execs or forks the real interpreter as a
// grandchild: exec.CommandContext's default cancellation only signals the
// direct child. Seatbelt's sandbox-exec re-execs in place (the grandchild
// problem doesn't arise there), but bwrap forks and supervises its target,
// so killing only the wrapper leaves the actual Python process — and
// whatever it's doing to the filesystem — running unbounded, and leaves
// Cmd.Wait() blocked forever waiting for a stdout pipe an orphaned
// grandchild still holds open. Setpgid plus a negative-PID kill reaches
// every process in the group in one signal, wrapper included.
func runConfined(ctx context.Context, argv []string, dir string, out io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
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

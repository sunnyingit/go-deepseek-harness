//go:build windows

package python

import (
	"context"
	"io"
	"os/exec"
)

// runConfined on Windows: best-effort, plain exec.CommandContext. This
// package's whole confined-execution path is moot on Windows anyway —
// sandbox/local.Provider has no Windows runner and always fails closed
// (see sandbox/local/README.md) — so run_python's Execute never reaches
// this function there in practice; it exists only so the package compiles.
func runConfined(ctx context.Context, argv []string, dir string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

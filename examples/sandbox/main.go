// Demo: sandbox/local.Provider actually confining real subprocesses — no
// mocking. Builds a temp workspace, then runs the same `touch` three ways:
// read-only (denied), workspace-write inside the workspace (allowed), and
// workspace-write outside it (denied) — proving the policy is enforced by
// the OS, not just described by this package.
//
//	go run .
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go-harness/sandbox/local"
	"go-harness/sandbox/sandbox"
)

func main() {
	workspace, err := os.MkdirTemp("", "sandbox-demo-workspace-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(workspace)

	// A directory under os.MkdirTemp("", ...) always nests inside
	// os.TempDir(), which WorkspaceWrite intentionally grants in full (the
	// workspace *and* the platform temp areas) — so it can't stand in for
	// "outside the granted roots." A directory under the current working
	// directory genuinely is.
	outside, err := os.MkdirTemp(".", "sandbox-demo-outside-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(outside)
	if outside, err = filepath.Abs(outside); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("workspace:", workspace)
	fmt.Println("outside:  ", outside)
	fmt.Println()

	provider := local.New()

	attempt(provider, sandbox.ReadOnly, workspace, filepath.Join(workspace, "a.txt"), "write inside workspace, read-only mode")
	attempt(provider, sandbox.WorkspaceWrite, workspace, filepath.Join(workspace, "b.txt"), "write inside workspace, workspace-write mode")
	attempt(provider, sandbox.WorkspaceWrite, workspace, filepath.Join(outside, "c.txt"), "write outside workspace, workspace-write mode")
}

func attempt(provider *local.Provider, mode sandbox.Mode, workspaceRoot, target, label string) {
	fmt.Println("---", label, "---")

	policy, err := sandbox.NewPolicy(mode, workspaceRoot, "")
	if err != nil {
		fmt.Println("policy error:", err)
		return
	}

	confined, err := provider.Confine([]string{"/usr/bin/touch", target}, policy)
	if err != nil {
		var unavailable *sandbox.UnavailableError
		if errors.As(err, &unavailable) {
			fmt.Println("sandbox unavailable on this platform:", err)
			fmt.Println("(this is the documented fail-closed behavior, not a bug)")
			return
		}
		fmt.Println("confine error:", err)
		return
	}

	cmd := exec.Command(confined.Argv[0], confined.Argv[1:]...)
	stderr, runErr := cmd.CombinedOutput()

	if runErr == nil {
		fmt.Println("result: ALLOWED —", target, "was written")
	} else {
		fmt.Println("result: DENIED —", denialReason(confined, string(stderr)))
	}
	fmt.Println()
}

func denialReason(confined sandbox.ConfinedArgv, stderr string) string {
	for _, rule := range confined.RunnerFailureRules {
		if strings.Contains(stderr, rule.Pattern) {
			return rule.Meaning
		}
	}
	return "command failed under confinement (no known runner-failure-rule matched: " + strings.TrimSpace(stderr) + ")"
}

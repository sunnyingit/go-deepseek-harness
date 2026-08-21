// The Linux runner: wraps argv with bubblewrap (bwrap), the unprivileged
// namespace sandboxing tool most Linux container/CI images ship. Structural
// port only — this development machine has no Linux/bwrap to run it
// against, so unlike seatbelt.go this is verified by argv-shape tests, not
// a real confined process. See README.md.
package local

import (
	"fmt"
	"os/exec"
	"runtime"

	"go-harness/sandbox/sandbox"
)

type bwrapRunner struct{}

func newBwrapRunner() *bwrapRunner { return &bwrapRunner{} }

func (r *bwrapRunner) name() string { return "bwrap" }

func (r *bwrapRunner) available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func (r *bwrapRunner) confine(argv []string, policy sandbox.Policy) (sandbox.ConfinedArgv, error) {
	if len(argv) == 0 {
		return sandbox.ConfinedArgv{}, fmt.Errorf("sandbox: confine requires a non-empty argv")
	}

	wrapped := []string{
		"bwrap",
		"--ro-bind", "/", "/", // whole filesystem visible but read-only by default
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp", // a fresh, empty /tmp — WritableRoots re-grants it explicitly below when applicable
		"--die-with-parent",
		"--unshare-all", // isolate every namespace, including networking — re-shared below iff policy.Network
	}
	if policy.Network {
		wrapped = append(wrapped, "--share-net")
	}
	for _, root := range sandbox.WritableRoots(policy.ExecutionPolicy) {
		wrapped = append(wrapped, "--bind", root, root)
	}
	wrapped = append(wrapped, "--")
	wrapped = append(wrapped, argv...)

	return sandbox.ConfinedArgv{
		Argv:             wrapped,
		Enforcement:      sandbox.Enforcement{Complete: true},
		DenialSignatures: []string{"Read-only file system", "Permission denied"},
		RunnerFailureRules: []sandbox.RunnerFailureRule{
			{Pattern: "Read-only file system", Meaning: "bwrap's read-only root bind denied a write outside the granted roots"},
		},
	}, nil
}

// The macOS runner: real confinement via /usr/bin/sandbox-exec, macOS's
// native (if deprecated-but-still-shipped) sandboxing tool. Port of
// profiles.ts's Seatbelt profile generation, verified against the real
// binary on this development machine (see seatbelt_test.go): sandbox-exec
// denies a write outside the granted roots with "Operation not permitted",
// and — critically — only when a granted root is compared in its
// *canonical* form, since macOS resolves /tmp to /private/tmp before the
// kernel-level check runs. This is exactly why sandbox.CanonicalPath exists.
package local

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"go-harness/sandbox/sandbox"
)

type seatbeltRunner struct{}

func newSeatbeltRunner() *seatbeltRunner { return &seatbeltRunner{} }

func (r *seatbeltRunner) name() string { return "seatbelt" }

func (r *seatbeltRunner) available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

func (r *seatbeltRunner) confine(argv []string, policy sandbox.Policy) (sandbox.ConfinedArgv, error) {
	if len(argv) == 0 {
		return sandbox.ConfinedArgv{}, fmt.Errorf("sandbox: confine requires a non-empty argv")
	}
	confined := append([]string{"sandbox-exec", "-p", seatbeltProfile(policy), "--"}, argv...)
	return sandbox.ConfinedArgv{
		Argv:             confined,
		Enforcement:      sandbox.Enforcement{Complete: true},
		DenialSignatures: []string{"Operation not permitted"},
		RunnerFailureRules: []sandbox.RunnerFailureRule{
			{Pattern: "Operation not permitted", Meaning: "Seatbelt denied a file or process operation outside the granted policy"},
		},
	}, nil
}

// seatbeltProfile renders the TinyScheme-syntax .sb profile text for
// policy. This backend confines writes, not reads (every runner in this
// port shares that posture, matching roots.ts's own write-only vocabulary):
// reads are allowed everywhere, writes are denied by default except
// /dev/null, plus sandbox.WritableRoots(policy) under workspace-write.
func seatbeltProfile(policy sandbox.Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	for _, root := range sandbox.WritableRoots(policy.ExecutionPolicy) {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", quoteSeatbelt(root))
	}
	return b.String()
}

// quoteSeatbelt escapes path for use inside a Seatbelt string literal, so a
// pathological workspace root (containing a `"` or `\`) can't break out of
// the literal and inject additional profile syntax.
func quoteSeatbelt(path string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path)
	return `"` + escaped + `"`
}

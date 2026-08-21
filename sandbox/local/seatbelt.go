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
//
// sysctl-read is also granted unconditionally, in the same spirit as
// file-read*: it's read-only kernel/system introspection (uname, CPU
// count, ...), not a write-confinement concern. Without it, importing
// numpy — and anything that transitively imports numpy, e.g. pandas —
// fails at import time: numpy's platform-detection code calls
// os.uname(), which macOS implements via sysctl, and Seatbelt's default
// deny blocks that even though it's exactly the kind of read this policy
// otherwise never restricts. Caught by tools/python's own real-package
// dependencies, not a synthetic test: a plain print()/file-write script
// never exercises this path, only a real `import pandas` does.
//
// policy.Network adds a blanket (allow network*) — outbound connect, DNS,
// the lot. Verified empirically against the real sandbox-exec binary (see
// seatbelt_test.go): network* alone is enough for a real DNS-resolved
// HTTPS request to succeed, no separate mach-lookup/system-socket rule
// needed. Not scoped further (e.g. to specific hosts/ports) — Seatbelt's
// operation-level grammar has nothing narrower than "outbound networking
// allowed or not," so a caller wanting less than that has to not request
// Network at all rather than ask for a subset of it.
func seatbeltProfile(policy sandbox.Policy) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	for _, root := range sandbox.WritableRoots(policy.ExecutionPolicy) {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", quoteSeatbelt(root))
	}
	if policy.Network {
		b.WriteString("(allow network*)\n")
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

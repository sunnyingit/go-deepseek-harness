package local

import (
	"runtime"
	"strings"
	"testing"

	"go-harness/sandbox/sandbox"
)

// confine() itself doesn't check runtime.GOOS/available() — it's pure argv
// construction, so its shape is testable on any machine even without a
// Linux/bwrap environment to actually run the result against. See README.md.

func TestBwrapConfineWrapsArgvWithSeparator(t *testing.T) {
	r := newBwrapRunner()
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	result, err := r.confine([]string{"echo", "hi"}, policy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if result.Argv[0] != "bwrap" {
		t.Fatalf("argv[0] = %q, want bwrap", result.Argv[0])
	}
	joined := strings.Join(result.Argv, " ")
	if !strings.Contains(joined, "-- echo hi") {
		t.Fatalf("argv = %v, want the original argv appended after a `--` separator", result.Argv)
	}
}

func TestBwrapConfineGrantsWritableRootsUnderWorkspaceWrite(t *testing.T) {
	r := newBwrapRunner()
	dir := t.TempDir()
	policy := mustPolicy(t, sandbox.WorkspaceWrite, dir)
	result, err := r.confine([]string{"touch", "x"}, policy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	canonical := sandbox.CanonicalPath(dir)
	found := false
	for i, arg := range result.Argv {
		if arg == "--bind" && i+2 < len(result.Argv) && result.Argv[i+1] == canonical && result.Argv[i+2] == canonical {
			found = true
		}
	}
	if !found {
		t.Fatalf("argv = %v, want --bind %s %s among the flags", result.Argv, canonical, canonical)
	}
}

func TestBwrapConfineReadOnlyGrantsNoWorkspaceBind(t *testing.T) {
	r := newBwrapRunner()
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	result, err := r.confine([]string{"echo"}, policy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	for i, arg := range result.Argv {
		if arg == "--bind" && result.Argv[i+1] == "/ws" {
			t.Fatalf("argv = %v, want no --bind for the workspace root under read-only", result.Argv)
		}
	}
}

func TestBwrapConfineRejectsEmptyArgv(t *testing.T) {
	r := newBwrapRunner()
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	if _, err := r.confine(nil, policy); err == nil {
		t.Fatal("want an error for an empty argv")
	}
}

func TestBwrapAvailableFalseOnNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this check only applies off Linux")
	}
	if newBwrapRunner().available() {
		t.Fatal("bwrap runner reported available on a non-Linux platform")
	}
}

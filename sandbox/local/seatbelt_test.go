package local

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go-harness/sandbox/sandbox"
)

func skipUnlessSeatbeltAvailable(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("seatbelt only exists on macOS")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not on PATH")
	}
}

func TestSeatbeltAvailable(t *testing.T) {
	skipUnlessSeatbeltAvailable(t)
	if !newSeatbeltRunner().available() {
		t.Fatal("want available() true when sandbox-exec is on PATH on darwin")
	}
}

func TestSeatbeltConfineRejectsEmptyArgv(t *testing.T) {
	r := newSeatbeltRunner()
	policy := mustPolicy(t, sandbox.ReadOnly, "/ws")
	if _, err := r.confine(nil, policy); err == nil {
		t.Fatal("want an error for an empty argv")
	}
}

func TestSeatbeltProfileEscapesQuotesInWorkspaceRoot(t *testing.T) {
	policy := mustPolicy(t, sandbox.WorkspaceWrite, `/tmp/weird"path`)
	profile := seatbeltProfile(policy)
	if strings.Contains(profile, `subpath "/tmp/weird"path"`) {
		t.Fatalf("profile did not escape the embedded quote:\n%s", profile)
	}
}

// TestSeatbeltRealConfinementDeniesWritesOutsideGrantedRoots actually spawns
// a confined `touch` via the real sandbox-exec binary — no mocking — and
// checks it is genuinely denied outside the granted roots and genuinely
// allowed inside them. This is what verified the whole design during
// development: a naive (non-canonicalized) workspace root silently grants
// nothing on macOS, because /tmp resolves to /private/tmp before the
// kernel-level check runs.
func TestSeatbeltRealConfinementDeniesWritesOutsideGrantedRoots(t *testing.T) {
	skipUnlessSeatbeltAvailable(t)

	workspace := t.TempDir()
	// t.TempDir() always nests under os.TempDir(), which WorkspaceWrite
	// intentionally grants in full (the TS original's "workspace and
	// backend-defined temp") — so it can't stand in for "outside the
	// granted roots." A directory under the module root genuinely is.
	outside, err := os.MkdirTemp(".", "sandbox-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	outside, err = filepath.Abs(outside)
	if err != nil {
		t.Fatal(err)
	}
	r := newSeatbeltRunner()

	roPolicy := mustPolicy(t, sandbox.ReadOnly, workspace)
	confined, err := r.confine([]string{"/usr/bin/touch", filepath.Join(workspace, "denied.txt")}, roPolicy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err == nil {
		t.Fatal("want a write denied under read-only, even inside the workspace root")
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); err == nil {
		t.Fatal("the denied write must not have happened")
	}

	wwPolicy := mustPolicy(t, sandbox.WorkspaceWrite, workspace)
	confined, err = r.confine([]string{"/usr/bin/touch", filepath.Join(workspace, "allowed.txt")}, wwPolicy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err != nil {
		t.Fatalf("want a write inside the workspace root to succeed under workspace-write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "allowed.txt")); err != nil {
		t.Fatal("the allowed write should have created the file")
	}

	confined, err = r.confine([]string{"/usr/bin/touch", filepath.Join(outside, "denied.txt")}, wwPolicy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err == nil {
		t.Fatal("want a write outside the workspace root denied even under workspace-write")
	}
}

func runConfined(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	return cmd.Run()
}

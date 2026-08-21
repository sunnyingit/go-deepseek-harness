package local

import (
	"net/http"
	"net/http/httptest"
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

// TestSeatbeltAllowsSysctlReadForPythonPackageImports guards a real gap
// found running a pandas-using skill script through web's bash tool end to
// end: numpy (and so pandas, and so anything that imports pandas) calls
// os.uname() at import
// time, which macOS implements via sysctl — Seatbelt's default deny blocks
// that even though it's a pure read, no different in spirit from the
// file-read* this profile already allows unconditionally. A script that
// only prints or writes a file never exercises this path; only a real
// `import pandas`-shaped script does, which is why no earlier test caught
// it. os.uname() alone reproduces the same denial without needing numpy
// installed, so that's what this test uses.
func TestSeatbeltAllowsSysctlReadForPythonPackageImports(t *testing.T) {
	skipUnlessSeatbeltAvailable(t)
	python3, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}

	r := newSeatbeltRunner()
	policy := mustPolicy(t, sandbox.ReadOnly, t.TempDir())
	confined, err := r.confine([]string{python3, "-c", "import os; os.uname()"}, policy)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err != nil {
		t.Fatalf("os.uname() denied under the sandbox: %v", err)
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

func TestSeatbeltProfileIncludesNetworkAllowOnlyWhenRequested(t *testing.T) {
	withoutNet := mustPolicy(t, sandbox.ReadOnly, "/ws")
	if strings.Contains(seatbeltProfile(withoutNet), "network") {
		t.Fatalf("profile allows network without policy.Network set:\n%s", seatbeltProfile(withoutNet))
	}

	withNet := mustPolicy(t, sandbox.ReadOnly, "/ws")
	withNet.Network = true
	if !strings.Contains(seatbeltProfile(withNet), "(allow network*)") {
		t.Fatalf("profile missing (allow network*) with policy.Network true:\n%s", seatbeltProfile(withNet))
	}
}

// TestSeatbeltRealConfinementNetworkAccess spawns a real curl against a
// local httptest server through the real sandbox-exec binary — no
// mocking — to verify policy.Network actually gates outbound networking,
// not just that the profile text contains the right words. Loopback rather
// than a real external host: Seatbelt's network* denies/allows by
// operation class, not by destination (confirmed empirically against
// 127.0.0.1 and a real internet host alike during development), so this is
// just as faithful a check and doesn't make the test suite depend on
// outbound internet access being available at all.
func TestSeatbeltRealConfinementNetworkAccess(t *testing.T) {
	skipUnlessSeatbeltAvailable(t)
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not on PATH")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newSeatbeltRunner()
	curlArgv := []string{curlPath, "-s", "-m", "3", "-o", "/dev/null", "-f", srv.URL}

	noNet := mustPolicy(t, sandbox.ReadOnly, t.TempDir())
	confined, err := r.confine(curlArgv, noNet)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err == nil {
		t.Fatal("want curl denied network access when policy.Network is false")
	}

	withNet := mustPolicy(t, sandbox.ReadOnly, t.TempDir())
	withNet.Network = true
	confined, err = r.confine(curlArgv, withNet)
	if err != nil {
		t.Fatalf("confine: %v", err)
	}
	if err := runConfined(confined.Argv); err != nil {
		t.Fatalf("want curl to succeed when policy.Network is true: %v", err)
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

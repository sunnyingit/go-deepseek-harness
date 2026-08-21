package python

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func skipUnlessPythonAndNetwork(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	conn, err := net.DialTimeout("tcp", "pypi.org:443", 2*time.Second)
	if err != nil {
		t.Skip("no network access to pypi.org")
	}
	conn.Close()
}

func TestEnsureEnvCreatesVenvAndInstallsPackage(t *testing.T) {
	skipUnlessPythonAndNetwork(t)
	dir := filepath.Join(t.TempDir(), "venv")

	env, err := EnsureEnv(context.Background(), EnvOptions{CacheDir: dir, Packages: []string{"six"}})
	if err != nil {
		t.Fatalf("EnsureEnv: %v", err)
	}
	if _, err := os.Stat(env.PythonPath); err != nil {
		t.Fatalf("PythonPath %q does not exist: %v", env.PythonPath, err)
	}

	cmd := exec.Command(env.PythonPath, "-c", "import six; print('ok')")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok\n" {
		t.Fatalf("import six failed: err=%v out=%q", err, out)
	}
}

func TestEnsureEnvReusesCachedVenv(t *testing.T) {
	skipUnlessPythonAndNetwork(t)
	dir := filepath.Join(t.TempDir(), "venv")
	opts := EnvOptions{CacheDir: dir, Packages: []string{"six"}}

	if _, err := EnsureEnv(context.Background(), opts); err != nil {
		t.Fatalf("first EnsureEnv: %v", err)
	}

	start := time.Now()
	if _, err := EnsureEnv(context.Background(), opts); err != nil {
		t.Fatalf("second EnsureEnv: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("second EnsureEnv took %v, want a fast cache hit (no venv/pip re-run)", elapsed)
	}
}

func TestEnsureEnvRebuildsOnPackageSetChange(t *testing.T) {
	skipUnlessPythonAndNetwork(t)
	dir := filepath.Join(t.TempDir(), "venv")

	if _, err := EnsureEnv(context.Background(), EnvOptions{CacheDir: dir, Packages: []string{"six"}}); err != nil {
		t.Fatalf("first EnsureEnv: %v", err)
	}
	env, err := EnsureEnv(context.Background(), EnvOptions{CacheDir: dir, Packages: []string{"six", "certifi"}})
	if err != nil {
		t.Fatalf("second EnsureEnv: %v", err)
	}

	cmd := exec.Command(env.PythonPath, "-c", "import certifi; print('ok')")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok\n" {
		t.Fatalf("import certifi failed after package-set change: err=%v out=%q", err, out)
	}
}

func TestEnsureEnvRequiresCacheDir(t *testing.T) {
	if _, err := EnsureEnv(context.Background(), EnvOptions{}); err == nil {
		t.Fatal("want an error when CacheDir is empty")
	}
}

func TestEnsureEnvReusesPreinstalledInterpreter(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(binDir, "python3")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeMarker(root, []string{"Pillow", "pandas"}); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "venv")
	env, err := EnsureEnv(context.Background(), EnvOptions{
		CacheDir:  cache,
		PythonBin: stub,
		Packages:  []string{"pandas", "Pillow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.PythonPath != stub {
		t.Fatalf("PythonPath = %q, want bundled interpreter %q", env.PythonPath, stub)
	}
	if env.Root != root {
		t.Fatalf("Root = %q, want prefix %q", env.Root, root)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Fatal("preinstalled interpreter should not create a venv")
	}
}

// Package python provisions a Python virtualenv with a curated set of
// document-processing libraries, and wraps running a script against it as a
// core/tools tool confined by sandbox/local — so an agent can generate or
// read Word/PPT/Excel/PDF documents without ever running unconfined code.
//
// This package is not a port of anything in deepseek-harness; the upstream
// project's Code Mode (packages/core/tools/src/code-mode.ts) is a different,
// much larger thing this codebase deliberately didn't port (see
// core/tools/README.md). This is a purpose-built capability on top of the
// two packages that already exist: core/tools and sandbox/local.
package python

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"
)

// DefaultDocumentPackages covers the common "office document toolkit" a
// document-processing agent needs: Word, PowerPoint, Excel, PDF, tabular
// data, and images.
var DefaultDocumentPackages = []string{
	"python-docx",
	"python-pptx",
	"openpyxl",
	"pypdf",
	"reportlab",
	"pandas",
	"Pillow",
}

// Env is a provisioned virtualenv ready to run scripts against.
type Env struct {
	Root       string   // the venv directory
	PythonPath string   // the venv's python interpreter
	Packages   []string // what EnsureEnv actually installed, as requested
}

// EnvOptions configures EnsureEnv.
type EnvOptions struct {
	// CacheDir is the venv's directory. Required. Reused across calls (and
	// across process restarts) when its recorded package set already
	// matches Packages, so a repeated run doesn't re-download anything.
	CacheDir string
	// Packages are pip package specifiers (name, or "name==version") to
	// have installed. Defaults to DefaultDocumentPackages when nil.
	Packages []string
	// PythonBin is the base interpreter EnsureEnv creates the venv with.
	// Defaults to "python3".
	PythonBin string
	// InstallTimeout bounds venv creation + pip install. Defaults to 5 minutes
	// — enough for a cold install of a heavy package like pandas over a slow
	// connection, without hanging forever on a genuinely stuck network.
	InstallTimeout time.Duration
}

const markerFileName = ".packages.json"

// EnsureEnv creates (or reuses) a virtualenv at opts.CacheDir with exactly
// opts.Packages installed. This is a provisioning step, not a confined one:
// it needs real filesystem and network access to run pip, and runs outside
// any sandbox.Provider — the venv it produces is what a later confined
// script execution treats as read-only input.
func EnsureEnv(ctx context.Context, opts EnvOptions) (*Env, error) {
	if opts.CacheDir == "" {
		return nil, fmt.Errorf("python: EnvOptions.CacheDir is required")
	}
	packages := opts.Packages
	if packages == nil {
		packages = DefaultDocumentPackages
	}
	pythonBin := opts.PythonBin
	if pythonBin == "" {
		pythonBin = "python3"
	}
	timeout := opts.InstallTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	sorted := slices.Clone(packages)
	slices.Sort(sorted)

	// Relocatable interpreters (python-build-standalone in the desktop
	// bundle) ship packages already installed, recorded next to the prefix.
	// Reuse that interpreter instead of creating a venv with baked-in paths.
	if prefix := interpreterPrefix(pythonBin); prefix != "" {
		if cached, ok := readMarker(prefix); ok && slices.Equal(cached, sorted) {
			return &Env{Root: prefix, PythonPath: pythonBin, Packages: packages}, nil
		}
	}

	if cached, ok := readMarker(opts.CacheDir); ok && slices.Equal(cached, sorted) {
		return &Env{Root: opts.CacheDir, PythonPath: venvPython(opts.CacheDir), Packages: packages}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if _, err := os.Stat(venvPython(opts.CacheDir)); err != nil {
		if err := runVisible(ctx, pythonBin, "-m", "venv", opts.CacheDir); err != nil {
			return nil, fmt.Errorf("python: creating venv at %s: %w", opts.CacheDir, err)
		}
	}

	pipArgs := append([]string{"install", "--upgrade"}, packages...)
	if err := runVisible(ctx, venvPip(opts.CacheDir), pipArgs...); err != nil {
		return nil, fmt.Errorf("python: pip install into %s: %w", opts.CacheDir, err)
	}

	if err := writeMarker(opts.CacheDir, sorted); err != nil {
		return nil, fmt.Errorf("python: recording installed packages: %w", err)
	}

	return &Env{Root: opts.CacheDir, PythonPath: venvPython(opts.CacheDir), Packages: packages}, nil
}

// interpreterPrefix is the install root of an absolute Python binary
// (…/python/bin/python3 or …/python/python.exe). Relative names like
// "python3" are looked up on PATH and are not treated as a bundle prefix.
func interpreterPrefix(pythonBin string) string {
	if pythonBin == "" || !filepath.IsAbs(pythonBin) {
		return ""
	}
	dir := filepath.Dir(pythonBin)
	base := filepath.Base(dir)
	if base == "bin" || base == "Scripts" {
		return filepath.Dir(dir)
	}
	return dir
}

func venvPython(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "Scripts", "python.exe")
	}
	return filepath.Join(root, "bin", "python3")
}

func venvPip(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "Scripts", "pip.exe")
	}
	return filepath.Join(root, "bin", "pip")
}

func runVisible(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func readMarker(root string) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join(root, markerFileName))
	if err != nil {
		return nil, false
	}
	var packages []string
	if err := json.Unmarshal(data, &packages); err != nil {
		return nil, false
	}
	slices.Sort(packages)
	return packages, true
}

func writeMarker(root string, sortedPackages []string) error {
	data, err := json.Marshal(sortedPackages)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, markerFileName), data, 0o644)
}

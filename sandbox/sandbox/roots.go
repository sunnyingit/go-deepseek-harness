// Port of roots.ts: the canonical, shared definition of "what's writable
// under this policy" every confinement backend (Seatbelt, bwrap, Landlock,
// the Windows ACL runner) builds its own dialect from, so a change to what
// workspace-write actually means only has to happen in one place.
package sandbox

import (
	"os"
	"path/filepath"
)

// CanonicalPath resolves path to the form an enforcement backend actually
// compares against — symlinks and all. On most platforms a temp directory
// is itself reached through a symlink (macOS: /tmp -> /private/tmp, and
// the whole /var tree -> /private/var); a backend that grants access to
// the un-resolved spelling silently grants nothing, since the kernel-level
// check sees the resolved path.
//
// filepath.EvalSymlinks requires the full path to exist, so a target that
// doesn't exist yet (a file about to be created, a workspace root that
// doesn't exist yet) can't be resolved directly. This walks upward to the
// nearest ancestor that does exist, resolves that, and rejoins the
// non-existent tail unchanged — so an ancestor symlink still gets caught
// even for a brand new nested path. Falls back to path unchanged only when
// no ancestor at all is resolvable (fixed by fs/fssandbox's own tests: a
// single-level fallback missed exactly this case for a multi-level-missing
// path under a symlinked temp root).
func CanonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir := filepath.Dir(path)
	tail := filepath.Base(path)
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, tail)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path // reached the filesystem root without resolving anything
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
	}
}

// WritableRoots derives the canonical, deduplicated allow-list of
// directories a policy's mode grants write access to. ReadOnly (and
// DangerFullAccess, which never reaches a Provider at all) has none;
// WorkspaceWrite is the workspace root plus the platform temp directories.
func WritableRoots(policy ExecutionPolicy) []string {
	if policy.Mode != WorkspaceWrite {
		return nil
	}
	seen := map[string]bool{}
	var roots []string
	add := func(path string) {
		if path == "" {
			return
		}
		canonical := CanonicalPath(path)
		if seen[canonical] {
			return
		}
		seen[canonical] = true
		roots = append(roots, canonical)
	}
	add(policy.WorkspaceRoot)
	add(os.TempDir())
	return roots
}

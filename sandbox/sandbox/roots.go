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
// compares against — symlinks and all. On most platforms /tmp is itself a
// symlink (macOS: /tmp -> /private/tmp); a backend that grants access to
// the un-resolved spelling silently grants nothing, since the kernel-level
// check sees the resolved path. Falls back to path unchanged when
// resolution fails (path, or a prefix of it, is missing or unreadable —
// still meaningful to grant, e.g. a workspace root that doesn't exist yet).
func CanonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
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

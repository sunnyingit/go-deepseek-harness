package sessionpersistence

import (
	"strings"

	"go-harness/core/session"
)

// noCWDDir is the directory name used when a session has no CWD — every
// path segment this package writes must be non-empty.
const noCWDDir = "_no-cwd_"

// escapeSegment turns s into a single filesystem-safe path segment: no
// separators, no traversal, no empty result. Deliberately lossy (like the
// TS original's own separator replacement) — two different inputs can
// collide onto the same segment, which is fine for the CWD segment (two
// project paths sharing a display name legitimately share a directory) and
// merely paranoid for the session-id segment (ids are already
// filesystem-safe in this codebase's own id generator, session-<n> or a
// caller-chosen string, but a persistence layer shouldn't trust that).
func escapeSegment(s string) string {
	if s == "" {
		return noCWDDir
	}
	var b strings.Builder
	b.Grow(len(s))
	lastUnderscore := false
	for _, r := range s {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-'
		if safe {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_.")
	if out == "" {
		return noCWDDir
	}
	const maxSegmentLen = 120 // stays well under every common filesystem's component limit
	if len(out) > maxSegmentLen {
		out = out[:maxSegmentLen]
	}
	return out
}

// sessionDir returns the directory sessionFile lives in:
// <root>/<escaped-cwd-or-_no-cwd_>/<escaped-session-id>/.
func sessionDir(root string, id session.SessionId, cwd string) string {
	cwdSeg := noCWDDir
	if cwd != "" {
		cwdSeg = escapeSegment(cwd)
	}
	return root + "/" + cwdSeg + "/" + escapeSegment(string(id))
}

// sessionFile is the one file a session's log lives in — always raw JSONL;
// see README.md for why this port doesn't offer the TS original's
// zstd-compressed encoding.
func sessionFile(root string, id session.SessionId, cwd string) string {
	return sessionDir(root, id, cwd) + "/session.jsonl"
}

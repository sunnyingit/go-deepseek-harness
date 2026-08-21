// Package fs is a Go port of packages/fs/fs (dsh-fs) from DeepSeek
// Harness: the abstract filesystem contract other packages implement
// (fs/fssandbox) or consume (fs/toolfs). This package defines the contract
// only — it confines nothing and touches no disk itself.
//
// "ctx.fs handles storage primitives ... sandbox confinement handles
// subprocess execution boundaries ... the contract is intentionally
// agnostic to confinement mechanism" — this package has no dependency on
// go-harness/sandbox/sandbox for exactly that reason; a confining
// implementation (fs/fssandbox) is free to enforce whatever policy it
// wants behind this interface.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package fs

import "context"

// EntryType classifies what a Target/DirEntry names.
type EntryType string

const (
	TypeFile      EntryType = "file"
	TypeDirectory EntryType = "directory"
	TypeOther     EntryType = "other" // symlink, device, socket, ...
)

// Target is an opaque, already-resolved filesystem location. Key is what an
// implementation actually compares/opens by — two Resolve calls for
// different spellings of the same file (a relative path, a path through a
// symlink) must produce equal Key values. DisplayPath is what a caller (a
// tool result, a rendered message) shows a human or a model — normally the
// path as the caller originally typed it, kept separate from Key so a
// symlinked or differently-spelled path still reads back the way the
// caller asked for it, not its canonical form.
type Target struct {
	Key         string
	DisplayPath string
}

// ResolveOptions configures Resolve/Lstat.
type ResolveOptions struct {
	// CWD is the base a relative path resolves against. Ignored for an
	// already-absolute path.
	CWD string
}

// Info is stat metadata — never content. Version is opaque and comparable:
// it changes whenever the target's content changes, and is what
// WriteExpectation's version guard compares against.
type Info struct {
	Version string
	Type    EntryType
	Size    int64 // meaningful only when Type == TypeFile
}

// PathInfo is Info plus whether the resolved path's final component is
// itself a symlink — Lstat does not follow it, unlike Stat.
type PathInfo struct {
	Info
	IsSymlink bool
}

// DirEntry is one child a ListDir call reports: cheap metadata only, never
// content.
type DirEntry struct {
	Name   string
	Type   EntryType
	Target Target
	Info   Info
}

// Edit is one literal-text replacement EditText applies atomically: find
// OldString, replace with NewString. ReplaceAll replaces every occurrence;
// otherwise OldString must appear exactly once (CodeAmbiguousEdit if more,
// CodeEditNotFound if none).
type Edit struct {
	OldString  string
	NewString  string
	ReplaceAll bool
}

// WriteExpectation is the optional guard WriteText/EditText enforce before
// mutating. The zero value (Unconditional) performs the write regardless of
// the target's current state.
type WriteExpectation struct {
	kind    expectationKind
	version string
}

type expectationKind int

const (
	expectAny expectationKind = iota
	expectAbsent
	expectVersion
)

// Unconditional performs the write/edit regardless of the target's current
// state. The zero value already means this; the constructor exists so a
// call site can say so explicitly rather than leaving WriteExpectation{}
// implicit.
func Unconditional() WriteExpectation { return WriteExpectation{kind: expectAny} }

// CreateIfAbsent fails with CodeAlreadyExists if the target already exists
// — the guard a caller uses to create a new file without risking a silent
// overwrite of one it didn't know was there.
func CreateIfAbsent() WriteExpectation { return WriteExpectation{kind: expectAbsent} }

// ReplaceIfVersion fails with CodeStaleVersion unless the target's current
// Info.Version still equals version — the standard optimistic-concurrency
// guard a caller uses after a prior Stat or read, to catch a write racing
// against a change it never saw.
func ReplaceIfVersion(version string) WriteExpectation {
	return WriteExpectation{kind: expectVersion, version: version}
}

// Check validates current (the target's Info.Version, or "" if the target
// doesn't currently exist) against the expectation, returning a *Error with
// the appropriate code on violation. Implementations call this from inside
// their own mutation lock so the check and the write it gates are atomic
// together.
func (e WriteExpectation) Check(exists bool, currentVersion string) error {
	switch e.kind {
	case expectAbsent:
		if exists {
			return &Error{Code: CodeAlreadyExists, Message: "target already exists"}
		}
	case expectVersion:
		if !exists {
			return &Error{Code: CodeStaleVersion, Message: "target no longer exists"}
		}
		if currentVersion != e.version {
			return &Error{Code: CodeStaleVersion, Message: "target has changed since it was last read"}
		}
	}
	return nil
}

// Code is a stable, model-facing error classification — mirrors the TS
// original's FsErrorCode. Not every TS code is ported; see README.md.
type Code string

const (
	CodeNotFound         Code = "FS_NOT_FOUND"
	CodeNotDirectory     Code = "FS_NOT_DIRECTORY"
	CodeNotRegularFile   Code = "FS_NOT_REGULAR_FILE"
	CodeNotText          Code = "FS_NOT_TEXT"
	CodeTooLarge         Code = "FS_TOO_LARGE"
	CodePermissionDenied Code = "FS_PERMISSION_DENIED"
	CodeIOError          Code = "FS_IO_ERROR"
	CodeStaleVersion     Code = "FS_STALE_VERSION"
	CodeAlreadyExists    Code = "FS_ALREADY_EXISTS"
	CodeAmbiguousEdit    Code = "FS_AMBIGUOUS_EDIT"
	CodeEditNotFound     Code = "FS_EDIT_NOT_FOUND"
	// CodeSandboxDenied is produced by fs/fssandbox specifically, not by
	// this package — declared here so it's part of the same recognizable
	// code family a caller switches on.
	CodeSandboxDenied Code = "FS_SANDBOX_DENIED"
)

// Error is a structured filesystem failure: Code is stable and
// switch-on-able, Message is the human/model-facing detail.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// FileSystem is the abstract contract packages/fs/fs defines — resolve,
// inspect, read, list, and atomically mutate a target — confinement
// -agnostic: nothing here says whether or how an implementation restricts
// what it will actually do.
type FileSystem interface {
	// Resolve turns path (relative to opts.CWD, or absolute) into a Target.
	// Resolving the same underlying file through different spellings
	// yields an equal Target.Key.
	Resolve(ctx context.Context, path string, opts ResolveOptions) (Target, error)
	// ProcessPath returns the canonical, subprocess-openable absolute path
	// for target — what a caller hands to os/exec, distinct from Target.Key.
	ProcessPath(target Target) string
	// FileURL returns a canonical file: URI for target.
	FileURL(target Target) string
	// Contains reports whether child is parent itself or a descendant of
	// it, by canonical identity — not by string prefix comparison.
	Contains(parent, child Target) bool

	// Stat returns target's metadata, or ok=false if it doesn't exist.
	Stat(ctx context.Context, target Target) (info Info, ok bool, err error)
	// Lstat resolves path without following its final path component if
	// that component is itself a symlink.
	Lstat(ctx context.Context, path string, opts ResolveOptions) (info PathInfo, ok bool, err error)

	// ReadText reads target as a complete UTF-8 string. CodeNotText if the
	// content isn't valid UTF-8; CodeNotRegularFile if target is a
	// directory or other non-regular entry.
	ReadText(ctx context.Context, target Target) (string, error)
	// ReadBytes reads target as raw bytes, up to maxBytes. CodeTooLarge if
	// the content exceeds it.
	ReadBytes(ctx context.Context, target Target, maxBytes int) ([]byte, error)
	// ListDir lists target's direct children in stable name order.
	// CodeNotDirectory if target isn't a directory.
	ListDir(ctx context.Context, target Target) ([]DirEntry, error)

	// WriteText atomically creates or replaces target's content, honoring
	// expect. Returns the written target's new Info.
	WriteText(ctx context.Context, target Target, content string, expect WriteExpectation) (Info, error)
	// WriteBytes is WriteText's binary counterpart, for content that isn't
	// (or isn't known to be) UTF-8 text — e.g. an uploaded image or PDF.
	WriteBytes(ctx context.Context, target Target, data []byte, expect WriteExpectation) (Info, error)
	// EditText atomically applies edit to target's current content — the
	// match against edit.OldString and the replacement happen inside the
	// same critical section as the expect check, so a concurrent writer
	// can't invalidate the match between reading and writing. Returns the
	// content before and after, and the new Info.
	EditText(ctx context.Context, target Target, edit Edit, expect WriteExpectation) (before, after string, info Info, err error)
}

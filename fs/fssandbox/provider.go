// Package fssandbox is a Go port of packages/fs/fs-sandbox (dsh-fs
// -sandbox) from DeepSeek Harness: an fs.FileSystem implementation that
// does real filesystem I/O directly (no subprocess spawning — "the
// operations are the seam's own (open, rename), only the target path is
// untrusted") and fences WriteText/EditText by a sandbox.Mode. Reads
// always pass through unconditionally: "every mode permits reading,"
// matching the exact same posture every other confinement backend in this
// module already has.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package fssandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	fsvocab "go-harness/fs/fs"
	"go-harness/sandbox/sandbox"
)

// Provider implements fs.FileSystem. One Provider has a fixed WorkspaceRoot
// and Mode for its whole lifetime — matching how tools/python and
// shell/toolbash's callers already build a fresh instance per session
// rather than threading a policy through every call. A caller wanting
// per-call escalation builds a second Provider at a wider Mode and uses it
// for that one call, the same pattern shell/toolbash's own escalation uses
// one layer up.
type Provider struct {
	WorkspaceRoot string
	Mode          sandbox.Mode

	// mu serializes every mutation across the whole Provider — the TS
	// original locks per-target; see README.md for why this port uses one
	// coarser lock instead.
	mu sync.Mutex
}

// New returns a Provider confined to workspaceRoot under mode. mode of ""
// defaults to sandbox.ReadOnly, matching the TS original's own default.
func New(workspaceRoot string, mode sandbox.Mode) *Provider {
	if mode == "" {
		mode = sandbox.ReadOnly
	}
	return &Provider{WorkspaceRoot: workspaceRoot, Mode: mode}
}

var _ fsvocab.FileSystem = (*Provider)(nil)

func (p *Provider) Resolve(_ context.Context, path string, opts fsvocab.ResolveOptions) (fsvocab.Target, error) {
	abs := path
	if !filepath.IsAbs(path) {
		base := opts.CWD
		if base == "" {
			base = p.WorkspaceRoot
		}
		abs = filepath.Join(base, path)
	}
	return fsvocab.Target{Key: sandbox.CanonicalPath(abs), DisplayPath: path}, nil
}

func (p *Provider) ProcessPath(target fsvocab.Target) string { return target.Key }

func (p *Provider) FileURL(target fsvocab.Target) string { return "file://" + target.Key }

func (p *Provider) Contains(parent, child fsvocab.Target) bool {
	if child.Key == parent.Key {
		return true
	}
	return strings.HasPrefix(child.Key, parent.Key+string(filepath.Separator))
}

func (p *Provider) Stat(_ context.Context, target fsvocab.Target) (fsvocab.Info, bool, error) {
	info, err := os.Stat(target.Key)
	if err != nil {
		if os.IsNotExist(err) {
			return fsvocab.Info{}, false, nil
		}
		return fsvocab.Info{}, false, ioError(err)
	}
	return toInfo(info), true, nil
}

func (p *Provider) Lstat(_ context.Context, path string, opts fsvocab.ResolveOptions) (fsvocab.PathInfo, bool, error) {
	abs := path
	if !filepath.IsAbs(path) {
		base := opts.CWD
		if base == "" {
			base = p.WorkspaceRoot
		}
		abs = filepath.Join(base, path)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return fsvocab.PathInfo{}, false, nil
		}
		return fsvocab.PathInfo{}, false, ioError(err)
	}
	return fsvocab.PathInfo{Info: toInfo(info), IsSymlink: info.Mode()&os.ModeSymlink != 0}, true, nil
}

func (p *Provider) ReadText(_ context.Context, target fsvocab.Target) (string, error) {
	data, err := p.readRegular(target)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", &fsvocab.Error{Code: fsvocab.CodeNotText, Message: target.DisplayPath}
	}
	return string(data), nil
}

func (p *Provider) ReadBytes(_ context.Context, target fsvocab.Target, maxBytes int) ([]byte, error) {
	info, err := os.Stat(target.Key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &fsvocab.Error{Code: fsvocab.CodeNotFound, Message: target.DisplayPath}
		}
		return nil, ioError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, &fsvocab.Error{Code: fsvocab.CodeNotRegularFile, Message: target.DisplayPath}
	}
	if info.Size() > int64(maxBytes) {
		return nil, &fsvocab.Error{Code: fsvocab.CodeTooLarge,
			Message: fmt.Sprintf("%s is %d bytes, over the %d-byte limit", target.DisplayPath, info.Size(), maxBytes)}
	}
	data, err := os.ReadFile(target.Key)
	if err != nil {
		return nil, ioError(err)
	}
	return data, nil
}

func (p *Provider) readRegular(target fsvocab.Target) ([]byte, error) {
	info, err := os.Stat(target.Key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &fsvocab.Error{Code: fsvocab.CodeNotFound, Message: target.DisplayPath}
		}
		return nil, ioError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, &fsvocab.Error{Code: fsvocab.CodeNotRegularFile, Message: target.DisplayPath}
	}
	data, err := os.ReadFile(target.Key)
	if err != nil {
		return nil, ioError(err)
	}
	return data, nil
}

func (p *Provider) ListDir(_ context.Context, target fsvocab.Target) ([]fsvocab.DirEntry, error) {
	info, err := os.Stat(target.Key)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &fsvocab.Error{Code: fsvocab.CodeNotFound, Message: target.DisplayPath}
		}
		return nil, ioError(err)
	}
	if !info.IsDir() {
		return nil, &fsvocab.Error{Code: fsvocab.CodeNotDirectory, Message: target.DisplayPath}
	}
	entries, err := os.ReadDir(target.Key)
	if err != nil {
		return nil, ioError(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	out := make([]fsvocab.DirEntry, 0, len(entries))
	for _, e := range entries {
		childInfo, err := e.Info()
		if err != nil {
			continue // an entry that vanished/broke between ReadDir and Info is skipped, not fatal to the whole listing
		}
		out = append(out, fsvocab.DirEntry{
			Name: e.Name(), Type: entryType(childInfo),
			Target: fsvocab.Target{Key: filepath.Join(target.Key, e.Name()), DisplayPath: filepath.Join(target.DisplayPath, e.Name())},
			Info:   toInfo(childInfo),
		})
	}
	return out, nil
}

// WriteText fences the mutation by mode before doing anything else, then
// re-checks expect and performs the write inside the same lock — expect's
// version check and the write it gates are atomic together, so a
// concurrent WriteText/EditText can't land between them. A Go string is
// just a byte sequence, so this is nothing more than WriteBytes with an
// implicit conversion — the distinct method exists for the same reason
// ReadText/ReadBytes are distinct: callers who know they have text
// shouldn't have to convert both ways.
func (p *Provider) WriteText(ctx context.Context, target fsvocab.Target, content string, expect fsvocab.WriteExpectation) (fsvocab.Info, error) {
	return p.WriteBytes(ctx, target, []byte(content), expect)
}

// WriteBytes is WriteText's binary counterpart — see fs.FileSystem's doc
// comment for when to use which. Shares every mechanic (the mode fence,
// the expect check, parent-directory creation) with WriteText; only the
// bytes handed to os.WriteFile differ.
func (p *Provider) WriteBytes(_ context.Context, target fsvocab.Target, data []byte, expect fsvocab.WriteExpectation) (fsvocab.Info, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.checkWritable(target.Key); err != nil {
		return fsvocab.Info{}, err
	}

	stat, statErr := os.Stat(target.Key)
	exists := statErr == nil
	var currentVersion string
	if exists {
		currentVersion = versionOf(stat)
	}
	if err := expect.Check(exists, currentVersion); err != nil {
		return fsvocab.Info{}, err
	}

	if err := os.MkdirAll(filepath.Dir(target.Key), 0o755); err != nil {
		return fsvocab.Info{}, ioError(err)
	}
	if err := os.WriteFile(target.Key, data, 0o644); err != nil {
		return fsvocab.Info{}, ioError(err)
	}
	newStat, err := os.Stat(target.Key)
	if err != nil {
		return fsvocab.Info{}, ioError(err)
	}
	return toInfo(newStat), nil
}

// EditText fences, checks expect, reads the current content, applies edit
// (CodeEditNotFound if OldString is absent, CodeAmbiguousEdit if it
// appears more than once and ReplaceAll is false), and writes the result —
// all inside the same lock as WriteText, so the match this computes can
// never go stale before the write that acts on it.
func (p *Provider) EditText(_ context.Context, target fsvocab.Target, edit fsvocab.Edit, expect fsvocab.WriteExpectation) (before, after string, info fsvocab.Info, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.checkWritable(target.Key); err != nil {
		return "", "", fsvocab.Info{}, err
	}

	stat, statErr := os.Stat(target.Key)
	exists := statErr == nil
	var currentVersion string
	if exists {
		currentVersion = versionOf(stat)
	}
	if err := expect.Check(exists, currentVersion); err != nil {
		return "", "", fsvocab.Info{}, err
	}
	if !exists {
		return "", "", fsvocab.Info{}, &fsvocab.Error{Code: fsvocab.CodeNotFound, Message: target.DisplayPath}
	}
	if !stat.Mode().IsRegular() {
		return "", "", fsvocab.Info{}, &fsvocab.Error{Code: fsvocab.CodeNotRegularFile, Message: target.DisplayPath}
	}

	data, err := os.ReadFile(target.Key)
	if err != nil {
		return "", "", fsvocab.Info{}, ioError(err)
	}
	if !utf8.Valid(data) {
		return "", "", fsvocab.Info{}, &fsvocab.Error{Code: fsvocab.CodeNotText, Message: target.DisplayPath}
	}
	before = string(data)

	count := strings.Count(before, edit.OldString)
	if count == 0 {
		return "", "", fsvocab.Info{}, &fsvocab.Error{Code: fsvocab.CodeEditNotFound, Message: "old_string not found in " + target.DisplayPath}
	}
	if !edit.ReplaceAll && count > 1 {
		return "", "", fsvocab.Info{}, &fsvocab.Error{Code: fsvocab.CodeAmbiguousEdit,
			Message: fmt.Sprintf("old_string appears %d times in %s; make it unique or set replace_all", count, target.DisplayPath)}
	}

	if edit.ReplaceAll {
		after = strings.ReplaceAll(before, edit.OldString, edit.NewString)
	} else {
		after = strings.Replace(before, edit.OldString, edit.NewString, 1)
	}

	if err := os.WriteFile(target.Key, []byte(after), 0o644); err != nil {
		return "", "", fsvocab.Info{}, ioError(err)
	}
	newStat, err := os.Stat(target.Key)
	if err != nil {
		return before, after, fsvocab.Info{}, ioError(err)
	}
	return before, after, toInfo(newStat), nil
}

// checkWritable is the mode fence: read-only denies every mutation,
// workspace-write allows one only when the target canonicalizes under a
// writable root (sandbox.WritableRoots — the exact same function
// shell/bashsandbox and sandbox/local's Seatbelt profile derive their own
// writable set from, so the fs fence and a shelled-out command cannot
// drift apart), danger-full-access allows everything.
func (p *Provider) checkWritable(targetKey string) error {
	switch p.Mode {
	case sandbox.DangerFullAccess:
		return nil
	case sandbox.WorkspaceWrite:
		// Re-resolved immediately before the check — narrowing (not
		// eliminating; see README.md) the TOCTOU window between Resolve
		// and this mutation. sandbox.CanonicalPath itself walks up to the
		// nearest existing ancestor when targetKey doesn't exist yet (a
		// create), so an ancestor symlink swapped since Resolve ran is
		// still caught even for a brand new file.
		canonical := sandbox.CanonicalPath(targetKey)
		for _, root := range sandbox.WritableRoots(sandbox.ExecutionPolicy{Mode: sandbox.WorkspaceWrite, WorkspaceRoot: p.WorkspaceRoot}) {
			if canonical == root || strings.HasPrefix(canonical, root+string(filepath.Separator)) {
				return nil
			}
		}
		return &fsvocab.Error{Code: fsvocab.CodeSandboxDenied,
			Message: fmt.Sprintf("write denied under workspace-write mode: %s is outside the granted workspace", targetKey)}
	default: // ReadOnly, or an unrecognized mode — fail closed
		return &fsvocab.Error{Code: fsvocab.CodeSandboxDenied, Message: "write denied under read-only mode"}
	}
}

func toInfo(info os.FileInfo) fsvocab.Info {
	return fsvocab.Info{Version: versionOf(info), Type: entryType(info), Size: info.Size()}
}

func entryType(info os.FileInfo) fsvocab.EntryType {
	switch {
	case info.IsDir():
		return fsvocab.TypeDirectory
	case info.Mode().IsRegular():
		return fsvocab.TypeFile
	default:
		return fsvocab.TypeOther
	}
}

// versionOf derives a comparable opaque version from mtime+size — cheap
// (no content hashing) and changes whenever either does, which is every
// case this port's own WriteText/EditText produce.
func versionOf(info os.FileInfo) string {
	return fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
}

func ioError(err error) error {
	if os.IsPermission(err) {
		return &fsvocab.Error{Code: fsvocab.CodePermissionDenied, Message: err.Error()}
	}
	return &fsvocab.Error{Code: fsvocab.CodeIOError, Message: err.Error()}
}

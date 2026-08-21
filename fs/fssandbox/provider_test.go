package fssandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	fsvocab "go-harness/fs/fs"
	"go-harness/sandbox/sandbox"
)

func mustTarget(t *testing.T, p *Provider, path string) fsvocab.Target {
	t.Helper()
	target, err := p.Resolve(context.Background(), path, fsvocab.ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestResolveCanonicalizesRelativeAndAbsolutePaths(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)

	rel := mustTarget(t, p, "a.txt")
	abs := mustTarget(t, p, filepath.Join(workspace, "a.txt"))
	if rel.Key != abs.Key {
		t.Fatalf("relative and absolute resolution of the same file disagree: %q vs %q", rel.Key, abs.Key)
	}
	if rel.DisplayPath != "a.txt" {
		t.Fatalf("DisplayPath = %q, want the original relative spelling preserved", rel.DisplayPath)
	}
}

func TestStatReportsAbsence(t *testing.T) {
	p := New(t.TempDir(), sandbox.ReadOnly)
	target := mustTarget(t, p, "missing.txt")
	_, ok, err := p.Stat(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("want ok=false for a missing target")
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "note.txt")

	if _, err := p.WriteText(context.Background(), target, "hello", fsvocab.Unconditional()); err != nil {
		t.Fatal(err)
	}
	got, err := p.ReadText(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("ReadText = %q", got)
	}
}

func TestWriteBytesThenReadBytesRoundTripsBinaryContent(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "image.bin")

	binary := []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0xff, 0xfe} // not valid UTF-8
	if _, err := p.WriteBytes(context.Background(), target, binary, fsvocab.Unconditional()); err != nil {
		t.Fatal(err)
	}
	got, err := p.ReadBytes(context.Background(), target, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("ReadBytes = %v, want %v", got, binary)
	}
	// ReadText must reject it — this is exactly why WriteBytes/ReadBytes
	// exist as the binary counterpart, not a workaround for WriteText.
	if _, err := p.ReadText(context.Background(), target); err == nil {
		t.Fatal("want ReadText to reject non-UTF-8 content written via WriteBytes")
	}
}

func TestWriteBytesDeniedUnderReadOnly(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, "image.bin")

	_, err := p.WriteBytes(context.Background(), target, []byte{0x00, 0x01}, fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeSandboxDenied {
		t.Fatalf("err = %v, want CodeSandboxDenied", err)
	}
}

func TestReadOnlyModeDeniesEveryMutation(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, "note.txt")

	_, err := p.WriteText(context.Background(), target, "denied", fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeSandboxDenied {
		t.Fatalf("err = %v, want CodeSandboxDenied", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "note.txt")); statErr == nil {
		t.Fatal("the denied write must not have happened")
	}
}

func TestWorkspaceWriteAllowsInsideWorkspaceDeniesOutside(t *testing.T) {
	workspace := t.TempDir()
	// t.TempDir() always nests under os.TempDir(), which WorkspaceWrite
	// intentionally grants in full — so it can't stand in for "outside the
	// granted roots." A directory under the module root genuinely is.
	outside, err := os.MkdirTemp(".", "fssandbox-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	outside, err = filepath.Abs(outside)
	if err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.WorkspaceWrite)

	inside := mustTarget(t, p, filepath.Join(workspace, "ok.txt"))
	if _, err := p.WriteText(context.Background(), inside, "ok", fsvocab.Unconditional()); err != nil {
		t.Fatalf("write inside the workspace should succeed: %v", err)
	}

	outsideTarget := mustTarget(t, p, filepath.Join(outside, "denied.txt"))
	_, err = p.WriteText(context.Background(), outsideTarget, "denied", fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeSandboxDenied {
		t.Fatalf("err = %v, want CodeSandboxDenied for a write outside the workspace", err)
	}
}

func TestDangerFullAccessAllowsAnywhere(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	p := New(workspace, sandbox.DangerFullAccess)

	target := mustTarget(t, p, filepath.Join(outside, "anywhere.txt"))
	if _, err := p.WriteText(context.Background(), target, "unconfined", fsvocab.Unconditional()); err != nil {
		t.Fatalf("danger-full-access should allow a write anywhere: %v", err)
	}
}

func TestWriteCreateIfAbsentRejectsExistingTarget(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")

	if _, err := p.WriteText(context.Background(), target, "v1", fsvocab.CreateIfAbsent()); err != nil {
		t.Fatal(err)
	}
	_, err := p.WriteText(context.Background(), target, "v2", fsvocab.CreateIfAbsent())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeAlreadyExists {
		t.Fatalf("err = %v, want CodeAlreadyExists", err)
	}
}

func TestWriteReplaceIfVersionDetectsStaleness(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")

	info, err := p.WriteText(context.Background(), target, "v1", fsvocab.Unconditional())
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent writer lands in between.
	if _, err := p.WriteText(context.Background(), target, "v2-from-elsewhere", fsvocab.Unconditional()); err != nil {
		t.Fatal(err)
	}

	_, err = p.WriteText(context.Background(), target, "v3", fsvocab.ReplaceIfVersion(info.Version))
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeStaleVersion {
		t.Fatalf("err = %v, want CodeStaleVersion", err)
	}
}

func TestReadTextRejectsBinaryContent(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "binary.dat")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, path)

	_, err := p.ReadText(context.Background(), target)
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeNotText {
		t.Fatalf("err = %v, want CodeNotText", err)
	}
}

func TestReadTextRejectsDirectory(t *testing.T) {
	workspace := t.TempDir()
	sub := filepath.Join(workspace, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, sub)

	_, err := p.ReadText(context.Background(), target)
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeNotRegularFile {
		t.Fatalf("err = %v, want CodeNotRegularFile", err)
	}
}

func TestReadBytesEnforcesMaxBytes(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "big.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, path)

	_, err := p.ReadBytes(context.Background(), target, 5)
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeTooLarge {
		t.Fatalf("err = %v, want CodeTooLarge", err)
	}

	data, err := p.ReadBytes(context.Background(), target, 100)
	if err != nil || string(data) != "0123456789" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestListDirSortedByNameWithMetadata(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(workspace, "c-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, workspace)

	entries, err := p.ListDir(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Name != "a.txt" || entries[1].Name != "b.txt" || entries[2].Name != "c-dir" {
		t.Fatalf("entries = %+v, want [a.txt b.txt c-dir]", entries)
	}
	if entries[2].Type != fsvocab.TypeDirectory {
		t.Fatalf("c-dir type = %q, want directory", entries[2].Type)
	}
	if entries[0].Type != fsvocab.TypeFile {
		t.Fatalf("a.txt type = %q, want file", entries[0].Type)
	}
}

func TestListDirRejectsNonDirectory(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, path)

	_, err := p.ListDir(context.Background(), target)
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeNotDirectory {
		t.Fatalf("err = %v, want CodeNotDirectory", err)
	}
}

func TestEditTextUniqueReplacement(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")
	if _, err := p.WriteText(context.Background(), target, "hello world", fsvocab.Unconditional()); err != nil {
		t.Fatal(err)
	}

	before, after, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "world", NewString: "there"}, fsvocab.Unconditional())
	if err != nil {
		t.Fatal(err)
	}
	if before != "hello world" || after != "hello there" {
		t.Fatalf("before=%q after=%q", before, after)
	}
	got, _ := p.ReadText(context.Background(), target)
	if got != "hello there" {
		t.Fatalf("file content = %q", got)
	}
}

func TestEditTextAmbiguousWithoutReplaceAll(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")
	p.WriteText(context.Background(), target, "aa aa aa", fsvocab.Unconditional())

	_, _, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "aa", NewString: "bb"}, fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeAmbiguousEdit {
		t.Fatalf("err = %v, want CodeAmbiguousEdit", err)
	}
}

func TestEditTextReplaceAllReplacesEveryOccurrence(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")
	p.WriteText(context.Background(), target, "aa aa aa", fsvocab.Unconditional())

	_, after, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "aa", NewString: "bb", ReplaceAll: true}, fsvocab.Unconditional())
	if err != nil {
		t.Fatal(err)
	}
	if after != "bb bb bb" {
		t.Fatalf("after = %q", after)
	}
}

func TestEditTextNotFound(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "x.txt")
	p.WriteText(context.Background(), target, "hello", fsvocab.Unconditional())

	_, _, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "nope", NewString: "x"}, fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeEditNotFound {
		t.Fatalf("err = %v, want CodeEditNotFound", err)
	}
}

func TestEditTextMissingTargetIsNotFound(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, "missing.txt")

	_, _, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "a", NewString: "b"}, fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeNotFound {
		t.Fatalf("err = %v, want CodeNotFound", err)
	}
}

func TestEditTextDeniedUnderReadOnly(t *testing.T) {
	workspace := t.TempDir()
	// Seed the file directly (bypassing the fence) since WorkspaceWrite
	// isn't under test here.
	path := filepath.Join(workspace, "x.txt")
	os.WriteFile(path, []byte("hello"), 0o644)

	p := New(workspace, sandbox.ReadOnly)
	target := mustTarget(t, p, path)
	_, _, _, err := p.EditText(context.Background(), target, fsvocab.Edit{OldString: "hello", NewString: "bye"}, fsvocab.Unconditional())
	fsErr, ok := err.(*fsvocab.Error)
	if !ok || fsErr.Code != fsvocab.CodeSandboxDenied {
		t.Fatalf("err = %v, want CodeSandboxDenied", err)
	}
}

func TestWriteToCreatesParentDirectories(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.WorkspaceWrite)
	target := mustTarget(t, p, filepath.Join(workspace, "nested", "dir", "file.txt"))

	if _, err := p.WriteText(context.Background(), target, "x", fsvocab.Unconditional()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "nested", "dir", "file.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestContainsReportsDescendantsAndSelf(t *testing.T) {
	workspace := t.TempDir()
	p := New(workspace, sandbox.ReadOnly)
	parent := mustTarget(t, p, workspace)
	child := mustTarget(t, p, filepath.Join(workspace, "a", "b.txt"))
	unrelated := mustTarget(t, p, t.TempDir())

	if !p.Contains(parent, parent) {
		t.Fatal("a target should contain itself")
	}
	if !p.Contains(parent, child) {
		t.Fatal("want parent to contain a nested descendant")
	}
	if p.Contains(parent, unrelated) {
		t.Fatal("want an unrelated directory not contained")
	}
}

package toolfs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go-harness/core/tools"
	"go-harness/fs/fssandbox"
	"go-harness/sandbox/sandbox"
)

func newTestTools(t *testing.T, mode sandbox.Mode, opts ToolOptions) (readT, writeT, editT tools.ToolDefinition, workspace string) {
	t.Helper()
	workspace = t.TempDir()
	opts.FS = fssandbox.New(workspace, mode)
	opts.CWD = workspace
	read, write, edit, err := NewTools(opts)
	if err != nil {
		t.Fatal(err)
	}
	return read, write, edit, workspace
}

func runViaRegistry(t *testing.T, defs []tools.ToolDefinition, name string, args map[string]any) tools.ExecutionResult {
	t.Helper()
	r := tools.NewRegistry()
	for _, def := range defs {
		if _, err := r.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return r.Execute(context.Background(), tools.ExecutionInput{CallID: "c1", Name: name, Arguments: args})
}

func TestNewToolsValidatesFS(t *testing.T) {
	if _, _, _, err := NewTools(ToolOptions{}); err == nil {
		t.Fatal("want an error for a missing FS")
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	read, write, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{read, write}

	result := runViaRegistry(t, defs, "write", map[string]any{"file_path": "note.txt", "content": "line one\nline two\n"})
	if result.IsError {
		t.Fatalf("write result = %+v", result)
	}
	if !strings.Contains(result.Value.(string), "created") {
		t.Fatalf("write output = %q, want it to say created", result.Value)
	}

	result = runViaRegistry(t, defs, "read", map[string]any{"file_path": "note.txt"})
	if result.IsError {
		t.Fatalf("read result = %+v", result)
	}
	out := result.Value.(string)
	if !strings.Contains(out, "1\tline one") || !strings.Contains(out, "2\tline two") {
		t.Fatalf("read output = %q", out)
	}
	if !strings.Contains(out, "End of file - total 2 lines") {
		t.Fatalf("read output missing end-of-file footer: %q", out)
	}
}

func TestWriteReportsUpdateForExistingFile(t *testing.T) {
	_, write, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{write}

	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "v1"})
	result := runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "v2"})
	if result.IsError || !strings.Contains(result.Value.(string), "updated") {
		t.Fatalf("result = %+v, want it to say updated", result)
	}
}

func TestWriteDeniedUnderReadOnly(t *testing.T) {
	_, write, _, _ := newTestTools(t, sandbox.ReadOnly, ToolOptions{})
	result := runViaRegistry(t, []tools.ToolDefinition{write}, "write", map[string]any{"file_path": "x.txt", "content": "denied"})
	if !result.IsError {
		t.Fatal("want an error — read-only must deny the write")
	}
	if !strings.Contains(result.Content[0].Text, "FS_SANDBOX_DENIED") {
		t.Fatalf("error content = %q, want the FS_SANDBOX_DENIED code visible", result.Content[0].Text)
	}
}

func TestReadOffsetAndLimit(t *testing.T) {
	read, write, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{read, write}
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i))
	}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "big.txt", "content": strings.Join(lines, "\n")})

	result := runViaRegistry(t, defs, "read", map[string]any{"file_path": "big.txt", "offset": 3, "limit": 2})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	out := result.Value.(string)
	if !strings.Contains(out, "3\tline-3") || !strings.Contains(out, "4\tline-4") || strings.Contains(out, "5\tline-5") {
		t.Fatalf("read output = %q, want exactly lines 3-4", out)
	}
	if !strings.Contains(out, "Showing lines 3-4 of 10. Use offset=5 to continue.") {
		t.Fatalf("read output missing pagination footer: %q", out)
	}
}

func TestReadOffsetOutOfRange(t *testing.T) {
	read, write, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{read, write}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "one line"})

	result := runViaRegistry(t, defs, "read", map[string]any{"file_path": "x.txt", "offset": 99})
	if !result.IsError {
		t.Fatal("want an error for an out-of-range offset")
	}
}

func TestReadLimitCappedAtConfiguredMax(t *testing.T) {
	read, _, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{ReadLimit: 5})
	result := runViaRegistry(t, []tools.ToolDefinition{read}, "read", map[string]any{"file_path": "x.txt", "limit": 100})
	if !result.IsError {
		t.Fatal("want an error requesting a limit above the configured max")
	}
}

func TestReadTruncatesLongLines(t *testing.T) {
	read, write, _, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{ReadMaxLineLength: 10})
	defs := []tools.ToolDefinition{read, write}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "0123456789ABCDEFGHIJ"})

	result := runViaRegistry(t, defs, "read", map[string]any{"file_path": "x.txt"})
	out := result.Value.(string)
	if !strings.Contains(out, "0123456789... (line truncated to 10 chars)") {
		t.Fatalf("read output = %q", out)
	}
}

func TestReadMissingFile(t *testing.T) {
	read, _, _, _ := newTestTools(t, sandbox.ReadOnly, ToolOptions{})
	result := runViaRegistry(t, []tools.ToolDefinition{read}, "read", map[string]any{"file_path": "missing.txt"})
	if !result.IsError {
		t.Fatal("want an error for a missing file")
	}
	if !strings.Contains(result.Content[0].Text, "FS_NOT_FOUND") {
		t.Fatalf("error content = %q", result.Content[0].Text)
	}
}

func TestEditUniqueReplacement(t *testing.T) {
	read, write, edit, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{read, write, edit}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "hello world"})

	result := runViaRegistry(t, defs, "edit", map[string]any{"file_path": "x.txt", "old_string": "world", "new_string": "there"})
	if result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Value.(string), "updated successfully") {
		t.Fatalf("edit output = %q", result.Value)
	}

	got := runViaRegistry(t, defs, "read", map[string]any{"file_path": "x.txt"})
	if !strings.Contains(got.Value.(string), "hello there") {
		t.Fatalf("file content after edit = %q", got.Value)
	}
}

func TestEditReplaceAll(t *testing.T) {
	_, write, edit, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{write, edit}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "aa aa aa"})

	result := runViaRegistry(t, defs, "edit", map[string]any{
		"file_path": "x.txt", "old_string": "aa", "new_string": "bb", "replace_all": true,
	})
	if result.IsError || !strings.Contains(result.Value.(string), "All occurrences") {
		t.Fatalf("result = %+v", result)
	}
}

func TestEditAmbiguousWithoutReplaceAll(t *testing.T) {
	_, write, edit, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	defs := []tools.ToolDefinition{write, edit}
	runViaRegistry(t, defs, "write", map[string]any{"file_path": "x.txt", "content": "aa aa"})

	result := runViaRegistry(t, defs, "edit", map[string]any{"file_path": "x.txt", "old_string": "aa", "new_string": "bb"})
	if !result.IsError {
		t.Fatal("want an error for an ambiguous match")
	}
	if !strings.Contains(result.Content[0].Text, "FS_AMBIGUOUS_EDIT") {
		t.Fatalf("error content = %q", result.Content[0].Text)
	}
}

func TestEditRejectsIdenticalOldAndNewString(t *testing.T) {
	_, _, edit, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	result := runViaRegistry(t, []tools.ToolDefinition{edit}, "edit", map[string]any{
		"file_path": "x.txt", "old_string": "same", "new_string": "same",
	})
	if !result.IsError {
		t.Fatal("want an error when old_string equals new_string")
	}
}

func TestEditMissingFile(t *testing.T) {
	_, _, edit, _ := newTestTools(t, sandbox.WorkspaceWrite, ToolOptions{})
	result := runViaRegistry(t, []tools.ToolDefinition{edit}, "edit", map[string]any{
		"file_path": "missing.txt", "old_string": "a", "new_string": "b",
	})
	if !result.IsError {
		t.Fatal("want an error for a missing file")
	}
}

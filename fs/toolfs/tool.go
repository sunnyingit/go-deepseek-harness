// Package toolfs is a Go port of packages/fs/tool-fs (dsh-tool-fs) from
// DeepSeek Harness: the model-facing read/write/edit tools built on
// go-harness/fs/fs's abstract FileSystem — typically an fs/fssandbox
// .Provider, but any implementation works.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package toolfs

import (
	"context"
	"errors"
	"fmt"

	"go-harness/core/session"
	"go-harness/core/tools"
	fsvocab "go-harness/fs/fs"
)

const (
	defaultReadLimit         = 2000
	defaultReadMaxLineLength = 2000
	defaultReadMaxBytes      = 51200
)

// ToolOptions configures NewTools.
type ToolOptions struct {
	// FS is the filesystem every tool dispatches through. Required.
	FS fsvocab.FileSystem
	// CWD is the default base a relative file_path resolves against.
	CWD string
	// ReadLimit caps how many lines read returns per call. Defaults to
	// 2000, matching the TS original's own default.
	ReadLimit int
	// ReadMaxLineLength truncates any single line past this many
	// characters. Defaults to 2000.
	ReadMaxLineLength int
	// ReadMaxBytes caps read's total rendered output. Defaults to 51200.
	ReadMaxBytes int
}

func (o ToolOptions) withDefaults() ToolOptions {
	if o.ReadLimit <= 0 {
		o.ReadLimit = defaultReadLimit
	}
	if o.ReadMaxLineLength <= 0 {
		o.ReadMaxLineLength = defaultReadMaxLineLength
	}
	if o.ReadMaxBytes <= 0 {
		o.ReadMaxBytes = defaultReadMaxBytes
	}
	return o
}

// NewTools builds the read, write, and edit tools, all sharing one
// fs.FileSystem.
func NewTools(opts ToolOptions) (read, write, edit tools.ToolDefinition, err error) {
	if opts.FS == nil {
		return tools.ToolDefinition{}, tools.ToolDefinition{}, tools.ToolDefinition{},
			fmt.Errorf("toolfs: ToolOptions.FS is required")
	}
	opts = opts.withDefaults()
	return newReadTool(opts), newWriteTool(opts), newEditTool(opts), nil
}

func resolve(ctx context.Context, opts ToolOptions, path string) (fsvocab.Target, error) {
	return opts.FS.Resolve(ctx, path, fsvocab.ResolveOptions{CWD: opts.CWD})
}

// renderFsError unwraps a *fsvocab.Error into a "<CODE>: <message>" string
// so the model sees the stable code, not just prose — the closest this
// port gets to the TS original's structured { name, code } result field
// without a separate error-result channel to carry it in.
func renderFsError(err error) error {
	var fsErr *fsvocab.Error
	if errors.As(err, &fsErr) {
		return fmt.Errorf("%s: %s", fsErr.Code, fsErr.Message)
	}
	return err
}

func renderStringResult(_ map[string]any, value any) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
}

func numberArg(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

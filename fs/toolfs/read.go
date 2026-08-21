package toolfs

import (
	"context"
	"fmt"
	"strings"

	"go-harness/core/tools"
)

func newReadTool(opts ToolOptions) tools.ToolDefinition {
	return tools.ToolDefinition{
		Name: "read",
		Description: fmt.Sprintf(
			"Read a UTF-8 text file, line-numbered. Returns up to %d lines starting at offset (1-based, "+
				"default 1); each line is truncated past %d characters, and total output is capped at %d "+
				"bytes — use the offset the footer suggests to continue reading past either limit.",
			opts.ReadLimit, opts.ReadMaxLineLength, opts.ReadMaxBytes),
		Parameters: tools.ParameterSchema{
			"file_path": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "path to the file"}, Required: true},
			"offset":    {ValueSchema: tools.ValueSchema{Kind: tools.KindInteger, Description: "1-based starting line, default 1"}},
			"limit": {ValueSchema: tools.ValueSchema{Kind: tools.KindInteger,
				Description: fmt.Sprintf("max lines to return, default and cap %d", opts.ReadLimit)}},
		},
		Output: tools.Output{Schema: tools.ValueSchema{Kind: tools.KindString}, Render: renderStringResult},
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			return executeRead(ctx, opts, args)
		},
	}
}

func executeRead(ctx context.Context, opts ToolOptions, args map[string]any) (string, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return "", fmt.Errorf("file_path must be a non-empty string")
	}
	offset := 1
	if v, ok := numberArg(args["offset"]); ok && v > 1 {
		offset = v
	}
	limit := opts.ReadLimit
	if v, ok := numberArg(args["limit"]); ok && v > 0 {
		if v > opts.ReadLimit {
			return "", fmt.Errorf("limit must be less than or equal to %d", opts.ReadLimit)
		}
		limit = v
	}

	target, err := resolve(ctx, opts, path)
	if err != nil {
		return "", renderFsError(err)
	}
	content, err := opts.FS.ReadText(ctx, target)
	if err != nil {
		return "", renderFsError(err)
	}

	lines := strings.Split(content, "\n")
	// A trailing newline produces one spurious empty final element; drop
	// it so "N lines" means what counting them by eye would.
	if len(lines) > 0 && lines[len(lines)-1] == "" && strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)

	if total > 0 && offset > total {
		return "", fmt.Errorf("offset %d is out of range for %q (%d lines)", offset, target.DisplayPath, total)
	}

	startIdx := offset - 1
	endIdx := min(startIdx+limit, total)

	var b strings.Builder
	bytesWritten := 0
	lastShown := startIdx - 1
	capped := false
	for i := startIdx; i < endIdx; i++ {
		line := lines[i]
		if len(line) > opts.ReadMaxLineLength {
			line = line[:opts.ReadMaxLineLength] + fmt.Sprintf("... (line truncated to %d chars)", opts.ReadMaxLineLength)
		}
		rendered := fmt.Sprintf("%6d\t%s\n", i+1, line)
		if bytesWritten+len(rendered) > opts.ReadMaxBytes {
			capped = true
			break
		}
		b.WriteString(rendered)
		bytesWritten += len(rendered)
		lastShown = i
	}

	switch {
	case capped:
		fmt.Fprintf(&b, "\n(Output capped. Showing lines %d-%d. Use offset=%d to continue.)", offset, lastShown+1, lastShown+2)
	case endIdx < total:
		fmt.Fprintf(&b, "\n(Showing lines %d-%d of %d. Use offset=%d to continue.)", offset, endIdx, total, endIdx+1)
	default:
		fmt.Fprintf(&b, "\n(End of file - total %d lines)", total)
	}

	return b.String(), nil
}

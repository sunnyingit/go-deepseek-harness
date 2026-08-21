package toolfs

import (
	"context"
	"fmt"

	"go-harness/core/tools"
	fsvocab "go-harness/fs/fs"
)

func newWriteTool(opts ToolOptions) tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "write",
		Description: "Create a new file or fully replace an existing one's content.",
		Parameters: tools.ParameterSchema{
			"file_path": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "path to the file"}, Required: true},
			"content":   {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "the complete replacement content"}, Required: true},
		},
		Output: tools.Output{Schema: tools.ValueSchema{Kind: tools.KindString}, Render: renderStringResult},
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			return executeWrite(ctx, opts, args)
		},
	}
}

func executeWrite(ctx context.Context, opts ToolOptions, args map[string]any) (string, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return "", fmt.Errorf("file_path must be a non-empty string")
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content is required")
	}

	target, err := resolve(ctx, opts, path)
	if err != nil {
		return "", renderFsError(err)
	}
	_, existed, err := opts.FS.Stat(ctx, target)
	if err != nil {
		return "", renderFsError(err)
	}
	if _, err := opts.FS.WriteText(ctx, target, content, fsvocab.Unconditional()); err != nil {
		return "", renderFsError(err)
	}
	op := "create"
	if existed {
		op = "update"
	}
	return fmt.Sprintf("%sd %s (%d bytes)", op, target.DisplayPath, len(content)), nil
}

package toolfs

import (
	"context"
	"fmt"

	"go-harness/core/tools"
	fsvocab "go-harness/fs/fs"
)

func newEditTool(opts ToolOptions) tools.ToolDefinition {
	return tools.ToolDefinition{
		Name: "edit",
		Description: "Replace an exact, literal snippet of text within an existing file. old_string must " +
			"appear exactly once unless replace_all is set.",
		Parameters: tools.ParameterSchema{
			"file_path":   {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "path to the file"}, Required: true},
			"old_string":  {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "exact text to find"}, Required: true},
			"new_string":  {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "replacement text"}, Required: true},
			"replace_all": {ValueSchema: tools.ValueSchema{Kind: tools.KindBoolean, Description: "replace every occurrence instead of requiring exactly one"}},
		},
		Output: tools.Output{Schema: tools.ValueSchema{Kind: tools.KindString}, Render: renderStringResult},
		Execute: func(ctx context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			return executeEdit(ctx, opts, args)
		},
	}
}

func executeEdit(ctx context.Context, opts ToolOptions, args map[string]any) (string, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return "", fmt.Errorf("file_path must be a non-empty string")
	}
	oldString, _ := args["old_string"].(string)
	if oldString == "" {
		return "", fmt.Errorf("old_string must be a non-empty string")
	}
	newString, _ := args["new_string"].(string)
	if oldString == newString {
		return "", fmt.Errorf("old_string and new_string must differ")
	}
	replaceAll, _ := args["replace_all"].(bool)

	target, err := resolve(ctx, opts, path)
	if err != nil {
		return "", renderFsError(err)
	}
	_, _, _, err = opts.FS.EditText(ctx, target,
		fsvocab.Edit{OldString: oldString, NewString: newString, ReplaceAll: replaceAll}, fsvocab.Unconditional())
	if err != nil {
		return "", renderFsError(err)
	}
	if replaceAll {
		return fmt.Sprintf("The file %s has been updated. All occurrences were successfully replaced.", target.DisplayPath), nil
	}
	return fmt.Sprintf("The file %s has been updated successfully.", target.DisplayPath), nil
}

// Display tools are presentation pseudo-tools: the model fills a
// strictly-schema'd payload (headers/rows, chart series, …) instead of
// dumping a table as prose. Execute does no work — it only returns the
// synthetic tool_result the agent loop requires before the next step.
// writeStreamEvent then re-labels a successful call as the frontend
// MessageType so the UI can render it.
package server

import (
	"context"

	"go-harness/core/session"
	coretools "go-harness/core/tools"
)

// displayedAck is the synthetic tool_result every display tool returns.
// These tools never touch a warehouse / filesystem / interpreter — they exist
// so the model can hand the UI a strictly-schema'd payload (a table, a chart,
// …) instead of hoping a prose dump will parse. The agent loop still requires
// a result for every model-issued call before the next step can start; without
// this acknowledgement the model sits waiting and the turn errors out.
const displayedAck = "Displayed to the user."

// displayToolTypes maps a model-facing tool name onto the frontend
// MessageType the SSE stream should emit once the call succeeds. Anything
// not in this map is a real tool and is forwarded as tool-call / tool-result.
var displayToolTypes = map[string]string{
	"show_table":      "table",
	"show_chart":      "chart",
	"show_code":       "code",
	"show_suggestion": "suggestion",
	"show_image":      "image",
	"show_error":      "error",
	"show_progress":   "progress",
}

func isDisplayTool(name string) bool {
	_, ok := displayToolTypes[name]
	return ok
}

// displayEvent is the tool/call → frontend-event translation: if call is a
// display tool, eventType is the MessageType and payload is the model's
// filled-in arguments (shallow-copied so later mutation of the log entry
// can't change what the UI sees). ok is false for real tools.
func displayEvent(call session.ContentBlock) (eventType string, payload map[string]any, ok bool) {
	eventType, ok = displayToolTypes[call.ToolName]
	if !ok {
		return "", nil, false
	}
	payload = make(map[string]any, len(call.Args))
	for k, v := range call.Args {
		payload[k] = v
	}
	return eventType, payload, true
}

func displayTools() []coretools.ToolDefinition {
	return []coretools.ToolDefinition{
		showTableTool(),
		showChartTool(),
		showCodeTool(),
		showSuggestionTool(),
		showImageTool(),
		showErrorTool(),
		showProgressTool(),
	}
}

func displayAckRender(_ map[string]any, value any) []session.ContentBlock {
	return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
}

func displayAckExecute(_ context.Context, _ map[string]any, _ *coretools.RunContext) (any, error) {
	return displayedAck, nil
}

func displayTool(name, description string, params coretools.ParameterSchema) coretools.ToolDefinition {
	return coretools.ToolDefinition{
		Name:        name,
		Description: description,
		Parameters:  params,
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: displayAckRender,
		},
		// No side effects: overlapping display calls in one step are fine.
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute:           displayAckExecute,
	}
}

func showTableTool() coretools.ToolDefinition {
	cell := coretools.ValueSchema{
		Kind: coretools.KindOneOf,
		OneOf: []coretools.ValueSchema{
			{Kind: coretools.KindString},
			{Kind: coretools.KindNumber},
			{Kind: coretools.KindBoolean},
			{Kind: coretools.KindNull},
		},
	}
	row := coretools.ValueSchema{
		Kind:        coretools.KindArray,
		Items:       &cell,
		Description: "One row; cell count should match headers.",
	}
	return displayTool("show_table",
		"Render a table in the user-facing chat. Call this instead of writing the table as markdown or prose whenever tabular data is the right shape. The UI renders your headers and rows; the tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"headers": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindArray,
					Items:       &coretools.ValueSchema{Kind: coretools.KindString},
					Description: "Column titles, left to right.",
				},
				Required: true,
			},
			"rows": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindArray,
					Items:       &row,
					Description: "Body rows. Each row is an array of cell values aligned with headers.",
				},
				Required: true,
			},
			"caption": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional short title shown above the table."},
			},
		})
}

func showChartTool() coretools.ToolDefinition {
	number := coretools.ValueSchema{Kind: coretools.KindNumber}
	seriesItem := coretools.ValueSchema{
		Kind: coretools.KindObject,
		Properties: coretools.ParameterSchema{
			"name":   {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Legend label for this series."}, Required: true},
			"values": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindArray, Items: &number, Description: "Numeric values, one per label."}, Required: true},
		},
	}
	return displayTool("show_chart",
		"Render a bar, line, or pie chart in the user-facing chat. Call this instead of describing the numbers in prose or ASCII. The UI renders it; the tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"type": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindString,
					Enum:        []any{"bar", "line", "pie"},
					Description: "Chart kind.",
				},
				Required: true,
			},
			"title": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional chart title."},
			},
			"labels": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindArray,
					Items:       &coretools.ValueSchema{Kind: coretools.KindString},
					Description: "Category labels along the axis (or pie slices).",
				},
				Required: true,
			},
			"series": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindArray,
					Items:       &seriesItem,
					Description: "One or more named series. Pie charts use only the first series.",
				},
				Required: true,
			},
		})
}

func showCodeTool() coretools.ToolDefinition {
	return displayTool("show_code",
		"Render a syntax-highlighted code block in the user-facing chat. Prefer this over fencing the code in ordinary text when the snippet is the main thing to show. The tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"language": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Highlighter language id, e.g. python, go, json, bash."},
				Required:    true,
			},
			"code": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "The source to display, verbatim."},
				Required:    true,
			},
			"filename": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional filename shown in the header."},
			},
		})
}

func showSuggestionTool() coretools.ToolDefinition {
	item := coretools.ValueSchema{
		Kind: coretools.KindObject,
		Properties: coretools.ParameterSchema{
			"label":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Button text the user sees."}, Required: true},
			"action": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Message to send when clicked. Defaults to label."}},
			"icon":   {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional Font Awesome class, e.g. fa-chart-bar."}},
		},
	}
	return displayTool("show_suggestion",
		"Render clickable suggestion chips under your reply — follow-up questions or next actions the user can tap instead of typing. The tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"title": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional heading above the chips."},
			},
			"items": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindArray,
					Items:       &item,
					Description: "One chip per item.",
				},
				Required: true,
			},
		})
}

func showImageTool() coretools.ToolDefinition {
	return displayTool("show_image",
		"Show an image in the user-facing chat. path is either a workspace-relative file the tools already wrote (e.g. plot.png) or an http(s) URL. The tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"path": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Workspace-relative path, or an http(s) URL."},
				Required:    true,
			},
			"alt": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Short accessible description."},
			},
		})
}

func showErrorTool() coretools.ToolDefinition {
	return displayTool("show_error",
		"Render a structured error card in the user-facing chat — use when something failed and the user should see a clear message, optional details, and a suggested next step. The tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"message": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Short error headline."},
				Required:    true,
			},
			"details": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional longer explanation."},
			},
			"suggestion": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional next-step hint."},
			},
		})
}

func showProgressTool() coretools.ToolDefinition {
	return displayTool("show_progress",
		"Render a progress bar in the user-facing chat. Call again with an updated percent as work advances. The tool result is only an acknowledgement — keep talking after it.",
		coretools.ParameterSchema{
			"status": {
				ValueSchema: coretools.ValueSchema{
					Kind:        coretools.KindString,
					Enum:        []any{"pending", "running", "completed", "failed"},
					Description: "Lifecycle state.",
				},
				Required: true,
			},
			"progress": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindInteger, Description: "Percent complete, 0–100."},
				Required:    true,
			},
			"message": {
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "Optional status text under the bar."},
			},
		})
}

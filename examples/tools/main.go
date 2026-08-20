// Demo: core/tools driving a real DeepSeek + ReAct loop through
// tools.Bridge, showing the two things a bare hand-rolled ToolExecutor
// (see examples/react) doesn't have: a monotonic Guard enforcing a policy
// no tool body can override, and an OnResult observer auditing every call —
// including the ones the guard denied.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	"go-harness/core/tools"
	"go-harness/llm/deepseek"
)

// fakeFS stands in for a real filesystem so the demo has no side effects.
var fakeFS = map[string]string{
	"/workspace/project/config.yaml": "name: project\nversion: 1.0.0\n",
	"/workspace/project/README.md":   "# Project\n\nA demo project.\n",
	"/etc/passwd":                    "root:x:0:0:root:/root:/bin/bash",
}

func readFileTool() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "read_file",
		Description: "Read a file's contents.",
		Parameters: tools.ParameterSchema{
			"path": {ValueSchema: tools.ValueSchema{Kind: tools.KindString, Description: "absolute file path"}, Required: true},
		},
		Output: tools.Output{
			Schema: tools.ValueSchema{Kind: tools.KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		Execute: func(_ context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			path, _ := args["path"].(string)
			content, ok := fakeFS[path]
			if !ok {
				return nil, fmt.Errorf("no such file: %s", path)
			}
			return content, nil
		},
	}
}

func listFilesTool() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "list_files",
		Description: "List files under a directory prefix.",
		Parameters: tools.ParameterSchema{
			"dir": {ValueSchema: tools.ValueSchema{Kind: tools.KindString}, Required: true},
		},
		Output: tools.Output{
			Schema: tools.ValueSchema{Kind: tools.KindString},
			Render: func(_ map[string]any, value any) []session.ContentBlock {
				return []session.ContentBlock{{Kind: session.BlockToolResult, Text: value.(string)}}
			},
		},
		// Read-only listing over an in-memory map is safe to run concurrently
		// with anything else — this is what actually lets list_files overlap
		// with another parallel-safe call in the same step's dispatch pool.
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(_ context.Context, args map[string]any, _ *tools.RunContext) (any, error) {
			dir, _ := args["dir"].(string)
			var names []string
			for path := range fakeFS {
				if strings.HasPrefix(path, dir) {
					names = append(names, path)
				}
			}
			sort.Strings(names)
			if len(names) == 0 {
				return "(no files found)", nil
			}
			return strings.Join(names, ", "), nil
		},
	}
}

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set DEEPSEEK_API_KEY to run this demo")
		os.Exit(1)
	}

	registry := tools.NewRegistry()
	must(registry.Register(readFileTool()))
	must(registry.Register(listFilesTool()))

	// Policy: no tool body gets to decide this — a call can slip past a
	// careless Execute implementation, but never past the guard.
	registry.Guard(func(exec tools.Execution) string {
		if exec.Name != "read_file" {
			return ""
		}
		path, _ := exec.Arguments["path"].(string)
		if !strings.HasPrefix(path, "/workspace/") {
			return fmt.Sprintf("access to %q is outside the allowed /workspace root", path)
		}
		return ""
	})

	// Observability: every call, allowed or denied, lands here — the model
	// never sees this, but an operator watching the process does.
	registry.OnResult(func(exec tools.Execution, result tools.ExecutionResult) {
		status := "ok"
		if result.IsError {
			status = "error: " + result.Error.Message
		}
		fmt.Printf("[audit] %s(%v) -> %s\n", exec.Name, exec.Arguments, status)
	})

	adapter, err := deepseek.New(deepseek.Config{
		APIKey:  apiKey,
		BaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "deepseek adapter:", err)
		os.Exit(1)
	}

	sess := agentloop.NewMemorySession()
	agent := agentloop.NewAgent(
		sess,
		adapter,
		tools.Bridge{Registry: registry},
		"You are a terse file assistant. Use the available tools instead of guessing at file contents.",
		agentloop.Hooks{},
	).WithModel("deepseek-chat")

	question := "List the files under /workspace/project, read /workspace/project/config.yaml, and also try to read /etc/passwd."
	fmt.Println("User:", question)
	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}}})
	agent.WhenIdle()

	printTranscript(sess)
}

func printTranscript(sess agentloop.Session) {
	fmt.Println()
	for _, ev := range sess.Events() {
		switch ev.Type {
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, b := range msg.Content {
				switch b.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", b.Text)
				case agentloop.BlockToolCall:
					fmt.Printf("Action:  %s(%s)\n", b.ToolName, formatArgs(b.Args))
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", msg.Content[0].Text)
		}
	}
}

func formatArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + formatArg(args[k])
	}
	return strings.Join(parts, ", ")
}

func formatArg(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func must(_ func(), err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		os.Exit(1)
	}
}

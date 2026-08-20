// Demo: a real DeepSeek + ReAct loop with a run_python tool backed by a
// provisioned virtualenv (python-docx, python-pptx, openpyxl, pypdf,
// reportlab, pandas, Pillow) and confined by sandbox/local — the model
// generates a Word document and a PowerPoint deck by writing and running
// Python, never with direct filesystem access of its own.
//
//	export DEEPSEEK_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go-harness/core/agentloop"
	"go-harness/llm/deepseek"
	"go-harness/sandbox/local"
	"go-harness/tools/python"

	coretools "go-harness/core/tools"
)

func main() {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "set DEEPSEEK_API_KEY to run this demo")
		os.Exit(1)
	}

	venvDir := filepath.Join(os.TempDir(), "go-harness-python-demo-venv")
	fmt.Println("provisioning Python environment at", venvDir, "(cached after the first run)...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	env, err := python.EnsureEnv(ctx, python.EnvOptions{CacheDir: venvDir})
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "provisioning python env:", err)
		os.Exit(1)
	}
	fmt.Println("ready:", env.Packages)

	workspace, err := os.MkdirTemp("", "go-harness-python-demo-workspace-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(workspace)
	fmt.Println("workspace:", workspace)

	runTool, err := python.NewRunTool(python.RunToolOptions{
		Env:           env,
		Provider:      local.New(),
		WorkspaceRoot: workspace,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "building run_python tool:", err)
		os.Exit(1)
	}

	registry := coretools.NewRegistry()
	if _, err := registry.Register(runTool); err != nil {
		fmt.Fprintln(os.Stderr, "registering run_python:", err)
		os.Exit(1)
	}
	registry.OnResult(func(exec coretools.Execution, result coretools.ExecutionResult) {
		status := "ok"
		if result.IsError {
			status = "error: " + result.Error.Message
		}
		fmt.Printf("[audit] %s -> %s\n", exec.Name, status)
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
		coretools.Bridge{Registry: registry},
		"You are a document-generation assistant. Use the run_python tool to create files; "+
			"it runs in a sandboxed Python interpreter that already has python-docx, python-pptx, "+
			"openpyxl, pypdf, reportlab, pandas, and Pillow installed. Write files as relative paths.",
		agentloop.Hooks{},
	).WithModel("deepseek-chat")

	question := "Create report.docx: a Word document with a heading 'Q3 Sales Report' and a paragraph saying revenue grew 12%. " +
		"Also create summary.pptx: a PowerPoint with one slide titled 'Q3 Highlights' and a bullet point 'Revenue up 12%'."
	fmt.Println("\nUser:", question)
	agent.Followup(agentloop.Message{Role: agentloop.RoleUser, Content: []agentloop.ContentBlock{{Kind: agentloop.BlockText, Text: question}}})
	agent.WhenIdle()

	printTranscript(sess)
	verifyWorkspace(workspace)
}

func printTranscript(sess agentloop.Session) {
	fmt.Println("\n=== Transcript ===")
	for _, ev := range sess.Events() {
		switch ev.Type {
		case "assistant/message":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			for _, b := range msg.Content {
				switch b.Kind {
				case agentloop.BlockText, agentloop.BlockReasoning:
					fmt.Println("Thought:", b.Text)
				case agentloop.BlockToolCall:
					fmt.Println("Action:  run_python(...)")
				}
			}
		case "tool/result":
			msg := ev.Data.(map[string]any)["message"].(agentloop.Message)
			fmt.Println("Observation:", msg.Content[0].Text)
		}
	}
}

func verifyWorkspace(workspace string) {
	fmt.Println("\n=== Workspace contents ===")
	entries, err := os.ReadDir(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		info, _ := os.Stat(filepath.Join(workspace, name))
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Printf("  %s (%d bytes)\n", name, size)
	}
}

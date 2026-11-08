package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	coretools "go-harness/core/tools"
)

var boardNamePattern = regexp.MustCompile(`^[a-z]{3,10}$`)

// registerBaiduBoardTool gives an analyst the Baidu hot-search board (the finance board by default) when its
// skills list has baidu-board. It reads the stock service, like the other data tools.
func (s *Server) registerBaiduBoardTool(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "baidu-board") {
		return nil
	}
	tool, err := s.fetchBaiduBoardTool()
	if err != nil {
		return err
	}
	_, err = toolRegistry.Register(tool)
	return err
}

func (s *Server) fetchBaiduBoardTool() (coretools.ToolDefinition, error) {
	script, err := s.skillScript("baidu-board", "scripts/fetch_baidu_board.py")
	if err != nil {
		return coretools.ToolDefinition{}, err
	}
	pythonBin := ""
	if s.pythonEnv != nil {
		pythonBin = s.pythonEnv.PythonPath
	}
	if pythonBin == "" {
		return coretools.ToolDefinition{}, errors.New("python 还没有准备好")
	}
	return coretools.ToolDefinition{
		Name:        "fetch_baidu_board",
		Description: "读取百度热搜榜，默认财经榜：名次、标题、热度、新/热标。board 可选 finance（财经榜）、realtime（实时热搜）、civil（民生榜）；limit 取前几条，默认 15，最多 50。只是「大家在搜什么」，不是利好利空。走股票服务 /api/v1/shared/baidu-board。",
		Parameters: coretools.ParameterSchema{
			"board": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "榜单，默认 finance"}},
			"limit": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "取前几条，1 到 50，默认 15"}},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		TimeoutMs:         35_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			board := strings.ToLower(strings.TrimSpace(fmt.Sprint(orDefault(args["board"], "finance"))))
			if !boardNamePattern.MatchString(board) {
				return "", fmt.Errorf("board 只能是 finance、realtime、civil 这种名字")
			}
			limit := strings.TrimSpace(fmt.Sprint(orDefault(args["limit"], "15")))
			if n, err := strconv.Atoi(limit); err != nil || n < 1 || n > 50 {
				return "", fmt.Errorf("limit 要是 1 到 50 的数字")
			}
			cmd := exec.CommandContext(ctx, pythonBin, script, board, limit)
			cmd.Env = basicEnv()
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				captured := strings.TrimSpace(out.String())
				if captured == "" {
					return "", fmt.Errorf("读取百度热搜榜失败: %v", err)
				}
				return "", fmt.Errorf("读取百度热搜榜失败: %s", captured)
			}
			text := strings.TrimSpace(out.String())
			if text == "" {
				return "", errors.New("百度热搜榜没有返回")
			}
			return text, nil
		},
	}, nil
}

func orDefault(v any, fallback string) any {
	if v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
		return fallback
	}
	return v
}

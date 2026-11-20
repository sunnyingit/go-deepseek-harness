package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	coretools "go-harness/core/tools"
)

// fetchWebSearchTool 给问问主理人与分析师 reply 一个通用网络搜索（必应国内版，无 key）。
// 问题不在个股基本面/资金/新闻射程内时（宏观政策、行业事件、人物、常识）用它补充事实。
func (s *Server) fetchWebSearchTool() (coretools.ToolDefinition, error) {
	script, err := s.skillScript("search-web", "scripts/web_search.py")
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
		Name: "fetch_web_search",
		Description: "通用网络搜索（必应）：返回前几条结果的标题、摘要、日期与链接。" +
			"当问题和某只股票的基本面/资金流/新闻无关时（宏观与货币政策、行业政策、公司公告之外的事件、人物、常识）用它补充事实。" +
			"摘要是网页公开信息，不是行情数据：转述数字要谨慎，回答开头注明「来自网络搜索，截至 X 日」；results 为空就直说没搜到，禁止编造。" +
			"维度分析师追问时只拿它补政策/事件等背景——本维度的结论仍以自己的取数工具为准，最多搜 1 次，不要用网搜替代取数。",
		Parameters: coretools.ParameterSchema{
			"q":     {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "搜索词，用能命中新闻的中文短句，如「美联储 12月 降息 概率」"}, Required: true},
			"count": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindNumber, Description: "取前几条，1 到 8，默认 6"}},
		},
		Output:    coretools.Output{Schema: coretools.ValueSchema{Kind: coretools.KindString}, Render: renderStringResult},
		TimeoutMs: 20_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			q := strings.TrimSpace(fmt.Sprint(orDefault(args["q"], "")))
			if q == "" || q == "<nil>" {
				return "", errors.New("缺少搜索词 q")
			}
			count := 6
			if n := flowDays(args["count"]); n > 0 && n <= 8 {
				count = n
			}
			cmd := exec.CommandContext(ctx, pythonBin, script, q, fmt.Sprint(count))
			cmd.Env = basicEnv()
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				captured := strings.TrimSpace(out.String())
				if captured == "" {
					return "", fmt.Errorf("网络搜索失败: %v", err)
				}
				return "", fmt.Errorf("网络搜索失败: %s", captured)
			}
			var payload struct {
				Results []struct {
					Title   string `json:"title"`
					Snippet string `json:"snippet"`
					Date    string `json:"date"`
					URL     string `json:"url"`
				} `json:"results"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &payload); err != nil {
				return "", fmt.Errorf("解析搜索结果失败: %v", err)
			}
			if payload.Error != "" {
				return "", errors.New(payload.Error)
			}
			if len(payload.Results) == 0 {
				return "网上没搜到相关结果。直说没搜到，不要编。", nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("网络搜索「%s」，前 %d 条（必应，公开网页摘要）：\n", q, len(payload.Results)))
			for i, r := range payload.Results {
				date := ""
				if r.Date != "" {
					date = "（" + r.Date + "）"
				}
				fmt.Fprintf(&b, "%d. %s%s\n   %s\n   来源: %s\n", i+1, r.Title, date, r.Snippet, r.URL)
			}
			b.WriteString("\n注意：以上是网页摘要不是行情数据；多条说法冲突时只取一致部分，并注明信息截至日期。\n")
			return b.String(), nil
		},
	}, nil
}

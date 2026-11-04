package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coretools "go-harness/core/tools"
	"go-harness/skill/skill"
)

// chartBox keeps the last D3 page a turn produced. It is not written into
// the group transcript.
type chartBox struct {
	htmls []string
}

func containsSkill(skills []string, name string) bool {
	for _, item := range skills {
		if item == name {
			return true
		}
	}
	return false
}

func withChart(msg map[string]any, charts *chartBox) map[string]any {
	if msg != nil && charts != nil && len(charts.htmls) > 0 {
		msg["chart_htmls"] = charts.htmls
		msg["chart_html"] = charts.htmls[len(charts.htmls)-1]
	}
	return msg
}

func (s *Server) renderChartTool(charts *chartBox) (coretools.ToolDefinition, error) {
	d3, err := s.vendorD3()
	if err != nil {
		return coretools.ToolDefinition{}, err
	}
	return coretools.ToolDefinition{
		Name:        "render_chart",
		Description: "用已安装的 d3js-visualization 画一张离线图。分类对比用 bar，随时间变化用 line。points 是 JSON 数组，每项有 label 和 value，数值必须来自刚取到的事实，不能编。",
		Parameters: coretools.ParameterSchema{
			"type":   {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "bar 或 line", Enum: []any{"bar", "line"}}, Required: true},
			"title":  {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "图的标题"}, Required: true},
			"unit":   {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "数值单位，例如 % 或 亿元"}},
			"points": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "JSON 数组，例如 [{\"label\":\"10-30\",\"value\":1.2}]"}, Required: true},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		TimeoutMs:         10_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(_ context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			kind, _ := args["type"].(string)
			title, _ := args["title"].(string)
			unit, _ := args["unit"].(string)
			raw, _ := args["points"].(string)
			page, err := buildChartHTML(kind, title, unit, raw, d3)
			if err != nil {
				return "", err
			}
			if charts != nil {
				charts.htmls = append(charts.htmls, page)
			}
			return "图已放进回答下方。请在 text 里继续用中文说明，不要再写 HTML。", nil
		},
	}, nil
}

func (s *Server) vendorD3() (string, error) {
	if s.skills == nil {
		return "", fmt.Errorf("d3js-visualization 还没有安装")
	}
	def, err := s.skills.Get(context.Background(), "d3js-visualization", skill.LookupOptions{})
	if err != nil || def == nil || def.ResourceBase == nil || def.ResourceBase.Path == "" {
		return "", fmt.Errorf("d3js-visualization 还没有安装")
	}
	path := filepath.Join(def.ResourceBase.Path, "vendor", "d3.v7.min.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("d3 库不存在: %s", path)
	}
	return string(raw), nil
}

type chartPoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

func buildChartHTML(kind, title, unit, raw, d3 string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind != "bar" && kind != "line" {
		return "", fmt.Errorf("图只支持 bar 或 line")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("图缺少标题")
	}
	var points []chartPoint
	if err := json.Unmarshal([]byte(raw), &points); err != nil {
		return "", fmt.Errorf("points 必须是 JSON 数组")
	}
	if len(points) < 2 || len(points) > 250 {
		return "", fmt.Errorf("图需要 2 到 250 个点")
	}
	for _, point := range points {
		if strings.TrimSpace(point.Label) == "" {
			return "", fmt.Errorf("图的每一项都要有 label")
		}
	}
	spec, err := json.Marshal(map[string]any{
		"type": kind, "title": title, "unit": strings.TrimSpace(unit), "points": points,
	})
	if err != nil {
		return "", err
	}
	return chartPage(string(spec), d3), nil
}

func chartPage(spec, d3 string) string {
	return `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<style>
  html,body { margin:0; background:#fff; color:#14171F; font-family:-apple-system,"PingFang SC","Noto Sans SC",sans-serif; }
  #chart { width:100%; }
  .axis text { font-size:11px; fill:#5B6372; }
  .axis path, .axis line { stroke:#E3E6EB; }
  .tip { position:fixed; pointer-events:none; background:#14171F; color:#fff; font-size:12px; padding:4px 8px; border-radius:4px; display:none; }
</style>
</head>
<body>
<div id="chart"></div>
<div class="tip"></div>
<script>` + d3 + `</script>
<script>
const spec = ` + spec + `;
const width = 640, height = 240;
const margin = {top: 28, right: 16, bottom: 36, left: 48};
const innerW = width - margin.left - margin.right;
const innerH = height - margin.top - margin.bottom;
const svg = d3.select("#chart").append("svg").attr("viewBox", "0 0 " + width + " " + height).attr("width", "100%");
svg.append("text").attr("x", margin.left).attr("y", 16).attr("font-size", 13).attr("font-weight", 600).text(spec.title + (spec.unit ? "（" + spec.unit + "）" : ""));
const g = svg.append("g").attr("transform", "translate(" + margin.left + "," + margin.top + ")");
const tip = d3.select(".tip");
const values = spec.points.map(d => d.value);
const yMin = Math.min(0, d3.min(values));
const yMax = Math.max(0, d3.max(values));
const y = d3.scaleLinear().domain([yMin, yMax]).nice().range([innerH, 0]);
const color = v => v > 0 ? "#D9483F" : v < 0 ? "#2E8B57" : "#8A919E";
if (spec.type === "bar") {
  const x = d3.scaleBand().domain(spec.points.map(d => d.label)).range([0, innerW]).padding(0.28);
  g.append("g").attr("class", "axis").attr("transform", "translate(0," + y(0) + ")").call(d3.axisBottom(x));
  g.append("g").attr("class", "axis").call(d3.axisLeft(y).ticks(4));
  g.selectAll("rect").data(spec.points).join("rect")
    .attr("x", d => x(d.label)).attr("width", x.bandwidth())
    .attr("y", d => Math.min(y(d.value), y(0)))
    .attr("height", d => Math.abs(y(d.value) - y(0)))
    .attr("fill", d => color(d.value))
    .on("mousemove", (event, d) => tip.style("display", "block").style("left", (event.clientX + 8) + "px").style("top", (event.clientY + 8) + "px").text(d.label + " " + d.value))
    .on("mouseleave", () => tip.style("display", "none"));
} else {
  const x = d3.scalePoint().domain(spec.points.map(d => d.label)).range([0, innerW]).padding(0.4);
  g.append("g").attr("class", "axis").attr("transform", "translate(0," + innerH + ")").call(d3.axisBottom(x));
  g.append("g").attr("class", "axis").call(d3.axisLeft(y).ticks(4));
  const line = d3.line().x(d => x(d.label)).y(d => y(d.value)).curve(d3.curveMonotoneX);
  g.append("path").datum(spec.points).attr("fill", "none").attr("stroke", "#2457D6").attr("stroke-width", 2).attr("d", line);
  g.selectAll("circle").data(spec.points).join("circle")
    .attr("cx", d => x(d.label)).attr("cy", d => y(d.value)).attr("r", 3.5).attr("fill", "#2457D6")
    .on("mousemove", (event, d) => tip.style("display", "block").style("left", (event.clientX + 8) + "px").style("top", (event.clientY + 8) + "px").text(d.label + " " + d.value))
    .on("mouseleave", () => tip.style("display", "none"));
}
</script>
</body>
</html>`
}

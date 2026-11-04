package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-harness/core/session"
	"go-harness/internal/appconfig"
	"go-harness/skill/skill"
)

func TestParseSpeakAcceptsFencedJSON(t *testing.T) {
	speak, err := parseSpeak("结论如下：\n```json\n{\"text\":\"近5日大单净流入 1.2 亿元\",\"status\":\"support\",\"status_text\":\"偏多\",\"fact_ids\":[\"fr.big_net_5d\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if speak.Text != "近5日大单净流入 1.2 亿元" || speak.Status != "support" || len(speak.FactIDs) != 1 {
		t.Fatalf("speak = %+v", speak)
	}
}

func TestHeadlineRejectsUnknownFactID(t *testing.T) {
	catalog := map[string]factRow{"fr.big_net_5d": {ID: "fr.big_net_5d", Label: "近5日大单", Text: "净流入 1.2 亿元"}}
	speak := analystSpeak{Text: "近5日净流入 9 亿元", Status: "support", FactIDs: []string{"fr.missing"}}
	if _, ok := headlineFromSpeak(speak, catalog, agentConfig{FactPrefix: "fr."}); ok {
		t.Fatal("unknown fact id was accepted")
	}
}

func TestHeadlineUsesCatalogText(t *testing.T) {
	catalog := map[string]factRow{"fr.big_net_5d": {ID: "fr.big_net_5d", Label: "近5日大单", Text: "净流入 1.2 亿元"}}
	speak := analystSpeak{Text: "近5日大单净流入 1.2 亿元", Status: "warn", FactIDs: []string{"fr.big_net_5d"}}
	headline, ok := headlineFromSpeak(speak, catalog, agentConfig{FactPrefix: "fr."})
	if !ok {
		t.Fatal("expected a headline")
	}
	if headline["status_text"] != "偏空" {
		t.Fatalf("status_text = %v", headline["status_text"])
	}
	facts := headline["facts"].([]map[string]string)
	if facts[0]["text"] != "净流入 1.2 亿元" {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestEmptyCatalogOnlyAllowsMissingWording(t *testing.T) {
	speak := analystSpeak{Text: "这次没有取到资金流事实", Status: "neutral"}
	if _, ok := headlineFromSpeak(speak, map[string]factRow{}, agentConfig{}); !ok {
		t.Fatal("missing-data sentence was rejected")
	}
	speak.Text = "近5日净流入 1 亿元"
	if _, ok := headlineFromSpeak(speak, map[string]factRow{}, agentConfig{}); ok {
		t.Fatal("a number without facts was accepted")
	}
}

func TestFactsFromToolResult(t *testing.T) {
	raw := `{"source":"detail","facts":[{"id":"fr.ret_5d","label":"5日涨跌","text":"+1.2%"}]}`
	msgs := []session.Message{{Role: session.RoleUser, Content: []session.ContentBlock{{
		Kind: session.BlockToolResult, Text: raw,
	}}}}
	got := factsFromMessages(msgs)
	if got["fr.ret_5d"].Text != "+1.2%" {
		t.Fatalf("catalog = %+v", got)
	}
}

func TestGroupLogStoresSpeechOnly(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: appconfig.Config{DataDir: dir}, groups: map[string][]groupLine{}}
	s.appendGroup("600519.SH", groupLine{Agent: "volume_flow", Name: "资金流分析师", Kind: "analysis", Text: "近5日大单净流入 1.2 亿元", Status: "support", StatusText: "偏多"})
	raw, err := os.ReadFile(filepath.Join(dir, "groups", "600519.SH.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["facts"]; ok {
		t.Fatal("group file stored a fact sheet")
	}
	lines := s.groupLines("600519.SH")
	if len(lines) != 1 || !strings.Contains(lines[0].Text, "1.2") {
		t.Fatalf("lines = %+v", lines)
	}
	prompt := analystUserPrompt(agentConfig{Opening: "请给出这只股票的资金流结论。"}, "600519.SH", "", lines, nil)
	if strings.Contains(prompt, `"facts"`) || strings.Contains(prompt, "fr.big_net_5d") {
		t.Fatalf("prompt leaked facts: %s", prompt)
	}
	if !strings.Contains(prompt, "资金流分析师") {
		t.Fatalf("prompt = %s", prompt)
	}
}

func TestLatestGroupViewKeepsTheLastRoundAndLaterQuestions(t *testing.T) {
	lines := []groupLine{
		{Agent: "volume_flow", Kind: "analysis", Text: "旧资金"},
		{Agent: "chief", Kind: "analysis", Text: "旧裁决"},
		{Agent: "volume_flow", Kind: "analysis", Text: "第一遍"},
		{Agent: "volume_flow", Kind: "analysis", Text: "最新资金"},
		{Agent: "news", Kind: "reply", Quote: "中途追问", Text: "先别算进这轮"},
		{Agent: "bull", Kind: "analysis", Text: "看多"},
		{Agent: "chief", Kind: "analysis", Text: "最新裁决"},
		{Agent: "news", Kind: "reply", Quote: "机器人", Text: "没有硬事件"},
		{Agent: "volume_flow", Kind: "analysis", Text: "没裁决完的新开场"},
	}
	got := latestGroupView(lines)
	if len(got) != 4 || got[0].Text != "最新资金" || got[1].Text != "看多" || got[2].Text != "最新裁决" || got[3].Quote != "机器人" {
		t.Fatalf("view = %+v", got)
	}
	if latestGroupView(nil) != nil {
		t.Fatal("empty log should not look like a record")
	}
}

func TestSkillAllowListHidesOtherSkills(t *testing.T) {
	reg := skill.NewRegistry()
	for _, name := range []string{"stock-facts", "stock-basic"} {
		if _, err := reg.Register(skill.Registration{Name: name, Description: name, Content: name}); err != nil {
			t.Fatal(err)
		}
	}
	tool := listSkillsTool(reg, []string{"stock-facts"})
	value, err := tool.Execute(t.Context(), map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := value.(string)
	if !strings.Contains(text, "stock-facts") || strings.Contains(text, "stock-basic") {
		t.Fatalf("list = %s", text)
	}
	load := loadSkillTool(reg, []string{"stock-facts"})
	if _, err := load.Execute(t.Context(), map[string]any{"name": "stock-basic"}, nil); err == nil {
		t.Fatal("stock-basic should not load on the flow agent")
	}
}

func TestFactsEnvDropsTushareAndPinsDimension(t *testing.T) {
	t.Setenv("TUSHARE_TOKEN", "secret")
	t.Setenv("STOCK_FACTS_DIMENSION", "news")
	t.Setenv("STOCK_API_BASE", "http://127.0.0.1:8090")
	env := factsEnv(agentConfig{Dimension: "volume_flow", DetailQuery: map[string]string{"window": "5"}})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "TUSHARE_TOKEN") || strings.Contains(joined, "secret") {
		t.Fatal("tushare token leaked into the fact script environment")
	}
	if !strings.Contains(joined, "STOCK_FACTS_DIMENSION=volume_flow") || !strings.Contains(joined, "STOCK_FACTS_QUERY=window=5") {
		t.Fatalf("env = %s", joined)
	}
	if strings.Contains(joined, "STOCK_FACTS_DIMENSION=news") {
		t.Fatal("caller dimension overrode the agent binding")
	}
}

func TestForbidAndFactPrefix(t *testing.T) {
	catalog := map[string]factRow{
		"fr.big_net_5d": {ID: "fr.big_net_5d", Label: "近5日大单", Text: "净流入 1.2 亿元"},
		"fd.value":      {ID: "fd.value", Label: "估值", Text: "10 倍"},
	}
	flow := agentConfig{FactPrefix: "fr.", Forbid: []string{"主力"}}
	if _, ok := headlineFromSpeak(analystSpeak{Text: "主力在净流入", Status: "support", FactIDs: []string{"fr.big_net_5d"}}, catalog, flow); ok {
		t.Fatal("forbidden wording was accepted")
	}
	if _, ok := headlineFromSpeak(analystSpeak{Text: "估值 10 倍", Status: "support", FactIDs: []string{"fd.value"}}, catalog, flow); ok {
		t.Fatal("another dimension's fact id was accepted")
	}
}

func TestAgentConfigsKeepTheirOwnSkills(t *testing.T) {
	want := map[string]string{
		"volume_flow": "flow-facts",
		"news":        "news-facts",
		"fundamental": "fundamental-facts",
		"finance":     "finance-facts",
		"sector":      "sector-facts",
	}
	seen := map[string]bool{}
	for id, skillName := range want {
		cfg, err := loadAgent(id)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Skills[0] != skillName || !containsSkill(cfg.Skills, "answer-outline") || !containsSkill(cfg.Skills, "answer-table") || !containsSkill(cfg.Skills, "d3js-visualization") || !containsSkill(cfg.Skills, "stock-basic") {
			t.Fatalf("%s skills = %v", id, cfg.Skills)
		}
		if id == "news" && !containsSkill(cfg.Skills, "news-raw") {
			t.Fatalf("news skills = %v", cfg.Skills)
		}
		if !strings.Contains(cfg.systemPrompt(), skillName) || cfg.Prompt == "" || cfg.Rules == "" {
			t.Fatalf("%s prompt/rules missing", id)
		}
		if seen[skillName] {
			t.Fatalf("skill %s reused", skillName)
		}
		seen[skillName] = true
		for _, tool := range cfg.Tools {
			if tool != "fetch_facts" {
				t.Fatalf("%s tool = %s", id, tool)
			}
		}
	}
	flow, err := loadAgent("volume_flow")
	if err != nil {
		t.Fatal(err)
	}
	news, err := loadAgent("news")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Prompt == news.Prompt || flow.Rules == news.Rules || flow.Skills[0] == news.Skills[0] {
		t.Fatal("agents share prompt, rules, or skills")
	}
	if news.DetailQuery["days"] != "3" || flow.DetailQuery["window"] != "5" {
		t.Fatalf("queries flow=%v news=%v", flow.DetailQuery, news.DetailQuery)
	}
}

func TestParseChiefPlanKeepsOnlyDimensionAnalysts(t *testing.T) {
	ids, note := parseChiefPlan("```json\n{\"analysts\":[\"volume_flow\",\"sector\",\"bull\",\"volume_flow\",\"news\",\"finance\"],\"note\":\"请资金流分析师、板块分析师来回答\"}\n```")
	if strings.Join(ids, ",") != "volume_flow,sector,news" {
		t.Fatalf("ids = %#v", ids)
	}
	if note != "请资金流分析师、板块分析师来回答" {
		t.Fatalf("note = %q", note)
	}
	empty, self := parseChiefPlan("不是 json")
	if len(empty) != 0 || self != "这个问题我来回答" {
		t.Fatalf("fallback = %#v %q", empty, self)
	}
}

func TestVisibleDraftShowsTheTextFieldAsItArrives(t *testing.T) {
	if got := visibleDraft(`{"text":"汽车板块处在`); got != "汽车板块处在" {
		t.Fatalf("partial = %q", got)
	}
	if got := visibleDraft("先看近 5 日，资金"); got != "先看近 5 日，资金" {
		t.Fatalf("markdown = %q", got)
	}
	if visibleDraft(`{"status":"neutral"`) != "" {
		t.Fatal("json without text should stay blank")
	}
}

func TestDimensionReplyFollowsTheQuestion(t *testing.T) {
	cfg, err := loadAgent("news")
	if err != nil {
		t.Fatal(err)
	}
	user := analystUserPrompt(cfg, "603986.SH", "查看下最新的和内存相关的新闻", nil, nil)
	if !strings.Contains(user, "fetch_news") || !strings.Contains(user, "分析时间") || strings.Contains(user, "问题涉及主题") {
		t.Fatalf("reply user prompt = %s", user)
	}
	if !strings.Contains(user, "查看下最新的和内存相关的新闻") {
		t.Fatalf("question missing: %s", user)
	}
	reply := cfg.replyPrompt()
	if strings.Contains(reply, "fact_ids") || strings.Contains(reply, "立刻结束") || !strings.Contains(reply, "fetch_news") || strings.Contains(reply, "先 load_skill 事实技能") {
		t.Fatalf("reply system prompt = %s", reply)
	}
	if !strings.Contains(cfg.systemPrompt(), "fact_ids") || !strings.Contains(cfg.systemPrompt(), "立刻结束") {
		t.Fatal("opening prompt lost the fact contract")
	}
	got := replyAnswer("近几日没有标题写内存的新闻。\n\n| 时间 | 标题 |\n| --- | --- |\n| 11-02 | 主力资金监控 |")
	if !strings.Contains(got, "内存") || !strings.Contains(got, "主力资金监控") {
		t.Fatalf("reply answer = %s", got)
	}
	bull, err := loadAgent("bull")
	if err != nil {
		t.Fatal(err)
	}
	ask := analystUserPrompt(bull, "603738.SH", "你觉得跌太多了，不是应该马上反转了吗？", nil, nil)
	if strings.Contains(ask, "唯一材料") || !strings.Contains(ask, "跌太多了") {
		t.Fatalf("bull reply user prompt = %s", ask)
	}
	side := bull.oneShotReplyPrompt()
	if strings.Contains(side, "points") || strings.Contains(side, "只从群里") || !strings.Contains(side, "fetch_basic") {
		t.Fatalf("bull reply prompt = %s", side)
	}
	flow, err := loadAgent("volume_flow")
	if err != nil {
		t.Fatal(err)
	}
	open := analystUserPrompt(flow, "603738.SH", "", nil, nil)
	if !strings.Contains(open, "背离") || !strings.Contains(open, "单日") {
		t.Fatalf("flow opening = %s", open)
	}
	askFlow := analystUserPrompt(flow, "603738.SH", "分析下资金流", nil, nil)
	if !strings.Contains(askFlow, "flow-reply") || !strings.Contains(flow.replyPrompt(), "fetch_flow_dc") || !strings.Contains(flow.replyPrompt(), "fetch_flow_margin") {
		t.Fatalf("flow reply = %s", flow.replyPrompt())
	}
	if !strings.Contains(flow.systemPrompt(), "资金在流入") || strings.Contains(flow.Rules, "主力进场") {
		t.Fatal("flow rules missing the desk checks")
	}
	sector, err := loadAgent("sector")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sector.systemPrompt(), "申万行业") || !strings.Contains(sector.Rules, "不要估算市占率") {
		t.Fatal("sector prompt missing the industry frame")
	}
	askSector := analystUserPrompt(sector, "601689.SH", "这个板块今天是不是主线", nil, nil)
	if !strings.Contains(askSector, "sector-reply") || strings.Contains(askSector, "整份 facts") == false {
		t.Fatalf("sector reply user = %s", askSector)
	}
	if !strings.Contains(sector.replyPrompt(), "fetch_theme_heat") || !strings.Contains(sector.replyPrompt(), "fetch_sector_rank") || !strings.Contains(sector.replyPrompt(), "fetch_sector_sw") || !strings.Contains(sector.replyPrompt(), "fetch_sector_dc") || strings.Contains(sector.replyPrompt(), "先 load_skill 事实技能") {
		t.Fatal("sector reply prompt missing the five tools")
	}
	fund, err := loadAgent("fundamental")
	if err != nil {
		t.Fatal(err)
	}
	if !containsSkill(fund.Skills, "fundamental-reply") {
		t.Fatalf("fundamental skills = %v", fund.Skills)
	}
	askFund := analystUserPrompt(fund, "601689.SH", "业务构成是什么", nil, nil)
	if strings.Contains(askFund, "整份 facts") == false || !strings.Contains(askFund, "fetch_business") || !strings.Contains(askFund, "fetch_share_float") {
		t.Fatalf("fundamental reply = %s", askFund)
	}
	if strings.Contains(fund.replyPrompt(), "先 load_skill 事实技能") || !strings.Contains(fund.replyPrompt(), "fetch_forecast") || !strings.Contains(fund.replyPrompt(), "fetch_dividend") {
		t.Fatal("fundamental reply still starts from the fact sheet")
	}
}

func TestChartHTMLUsesBarOrLineOnly(t *testing.T) {
	page, err := buildChartHTML("bar", "近5日大单", "亿元", `[{"label":"10-28","value":1.2},{"label":"10-29","value":-0.4}]`, "/*d3*/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "近5日大单") || !strings.Contains(page, "scaleBand") || !strings.Contains(page, "/*d3*/") {
		t.Fatal("bar page missing chart pieces")
	}
	if _, err := buildChartHTML("pie", "构成", "", `[{"label":"a","value":1},{"label":"b","value":2}]`, ""); err == nil {
		t.Fatal("pie chart was accepted")
	}
}

func TestDebateAgentsDoNotFetchFacts(t *testing.T) {
	for _, id := range []string{"bull", "bear", "chief"} {
		cfg, err := loadAgent(id)
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.oneShot() || len(cfg.Tools) != 0 {
			t.Fatalf("%s tools = %v speak = %s", id, cfg.Tools, cfg.Speak)
		}
		if !containsSkill(cfg.Skills, "stock-basic") {
			t.Fatalf("%s missing stock-basic", id)
		}
		if strings.Contains(cfg.systemPrompt(), "fetch_facts") {
			t.Fatalf("%s prompt still asks to fetch facts", id)
		}
	}
}

func TestPointsMustCiteSomeoneWhoSpoke(t *testing.T) {
	raw := `{"points":[{"text":"近5日大单净流入 1.2 亿元","dimension":"volume_flow","weight":2},{"text":"新闻利空","dimension":"news","weight":1}]}`
	points, err := parsePoints(raw, "bull", map[string]bool{"volume_flow": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].ID != "bull.1" || points[0].Dimension != "volume_flow" {
		t.Fatalf("points = %+v", points)
	}
}

func TestChiefScoreUsesAdoptedWeights(t *testing.T) {
	file := &debateFile{
		Bull: []debatePoint{{ID: "bull.1", Text: "资金流入", Dimension: "volume_flow", Weight: 2}},
		Bear: []debatePoint{{ID: "bear.1", Text: "估值偏高", Dimension: "fundamental", Weight: 1}},
	}
	decision, err := parseChief(`{"verdict":"偏多","text":"资金流入盖过估值","adopted":["bull.1"],"rejected":[{"id":"bear.1","reason":"证据不足"}]}`, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, decision, file)
	score := msg["score"].(map[string]any)
	if score["bull"] != 2 || score["bear"] != 0 || score["text"] != "2:0，分差 2" {
		t.Fatalf("score = %+v", score)
	}
	if msg["adopted"] != "看多①" || msg["rejected"] != "看空①（证据不足）" {
		t.Fatalf("lines adopted=%v rejected=%v", msg["adopted"], msg["rejected"])
	}
	annotated := pointPayload(file.Bear, decision)
	verdict := annotated[0]["verdict"].(map[string]any)
	if verdict["adopt"] != false {
		t.Fatalf("verdict = %+v", verdict)
	}
}

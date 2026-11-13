package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	broken := `{"stance":"偏多","strength":"弱","text":"媒体称"算力金属"业绩大涨","fact_ids":["e1"],"suggested_questions":[]}`
	fixed, err := parseSpeak(broken)
	if err != nil || fixed.Text != `媒体称"算力金属"业绩大涨` || fixed.Status != "support" {
		t.Fatalf("should repair bare quotes in text, got %+v err=%v", fixed, err)
	}
	if speak.Text != "近5日大单净流入 1.2 亿元" || speak.Status != "support" || len(speak.FactIDs) != 1 {
		t.Fatalf("speak = %+v", speak)
	}
	// 顶层 text 完好时，不得因 suggested_questions[].text 再修一遍而弄破 JSON
	nested := `{"stance":"偏多","strength":"弱","text":"近3日有公告利好，尚未明显消化。","fact_ids":["e1"],"suggested_questions":[{"text":"后续有无补充公告","tool":"fetch_news","focus":"进展"}]}`
	okNested, err := parseSpeak(nested)
	if err != nil || okNested.Text != "近3日有公告利好，尚未明显消化。" || len(okNested.SuggestedQuestions) != 1 {
		t.Fatalf("nested suggested_questions text must stay intact, got %+v err=%v", okNested, err)
	}
	// 顶层 text 有裸引号 + 后面还有 SQ.text，只应修好顶层
	both := `{"stance":"中性","strength":"弱","text":"热搜称"某某概念"升温","fact_ids":["e2"],"suggested_questions":[{"text":"热搜后续","tool":"fetch_baidu_board","focus":"情绪"}]}`
	okBoth, err := parseSpeak(both)
	if err != nil || okBoth.Text != `热搜称"某某概念"升温` || len(okBoth.SuggestedQuestions) != 1 {
		t.Fatalf("repair top-level text without breaking SQ, got %+v err=%v", okBoth, err)
	}
	// SQ.text 本身带裸引号时，旧逻辑不解；必须整段修好
	sqBroken := `{"stance":"偏多","strength":"弱","text":"近3日回购进展，尚未明显消化。","fact_ids":["e1"],"suggested_questions":[{"text":"后续有无"补充公告"","tool":"fetch_news","focus":"进展"}]}`
	okSQ, err := parseSpeak(sqBroken)
	if err != nil || okSQ.Text != "近3日回购进展，尚未明显消化。" || len(okSQ.SuggestedQuestions) != 1 {
		t.Fatalf("repair suggested_questions text quotes, got %+v err=%v", okSQ, err)
	}
	if got, _ := okSQ.SuggestedQuestions[0]["text"].(string); got != `后续有无"补充公告"` {
		t.Fatalf("SQ text = %q", got)
	}
	// 弯引号当分隔符
	curly := "{“stance”:“偏空”,“strength”:“弱”,“text”:“单季扣非转负。”,“fact_ids”:[“ff.growth.q_ded_yoy”],“suggested_questions”:[]}"
	okCurly, err := parseSpeak(curly)
	if err != nil || okCurly.Status != "warn" || okCurly.Text != "单季扣非转负。" {
		t.Fatalf("curly quotes speak = %+v err=%v", okCurly, err)
	}
}

func TestOpeningSkillsNewsTrimsReplyOnly(t *testing.T) {
	cfg := agentConfig{ID: "news", Skills: []string{"news-facts", "news-raw", "baidu-board", "stock-basic", "d3js-visualization"}}
	got := cfg.openingSkills()
	if strings.Join(got, ",") != "news-facts,stock-basic" {
		t.Fatalf("openingSkills = %v", got)
	}
	fund := agentConfig{ID: "fundamental", Skills: []string{"fundamental-facts", "fundamental-reply", "stock-basic", "d3js-visualization"}}
	if got := fund.openingSkills(); strings.Join(got, ",") != "fundamental-facts,stock-basic" {
		t.Fatalf("fundamental openingSkills = %v", got)
	}
	fin := agentConfig{ID: "finance", Skills: []string{"finance-facts", "answer-outline", "d3js-visualization", "stock-basic"}}
	if got := fin.openingSkills(); strings.Join(got, ",") != "finance-facts,stock-basic" {
		t.Fatalf("finance openingSkills = %v", got)
	}
	mkt := agentConfig{ID: "market", Skills: []string{"market-facts", "market-reply", "d3js-visualization", "stock-basic"}}
	if got := mkt.openingSkills(); strings.Join(got, ",") != "market-facts,stock-basic" {
		t.Fatalf("market openingSkills = %v", got)
	}
	flow := agentConfig{ID: "volume_flow", Skills: []string{"flow-facts", "d3js-visualization"}}
	if got := flow.openingSkills(); strings.Join(got, ",") != strings.Join(flow.Skills, ",") {
		t.Fatalf("non-news openingSkills should be unchanged, got %v", got)
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
		"market":      "market-facts",
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
	if news.DetailQuery["days"] != "3" || flow.DetailQuery["window"] != "3" {
		t.Fatalf("queries flow=%v news=%v", flow.DetailQuery, news.DetailQuery)
	}
}

func TestParseChiefPlanKeepsOnlyDimensionAnalysts(t *testing.T) {
	ids, note := parseChiefPlan("```json\n{\"analysts\":[\"volume_flow\",\"sector\",\"bull\",\"volume_flow\",\"news\",\"finance\"],\"note\":\"该资金流分析师、板块分析师开麦了\"}\n```")
	if strings.Join(ids, ",") != "volume_flow,sector,news" {
		t.Fatalf("ids = %#v", ids)
	}
	if note != "该资金流分析师、板块分析师开麦了" {
		t.Fatalf("note = %q", note)
	}
	empty, self := parseChiefPlan("不是 json")
	if len(empty) != 0 || self != "" {
		t.Fatalf("fallback = %#v %q", empty, self)
	}
}

func TestApplyStickyPlanKeepsPreviousUnlessClearSwitch(t *testing.T) {
	prev := []string{"volume_flow"}
	got := applyStickyPlan(prev, []string{"news"}, "那单日流出有多异常")
	if strings.Join(got, ",") != "volume_flow" {
		t.Fatalf("follow-up should stick, got %#v", got)
	}
	got = applyStickyPlan(prev, []string{"news"}, "公司刚发的公告怎么看")
	if strings.Join(got, ",") != "news" {
		t.Fatalf("clear news switch = %#v", got)
	}
	got = applyStickyPlan(prev, nil, "再展开说说")
	if strings.Join(got, ",") != "volume_flow" {
		t.Fatalf("empty plan should stick, got %#v", got)
	}
	got = applyStickyPlan(nil, []string{"sector"}, "这板块怎么样")
	if strings.Join(got, ",") != "sector" {
		t.Fatalf("no previous keeps plan, got %#v", got)
	}
	got = applyStickyPlan(prev, []string{"market"}, "市场资金怎么走")
	if strings.Join(got, ",") != "market" {
		t.Fatalf("市场资金 should switch to market not stick volume_flow, got %#v", got)
	}
	got = applyStickyPlan([]string{"sector"}, []string{"market"}, "市场板块谁在轮动")
	if strings.Join(got, ",") != "market" {
		t.Fatalf("市场板块 should prefer market over sector, got %#v", got)
	}
}

func TestApplyStickyPlanTrustsTheModelWhenNothingContradictsIt(t *testing.T) {
	prev := []string{"volume_flow"}
	join := func(ids []string) string { return strings.Join(ids, ",") }
	// 没有任何关键词：模型换了人就听模型的，不再一直停在资金流
	if got := applyStickyPlan(prev, []string{"fundamental"}, "它的竞争对手有谁"); join(got) != "fundamental" {
		t.Fatalf("model switch without contradicting words = %#v", got)
	}
	// 模型想加人：保留上一轮、再加一位
	if got := applyStickyPlan(prev, []string{"volume_flow", "news"}, "资金和消息面一起看看"); join(got) != "volume_flow,news" {
		t.Fatalf("model adds = %#v", got)
	}
	// 题目里的词指向上一轮、没有词指向模型选的人：拦下来
	if got := applyStickyPlan(prev, []string{"sector"}, "大单净流入多少"); join(got) != "volume_flow" {
		t.Fatalf("words point at previous = %#v", got)
	}
	// 题目里的词指向模型选的人：放行
	if got := applyStickyPlan(prev, []string{"sector"}, "大单之外这个行业怎么样"); join(got) != "sector" {
		t.Fatalf("words point at the model's pick = %#v", got)
	}
	// 模型没选出人（解析失败、空数组）：沿用上一轮
	if got := applyStickyPlan(prev, nil, "那它呢"); join(got) != "volume_flow" {
		t.Fatalf("empty plan = %#v", got)
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
	if !strings.Contains(user, "查看下最新的和内存相关的新闻") || !strings.Contains(user, "追问任务") || !strings.Contains(user, "当前时间：") {
		t.Fatalf("reply user prompt = %s", user)
	}
	reply := cfg.replyPrompt()
	sys := cfg.systemPrompt()
	if !strings.Contains(sys, "fact_ids") || !strings.Contains(sys, "立刻结束") || !strings.Contains(sys, "stance") || !strings.Contains(sys, "suggested_questions") {
		t.Fatal("news config prompt lost output/tool contract")
	}
	if !strings.Contains(sys, "48 小时～3 日") || !strings.Contains(sys, "news-facts") {
		t.Fatal("news prompt should clarify mid bucket and opening tool")
	}
	if !strings.Contains(sys, "事件去重") || !strings.Contains(sys, "消化判断") || !strings.Contains(sys, "证据不足") {
		t.Fatal("news prompt should include the three hard rules")
	}
	if !strings.Contains(sys, "有条件生成") || !strings.Contains(sys, "不代表上涨空间") {
		t.Fatal("news prompt should constrain SQ and strength meaning")
	}
	if !strings.Contains(sys, "抖音热榜") || !strings.Contains(sys, "百度热搜") || !strings.Contains(sys, "情绪") {
		t.Fatal("news prompt should prioritize douyin/baidu hot lists as sentiment")
	}
	if strings.Contains(sys, "先 load_skill 事实技能") {
		t.Fatal("harness must not inject legacy skill boilerplate")
	}
	if strings.TrimSpace(cfg.Reply) == "" || !strings.Contains(reply, "fetch_news") || !strings.Contains(reply, "可以接着问") {
		t.Fatal("news reply.md should list fetch_news and 可以接着问")
	}
	if strings.Contains(reply, "## 开场输出") || strings.Contains(reply, "开场工具") {
		t.Fatal("news reply.md must not drag opening sections")
	}
	if cfg.MaxSteps < 5 || cfg.MaxSteps > 10 {
		t.Fatalf("news max_steps should stay lean but allow fetch+json, got %d", cfg.MaxSteps)
	}
	got, err := replyAnswer("近几日没有标题写内存的新闻。\n\n| 时间 | 标题 |\n| --- | --- |\n| 11-02 | 主力资金监控 |")
	if err != nil || !strings.Contains(got, "内存") || !strings.Contains(got, "主力资金监控") {
		t.Fatalf("reply answer = %s err=%v", got, err)
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
	if !strings.Contains(side, "stock-basic") || !strings.Contains(side, "fetch_basic") || strings.Contains(side, "只从群里") {
		t.Fatalf("bull config prompt = %s", side)
	}
	if strings.TrimSpace(bull.Reply) == "" || strings.Contains(bull.replyPrompt(), "## 开场输出") {
		t.Fatal("bull reply.md should exist and not drag opening sections")
	}
	bullSys := bull.systemPrompt()
	if !strings.Contains(bullSys, `"points"`) || !strings.Contains(bullSys, "weight=1") || !strings.Contains(bullSys, "长文论据模板") {
		t.Fatal("bull opening should require points JSON with weight demotion, not legacy long-form")
	}
	bear, err := loadAgent("bear")
	if err != nil {
		t.Fatal(err)
	}
	bearSys := bear.systemPrompt()
	if !strings.Contains(bearSys, `"points"`) || !strings.Contains(bearSys, "weight=1") || !strings.Contains(bearSys, "长文论据模板") {
		t.Fatal("bear opening should require points JSON with weight demotion, not legacy long-form")
	}
	if strings.TrimSpace(bear.Reply) == "" || !strings.Contains(bear.replyPrompt(), "fetch_basic") || strings.Contains(bear.replyPrompt(), "## 开场输出") {
		t.Fatal("bear reply.md should exist and mention fetch_basic without opening sections")
	}
	flow, err := loadAgent("volume_flow")
	if err != nil {
		t.Fatal(err)
	}
	open := analystUserPrompt(flow, "603738.SH", "", nil, nil)
	if !strings.Contains(open, "开场任务") || !strings.Contains(open, flow.Opening) {
		t.Fatalf("flow opening = %s", open)
	}
	askFlow := analystUserPrompt(flow, "603738.SH", "分析下资金流", nil, nil)
	if !strings.Contains(askFlow, "分析下资金流") || !strings.Contains(askFlow, "追问任务") {
		t.Fatalf("flow reply user = %s", askFlow)
	}
	flowReply := flow.replyPrompt()
	if strings.TrimSpace(flow.Reply) == "" {
		t.Fatal("volume_flow must have reply.md")
	}
	if strings.Contains(flowReply, "## 一、你是谁") || !strings.Contains(flowReply, strings.TrimSpace(flow.Reply)) {
		t.Fatal("volume_flow reply must use reply.md, not full prompt.md")
	}
	if !strings.Contains(flowReply, "fetch_flow_dc") || !strings.Contains(flowReply, "禁止输出开场 JSON") {
		t.Fatal("flow reply.md should list tools and forbid opening JSON")
	}
	if !strings.Contains(flow.systemPrompt(), "stance") || !strings.Contains(flow.systemPrompt(), `"text"`) {
		t.Fatal("flow opening contract missing from config")
	}
	if !strings.Contains(flow.systemPrompt(), "80～120") || strings.Contains(flow.systemPrompt(), "开场只给结论，不展开") {
		t.Fatal("flow opening should analyze fully then compress to ~80–120字 text")
	}
	if !strings.Contains(flow.systemPrompt(), "0～2") {
		t.Fatal("flow opening tools should allow limited gap fill like sector")
	}
	if _, err := replyAnswer(`{"stance":"中性","strength":"弱","text":"资金面无明显信号"}`); !errors.Is(err, errOpeningJSONOnReply) {
		t.Fatalf("opening JSON on reply should be rejected, err=%v", err)
	}
	if strings.Contains(flow.Rules, "主力进场") {
		t.Fatal("flow rules must not revive banned-phrase desk copy")
	}
	legacy := compactFlowReply("前言\n\n## 资金流分析：测试\n### 一、价格与成交底色\n很长\n### 八、结论\n尾巴")
	if strings.Contains(legacy, "### 一、") || !strings.Contains(legacy, "前言") {
		t.Fatalf("compactFlowReply should strip nine-step body: %q", legacy)
	}
	polluted := analystUserPrompt(flow, "603738.SH", "还在流出吗", []groupLine{{
		Agent: "volume_flow", Name: "资金流分析师", Kind: "reply", Quote: "分析下",
		Text: "## 资金流分析\n### 一、价格与成交底色\n旧长文范例不应原样进追问上下文\n**结论**：其实有结论",
	}}, nil)
	if strings.Contains(polluted, "旧长文范例") || strings.Contains(polluted, "### 一、价格") {
		t.Fatalf("reply context must sanitize prior long flow text: %s", polluted)
	}
	sector, err := loadAgent("sector")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sector.systemPrompt(), "申万行业") || !strings.Contains(sector.Rules, "不要估算市占率") {
		t.Fatal("sector prompt missing the industry frame")
	}
	askSector := analystUserPrompt(sector, "601689.SH", "这个板块今天是不是主线", nil, nil)
	if !strings.Contains(askSector, "这个板块今天是不是主线") || !strings.Contains(askSector, "追问任务") {
		t.Fatalf("sector reply user = %s", askSector)
	}
	if !strings.Contains(sector.replyPrompt(), "fetch_theme_heat") || !strings.Contains(sector.replyPrompt(), "fetch_sector_dc") || strings.Contains(sector.replyPrompt(), "先 load_skill 事实技能") {
		t.Fatal("sector config prompt missing tools")
	}
	if !strings.Contains(sector.systemPrompt(), "stance") || !strings.Contains(sector.systemPrompt(), "strength") || !strings.Contains(sector.systemPrompt(), "suggested_questions") {
		t.Fatal("sector opening should use stance/strength/suggested_questions like volume_flow")
	}
	if !strings.Contains(sector.replyPrompt(), "可以接着问") {
		t.Fatal("sector reply should end with 可以接着问 like volume_flow")
	}
	fund, err := loadAgent("fundamental")
	if err != nil {
		t.Fatal(err)
	}
	if !containsSkill(fund.Skills, "fundamental-reply") {
		t.Fatalf("fundamental skills = %v", fund.Skills)
	}
	if strings.TrimSpace(fund.Reply) == "" || !strings.Contains(fund.replyPrompt(), "fetch_business") || !strings.Contains(fund.replyPrompt(), "可以接着问") {
		t.Fatal("fundamental reply.md should list tools and 可以接着问")
	}
	if !strings.Contains(fund.systemPrompt(), "stance") || !strings.Contains(fund.systemPrompt(), "strength") || !strings.Contains(fund.systemPrompt(), "suggested_questions") {
		t.Fatal("fundamental opening should use stance/strength/suggested_questions")
	}
	if !strings.Contains(fund.Rules, "不要写 status / status_text") {
		t.Fatal("fundamental rules should ban status/status_text like news")
	}
	askFund := analystUserPrompt(fund, "601689.SH", "业务构成是什么", nil, nil)
	if !strings.Contains(askFund, "业务构成是什么") {
		t.Fatalf("fundamental reply = %s", askFund)
	}
	if strings.Contains(fund.replyPrompt(), "先 load_skill 事实技能") || !strings.Contains(fund.replyPrompt(), "fetch_chain") {
		t.Fatal("fundamental config prompt missing tools")
	}
	speakRaw := `{"stance":"偏多","strength":"弱","text":"主要靠白酒赚钱，估值五年分位不高，近窗无明显解禁。","fact_ids":["fd.biz_top"],"suggested_questions":[{"text":"业务构成怎么拆","tool":"fetch_business","focus":"产品线"}]}`
	fundSpeak, err := parseSpeak(speakRaw)
	if err != nil || fundSpeak.Status != "support" || fundSpeak.Strength != "弱" {
		t.Fatalf("fundamental stance speak = %+v err=%v", fundSpeak, err)
	}
	finance, err := loadAgent("finance")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(finance.Reply) == "" || !strings.Contains(finance.replyPrompt(), "fetch_facts") || !strings.Contains(finance.replyPrompt(), "可以接着问") {
		t.Fatal("finance reply.md should list fetch_facts and 可以接着问")
	}
	if !strings.Contains(finance.systemPrompt(), "stance") || !strings.Contains(finance.systemPrompt(), "strength") || !strings.Contains(finance.systemPrompt(), "suggested_questions") {
		t.Fatal("finance opening should use stance/strength/suggested_questions")
	}
	if !strings.Contains(finance.Rules, "不要写 status / status_text") {
		t.Fatal("finance rules should ban status/status_text like news")
	}
	finSpeak, err := parseSpeak(`{"stance":"偏空","strength":"弱","text":"单季扣非转负，经营现金流弱于利润。","fact_ids":["ff.growth.q_ded_yoy"],"suggested_questions":[{"text":"经营现金流和利润匹配吗","tool":"fetch_facts","focus":"现金流"}]}`)
	if err != nil || finSpeak.Status != "warn" || finSpeak.Strength != "弱" {
		t.Fatalf("finance stance speak = %+v err=%v", finSpeak, err)
	}
	market, err := loadAgent("market")
	if err != nil {
		t.Fatal(err)
	}
	if !containsSkill(market.Skills, "market-reply") || strings.TrimSpace(market.Reply) == "" {
		t.Fatalf("market skills/reply = %v", market.Skills)
	}
	if !strings.Contains(market.systemPrompt(), "stance") || !strings.Contains(market.replyPrompt(), "fetch_market_fund") || !strings.Contains(market.replyPrompt(), "可以接着问") {
		t.Fatal("market should use stance JSON opening and market-reply tools")
	}
	if !strings.Contains(market.Rules, "不要写 status / status_text") {
		t.Fatal("market rules should ban status/status_text")
	}
	mSpeak, err := parseSpeak(`{"stance":"偏多","strength":"弱","text":"市场总档偏顺风，钱在进。","fact_ids":["mk.conclusion.level"],"suggested_questions":[{"text":"市场资金怎么走","tool":"fetch_market_fund","focus":"成交与宽基"}]}`)
	if err != nil || mSpeak.Status != "support" {
		t.Fatalf("market stance speak = %+v err=%v", mSpeak, err)
	}
}

func TestVolumeFlowStanceSpeakNormalizes(t *testing.T) {
	raw := `{"stance":"偏空","strength":"弱","text":"资金流出集中在一天，力度常态内","fact_ids":["fr.path"],"suggested_questions":[{"text":"那天有多异常","tool":"fetch_flow_dc"}]}`
	speak, err := parseSpeak(raw)
	if err != nil {
		t.Fatal(err)
	}
	if speak.Status != "warn" || speak.Text != "资金流出集中在一天，力度常态内" || speak.Strength != "弱" {
		t.Fatalf("speak = %+v", speak)
	}
	catalog := map[string]factRow{"fr.path": {ID: "fr.path", Label: "路径", Text: "转出"}}
	out, ok := headlineFromSpeak(speak, catalog, agentConfig{ID: "volume_flow", FactPrefix: "fr."})
	if !ok {
		t.Fatal("headline rejected")
	}
	if out["text"] != speak.Text || out["status"] != "warn" || out["strength"] != "弱" {
		t.Fatalf("headline = %#v", out)
	}
	if qs, _ := out["suggested_questions"].([]map[string]any); len(qs) != 1 {
		t.Fatalf("suggested_questions = %#v", out["suggested_questions"])
	}
	// 旧字段 summary 仍兼容
	legacy, err := parseSpeak(`{"stance":"偏多","strength":"强","summary":"近3日净流入明显","fact_ids":["fr.path"]}`)
	if err != nil || legacy.Text != "近3日净流入明显" || legacy.Status != "support" {
		t.Fatalf("summary alias = %+v err=%v", legacy, err)
	}
	long := "主信号偏多但把握弱：近5日净流入明显。" + strings.Repeat("续", 100) + "大单-0.49亿元独撑而且没有句号"
	if n := len([]rune(long)); n <= 120 {
		t.Fatalf("fixture should exceed 120 runes, got %d", n)
	}
	longSpeak, err := parseSpeak(`{"stance":"中性","strength":"弱","text":"` + long + `","fact_ids":["fr.path"]}`)
	if err != nil {
		t.Fatal(err)
	}
	longOut, ok := headlineFromSpeak(longSpeak, catalog, agentConfig{ID: "volume_flow", FactPrefix: "fr."})
	if !ok {
		t.Fatal("long headline rejected")
	}
	got := fmt.Sprint(longOut["text"])
	if n := len([]rune(got)); n > 120 {
		t.Fatalf("volume_flow text soft cap should be ≤120 runes, got %d", n)
	}
	if got != "主信号偏多但把握弱：近5日净流入明显。" || strings.Contains(got, "大单-0.49") {
		t.Fatalf("should soft-trim at first sentence end, got %q", got)
	}
	secSpeak, err := parseSpeak(`{"stance":"中性","strength":"弱","text":"板块偏弱阶段不清，对个股暂不构成支撑。","fact_ids":["sp.p.name"],"suggested_questions":[{"text":"相对板块差在哪","tool":"fetch_sector_tape"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	secCat := map[string]factRow{"sp.p.name": {ID: "sp.p.name", Label: "主归属", Text: "汽车零部件"}}
	secOut, ok := headlineFromSpeak(secSpeak, secCat, agentConfig{ID: "sector", FactPrefix: "sp."})
	if !ok || secOut["status"] != "neutral" || secOut["strength"] != "弱" {
		t.Fatalf("sector stance headline = %#v ok=%v", secOut, ok)
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
		if id == "chief" && (!strings.Contains(cfg.systemPrompt(), "板块 vs 资金流") || !strings.Contains(cfg.Rules, "禁止把两维平均")) {
			t.Fatal("chief must include sector×volume_flow conflict rule")
		}
		if id == "chief" && (!strings.Contains(cfg.systemPrompt(), "新闻默认只解释短冲击") || !strings.Contains(cfg.Rules, "新闻权重")) {
			t.Fatal("chief must include news short-impact weight rule")
		}
		if id == "chief" && (!strings.Contains(cfg.systemPrompt(), "裁决一定要看大盘") || !strings.Contains(cfg.systemPrompt(), "直接决定大多数股票") || !strings.Contains(cfg.systemPrompt(), "投资计划必须先对齐大盘") || !strings.Contains(cfg.Rules, "裁决一定要看大盘") || !strings.Contains(cfg.Rules, "必须先对齐大盘总档")) {
			t.Fatal("chief must require market-led verdict and investment plan (0d)")
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

func TestReadQuestionKeepsLongQuestionsAndSaysSoWhenTooLong(t *testing.T) {
	post := func(question string) (*httptest.ResponseRecorder, *http.Request) {
		raw, _ := json.Marshal(map[string]string{"question": question})
		return httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(raw))
	}
	// 追问正文可能较长（多位分析师串联时尤甚）：1 万多个字节，原来 8KB 的上限会把它截断，读出来是空的，报「缺少 question」。
	long := strings.Repeat("资金流比自己平时偏流入。", 900)
	if len(long) <= 8192 {
		t.Fatalf("fixture must exceed the old 8KB limit, got %d", len(long))
	}
	w, r := post(long)
	got, err := readQuestion(w, r)
	if err != nil || got != long {
		t.Fatalf("long question lost: len=%d err=%v", len(got), err)
	}
	w, r = post(strings.Repeat("x", maxQuestionBytes+10))
	if _, err := readQuestion(w, r); !errors.Is(err, errQuestionTooLong) {
		t.Fatalf("oversized question should say so, got %v", err)
	}
	w, r = post("")
	if got, _ := readQuestion(w, r); got != "" {
		t.Fatalf("empty question = %q", got)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/x?q=%E8%B5%84%E9%87%91", nil)
	if got, _ := readQuestion(w, r); got != "资金" {
		t.Fatalf("q= fallback = %q", got)
	}
}

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	coretools "go-harness/core/tools"
)

var planAllowed = []string{"volume_flow", "market", "sector", "news", "finance", "fundamental"}

var planName = map[string]string{
	"volume_flow": "资金流分析师",
	"market":      "大盘分析师",
	"sector":      "板块分析师",
	"news":        "新闻分析师",
	"finance":     "财报分析师",
	"fundamental": "基本面分析师",
}

// 各维度的强特征词：只有明显偏向「上一轮以外」的维度时才换人。
// 更长的词权重大（见 exclusiveOtherDomain），避免「市场资金」被「资金」抢走。
var planDomainHints = map[string][]string{
	"volume_flow": {"资金流", "融资", "龙虎榜", "筹码", "均线", "macd", "大单", "超大单", "净流入", "净流出", "流入", "流出", "path", "level"},
	"market":      {"大盘", "市场结论", "市场资金", "国家队", "市场板块", "好不好做", "涨停", "跌停", "情绪", "顺风", "逆风", "两市", "宽基", "沪深300", "上证", "市场分析"},
	"sector":      {"板块", "行业", "概念", "题材", "主线", "申万", "同花顺"},
	"news":        {"新闻", "公告", "政策", "舆情", "报道", "消息"},
	"finance":     {"财报", "营收", "利润", "净利", "现金流", "季报", "年报", "同比", "环比"},
	"fundamental": {"基本面", "估值", "市盈", "市净", "股东", "解禁", "分红", "调研", "业绩预告", "业绩快报", "主营"},
}

func (s *Server) handleChiefPlan(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	question, previous, err := readPlanRequest(w, r)
	if errors.Is(err, errQuestionTooLong) {
		writeError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if question == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少 question"))
		return
	}
	if s.llm == nil {
		writeError(w, http.StatusInternalServerError, errors.New("模型还没有配置"))
		return
	}
	sess := session.New(session.SessionId("chief-plan-"+sanitizeCode(code)+"-"+fmt.Sprintf("%d", time.Now().UnixNano())), session.Header{})
	ag := s.newAnalyst(sess, coretools.NewRegistry(), 1, chiefPlanPrompt())
	ctx := r.Context()
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: "client disconnected"}, false)
		case <-stopped:
		}
	}()
	ag.Followup(session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{
		Kind: session.BlockText, Text: planUserMessage(question, previous, s.planContext(code)),
	}}})
	ag.WhenIdle()
	close(stopped)
	analysts, note := parseChiefPlan(lastAssistantText(sess.DeriveMessages()))
	analysts = applyStickyPlan(previous, analysts, question)
	note = planNote(analysts, note)
	writeJSON(w, http.StatusOK, map[string]any{"analysts": analysts, "note": note})
}

func readPlanRequest(w http.ResponseWriter, r *http.Request) (question string, previous []string, err error) {
	var body struct {
		Question  string   `json:"question"`
		Previous  []string `json:"previous"`
		Previous1 string   `json:"previous_analyst"` // 兼容单字符串
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQuestionBytes)).Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", nil, errQuestionTooLong
		}
		// 空 body 再试 query
	}
	question = strings.TrimSpace(body.Question)
	if question == "" {
		question = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	previous = normalizePlanIDs(body.Previous)
	if len(previous) == 0 && strings.TrimSpace(body.Previous1) != "" {
		previous = normalizePlanIDs([]string{body.Previous1})
	}
	return question, previous, nil
}

// planContext：给规划用的背景——股票名，加上最近几条问答（只截开头，用来判断「它」「这个」「再展开」指的是什么）。
func (s *Server) planContext(code string) string {
	var b strings.Builder
	if name := s.stockName(code); name != code {
		fmt.Fprintf(&b, "当前股票：%s（%s）\n", name, code)
	}
	lines := s.groupLines(code)
	var recent []groupLine
	for i := len(lines) - 1; i >= 0 && len(recent) < planRecentLines; i-- {
		if strings.TrimSpace(lines[i].Text) != "" && (lines[i].Kind == "reply" || lines[i].Kind == "analysis") {
			recent = append([]groupLine{lines[i]}, recent...)
		}
	}
	if len(recent) > 0 {
		b.WriteString("最近的问答（只用来判断指代，不是这一题的材料）：\n")
		for _, line := range recent {
			if line.Quote != "" {
				fmt.Fprintf(&b, "- %s 回答「%s」：%s\n", line.Name, clipRunes(line.Quote, 40), clipRunes(strings.ReplaceAll(line.Text, "\n", " "), 70))
			} else {
				fmt.Fprintf(&b, "- %s：%s\n", line.Name, clipRunes(strings.ReplaceAll(line.Text, "\n", " "), 70))
			}
		}
	}
	return b.String()
}

const planRecentLines = 3

func planUserMessage(question string, previous []string, background string) string {
	var b strings.Builder
	b.WriteString(background)
	b.WriteString("用户提问：")
	b.WriteString(question)
	b.WriteString("\n")
	if len(previous) == 0 {
		b.WriteString("上一轮分析师：无（这是新开的问题）。\n")
		return b.String()
	}
	b.WriteString("上一轮分析师：")
	names := make([]string, 0, len(previous))
	for _, id := range previous {
		names = append(names, planName[id]+"（"+id+"）")
	}
	b.WriteString(strings.Join(names, "、"))
	b.WriteString("。\n")
	b.WriteString("除非本题明确属于另一位分析师的职责，否则继续请上一轮的人；推荐问、追问细节默认不换人。\n")
	return b.String()
}

func chiefPlanPrompt() string {
	return `你是主分析师。用户刚提了一个问题。你只决定请哪几位分析师来回答，自己不要分析，不要写数字。

只能从这六位里选，id 必须原样书写：
- volume_flow：个股资金流、大单、超大单、融资、龙虎榜、筹码、均线、MACD
- market：大盘、市场结论、市场资金（两市成交/宽基/两融）、国家队、市场板块轮动、好不好做、涨停情绪
- sector：这只股属于哪个板块、行业、概念、题材、个股相对板块
- news：新闻、政策、公告
- finance：已披露财报、单季利润、营收、现金流
- fundamental：基本面、估值、主营构成、股东、业绩预告、业绩快报、限售解禁、机构调研、现金分红

路由规则：
1. 若消息里给了「上一轮分析师」，默认继续请他们（尤其是追问、推荐问、把上一轮结论问深）。
2. 只有本题明确属于另一位的职责（例如上一轮是资金流，这题只问公告/财报/板块归属），才换成那一位。
3. 用户写了 @某位分析师，当作强倾向。
4. 一次最多 3 位。完全用不上时，analysts 用空数组。
5. 用户要分析一条新闻/事件、没有问这只股票的资金/涨跌/板块/财报时，只选 news。
6. 「它」「这个」「再展开」「那…呢」这类省略了主语的追问，指的是「最近的问答」里最后一条的话题，按那条的主题选人，不要因为这句话本身没有关键词就换人或不选人。
7. 「市场资金/国家队/市场板块/好不好做/大盘」选 market；「这只股的板块归属/板内角色」选 sector；「这只股大单」选 volume_flow。

最后一条回复只能是一个 JSON 对象，不要用代码围栏：
{"analysts":["volume_flow","sector"],"note":"请资金流分析师、板块分析师来回答"}
note 用一句话，点出被请到的人。analysts 为空时，note 写「这个问题我来回答」。`
}

func parseChiefPlan(raw string) ([]string, string) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	var body struct {
		Analysts []string `json:"analysts"`
		Note     string   `json:"note"`
	}
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(text[start:end+1]), &body)
	}
	return normalizePlanIDs(body.Analysts), strings.TrimSpace(body.Note)
}

// applyStickyPlan：模型和关键词一起定人。
//   - 点名（@某位）、或题目明显只属于上一轮以外的一维：换人。
//   - 模型没选出人（空、解析失败）：沿用上一轮。
//   - 模型选了人：听模型的；只有题目里的词明明指向上一轮、却没有一个词指向模型选的人时，才拦下来沿用上一轮。
//     （以前这里一律沿用上一轮，模型的判断被丢掉，追问「它的竞争对手有谁」会一直停在资金流。）
func applyStickyPlan(previous, planned []string, question string) []string {
	prev := normalizePlanIDs(previous)
	planned = normalizePlanIDs(planned)
	if len(prev) == 0 {
		return planned
	}
	if at := mentionedPlanAnalyst(question); at != "" && !containsPlanID(prev, at) {
		if len(planned) > 0 {
			return planned
		}
		return []string{at}
	}
	if other := exclusiveOtherDomain(question, prev); other != "" {
		if containsPlanID(planned, other) {
			return planned
		}
		if len(planned) > 0 && !planOverlaps(planned, prev) {
			return planned
		}
		return []string{other}
	}
	if len(planned) == 0 {
		return prev // 模型没主意：推荐问 / 模糊追问，粘上一轮
	}
	if planOverlaps(planned, prev) {
		return planned
	}
	scores := domainScores(question)
	if planScore(scores, prev) > 0 && planScore(scores, planned) == 0 {
		return prev // 题目里的词指向上一轮，模型却换了人，没有依据
	}
	return planned
}

func planScore(scores map[string]int, ids []string) int {
	total := 0
	for _, id := range ids {
		total += scores[id]
	}
	return total
}

func normalizePlanIDs(ids []string) []string {
	allowed := map[string]bool{}
	for _, id := range planAllowed {
		allowed[id] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !allowed[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == 3 {
			break
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func containsPlanID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func planOverlaps(a, b []string) bool {
	for _, x := range a {
		if containsPlanID(b, x) {
			return true
		}
	}
	return false
}

func mentionedPlanAnalyst(question string) string {
	q := question
	for id, name := range planName {
		if strings.Contains(q, "@"+name) || strings.Contains(q, "@"+strings.TrimSuffix(name, "分析师")) {
			return id
		}
	}
	// 也认 @volume_flow 这类
	for _, id := range planAllowed {
		if strings.Contains(q, "@"+id) {
			return id
		}
	}
	return ""
}

// domainScores：题目里命中各维度强特征词的得分，词越长权重越大。
func domainScores(question string) map[string]int {
	scores := map[string]int{}
	lower := strings.ToLower(question)
	for id, hints := range planDomainHints {
		for _, h := range hints {
			if strings.Contains(lower, strings.ToLower(h)) {
				scores[id] += len([]rune(h))
			}
		}
	}
	return scores
}

// exclusiveOtherDomain：问题明显只属于「上一轮以外」的某一维时，返回该 id。
func exclusiveOtherDomain(question string, previous []string) string {
	scores := domainScores(question)
	bestID, best := "", 0
	for id, n := range scores {
		if n > best {
			best, bestID = n, id
		}
	}
	if bestID == "" || best < 1 {
		return ""
	}
	if containsPlanID(previous, bestID) {
		return ""
	}
	// 上一轮维度也有命中 → 不算「明确换人」
	prevHit := 0
	for _, id := range previous {
		prevHit += scores[id]
	}
	if prevHit > 0 && best <= prevHit {
		return ""
	}
	return bestID
}

func planNote(ids []string, modelNote string) string {
	note := clipRunes(strings.TrimSpace(modelNote), 80)
	if len(ids) == 0 {
		if note == "" {
			return "这个问题我来回答"
		}
		return note
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, planName[id])
	}
	fallback := "请" + strings.Join(names, "、") + "来回答"
	if note == "" {
		return fallback
	}
	return note
}

func clipRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

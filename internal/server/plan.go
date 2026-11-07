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

var planAllowed = []string{"volume_flow", "sector", "news", "finance", "fundamental"}

var planName = map[string]string{
	"volume_flow": "资金流分析师",
	"sector":      "板块分析师",
	"news":        "新闻分析师",
	"finance":     "财报分析师",
	"fundamental": "基本面分析师",
}

func (s *Server) handleChiefPlan(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	question, err := readQuestion(w, r)
	if errors.Is(err, errQuestionTooLong) {
		writeError(w, http.StatusRequestEntityTooLarge, err)
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
		Kind: session.BlockText, Text: "用户提问：" + question,
	}}})
	ag.WhenIdle()
	close(stopped)
	analysts, note := parseChiefPlan(lastAssistantText(sess.DeriveMessages()))
	writeJSON(w, http.StatusOK, map[string]any{"analysts": analysts, "note": note})
}

func chiefPlanPrompt() string {
	return `你是主分析师。用户刚提了一个问题。你只决定请哪几位分析师来回答，自己不要分析，不要写数字。

只能从这五位里选，id 必须原样书写：
- volume_flow：资金、资金流、融资、龙虎榜、筹码、成交、均线、MACD
- sector：板块、行业、概念、题材
- news：新闻、政策、公告
- finance：已披露财报、单季利润、营收、现金流
- fundamental：基本面、估值、主营构成、股东、业绩预告、业绩快报、限售解禁、机构调研、现金分红

一次最多 3 位。问题用不上他们时，analysts 用空数组。用户写了 @某位分析师，只当作倾向，仍按问题决定。
用户要分析一条新闻、一个事件，没有问这只股票的资金、涨跌、板块或财报时，只选 news，不要把 volume_flow 或其他人拉进来。
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
	allowed := map[string]bool{}
	for _, id := range planAllowed {
		allowed[id] = true
	}
	var picked []string
	seen := map[string]bool{}
	for _, id := range body.Analysts {
		id = strings.TrimSpace(id)
		if !allowed[id] || seen[id] {
			continue
		}
		seen[id] = true
		picked = append(picked, id)
		if len(picked) == 3 {
			break
		}
	}
	if picked == nil {
		picked = []string{}
	}
	return picked, planNote(picked, body.Note)
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

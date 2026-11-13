package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	coretools "go-harness/core/tools"
	"go-harness/llm/llmretry"
)

// askIntent is the router decision for the 问问 page: bind a stock or not,
// rewrite the question, optionally hint analysts. Produced by a tiny
// one-shot model call — not by hardcoded keyword rules on the client.
type askIntent struct {
	Intent     string   `json:"intent"`
	ReuseLast  bool     `json:"reuse_last"`
	StockQuery string   `json:"stock_query"`
	Theme      string   `json:"theme"`
	Ask        string   `json:"ask"`
	Note       string   `json:"note"`
	Analysts   []string `json:"analysts"`
}

var askIntentAllowed = map[string]bool{
	"stock_followup": true,
	"stock_new":      true,
	"sector_theme":   true,
	"market_screen":  true,
	"general":        true,
}

// askIntentTimeout caps the router so a slow/thinking model never blocks
// the 问问 send path for long. Empty/timeout replies fall through to general.
const askIntentTimeout = 1200 * time.Millisecond

func (s *Server) handleAskIntent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
		LastCode string `json:"last_code"`
		LastName string `json:"last_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQuestionBytes)).Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, errQuestionTooLong)
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("读请求失败: %w", err))
		return
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少 question"))
		return
	}
	if s.intentModel() == nil {
		writeError(w, http.StatusInternalServerError, errors.New("模型还没有配置"))
		return
	}
	lastCode := strings.ToUpper(strings.TrimSpace(body.LastCode))
	lastName := strings.TrimSpace(body.LastName)
	if lastCode != "" && !tsCodePattern.MatchString(lastCode) {
		lastCode, lastName = "", ""
	}

	out := s.resolveAskIntent(r.Context(), question, lastCode, lastName)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) intentModel() agentloop.LLM {
	if s.intentLLM != nil {
		return s.intentLLM
	}
	if s.llm != nil {
		return s.llm
	}
	return nil
}

func (s *Server) resolveAskIntent(parent context.Context, question, lastCode, lastName string) askIntent {
	llm := s.intentModel()
	if llm == nil {
		return askIntent{Intent: "general"}
	}
	sess := session.New(session.SessionId(fmt.Sprintf("ask-intent-%d", time.Now().UnixNano())), session.Header{})
	// Intent is a tiny JSON classify — disable thinking and keep the budget small
	// so DeepSeek doesn't burn the whole send path on chain-of-thought.
	ag := agentloop.NewAgent(sess, llm, coretools.Bridge{Registry: coretools.NewRegistry()}, askIntentPrompt(), agentloop.Hooks{
		PreStep: func(_ context.Context, _ *agentloop.Agent, _, step int, messages []session.Message) (bool, []session.Message) {
			if step > 1 {
				return false, nil
			}
			return true, messages
		},
		RequestError: llmretry.Hook(s.cfg.Retry),
	}).WithModel(s.cfg.Model).WithTemperature(0.2).WithMaxTokens(256).WithReasoningEffort("off")

	ctx, cancel := context.WithTimeout(parent, askIntentTimeout)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: "ask intent timeout"}, false)
		case <-stopped:
		}
	}()
	ag.Followup(session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{
		Kind: session.BlockText, Text: askIntentUserMessage(question, lastCode, lastName),
	}}})
	ag.WhenIdle()
	close(stopped)

	raw := lastAssistantText(sess.DeriveMessages())
	if raw == "" {
		raw = lastAssistantReasoning(sess.DeriveMessages())
	}
	if raw == "" || ctx.Err() != nil {
		return askIntent{Intent: "general"}
	}
	return parseAskIntent(raw, lastCode)
}

func lastAssistantReasoning(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != session.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, block := range msgs[i].Content {
			if block.Kind == session.BlockReasoning {
				b.WriteString(block.Text)
			}
		}
		if text := strings.TrimSpace(b.String()); text != "" {
			return text
		}
	}
	return ""
}

func askIntentPrompt() string {
	return `你是问问页的意图路由器。根据用户这一句，判断要不要绑定股票、问题该怎么交给下游。自己不要分析行情，不要写长文。

可选 intent（只能选一个）：
- stock_followup：在追问本会话上一只股票（「它怎么样」「这个产业走下坡路吗」「再展开」）。需要 last_code 存在。
- stock_new：新点名一只股票（「分析三花智控」「603986怎么看」）。
- sector_theme：在问某个具名板块/产业/概念本身（「分析AI硬件这个板块」「半导体板块怎么样」），即使会话里刚聊过某只股票，也不要绑那只股票。
- market_screen：全市场筛选/名单（「连涨3天以上有哪些」「涨停池」），或「推荐几只某类票」但未落到单票。
- general：其他闲聊或无法归类、且不该绑上一只股票。

规则：
1. 「AI硬件这个板块」「光伏这个行业」里的「这个」修饰的是前面的板块名，不是上一只股票 → sector_theme，reuse_last=false。
2. 「这个产业/这个板块/它」在没有新板块名时，若有 last_code → stock_followup，reuse_last=true；ask 里写清股票名和代码及所属产业/板块语境。
3. 用户明确写了股票名或代码 → stock_new；stock_query 填名称或代码。
4. sector_theme / market_screen / general：reuse_last 必须为 false；不要填 stock_query（除非用户同句点了股票）。
5. 「推荐两只这几个板块的优质股票并对比」这类：若本句没有具名板块 → market_screen 或 general；ask 保持用户原意（推荐+对比），不要改写成「先确认板块范围」；theme 可空；note 可用「板块未点名，交主理人澄清」。
6. analysts 仅在需要个股维度分析时给，从 market,volume_flow,sector,news,finance,fundamental 里选，最多 3 个；有大盘时 market 放第一；板块主题可建议 ["market","sector"]；拿不准就空数组。
7. note 用一句短中文，给界面提示（例如「按板块话题，不绑定上一只股票」）。不要写成长指令。

最后一条回复只能是一个 JSON 对象，不要代码围栏：
{"intent":"sector_theme","reuse_last":false,"stock_query":"","theme":"AI硬件","ask":"分析AI硬件板块的景气、资金与结构","note":"按板块话题，不绑定上一只股票","analysts":["sector","market"]}`
}

func askIntentUserMessage(question, lastCode, lastName string) string {
	var b strings.Builder
	if lastCode == "" {
		b.WriteString("本会话上一只股票：无\n")
	} else if lastName != "" && lastName != lastCode {
		fmt.Fprintf(&b, "本会话上一只股票：%s（%s）\n", lastName, lastCode)
	} else {
		fmt.Fprintf(&b, "本会话上一只股票：%s\n", lastCode)
	}
	b.WriteString("用户提问：")
	b.WriteString(question)
	b.WriteString("\n请输出意图 JSON。")
	return b.String()
}

func parseAskIntent(raw, lastCode string) askIntent {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	var body askIntent
	if start >= 0 && end > start {
		_ = json.Unmarshal([]byte(text[start:end+1]), &body)
	}
	body.Intent = strings.TrimSpace(body.Intent)
	if !askIntentAllowed[body.Intent] {
		body.Intent = "general"
		body.ReuseLast = false
	}
	body.StockQuery = strings.TrimSpace(body.StockQuery)
	body.Theme = strings.TrimSpace(body.Theme)
	body.Ask = strings.TrimSpace(body.Ask)
	body.Note = strings.TrimSpace(body.Note)
	body.Analysts = normalizePlanIDs(body.Analysts)

	switch body.Intent {
	case "stock_followup":
		if lastCode == "" {
			body.Intent = "general"
			body.ReuseLast = false
		} else {
			body.ReuseLast = true
		}
	case "sector_theme", "market_screen", "general":
		body.ReuseLast = false
	case "stock_new":
		body.ReuseLast = false
	}
	if body.ReuseLast && lastCode == "" {
		body.ReuseLast = false
	}
	return body
}

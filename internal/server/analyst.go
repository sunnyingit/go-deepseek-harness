package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-harness/core/agentloop"
	"go-harness/core/session"
	coretools "go-harness/core/tools"
	"go-harness/llm/llmretry"
	"go-harness/skill/skill"

	"gopkg.in/yaml.v3"
)

const (
	maxAnalystSteps = 4
	maxGroupLines   = 200
)

var (
	tsCodePattern = regexp.MustCompile(`^([0-9]{6}\.(SH|SZ|BJ)|[0-9]{5}\.HK)$`)
	statusTextOf  = map[string]string{"support": "偏多", "warn": "偏空", "neutral": "不明"}
)

// groupLine is one spoken turn in a stock's shared 研判群. Fact sheets stay
// out of this log; the next analyst only sees what was actually said.
type groupLine struct {
	Agent      string `json:"agent"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Quote      string `json:"quote,omitempty"`
	Text       string `json:"text"`
	Status     string `json:"status,omitempty"`
	StatusText string `json:"status_text,omitempty"`
	Day        string `json:"day,omitempty"`
}

type analystSpeak struct {
	Text       string   `json:"text"`
	Status     string   `json:"status"`
	StatusText string   `json:"status_text"`
	FactIDs    []string `json:"fact_ids"`
}

type factRow struct {
	ID    string
	Label string
	Text  string
}

func (s *Server) handleAnalystStream(w http.ResponseWriter, r *http.Request) {
	s.handleAnalyst(w, r, "analysis", "")
}

// maxQuestionBytes 追问正文的上限。主分析师「合说」那一问会把几位分析师的回答整段带上，
// 中文一个字三个字节，几位加起来很容易超过原来的 8KB，被截断后读不出 question。
const maxQuestionBytes = 256 << 10

var errQuestionTooLong = errors.New("问题太长，超过了上限")

// readQuestion 从请求体（{"question": "..."}）或 ?q= 读出追问。正文太长要明说，不能当成「没有 question」。
func readQuestion(w http.ResponseWriter, r *http.Request) (string, error) {
	var body struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxQuestionBytes)).Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", errQuestionTooLong
		}
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		question = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	return question, nil
}

func (s *Server) handleAnalystReply(w http.ResponseWriter, r *http.Request) {
	question, err := readQuestion(w, r)
	if errors.Is(err, errQuestionTooLong) {
		writeError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	if question == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少 question"))
		return
	}
	s.handleAnalyst(w, r, "reply", question)
}

func (s *Server) handleAnalyst(w http.ResponseWriter, r *http.Request, kind, question string) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	cfg, err := loadAgent(r.PathValue("agent"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	unlock := s.lockCode(code)
	defer unlock()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	writeAnalystSSE(w, flusher, "start", map[string]string{"dimension": cfg.ID, "name": cfg.Name})

	ag, charts, err := s.prepareAnalyst(r.Context(), cfg, code, kind, question)
	if err != nil {
		writeAnalystSSE(w, flusher, "error", map[string]string{"dimension": cfg.ID, "error": analystErrText(cfg.Name, err)})
		return
	}
	msg, err := s.watchAnalyst(r.Context(), w, flusher, ag, charts, cfg, code, kind, question)
	if err != nil {
		writeAnalystSSE(w, flusher, "error", map[string]string{"dimension": cfg.ID, "error": analystErrText(cfg.Name, err)})
		return
	}
	writeAnalystSSE(w, flusher, "message", msg)
	writeAnalystSSE(w, flusher, "done", map[string]string{"dimension": cfg.ID})
}

func visibleDraft(buf string) string {
	trimmed := strings.TrimSpace(buf)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "{") || strings.Contains(trimmed, `"text"`) {
		parts := jsonTextFields(buf)
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n")
	}
	return buf
}

// jsonTextFields pulls every JSON "text" string out of a partial object.
// The last one may still be unfinished; it is shown as far as it has arrived.
func jsonTextFields(buf string) []string {
	var out []string
	rest := buf
	for {
		i := strings.Index(rest, `"text"`)
		if i < 0 {
			break
		}
		rest = strings.TrimLeft(rest[i+len(`"text"`):], " \t\r\n")
		if !strings.HasPrefix(rest, ":") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n")
		val, next, closed := readJSONString(rest)
		if val == "" && !closed {
			break
		}
		if val != "" || closed {
			out = append(out, val)
		}
		if !closed {
			break
		}
		rest = next
	}
	return out
}

func readJSONString(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	var b strings.Builder
	esc := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		if esc {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(c)
			}
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			continue
		}
		if c == '"' {
			return b.String(), s[i+1:], true
		}
		b.WriteByte(c)
	}
	return b.String(), "", false
}

func analystToolStatus(name string) string {
	switch name {
	case "fetch_facts":
		return "正在读取事实"
	case "fetch_basic":
		return "正在查行情"
	case "load_skill", "list_skills":
		return "正在加载技能"
	case "render_chart":
		return "正在画图"
	case "fetch_news":
		return "正在搜索新闻"
	case "fetch_sector_membership":
		return "正在查板块归属"
	case "fetch_sector_tape":
		return "正在看板块走势"
	case "fetch_sector_position":
		return "正在看成分位置"
	case "fetch_sector_rank":
		return "正在排行业强弱"
	case "fetch_sector_sw":
		return "正在读申万日线"
	case "fetch_theme_heat":
		return "正在看今日题材"
	case "fetch_sector_dc":
		return "正在读东财板块"
	case "fetch_flow_dc":
		return "正在读个股资金"
	case "fetch_flow_factor":
		return "正在读技术指标"
	case "fetch_flow_chip":
		return "正在读筹码"
	case "fetch_flow_bar":
		return "正在读行情"
	case "fetch_flow_premarket":
		return "正在读涨跌停价"
	case "fetch_flow_limit":
		return "正在查涨跌停"
	case "fetch_flow_toplist":
		return "正在查龙虎榜"
	case "fetch_flow_margin":
		return "正在读融资"
	case "fetch_company", "fetch_business", "fetch_valuation", "fetch_quality", "fetch_holders",
		"fetch_forecast", "fetch_express", "fetch_share_float", "fetch_survey", "fetch_mainbz", "fetch_dividend":
		return "正在读基本面"
	default:
		if name == "" {
			return ""
		}
		return "正在查询"
	}
}

func writeAnalystSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	writeSSE(w, event, data)
	flusher.Flush()
}

func analystErrText(name string, err error) string {
	text := err.Error()
	lower := strings.ToLower(text)
	if strings.Contains(lower, "api_key") || strings.Contains(lower, "sk-") || strings.Contains(lower, "token") {
		return name + "这次没有说完"
	}
	if len([]rune(text)) > 180 {
		return name + "这次没有说完"
	}
	return text
}

// agentConfig is one named analyst. Prompt, rules, and installed skills come
// from web/agents/<id>/, not from the Go source.
type agentConfig struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Dimension   string            `yaml:"dimension"`
	Skills      []string          `yaml:"skills"`
	Tools       []string          `yaml:"tools"`
	FactPrefix  string            `yaml:"fact_prefix"`
	DetailQuery map[string]string `yaml:"detail_query"`
	MaxSteps    int               `yaml:"max_steps"`
	Opening     string            `yaml:"opening"`
	Forbid      []string          `yaml:"forbid"`
	// Speak is headline for a dimension analyst, points for 看多/看空, chief for 主分析师.
	Speak  string `yaml:"speak"`
	Prompt string `yaml:"-"`
	Rules  string `yaml:"-"`
}

func (c agentConfig) oneShot() bool {
	return c.Speak == "points" || c.Speak == "chief"
}

// runAnalyst builds a short-lived agent for one utterance. The group log
// stays keyed by the stock code, so a later analyst on the same stock sees
// this speech and does not open another session.
func (s *Server) runAnalyst(ctx context.Context, cfg agentConfig, code, kind, question string) (map[string]any, error) {
	ag, charts, err := s.prepareAnalyst(ctx, cfg, code, kind, question)
	if err != nil {
		return nil, err
	}
	if ag == nil {
		return withChart(s.pointsAfterChiefMessage(code, cfg), charts), nil
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: "client disconnected"}, false)
		case <-stopped:
		}
	}()
	ag.WhenIdle()
	close(stopped)
	return s.concludeAnalyst(cfg, code, kind, question, ag.Session(), charts)
}

// prepareAnalyst starts one utterance. A nil agent means the reply was
// already decided locally (看多/看空在主分析师裁决之后补采纳标记).
func (s *Server) prepareAnalyst(ctx context.Context, cfg agentConfig, code, kind, question string) (*agentloop.Agent, *chartBox, error) {
	if s.llm == nil {
		return nil, nil, errors.New("模型还没有配置")
	}
	if cfg.Speak == "points" && strings.TrimSpace(question) == "" {
		if _, ok := s.pointsAfterChief(code, cfg); ok {
			return nil, &chartBox{}, nil
		}
	}
	prior := s.groupLines(code)
	sess := session.New(session.SessionId(cfg.ID+"-"+sanitizeCode(code)+"-"+fmt.Sprintf("%d", time.Now().UnixNano())), session.Header{})
	charts := &chartBox{}
	ag, err := s.buildConfiguredAgent(sess, cfg, charts, kind)
	if err != nil {
		return nil, nil, err
	}
	ag.Followup(session.Message{Role: session.RoleUser, Content: []session.ContentBlock{{
		Kind: session.BlockText, Text: analystUserPrompt(cfg, code, question, prior, s.loadDebate(code)),
	}}})
	return ag, charts, nil
}

func (s *Server) pointsAfterChiefMessage(code string, cfg agentConfig) map[string]any {
	msg, _ := s.pointsAfterChief(code, cfg)
	return msg
}

// watchAnalyst tails the session log and pushes each new sentence to the
// client. The final message event still carries the cleaned speech.
func (s *Server) watchAnalyst(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, ag *agentloop.Agent, charts *chartBox, cfg agentConfig, code, kind, question string) (map[string]any, error) {
	if ag == nil {
		return withChart(s.pointsAfterChiefMessage(code, cfg), charts), nil
	}
	sess := ag.Session()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	lastSeq := 0
	stepText := ""
	lastDraft := ""
	lastStatus := ""
	lastActivity := time.Now()
	started := time.Now()
	for {
		select {
		case <-ctx.Done():
			ag.Cancel(agentloop.CancelCause{Kind: "user", Reason: "client disconnected"}, false)
			return nil, ctx.Err()
		case <-ticker.C:
			events := sess.Events()
			for ; lastSeq < len(events); lastSeq++ {
				ev := events[lastSeq]
				data, _ := ev.Data.(map[string]any)
				switch ev.Type {
				case "step/start":
					stepText = ""
				case "assistant/chunk":
					block, ok := data["chunk"].(session.ContentBlock)
					if !ok || block.Kind != session.BlockText || block.Text == "" {
						continue
					}
					stepText += block.Text
					draft := visibleDraft(stepText)
					if draft != lastDraft {
						lastDraft = draft
						writeAnalystSSE(w, flusher, "draft", map[string]string{"text": draft, "status": lastStatus})
						lastActivity = time.Now()
					}
				case "tool/call":
					call, ok := data["call"].(session.ContentBlock)
					if !ok {
						continue
					}
					status := analystToolStatus(call.ToolName)
					if status != "" && status != lastStatus {
						lastStatus = status
						writeAnalystSSE(w, flusher, "draft", map[string]string{"text": lastDraft, "status": status})
						lastActivity = time.Now()
					}
				}
			}
			if ag.Status() == "idle" && (len(sess.Events()) > 0 || time.Since(started) > 2*time.Second) {
				return s.concludeAnalyst(cfg, code, kind, question, sess, charts)
			}
			if time.Since(lastActivity) >= streamHeartbeatInterval {
				writeAnalystSSE(w, flusher, "heartbeat", map[string]any{})
				lastActivity = time.Now()
			}
		}
	}
}

func (s *Server) concludeAnalyst(cfg agentConfig, code, kind, question string, sess *session.Session, charts *chartBox) (map[string]any, error) {
	msgs := sess.DeriveMessages()
	if cfg.oneShot() {
		msg, err := s.finishOneShot(cfg, code, kind, question, lastAssistantText(msgs), lastTurnError(sess))
		return withChart(msg, charts), err
	}
	if kind == "reply" {
		text := replyAnswer(lastAssistantText(msgs))
		if text == "" || forbiddenHit(text, cfg.Forbid) {
			if text == "" {
				if turnErr := lastTurnError(sess); turnErr != "" {
					return nil, errors.New(turnErr)
				}
			}
			return withChart(emptyAnalystMessage(cfg, kind, question), charts), nil
		}
		s.appendGroup(code, groupLine{
			Agent: cfg.ID, Name: cfg.Name, Kind: kind, Quote: question, Text: text,
		})
		return withChart(analystMessage(cfg, kind, question, text, nil), charts), nil
	}
	catalog := factsFromMessages(msgs)
	speak, speakErr := parseSpeak(lastAssistantText(msgs))
	if speakErr != nil {
		if turnErr := lastTurnError(sess); turnErr != "" {
			return nil, errors.New(turnErr)
		}
		return withChart(emptyAnalystMessage(cfg, kind, question), charts), nil
	}
	headline, ok := headlineFromSpeak(speak, catalog, cfg)
	if !ok {
		return withChart(emptyAnalystMessage(cfg, kind, question), charts), nil
	}
	text, _ := headline["text"].(string)
	s.appendGroup(code, groupLine{
		Agent: cfg.ID, Name: cfg.Name, Kind: kind, Quote: question,
		Text: text, Status: speak.Status, StatusText: statusTextOf[speak.Status],
	})
	return withChart(analystMessage(cfg, kind, question, text, headline), charts), nil
}

func emptyAnalystMessage(cfg agentConfig, kind, quote string) map[string]any {
	return analystMessage(cfg, kind, quote, "", nil)
}

func analystMessage(cfg agentConfig, kind, quote, text string, headline map[string]any) map[string]any {
	return map[string]any{
		"role":      "analyst",
		"dimension": cfg.ID,
		"name":      cfg.Name,
		"kind":      kind,
		"quote":     quote,
		"text":      text,
		"headline":  headline,
	}
}

func analystUserPrompt(cfg agentConfig, code, question string, lines []groupLine, debate *debateFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "股票代码：%s\n", code)
	if strings.TrimSpace(question) == "" {
		opening := strings.TrimSpace(cfg.Opening)
		if opening == "" {
			opening = "请给出结论。"
		}
		b.WriteString(opening + "\n")
		if cfg.ID == "news" {
			fmt.Fprintf(&b, "分析时间：%s。\n", time.Now().Format("2006-01-02 15:04"))
			b.WriteString("用 100 字以内写一段结论：方向和信号强度，哪些在 48 小时内，哪些当日已反映，哪些只是背景。不要表格，不要标题，不要清单。\n")
		} else if cfg.ID == "volume_flow" {
			b.WriteString("用 100 字以内写一段结论：近 5 日价和资金是同向还是背离、是连续还是单日脉冲，以及命中的位置预警。不要表格，不要标题，不要清单。\n")
		} else if cfg.ID == "sector" {
			b.WriteString("用 100 字以内写一段结论：主板块和这一轮阶段，成分里是核心还是跟风。行业和概念分开。不要表格，不要标题，不要清单。\n")
		} else if !cfg.oneShot() {
			b.WriteString("用 100 字以内写一段结论，覆盖这次读到的要点。不要表格，不要标题，不要清单。\n")
		}
	} else if cfg.oneShot() {
		fmt.Fprintf(&b, "用户提问：%s\n按这个问题回答。\n", strings.TrimSpace(question))
	} else if cfg.ID == "news" {
		fmt.Fprintf(&b, "用户提问：%s\n分析时间：%s。\n先 load_skill news-raw，再调用 fetch_news。keyword 传用户要搜的原话，不要加上股票名称，也不要留空。这是按这句话搜索新闻，不限于这只股票。不要只复述近几日 detail。用户是在分析这条新闻本身时，按新闻写谁、发生了什么、冲击和时效；不要改写成对当前股票有没有影响。只有用户问到这只股票，或新闻写到这家公司，才补一句关系。\n", strings.TrimSpace(question), time.Now().Format("2006-01-02 15:04"))
	} else if cfg.ID == "fundamental" {
		fmt.Fprintf(&b, "用户提问：%s\n按这个问题回答。公司用 fetch_company，业务构成用 fetch_business 或 fetch_mainbz，估值用 fetch_valuation，质地用 fetch_quality，股东用 fetch_holders，业绩预告用 fetch_forecast，业绩快报用 fetch_express，解禁用 fetch_share_float，机构调研用 fetch_survey，分红用 fetch_dividend。不要先拿整份 facts 来框住回答。\n", strings.TrimSpace(question))
	} else if cfg.ID == "sector" {
		fmt.Fprintf(&b, "用户提问：%s\n先 load_skill sector-reply，再按问题调用对应工具。不要先拿整份 facts 来框住回答。\n", strings.TrimSpace(question))
	} else if cfg.ID == "volume_flow" {
		fmt.Fprintf(&b, "用户提问：%s\n先 load_skill flow-reply，再按问题调用对应工具。不要先拿整份 facts 来框住回答。\n", strings.TrimSpace(question))
	} else {
		fmt.Fprintf(&b, "用户提问：%s\n先调用 fetch_facts 读取 detail，回答里带上这些事实。这个问题还需要其他数据时，再调用已安装的工具。\n", strings.TrimSpace(question))
	}
	if cfg.oneShot() && strings.TrimSpace(question) == "" {
		b.WriteString("群里已经说过的话是唯一材料，不要调用综合判断接口，不要新编数字：\n")
	} else if cfg.oneShot() {
		b.WriteString("群里已经说过的话只作上下文。数字以这些发言和这次工具返回的为准，不要新编：\n")
	} else if strings.TrimSpace(question) == "" {
		b.WriteString("此前群里的发言只作上下文，数字仍以这次 detail 返回的事实为准：\n")
	} else {
		b.WriteString("群里已经说过的话只作上下文，数字以这次工具返回的为准：\n")
	}
	if len(lines) == 0 {
		b.WriteString("（还没有人说过）\n")
	} else {
		start := 0
		if len(lines) > 20 {
			start = len(lines) - 20
		}
		for _, line := range lines[start:] {
			if line.Quote != "" {
				fmt.Fprintf(&b, "%s 回答「%s」：%s\n", line.Name, line.Quote, line.Text)
			} else {
				fmt.Fprintf(&b, "%s：%s\n", line.Name, line.Text)
			}
		}
	}
	if cfg.Speak == "chief" && debate != nil {
		b.WriteString("\n看多论据：\n")
		b.WriteString(formatPoints(debate.Bull))
		b.WriteString("看空论据：\n")
		b.WriteString(formatPoints(debate.Bear))
	}
	return b.String()
}

func (c agentConfig) systemPrompt() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(c.Prompt))
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(c.Rules))
	if c.oneShot() {
		fmt.Fprintf(&b, "\n\n已安装技能：%s。先 load_skill 加载这些技能。问现价、涨跌、市值时再调用 fetch_basic。写 text 时按 answer-outline 和 answer-table 排版。不要读取维度 detail，也不要调用综合判断接口。\n", strings.Join(c.Skills, "、"))
		b.WriteString(c.oneShotContract())
		return b.String()
	}
	b.WriteString("\n\n已安装技能：")
	b.WriteString(strings.Join(c.Skills, "、"))
	b.WriteString("。先 load_skill 把事实技能、answer-outline、answer-table 加载。需要图时再 load_skill d3js-visualization：分类对比用柱状图，随时间变化用折线图，然后调用 render_chart。不要自己写 HTML，不要下载 D3。\n")
	b.WriteString("需要事实时只调用配置里的工具，参数只有股票代码。维度已绑定，不能改。不要调用 Tushare，不要把没有的数写成 0。\n")
	if containsSkill(c.Skills, "stock-basic") {
		b.WriteString("问现价、涨跌、市值、PE、PB、ROE 时，先 load_skill stock-basic，再调用 fetch_basic。数字只用返回的 data，null 不要写成 0。\n")
	}
	b.WriteString("拿到事实后立刻结束，不要再调工具。\n")
	b.WriteString("最后一条回复只能是一个 JSON 对象，不要用代码围栏包住它。\n")
	b.WriteString("text 是给用户看的正文。开场只写 100 字以内的一段结论，不要表格，不要标题，不要清单。\n")
	b.WriteString("追问按问题展开，也不必用表格。\n")
	b.WriteString("单元格只抄 facts 的 text，或新闻 rows 里的字段。不要编没有的行，不要把没有的数写成 0。\n")
	b.WriteString("{\"text\":\"结论\\n\\n| 项目 | 内容 |\\n| --- | --- |\\n| ... | ... |\",\"status\":\"support|warn|neutral\",\"status_text\":\"偏多|偏空|不明\",\"fact_ids\":[\"id\"]}\n")
	if c.FactPrefix != "" {
		fmt.Fprintf(&b, "fact_ids 必须以 %s 开头，并且出现在本次 facts 里。\n", c.FactPrefix)
	} else {
		b.WriteString("fact_ids 只能填本次 facts 里的 id。\n")
	}
	b.WriteString("没有事实时 text 说明这次没有取到，status 用 neutral，fact_ids 用空数组。\n")
	return b.String()
}

// replyPrompt is the follow-up system prompt. Opening rules stay in systemPrompt.
func (c agentConfig) replyPrompt() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(c.Prompt))
	b.WriteString("\n\n已安装技能：")
	b.WriteString(strings.Join(c.Skills, "、"))
	if containsSkill(c.Skills, "fundamental-reply") {
		b.WriteString("。追问按用户的问题选工具，不要先读整份 facts，也不要一次调完。公司用 fetch_company，业务构成用 fetch_business 或 fetch_mainbz，估值用 fetch_valuation，质地用 fetch_quality，股东用 fetch_holders，业绩预告用 fetch_forecast，业绩快报用 fetch_express，未来三个月解禁用 fetch_share_float，机构调研用 fetch_survey，现金分红用 fetch_dividend。要图时用返回的 series 调用 render_chart。解禁占比达到 1% 先说供给。调研不是买卖信号。没有分红不要写成 0。\n")
	} else if containsSkill(c.Skills, "sector-reply") {
		b.WriteString("。追问先 load_skill sector-reply，再按问题选一到两个工具，不要一次调完所有工具，也不要先读整份 facts。归属用 fetch_sector_membership，走势和板块资金用 fetch_sector_tape，成分位置和个股资金用 fetch_sector_position，行业前后名用 fetch_sector_rank，申万日线用 fetch_sector_sw，今天的涨停、连板和龙虎榜用 fetch_theme_heat，东财板块和人气榜用 fetch_sector_dc。\n")
	} else if containsSkill(c.Skills, "flow-reply") {
		b.WriteString("。追问先 load_skill flow-reply，再按问题选一到两个工具，不要一次调完。个股资金用 fetch_flow_dc，技术指标用 fetch_flow_factor，筹码用 fetch_flow_chip，前复权行情用 fetch_flow_bar，涨跌停价用 fetch_flow_premarket，涨跌停记录用 fetch_flow_limit，龙虎榜用 fetch_flow_toplist，融资余额和融资净买入用 fetch_flow_margin。不要把大单叫做主力。\n")
	} else if containsSkill(c.Skills, "news-raw") {
		b.WriteString("。每次追问都要 load_skill news-raw，并调用 fetch_news。keyword 传用户要搜的原话，不要加上股票名称，也不要留空。搜索不限于这只股票。不要只根据近几日 detail 回答。用户没问这只股票时，不要把答案收成「对这只股票没有影响」。\n")
	} else {
		b.WriteString("。先 load_skill 事实技能，调用 fetch_facts，把这次 detail 读进来。回答里要带上这些事实。\n")
		b.WriteString("用户的问题还需要别的数据时，继续 load_skill 并调用对应工具。不要在拿到 detail 后停住。\n")
	}
	b.WriteString("不要调用 Tushare。数字只用工具返回的内容，没有的数不要写成 0。\n")
	b.WriteString("用 markdown 直接回答。不必用表格，条目用分段或列表写清楚。不要输出 JSON。\n")
	return b.String()
}

func (c agentConfig) oneShotReplyPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是%s。这是用户追问，按问题回答。\n\n", c.Name)
	b.WriteString("已安装技能：")
	b.WriteString(strings.Join(c.Skills, "、"))
	b.WriteString("。按问题自己选用，需要时再 load_skill，再调用对应工具。\n")
	b.WriteString("问现价、涨跌、市值时，先 load_skill stock-basic，再调用 fetch_basic。\n")
	b.WriteString("不要调用 Tushare，不要调用综合判断接口。数字只用群里已经说过的话和这次工具返回的内容，没有的数不要写成 0。\n")
	b.WriteString("用 markdown 直接回答。不必用表格，条目用分段或列表写清楚。不要输出 JSON，也不要重贴开场论据。\n")
	return b.String()
}

func (c agentConfig) oneShotContract() string {
	if c.Speak == "chief" {
		return "最后一条回复只能是一个 JSON 对象，不要用代码围栏包住它。\n" +
			"{\"verdict\":\"偏多|偏空|不明\",\"text\":\"裁决。追问需要展开时，text 按渲染技能写成结论加表格\",\"adopted\":[\"bull.1\"],\"rejected\":[{\"id\":\"bear.1\",\"reason\":\"证据不足\"}],\"however\":\"\",\"if_wrong\":\"\"}\n" +
			"adopted 和 rejected 的 id 只能来自上面列出的看多、看空论据。同一条不能又采纳又未采纳。\n"
	}
	return "最后一条回复只能是一个 JSON 对象，不要用代码围栏包住它。\n" +
		"{\"points\":[{\"text\":\"一句话，数字必须来自该分析师已说过的话\",\"dimension\":\"volume_flow\",\"weight\":2}]}\n" +
		"dimension 只能是 volume_flow、news、finance、fundamental、sector 里这次确实说过结论的。1 到 3 条。weight 只能是 1、2、3。points 里不要放表格。\n"
}

func (s *Server) buildConfiguredAgent(sess *session.Session, cfg agentConfig, charts *chartBox, kind string) (*agentloop.Agent, error) {
	toolRegistry := coretools.NewRegistry()
	if cfg.oneShot() {
		if _, err := toolRegistry.Register(listSkillsTool(s.skills, cfg.Skills)); err != nil {
			return nil, err
		}
		if _, err := toolRegistry.Register(loadSkillTool(s.skills, cfg.Skills)); err != nil {
			return nil, err
		}
		if err := s.registerBasicTool(toolRegistry, cfg); err != nil {
			return nil, err
		}
		steps := cfg.MaxSteps
		if steps <= 0 {
			steps = 3
		}
		prompt := cfg.systemPrompt()
		if kind == "reply" {
			prompt = cfg.oneShotReplyPrompt()
			if steps < 6 {
				steps = 6
			}
		}
		return s.newAnalyst(sess, toolRegistry, steps, prompt), nil
	}
	if _, err := toolRegistry.Register(listSkillsTool(s.skills, cfg.Skills)); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(loadSkillTool(s.skills, cfg.Skills)); err != nil {
		return nil, err
	}
	for _, name := range cfg.Tools {
		switch name {
		case "fetch_facts":
			fetch, err := s.fetchFactsTool(cfg)
			if err != nil {
				return nil, err
			}
			if _, err := toolRegistry.Register(fetch); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("未知工具 %s", name)
		}
	}
	if containsSkill(cfg.Skills, "d3js-visualization") {
		chartTool, err := s.renderChartTool(charts)
		if err != nil {
			return nil, err
		}
		if _, err := toolRegistry.Register(chartTool); err != nil {
			return nil, err
		}
	}
	if err := s.registerBasicTool(toolRegistry, cfg); err != nil {
		return nil, err
	}
	if err := s.registerNewsTool(toolRegistry, cfg); err != nil {
		return nil, err
	}
	if err := s.registerFundamentalTools(toolRegistry, cfg); err != nil {
		return nil, err
	}
	if err := s.registerSectorTools(toolRegistry, cfg); err != nil {
		return nil, err
	}
	if err := s.registerFlowTools(toolRegistry, cfg); err != nil {
		return nil, err
	}
	steps := cfg.MaxSteps
	if steps <= 0 {
		steps = maxAnalystSteps
	}
	prompt := cfg.systemPrompt()
	if kind == "reply" {
		prompt = cfg.replyPrompt()
	}
	return s.newAnalyst(sess, toolRegistry, steps, prompt), nil
}

func (s *Server) newAnalyst(sess *session.Session, toolRegistry *coretools.Registry, steps int, prompt string) *agentloop.Agent {
	return agentloop.NewAgent(sess, s.llm, coretools.Bridge{Registry: toolRegistry}, prompt, agentloop.Hooks{
		PreStep: func(_ context.Context, _ *agentloop.Agent, _, step int, messages []session.Message) (bool, []session.Message) {
			if step > steps {
				return false, nil
			}
			return true, messages
		},
		RequestError: llmretry.Hook(s.cfg.Retry),
	}).WithModel(s.cfg.Model).WithTemperature(0.2).WithMaxTokens(2048)
}

// fetchFactsTool runs the shared detail script. The model only passes a
// stock code; dimension and query come from this agent's config.
func (s *Server) fetchFactsTool(cfg agentConfig) (coretools.ToolDefinition, error) {
	script, err := factsScript()
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
		Name:        "fetch_facts",
		Description: "读取这只股票在当前分析师维度上已经算好的 detail。只能传股票代码。维度已绑定，不能改，也不能改走 Tushare。",
		Parameters: coretools.ParameterSchema{
			"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		TimeoutMs:         90_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
			if !tsCodePattern.MatchString(code) {
				return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
			}
			cmd := exec.CommandContext(ctx, pythonBin, script, code)
			cmd.Env = factsEnv(cfg)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				captured := strings.TrimSpace(out.String())
				if captured == "" {
					return "", fmt.Errorf("读取 detail 失败: %v", err)
				}
				return "", fmt.Errorf("读取 detail 失败: %s", captured)
			}
			text := strings.TrimSpace(out.String())
			if text == "" {
				return "", errors.New("detail 没有返回事实")
			}
			return text, nil
		},
	}, nil
}

func (s *Server) registerBasicTool(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "stock-basic") {
		return nil
	}
	tool, err := s.fetchBasicTool()
	if err != nil {
		return err
	}
	_, err = toolRegistry.Register(tool)
	return err
}

func (s *Server) registerFundamentalTools(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "fundamental-reply") {
		return nil
	}
	script, err := s.skillScript("fundamental-reply", "scripts/fetch_part.py")
	if err != nil {
		return err
	}
	pythonBin := ""
	if s.pythonEnv != nil {
		pythonBin = s.pythonEnv.PythonPath
	}
	if pythonBin == "" {
		return errors.New("python 还没有准备好")
	}
	parts := []struct{ name, text, via string }{
		{"fetch_company", "读取公司是做什么的、所属行业。追问公司本身时用。只能传股票代码。", "走基本面 detail，不能改走 Tushare。"},
		{"fetch_business", "读取业务构成：产品线、收入、占比、同比、毛利率。追问靠什么赚钱时用。只能传股票代码。", "走基本面 detail，不能改走 Tushare。"},
		{"fetch_valuation", "读取估值当前值、五年分位、同行中位数和估值走势。追问贵不贵时用。只能传股票代码。股价高低不是估值。", "走基本面 detail，不能改走 Tushare。"},
		{"fetch_quality", "读取盈利、现金流、负债等质地和同行分位。追问赚的是不是现金时用。只能传股票代码。", "走基本面 detail，不能改走 Tushare。"},
		{"fetch_holders", "读取股东增减持、质押、回购。追问股东在干什么时用。只能传股票代码。", "走基本面 detail，不能改走 Tushare。"},
		{"fetch_forecast", "读取业绩预告：预告类型、归母净利润区间和变动幅度。追问预增预减时用。只能传股票代码。这是公司自己的预告，不是一致预期。", "走基本面追问接口，不能改走 Tushare。"},
		{"fetch_express", "读取业绩快报：营收、净利润、同比和每股收益。正式财报出来前用。只能传股票代码。快报还不是正式财报。", "走基本面追问接口，不能改走 Tushare。"},
		{"fetch_share_float", "读取未来 1 到 3 个月的限售解禁：日期、股东、股数和占总股本比例，并标出占比达到 1% 的大额解禁。给出可以关注之前先看这里。只能传股票代码。", "走基本面追问接口，不能改走 Tushare。"},
		{"fetch_survey", "读取近 90 日机构调研：日期、接待人员、参与机构和方式。密集调研只是需要再看的提示，不是买卖信号。只能传股票代码。", "走基本面追问接口，不能改走 Tushare。"},
		{"fetch_mainbz", "读取最新一期主营构成，按产品和地区拆收入、成本和毛利率。用来核对公司说的业务和钱从哪来是否一致。只能传股票代码。", "走基本面追问接口，不能改走 Tushare。"},
		{"fetch_dividend", "读取近年现金分红和送转：每股派现、进度、近 5 年有没有真金白银回报。只能传股票代码。没有分红不要写成 0。", "走基本面追问接口，不能改走 Tushare。"},
	}
	for _, part := range parts {
		name := strings.TrimPrefix(part.name, "fetch_")
		tool := coretools.ToolDefinition{
			Name:        part.name,
			Description: part.text + part.via,
			Parameters: coretools.ParameterSchema{
				"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
				"days":    {ValueSchema: coretools.ValueSchema{Kind: coretools.KindNumber, Description: "要看多少个交易日，1 到 120。不传就用默认的多个时间尺度。用户问了具体天数或「最近一个月、三个月」时再传（一个月约 20 日，三个月约 60 日）"}},
			},
			Output: coretools.Output{
				Schema: coretools.ValueSchema{Kind: coretools.KindString},
				Render: renderStringResult,
			},
			TimeoutMs:         80_000,
			IsConcurrencySafe: func(map[string]any) bool { return true },
			Execute:           s.runPart(pythonBin, script, name),
		}
		if _, err := toolRegistry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registerFlowTools(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "flow-reply") {
		return nil
	}
	script, err := s.skillScript("flow-reply", "scripts/fetch_part.py")
	if err != nil {
		return err
	}
	pythonBin := ""
	if s.pythonEnv != nil {
		pythonBin = s.pythonEnv.PythonPath
	}
	if pythonBin == "" {
		return errors.New("python 还没有准备好")
	}
	parts := []struct{ name, text string }{
		{"fetch_flow_dc", "读取东财个股资金流：近 1、3、5、10、20、60 日（历史够的才给）的净流入、超大单、大单、中单、小单，以及逐日净流入。默认逐日只给近 5 日；用户问某个天数（比如近 30 日、近 3 个月），就传 days，会多给这个窗口的合计和这么多天的逐日。不要把净流入叫做主力。"},
		{"fetch_flow_factor", "读取 MACD、RSI、KDJ、布林带和 CCI。只能传股票代码。只报数字，不要写成金叉、死叉或已经反转。"},
		{"fetch_flow_chip", "读取筹码加权成本和胜率，以及收盘相对成本的位置。只能传股票代码。"},
		{"fetch_flow_bar", "读取前复权收盘、涨跌幅、5 日和 20 日均线、换手率，各时间尺度（3、5、10、20、60 日）的累计涨跌，以及逐日涨跌（默认近 5 日，传 days 可以看更长）。"},
		{"fetch_flow_premarket", "读取总股本、流通股本、昨收、涨停价和跌停价。只能传股票代码。"},
		{"fetch_flow_limit", "读取本股涨停、跌停、炸板和连板，默认近 20 日；传 days 可以看更长（最多 120 日）。"},
		{"fetch_flow_toplist", "读取本股龙虎榜净买额和上榜理由，默认近 20 日；传 days 可以看更长。不分析席位。"},
		{"fetch_flow_margin", "读取每日融资余额、融资买入额和融资净买入，以及近 5、10、20 日合计；传 days 可以多看几天的逐日。融资净买入是买入额减去偿还额。"},
	}
	for _, part := range parts {
		name := strings.TrimPrefix(part.name, "fetch_flow_")
		tool := coretools.ToolDefinition{
			Name:        part.name,
			Description: part.text + "走资金流服务，不能改走 Tushare。",
			Parameters: coretools.ParameterSchema{
				"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
			},
			Output: coretools.Output{
				Schema: coretools.ValueSchema{Kind: coretools.KindString},
				Render: renderStringResult,
			},
			TimeoutMs:         80_000,
			IsConcurrencySafe: func(map[string]any) bool { return true },
			Execute:           s.runFlowPart(pythonBin, script, name),
		}
		if _, err := toolRegistry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runFlowPart(pythonBin, script, part string) func(context.Context, map[string]any, *coretools.RunContext) (any, error) {
	return func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
		code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
		if !tsCodePattern.MatchString(code) {
			return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
		}
		argv := []string{script, code, part}
		if days := flowDays(args["days"]); days > 0 {
			argv = append(argv, strconv.Itoa(days))
		}
		cmd := exec.CommandContext(ctx, pythonBin, argv...)
		cmd.Env = basicEnv()
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			captured := strings.TrimSpace(out.String())
			if captured == "" {
				return "", fmt.Errorf("读取资金流失败: %v", err)
			}
			return "", fmt.Errorf("读取资金流失败: %s", captured)
		}
		text := strings.TrimSpace(out.String())
		if text == "" {
			return "", errors.New("资金流没有返回")
		}
		return text, nil
	}
}

// flowDays 把模型传来的 days 收成 1 到 120 的整数；没传或不合法返回 0（用默认的多尺度）。
func flowDays(value any) int {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case int:
		n = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0
		}
		n = parsed
	default:
		return 0
	}
	if n < 1 {
		return 0
	}
	if n > 120 {
		return 120
	}
	return int(n)
}

func (s *Server) registerSectorTools(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "sector-reply") {
		return nil
	}
	script, err := s.skillScript("sector-reply", "scripts/fetch_part.py")
	if err != nil {
		return err
	}
	pythonBin := ""
	if s.pythonEnv != nil {
		pythonBin = s.pythonEnv.PythonPath
	}
	if pythonBin == "" {
		return errors.New("python 还没有准备好")
	}
	parts := []struct{ name, text string }{
		{"fetch_sector_membership", "读取申万一级到三级，以及挂了哪些同花顺概念。追问属于哪个板块时用。只能传股票代码。"},
		{"fetch_sector_tape", "读取主板块阶段、近 20 日相对沪深300，以及板块近 5 日和近 20 日资金。追问这一轮走到哪、有没有资金配合时用。只能传股票代码。"},
		{"fetch_sector_position", "读取市值名次、涨幅名次、领涨和垫底的几只，以及个股大资金有没有跟上板块。只能传股票代码。不返回整张成分表。"},
		{"fetch_sector_rank", "读取申万一级近 20 日涨幅排行和前后几名。追问哪个行业最强、本行业排第几时用。只能传股票代码。"},
		{"fetch_sector_sw", "读取本股申万一级、二级、三级行业的最新收盘、涨跌幅、市盈率、市净率、成交额和近 5 日、近 20 日走势。只能传股票代码。"},
		{"fetch_theme_heat", "读取主板块和次板块成分里的今日涨停、炸板、跌停、连板天梯和龙虎榜。追问是不是今天的主线时用。只能传股票代码。"},
		{"fetch_sector_dc", "读取东财行业和概念板块的今日涨跌、上涨下跌家数、领涨股、近 20 日行情，以及东财人气榜。只能传股票代码。这是东财分类，不要和申万、同花顺并成同一个板块。"},
	}
	for _, part := range parts {
		name := strings.TrimPrefix(part.name, "fetch_")
		name = strings.TrimPrefix(name, "sector_")
		name = strings.TrimPrefix(name, "theme_")
		tool := coretools.ToolDefinition{
			Name:        part.name,
			Description: part.text + "走板块服务，不能改走 Tushare。",
			Parameters: coretools.ParameterSchema{
				"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
			},
			Output: coretools.Output{
				Schema: coretools.ValueSchema{Kind: coretools.KindString},
				Render: renderStringResult,
			},
			TimeoutMs:         120_000,
			IsConcurrencySafe: func(map[string]any) bool { return true },
			Execute:           s.runSectorPart(pythonBin, script, name),
		}
		if _, err := toolRegistry.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runSectorPart(pythonBin, script, part string) func(context.Context, map[string]any, *coretools.RunContext) (any, error) {
	return func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
		code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
		if !tsCodePattern.MatchString(code) {
			return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
		}
		cmd := exec.CommandContext(ctx, pythonBin, script, code, part)
		cmd.Env = basicEnv()
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			captured := strings.TrimSpace(out.String())
			if captured == "" {
				return "", fmt.Errorf("读取板块失败: %v", err)
			}
			return "", fmt.Errorf("读取板块失败: %s", captured)
		}
		text := strings.TrimSpace(out.String())
		if text == "" {
			return "", errors.New("板块没有返回")
		}
		return text, nil
	}
}

func (s *Server) runPart(pythonBin, script, part string) func(context.Context, map[string]any, *coretools.RunContext) (any, error) {
	return func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
		code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
		if !tsCodePattern.MatchString(code) {
			return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
		}
		cmd := exec.CommandContext(ctx, pythonBin, script, code, part)
		cmd.Env = basicEnv()
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			captured := strings.TrimSpace(out.String())
			if captured == "" {
				return "", fmt.Errorf("读取基本面失败: %v", err)
			}
			return "", fmt.Errorf("读取基本面失败: %s", captured)
		}
		text := strings.TrimSpace(out.String())
		if text == "" {
			return "", errors.New("基本面没有返回")
		}
		return text, nil
	}
}

func (s *Server) skillScript(skillName, rel string) (string, error) {
	if s.skills == nil {
		return "", fmt.Errorf("%s 还没有安装", skillName)
	}
	def, err := s.skills.Get(context.Background(), skillName, skill.LookupOptions{})
	if err != nil || def == nil || def.ResourceBase == nil || def.ResourceBase.Path == "" {
		return "", fmt.Errorf("%s 还没有安装", skillName)
	}
	path := filepath.Join(def.ResourceBase.Path, rel)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("脚本不存在: %s", path)
	}
	return path, nil
}

func (s *Server) registerNewsTool(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "news-raw") {
		return nil
	}
	tool, err := s.fetchNewsTool()
	if err != nil {
		return err
	}
	_, err = toolRegistry.Register(tool)
	return err
}

func (s *Server) fetchNewsTool() (coretools.ToolDefinition, error) {
	script, err := s.newsScript()
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
		Name:        "fetch_news",
		Description: "按用户原话搜索新闻，不限于当前股票。keyword 传要搜的内容，例如最新的财经资讯、物理AI、国家政策。不要在前面加股票名称。走股票服务 /api/stocks/news，不能改走 Tushare。",
		Parameters: coretools.ParameterSchema{
			"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
			"keyword": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "主题词，可空。例如 储存"}},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		TimeoutMs:         40_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
			if !tsCodePattern.MatchString(code) {
				return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
			}
			keyword, _ := args["keyword"].(string)
			argv := []string{pythonBin, script, code}
			if strings.TrimSpace(keyword) != "" {
				argv = append(argv, strings.TrimSpace(keyword))
			}
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Env = basicEnv()
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				captured := strings.TrimSpace(out.String())
				if captured == "" {
					return "", fmt.Errorf("读取新闻原文失败: %v", err)
				}
				return "", fmt.Errorf("读取新闻原文失败: %s", captured)
			}
			text := strings.TrimSpace(out.String())
			if text == "" {
				return "", errors.New("新闻原文没有返回")
			}
			return text, nil
		},
	}, nil
}

func (s *Server) newsScript() (string, error) {
	if s.skills == nil {
		return "", errors.New("news-raw 还没有安装")
	}
	def, err := s.skills.Get(context.Background(), "news-raw", skill.LookupOptions{})
	if err != nil || def == nil || def.ResourceBase == nil || def.ResourceBase.Path == "" {
		return "", errors.New("news-raw 还没有安装")
	}
	path := filepath.Join(def.ResourceBase.Path, "scripts", "fetch_news.py")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("新闻原文脚本不存在: %s", path)
	}
	return path, nil
}

func (s *Server) fetchBasicTool() (coretools.ToolDefinition, error) {
	script, err := s.basicScript()
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
		Name:        "fetch_basic",
		Description: "读取这只股票的基本行情卡片：现价、涨跌、市值、PE、PB、ROE。只能传股票代码。走股票服务 /api/stocks/basic，不能改走 Tushare。",
		Parameters: coretools.ParameterSchema{
			"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
		},
		Output: coretools.Output{
			Schema: coretools.ValueSchema{Kind: coretools.KindString},
			Render: renderStringResult,
		},
		TimeoutMs:         30_000,
		IsConcurrencySafe: func(map[string]any) bool { return true },
		Execute: func(ctx context.Context, args map[string]any, _ *coretools.RunContext) (any, error) {
			code := strings.ToUpper(strings.TrimSpace(fmt.Sprint(args["ts_code"])))
			if !tsCodePattern.MatchString(code) {
				return "", fmt.Errorf("ts_code 必须是 600519.SH 这种代码")
			}
			cmd := exec.CommandContext(ctx, pythonBin, script, code)
			cmd.Env = basicEnv()
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				captured := strings.TrimSpace(out.String())
				if captured == "" {
					return "", fmt.Errorf("读取基本行情失败: %v", err)
				}
				return "", fmt.Errorf("读取基本行情失败: %s", captured)
			}
			text := strings.TrimSpace(out.String())
			if text == "" {
				return "", errors.New("基本行情没有返回")
			}
			return text, nil
		},
	}, nil
}

func (s *Server) basicScript() (string, error) {
	if s.skills == nil {
		return "", errors.New("stock-basic 还没有安装")
	}
	def, err := s.skills.Get(context.Background(), "stock-basic", skill.LookupOptions{})
	if err != nil || def == nil || def.ResourceBase == nil || def.ResourceBase.Path == "" {
		return "", errors.New("stock-basic 还没有安装")
	}
	path := filepath.Join(def.ResourceBase.Path, "scripts", "fetch_basic.py")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("基本行情脚本不存在: %s", path)
	}
	return path, nil
}

func basicEnv() []string {
	env := make([]string, 0, 8)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "TUSHARE_") || strings.HasPrefix(item, "STOCK_API_BASE=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "STOCK_API_BASE="+stockAPIBase())
}

func factsScript() (string, error) {
	root, err := agentsRoot()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "_shared", "fetch_facts.py")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("取事实脚本不存在: %s", path)
	}
	return path, nil
}

func factsEnv(cfg agentConfig) []string {
	env := make([]string, 0, 8)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "TUSHARE_") || strings.HasPrefix(item, "STOCK_FACTS_") || strings.HasPrefix(item, "STOCK_API_BASE=") {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"STOCK_FACTS_DIMENSION="+cfg.Dimension,
		"STOCK_API_BASE="+stockAPIBase(),
		"STOCK_FACTS_QUERY="+detailQuery(cfg.DetailQuery),
	)
	return env
}

func detailQuery(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	q := url.Values{}
	for _, key := range keys {
		q.Set(key, values[key])
	}
	return q.Encode()
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func loadAgent(id string) (agentConfig, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return agentConfig{}, fmt.Errorf("没有这位分析师: %s", id)
	}
	root, err := agentsRoot()
	if err != nil {
		return agentConfig{}, err
	}
	dir := filepath.Join(root, id)
	raw, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		return agentConfig{}, fmt.Errorf("没有这位分析师: %s", id)
	}
	var cfg agentConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return agentConfig{}, fmt.Errorf("分析师配置无法读取: %s", id)
	}
	if cfg.ID != id {
		return agentConfig{}, fmt.Errorf("分析师 %s 的 id 与目录不一致", id)
	}
	if cfg.Speak == "" {
		cfg.Speak = "headline"
	}
	if cfg.Speak != "headline" && cfg.Speak != "points" && cfg.Speak != "chief" {
		return agentConfig{}, fmt.Errorf("分析师 %s 的 speak 无法识别", id)
	}
	if cfg.Name == "" || len(cfg.Skills) == 0 || (cfg.Speak == "headline" && (cfg.Dimension == "" || len(cfg.Tools) == 0)) {
		return agentConfig{}, fmt.Errorf("分析师 %s 的配置不完整", id)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
	if err != nil {
		return agentConfig{}, fmt.Errorf("分析师 %s 缺少 prompt.md", id)
	}
	rules, err := os.ReadFile(filepath.Join(dir, "rules.md"))
	if err != nil {
		return agentConfig{}, fmt.Errorf("分析师 %s 缺少 rules.md", id)
	}
	cfg.Prompt = string(prompt)
	cfg.Rules = string(rules)
	return cfg, nil
}

func agentsRoot() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("HARNESS_AGENTS_DIR")); dir != "" {
		return dir, nil
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 6; i++ {
			for _, rel := range []string{"agents", filepath.Join("web", "agents")} {
				cand := filepath.Join(dir, rel)
				if st, err := os.Stat(filepath.Join(cand, "volume_flow", "agent.yaml")); err == nil && !st.IsDir() {
					return cand, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", errors.New("找不到 Agent 配置目录")
}

func stockAPIBase() string {
	if value := strings.TrimSpace(os.Getenv("STOCK_API_BASE")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "http://127.0.0.1:8090"
}

func replyAnswer(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if speak, err := parseSpeak(text); err == nil && strings.TrimSpace(speak.Text) != "" {
		return strings.TrimSpace(speak.Text)
	}
	text = strings.TrimPrefix(text, "```markdown")
	text = strings.TrimPrefix(text, "```md")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	return strings.TrimSpace(text)
}

func parseSpeak(raw string) (analystSpeak, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	text = strings.TrimSpace(text)
	var speak analystSpeak
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		speak = analystSpeak{}
		if err := dec.Decode(&speak); err != nil {
			continue
		}
		if strings.TrimSpace(speak.Text) != "" || speak.FactIDs != nil || speak.Status != "" {
			speak.Text = strings.TrimSpace(speak.Text)
			speak.Status = strings.TrimSpace(speak.Status)
			return speak, nil
		}
	}
	return analystSpeak{}, errors.New("没有 JSON 结论")
}

func headlineFromSpeak(speak analystSpeak, catalog map[string]factRow, cfg agentConfig) (map[string]any, bool) {
	if speak.Text == "" || forbiddenHit(speak.Text, cfg.Forbid) {
		return nil, false
	}
	label, known := statusTextOf[speak.Status]
	if !known {
		return nil, false
	}
	if len(catalog) == 0 {
		if len(speak.FactIDs) != 0 || !missingFactText(speak.Text) {
			return nil, false
		}
		return map[string]any{
			"text": speak.Text, "status": speak.Status, "status_text": label, "facts": []any{},
		}, true
	}
	if len(speak.FactIDs) == 0 {
		if missingFactText(speak.Text) {
			return map[string]any{
				"text": speak.Text, "status": speak.Status, "status_text": label, "facts": []any{},
			}, true
		}
		return nil, false
	}
	facts := make([]map[string]string, 0, len(speak.FactIDs))
	seen := map[string]bool{}
	for _, id := range speak.FactIDs {
		id = strings.TrimSpace(id)
		if cfg.FactPrefix != "" && !strings.HasPrefix(id, cfg.FactPrefix) {
			return nil, false
		}
		row, ok := catalog[id]
		if !ok || seen[id] {
			if !ok {
				return nil, false
			}
			continue
		}
		seen[id] = true
		name := row.Label
		if name == "" {
			name = id
		}
		facts = append(facts, map[string]string{"id": id, "label": name, "text": row.Text})
	}
	if len(facts) == 0 {
		return nil, false
	}
	return map[string]any{
		"text": speak.Text, "status": speak.Status, "status_text": label, "facts": facts,
	}, true
}

func forbiddenHit(text string, words []string) bool {
	for _, word := range words {
		if word != "" && strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func missingFactText(text string) bool {
	return strings.Contains(text, "没有取到") || strings.Contains(text, "没有数据") || strings.Contains(text, "没有事实")
}

func factsFromMessages(msgs []session.Message) map[string]factRow {
	out := map[string]factRow{}
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Kind != session.BlockToolResult || block.Text == "" {
				continue
			}
			absorbFacts(out, block.Text)
		}
	}
	return out
}

func absorbFacts(dst map[string]factRow, raw string) {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(raw[i:]))
		var doc struct {
			Facts []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
				Text  string `json:"text"`
			} `json:"facts"`
		}
		if err := dec.Decode(&doc); err != nil || len(doc.Facts) == 0 {
			continue
		}
		for _, fact := range doc.Facts {
			if fact.ID == "" {
				continue
			}
			dst[fact.ID] = factRow{ID: fact.ID, Label: fact.Label, Text: fact.Text}
		}
	}
}

func lastAssistantText(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != session.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, block := range msgs[i].Content {
			if block.Kind == session.BlockText {
				b.WriteString(block.Text)
			}
		}
		if strings.TrimSpace(b.String()) != "" {
			return b.String()
		}
	}
	return ""
}

func lastTurnError(sess *session.Session) string {
	events := sess.Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "turn/end" {
			continue
		}
		data, ok := events[i].Data.(map[string]any)
		if !ok {
			return ""
		}
		errText, _ := data["error"].(string)
		return strings.TrimSpace(errText)
	}
	return ""
}

func (s *Server) groupLines(code string) []groupLine {
	s.groupsMu.Lock()
	defer s.groupsMu.Unlock()
	if s.groups == nil {
		s.groups = map[string][]groupLine{}
	}
	if lines, ok := s.groups[code]; ok {
		return append([]groupLine(nil), lines...)
	}
	lines := readGroupFile(s.groupPath(code))
	s.groups[code] = lines
	return append([]groupLine(nil), lines...)
}

func (s *Server) appendGroup(code string, line groupLine) {
	s.groupsMu.Lock()
	defer s.groupsMu.Unlock()
	if s.groups == nil {
		s.groups = map[string][]groupLine{}
	}
	lines := s.groups[code]
	if lines == nil {
		lines = readGroupFile(s.groupPath(code))
	}
	if line.Day == "" {
		line.Day = diagnosisDay(time.Now())
	}
	lines = append(lines, line)
	if len(lines) > maxGroupLines {
		lines = lines[len(lines)-maxGroupLines:]
	}
	s.groups[code] = lines
	_ = writeGroupFile(s.groupPath(code), lines)
}

func (s *Server) handleGroupRecord(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("股票代码无法识别: %s", code))
		return
	}
	writeJSON(w, http.StatusOK, s.groupRecord(code))
}

func (s *Server) groupRecord(code string) map[string]any {
	view := latestGroupView(s.groupLines(code))
	if len(view) == 0 {
		return map[string]any{"has_record": false, "lines": []groupLine{}}
	}
	file := s.loadDebate(code)
	out := map[string]any{"has_record": true, "lines": view}
	debate := map[string]any{}
	if len(file.Bull) > 0 {
		debate["bull"] = pointPayload(file.Bull, file.Chief)
	}
	if len(file.Bear) > 0 {
		debate["bear"] = pointPayload(file.Bear, file.Chief)
	}
	if file.Chief != nil {
		debate["chief"] = chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, file.Chief, file)
	}
	if len(debate) > 0 {
		out["debate"] = debate
	}
	return out
}

// latestGroupView is the last finished round plus questions asked after it.
// Repeated openings stay in the log; the page shows only the latest one.
func latestGroupView(lines []groupLine) []groupLine {
	if len(lines) == 0 {
		return nil
	}
	lastChief := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Agent == "chief" && lines[i].Kind == "analysis" && strings.TrimSpace(lines[i].Text) != "" {
			lastChief = i
			break
		}
	}
	start := 0
	if lastChief >= 0 {
		for i := lastChief - 1; i >= 0; i-- {
			if lines[i].Agent == "chief" && lines[i].Kind == "analysis" {
				start = i + 1
				break
			}
		}
	}
	picked := map[string]groupLine{}
	var replies []groupLine
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if line.Kind == "reply" && strings.TrimSpace(line.Text) != "" && (lastChief < 0 || i > lastChief) {
			replies = append(replies, line)
			continue
		}
		if lastChief >= 0 && i > lastChief {
			continue
		}
		if line.Kind == "analysis" && strings.TrimSpace(line.Text) != "" {
			picked[line.Agent] = line
		}
	}
	var out []groupLine
	for _, id := range []string{"volume_flow", "sector", "news", "finance", "fundamental", "bull", "bear", "chief"} {
		if line, ok := picked[id]; ok {
			out = append(out, line)
		}
	}
	out = append(out, replies...)
	return out
}

func (s *Server) groupPath(code string) string {
	if strings.TrimSpace(s.cfg.DataDir) == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "groups", sanitizeCode(code)+".json")
}

func sanitizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, code)
}

func readGroupFile(path string) []groupLine {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Lines []groupLine `json:"lines"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	return doc.Lines
}

func writeGroupFile(path string, lines []groupLine) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]any{"lines": lines})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

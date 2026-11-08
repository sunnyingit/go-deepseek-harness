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
	// neutral 展示为「中性」，与主分析师裁决「不明」区分开，避免详情里大盘中性被看成整单不明。
	statusTextOf = map[string]string{"support": "偏多", "warn": "偏空", "neutral": "中性"}
)

// groupLine is one spoken turn in a stock's shared 研判群. Fact sheets stay
// out of this log; the next analyst only sees what was actually said.
type groupLine struct {
	Agent              string           `json:"agent"`
	Name               string           `json:"name"`
	Kind               string           `json:"kind"`
	Quote              string           `json:"quote,omitempty"`
	Text               string           `json:"text"`
	Status             string           `json:"status,omitempty"`
	StatusText         string           `json:"status_text,omitempty"`
	SuggestedQuestions []map[string]any `json:"suggested_questions,omitempty"`
	Tools              []map[string]string `json:"tools,omitempty"`
	Day                string              `json:"day,omitempty"`
}

type analystSpeak struct {
	Text       string   `json:"text"`
	Status     string   `json:"status"`
	StatusText string   `json:"status_text"`
	FactIDs    []string `json:"fact_ids"`
	// 开场契约：stance/strength + text（板块/资金流统一）；summary 兼容旧输出
	Stance             string           `json:"stance"`
	Strength           string           `json:"strength"`
	Summary            string           `json:"summary"`
	SuggestedQuestions []map[string]any `json:"suggested_questions"`
}

type factRow struct {
	ID    string
	Label string
	Text  string
}

func (s *Server) handleAnalystStream(w http.ResponseWriter, r *http.Request) {
	s.handleAnalyst(w, r, "analysis", "")
}

// maxQuestionBytes 追问正文的上限。多分析师串联回答时，正文可能较长，
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
	if strings.HasPrefix(trimmed, "{") || strings.Contains(trimmed, `"text"`) || strings.Contains(trimmed, `"summary"`) {
		parts := jsonTextFields(buf)
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n")
	}
	return buf
}

// jsonTextFields pulls every JSON "text"/"summary" string out of a partial object.
// The last one may still be unfinished; it is shown as far as it has arrived.
func jsonTextFields(buf string) []string {
	var out []string
	rest := buf
	for {
		iText := strings.Index(rest, `"text"`)
		iSum := strings.Index(rest, `"summary"`)
		i := -1
		keyLen := 0
		switch {
		case iText < 0 && iSum < 0:
			return out
		case iText < 0:
			i, keyLen = iSum, len(`"summary"`)
		case iSum < 0:
			i, keyLen = iText, len(`"text"`)
		case iText < iSum:
			i, keyLen = iText, len(`"text"`)
		default:
			i, keyLen = iSum, len(`"summary"`)
		}
		rest = strings.TrimLeft(rest[i+keyLen:], " \t\r\n")
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
	label := analystToolLabel(name)
	if label == "" {
		return ""
	}
	return "正在" + label
}

// analystToolLabel is the short Chinese description shown in the bubble's
// collapsed「调用了 N 个工具」list (and reused by the live status note).
func analystToolLabel(name string) string {
	switch name {
	case "fetch_facts":
		return "读取事实"
	case "fetch_basic":
		return "查行情"
	case "load_skill", "list_skills":
		return "加载技能"
	case "render_chart":
		return "画图"
	case "fetch_news":
		return "搜索新闻"
	case "fetch_baidu_board":
		return "看百度财经榜"
	case "fetch_sector_membership":
		return "查板块归属"
	case "fetch_sector_tape":
		return "看板块走势"
	case "fetch_sector_position":
		return "看成分位置"
	case "fetch_sector_rank":
		return "排行业强弱"
	case "fetch_sector_sw":
		return "读申万日线"
	case "fetch_theme_heat":
		return "看今日题材"
	case "fetch_sector_dc":
		return "读东财板块"
	case "fetch_flow_dc":
		return "读个股资金"
	case "fetch_flow_factor":
		return "读技术指标"
	case "fetch_flow_chip":
		return "读筹码"
	case "fetch_flow_bar":
		return "读行情"
	case "fetch_flow_premarket":
		return "读涨跌停价"
	case "fetch_flow_limit":
		return "查涨跌停"
	case "fetch_flow_toplist":
		return "查龙虎榜"
	case "fetch_flow_margin":
		return "读融资"
	case "fetch_company", "fetch_business", "fetch_valuation", "fetch_quality", "fetch_holders",
		"fetch_forecast", "fetch_express", "fetch_share_float", "fetch_survey", "fetch_mainbz", "fetch_dividend":
		return "读基本面"
	case "fetch_chain":
		return "看产业链"
	case "fetch_market_fund":
		return "读市场资金"
	case "fetch_market_team":
		return "读国家队"
	case "fetch_market_sector":
		return "读市场板块"
	case "fetch_market_mood":
		return "读好不好做"
	case "fetch_market_cal":
		return "读市场日历"
	case "fetch_market_macro":
		return "读宏观"
	default:
		if name == "" {
			return ""
		}
		return "查询"
	}
}

func toolArgsNote(args map[string]any) string {
	if args == nil {
		return ""
	}
	for _, key := range []string{"part", "focus", "window", "days", "skill", "name"} {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			continue
		}
		if len([]rune(s)) > 24 {
			s = string([]rune(s)[:24]) + "…"
		}
		return s
	}
	return ""
}

func toolsFromMessages(msgs []session.Message) []map[string]string {
	var out []map[string]string
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Kind != session.BlockToolCall || block.ToolName == "" {
				continue
			}
			label := analystToolLabel(block.ToolName)
			if label == "" {
				continue
			}
			row := map[string]string{"name": block.ToolName, "label": label}
			if note := toolArgsNote(block.Args); note != "" {
				row["note"] = note
			}
			out = append(out, row)
		}
	}
	return out
}

func withTools(msg map[string]any, tools []map[string]string) map[string]any {
	if msg == nil || len(tools) == 0 {
		return msg
	}
	msg["tools"] = tools
	return msg
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
	Speak string `yaml:"speak"`
	Prompt string `yaml:"-"`
	Rules  string `yaml:"-"`
	// Reply 可选：web/agents/<id>/reply.md；有则追问只用它，避免开场 JSON 示例带节奏。
	Reply string `yaml:"-"`
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
	var draftTools []map[string]string
	lastActivity := time.Now()
	started := time.Now()
	pushDraft := func(text, status string) {
		payload := map[string]any{"text": text, "status": status}
		if len(draftTools) > 0 {
			payload["tools"] = draftTools
		}
		writeAnalystSSE(w, flusher, "draft", payload)
	}
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
						pushDraft(draft, lastStatus)
						lastActivity = time.Now()
					}
				case "tool/call":
					call, ok := data["call"].(session.ContentBlock)
					if !ok {
						continue
					}
					if label := analystToolLabel(call.ToolName); label != "" {
						row := map[string]string{"name": call.ToolName, "label": label}
						if note := toolArgsNote(call.Args); note != "" {
							row["note"] = note
						}
						draftTools = append(draftTools, row)
					}
					status := analystToolStatus(call.ToolName)
					if status != "" {
						lastStatus = status
					}
					pushDraft(lastDraft, lastStatus)
					lastActivity = time.Now()
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
	tools := toolsFromMessages(msgs)
	if cfg.oneShot() {
		msg, err := s.finishOneShot(cfg, code, kind, question, lastAssistantText(msgs), lastTurnError(sess))
		return withChart(withTools(msg, tools), charts), err
	}
	if kind == "reply" {
		text, replyErr := replyAnswer(lastAssistantText(msgs))
		if replyErr != nil {
			return nil, replyErr
		}
		if cfg.ID == "volume_flow" {
			text = compactFlowReply(text)
		}
		if text == "" || forbiddenHit(text, cfg.Forbid) {
			if text == "" {
				if turnErr := lastTurnError(sess); turnErr != "" {
					return nil, errors.New(turnErr)
				}
				if n := len(tools); n > 0 {
					return nil, fmt.Errorf("调用了 %d 个工具但没有写出正文，请再问一次", n)
				}
				return nil, errors.New("没有写出回答正文，请再问一次")
			}
			return nil, errors.New("回答触碰了禁止表述，请换个问法")
		}
		s.appendGroup(code, groupLine{
			Agent: cfg.ID, Name: cfg.Name, Kind: kind, Quote: question, Text: text, Tools: tools,
		})
		return withChart(withTools(analystMessage(cfg, kind, question, text, nil), tools), charts), nil
	}
	catalog := factsFromMessages(msgs)
	rawSpeak := lastParseableAssistantText(msgs)
	speak, speakErr := parseSpeak(rawSpeak)
	if speakErr != nil {
		if turnErr := lastTurnError(sess); turnErr != "" {
			return nil, errors.New(turnErr)
		}
		if strings.TrimSpace(rawSpeak) == "" {
			if n := len(tools); n > 0 {
				return nil, fmt.Errorf("开场调用了 %d 个工具但没有写出 JSON 结论（可能步数用尽还在调工具）", n)
			}
			return nil, errors.New("开场没有写出 JSON 结论")
		}
		logUnparsedSpeak(cfg.ID, code, rawSpeak)
		return nil, errors.New("开场没有给出可用的 JSON 结论（常见原因：正文里的英文引号弄破了 JSON，或步数用尽还在调工具）")
	}
	headline, ok := headlineFromSpeak(speak, catalog, cfg)
	if !ok {
		if turnErr := lastTurnError(sess); turnErr != "" {
			return nil, errors.New(turnErr)
		}
		return nil, errors.New("开场结论未通过校验（status/fact_ids 或事实表对不上）")
	}
	text, _ := headline["text"].(string)
	line := groupLine{
		Agent: cfg.ID, Name: cfg.Name, Kind: kind, Quote: question,
		Text: text, Status: speak.Status, StatusText: statusTextOf[speak.Status], Tools: tools,
	}
	if qs, _ := headline["suggested_questions"].([]map[string]any); len(qs) > 0 {
		line.SuggestedQuestions = qs
	}
	s.appendGroup(code, line)
	return withChart(withTools(analystMessage(cfg, kind, question, text, headline), tools), charts), nil
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
	fmt.Fprintf(&b, "当前时间：%s\n", time.Now().Format("2006-01-02 15:04"))
	if strings.TrimSpace(question) == "" {
		opening := strings.TrimSpace(cfg.Opening)
		if opening == "" {
			opening = "请给出结论。"
		}
		b.WriteString(opening + "\n")
		b.WriteString("（开场任务；输出格式以系统提示 / rules 为准）\n")
	} else {
		fmt.Fprintf(&b, "用户提问：%s\n", strings.TrimSpace(question))
		b.WriteString("（追问任务；按系统提示回答）\n")
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
	lines = sanitizeFlowGroupContext(cfg, question, lines)
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

// configPrompt 只拼配置文件。body 为 prompt.md 或 reply.md。
func (c agentConfig) configPrompt(body string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(body))
	if rules := strings.TrimSpace(c.Rules); rules != "" {
		b.WriteString("\n\n")
		b.WriteString(rules)
	}
	if len(c.Skills) > 0 {
		fmt.Fprintf(&b, "\n\n已安装技能：%s。先 load_skill 加载需要的技能，再调用工具。\n", strings.Join(c.Skills, "、"))
	}
	return b.String()
}

func (c agentConfig) systemPrompt() string { return c.configPrompt(c.Prompt) }

// replyPrompt：若有 reply.md 则只用追问配置，避免开场 JSON 示例带节奏。
func (c agentConfig) replyPrompt() string {
	if body := strings.TrimSpace(c.Reply); body != "" {
		return c.configPrompt(body)
	}
	return c.configPrompt(c.Prompt)
}

func (c agentConfig) oneShotReplyPrompt() string { return c.replyPrompt() }

func (s *Server) buildConfiguredAgent(sess *session.Session, cfg agentConfig, charts *chartBox, kind string) (*agentloop.Agent, error) {
	toolRegistry := coretools.NewRegistry()
	work := cfg
	if kind != "reply" {
		// 开场收窄技能列表，避免提示里仍列出追问专用技能（新闻尤甚）
		work.Skills = cfg.openingSkills()
	}
	if work.oneShot() {
		if _, err := toolRegistry.Register(listSkillsTool(s.skills, work.Skills)); err != nil {
			return nil, err
		}
		if _, err := toolRegistry.Register(loadSkillTool(s.skills, work.Skills)); err != nil {
			return nil, err
		}
		if err := s.registerBasicTool(toolRegistry, work); err != nil {
			return nil, err
		}
		steps := work.MaxSteps
		if steps <= 0 {
			steps = 3
		}
		prompt := work.systemPrompt()
		if kind == "reply" {
			prompt = work.oneShotReplyPrompt()
			if steps < 6 {
				steps = 6
			}
		}
		return s.newAnalyst(sess, toolRegistry, steps, prompt), nil
	}
	if _, err := toolRegistry.Register(listSkillsTool(s.skills, work.Skills)); err != nil {
		return nil, err
	}
	if _, err := toolRegistry.Register(loadSkillTool(s.skills, work.Skills)); err != nil {
		return nil, err
	}
	for _, name := range work.Tools {
		switch name {
		case "fetch_facts":
			fetch, err := s.fetchFactsTool(work)
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
	if containsSkill(work.Skills, "d3js-visualization") {
		chartTool, err := s.renderChartTool(charts)
		if err != nil {
			return nil, err
		}
		if _, err := toolRegistry.Register(chartTool); err != nil {
			return nil, err
		}
	}
	if err := s.registerBasicTool(toolRegistry, work); err != nil {
		return nil, err
	}
	// 新闻开场禁止搜新闻/热搜；追问才注册，避免步数被吃光
	if kind == "reply" {
		if err := s.registerNewsTool(toolRegistry, work); err != nil {
			return nil, err
		}
		if err := s.registerBaiduBoardTool(toolRegistry, work); err != nil {
			return nil, err
		}
	}
	if err := s.registerFundamentalTools(toolRegistry, work); err != nil {
		return nil, err
	}
	if err := s.registerSectorTools(toolRegistry, work); err != nil {
		return nil, err
	}
	if err := s.registerMarketTools(toolRegistry, work); err != nil {
		return nil, err
	}
	if err := s.registerFlowTools(toolRegistry, work); err != nil {
		return nil, err
	}
	steps := work.MaxSteps
	if steps <= 0 {
		steps = maxAnalystSteps
	}
	prompt := work.systemPrompt()
	maxTokens := 2048
	if kind == "reply" {
		prompt = work.replyPrompt()
		if work.ID == "volume_flow" {
			// 总览追问需要展开写，不要再卡短输出
			if steps < 6 {
				steps = 6
			}
			maxTokens = 2048
		}
	}
	return s.newAnalyst(sess, toolRegistry, steps, prompt, maxTokens), nil
}

// openingSkills 开场可用技能。新闻/基本面/财报去掉展示类技能，避免开场空转调工具。
func (c agentConfig) openingSkills() []string {
	var keep map[string]bool
	switch c.ID {
	case "news":
		keep = map[string]bool{"news-facts": true, "stock-basic": true}
	case "fundamental":
		keep = map[string]bool{"fundamental-facts": true, "stock-basic": true}
	case "finance":
		keep = map[string]bool{"finance-facts": true, "stock-basic": true}
	case "market":
		keep = map[string]bool{"market-facts": true, "stock-basic": true}
	default:
		return c.Skills
	}
	out := make([]string, 0, 2)
	for _, name := range c.Skills {
		if keep[name] {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return c.Skills
	}
	return out
}

func (s *Server) newAnalyst(sess *session.Session, toolRegistry *coretools.Registry, steps int, prompt string, maxTokens ...int) *agentloop.Agent {
	tok := 2048
	if len(maxTokens) > 0 && maxTokens[0] > 0 {
		tok = maxTokens[0]
	}
	return agentloop.NewAgent(sess, s.llm, coretools.Bridge{Registry: toolRegistry}, prompt, agentloop.Hooks{
		PreStep: func(_ context.Context, _ *agentloop.Agent, _, step int, messages []session.Message) (bool, []session.Message) {
			if step > steps {
				return false, nil
			}
			return true, messages
		},
		RequestError: llmretry.Hook(s.cfg.Retry),
	}).WithModel(s.cfg.Model).WithTemperature(0.2).WithMaxTokens(tok)
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

func (s *Server) registerMarketTools(toolRegistry *coretools.Registry, cfg agentConfig) error {
	if !containsSkill(cfg.Skills, "market-reply") {
		return nil
	}
	script, err := s.skillScript("market-reply", "scripts/fetch_part.py")
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
		{"fetch_market_fund", "读取市场资金：两市成交、宽基 ETF、两融。追问市场资金时用。不是个股大单。"},
		{"fetch_market_team", "读取国家队相关：宽基份额托市/异动口径。没取到就如实说，不要编申购金额。"},
		{"fetch_market_sector", "读取全市场板块轮动与分桶。不是这只股的板块归属。"},
		{"fetch_market_mood", "读取好不好做：宽度、涨跌停、情绪阶段。"},
		{"fetch_market_cal", "读取市场日历与近期事件。"},
		{"fetch_market_macro", "读取宏观摘要。"},
	}
	for _, part := range parts {
		name := strings.TrimPrefix(part.name, "fetch_market_")
		tool := coretools.ToolDefinition{
			Name:        part.name,
			Description: part.text + "走市场服务，不能改走 Tushare。",
			Parameters: coretools.ParameterSchema{
				"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH（市场级数据与代码无关，但接口需要）"}, Required: true},
			},
			Output: coretools.Output{
				Schema: coretools.ValueSchema{Kind: coretools.KindString},
				Render: renderStringResult,
			},
			TimeoutMs:         120_000,
			IsConcurrencySafe: func(map[string]any) bool { return true },
			Execute:           s.runPart(pythonBin, script, name),
		}
		if _, err := toolRegistry.Register(tool); err != nil {
			return err
		}
	}
	return nil
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
		{"fetch_chain", "读取产业链位置：所处行业层级、概念、主营按产品的占比和毛利率、销售区域、公司披露的下游客户和上游原料、下游景气、同业可比公司。追问上下游、在产业链哪一环、行业景气时用。只能传股票代码。上下游只是公司自己披露的原文，没写的就是没披露；同业不是上下游。", "走基本面追问接口，不能改走 Tushare。"},
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
	// 追问切片脚本挂在 flow-facts 下；有无 flow-reply 技能不影响注册。
	if cfg.ID != "volume_flow" {
		return nil
	}
	script, err := s.skillScript("flow-facts", "scripts/fetch_part.py")
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
		{"fetch_flow_dc", "读取东财个股资金流：近 1、3、5、10、20、60 日分档净流入与逐日，并附带算法层路径/力度/结构摘要（fl.path、fl.level、fl.structure、fl.price_flow；short 主窗 3 日）。用户问具体天数可传 days。不要把净流入叫做主力。"},
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
		{"fetch_sector_membership", "读取申万一级到三级，以及挂了哪些同花顺概念。追问属于哪个板块时用。"},
		{"fetch_sector_tape", "读取主板块阶段、相对沪深300，以及板块资金。默认给出近 5/20/60/120 日多窗口；传 days（1～120）可指定单一窗口。"},
		{"fetch_sector_position", "读取市值名次、涨幅名次、领涨和垫底的几只，以及个股大资金是否跟上。资金默认多窗口（5/20/60/120）；可传 days。不返回整张成分表。"},
		{"fetch_sector_rank", "读取申万一级涨幅排行和前后几名。默认近 60 日；可传 days（1～120）。"},
		{"fetch_sector_sw", "读取本股申万一级、二级、三级行业的最新收盘、涨跌、PE/PB、成交额和多窗口走势（默认 20/60/120 日，可传 days）。"},
		{"fetch_theme_heat", "读取主板块和次板块成分里的今日涨停、炸板、跌停、连板天梯和龙虎榜。追问是不是今天的主线时用。"},
		{"fetch_sector_dc", "读取东财行业和概念板块的今日涨跌、上涨下跌家数、领涨股、多窗口行情（默认最长 120 日，可传 days），以及东财人气榜。这是东财分类，不要和申万、同花顺并成同一个板块。"},
	}
	for _, part := range parts {
		name := strings.TrimPrefix(part.name, "fetch_")
		name = strings.TrimPrefix(name, "sector_")
		name = strings.TrimPrefix(name, "theme_")
		params := coretools.ParameterSchema{
			"ts_code": {ValueSchema: coretools.ValueSchema{Kind: coretools.KindString, Description: "股票代码，例如 600519.SH"}, Required: true},
		}
		if name != "membership" && name != "heat" {
			params["days"] = coretools.Param{
				ValueSchema: coretools.ValueSchema{Kind: coretools.KindNumber, Description: "要看多少个交易日，1 到 120。不传则用默认多窗口（含 60/120 日），不要以为最长只有 20 日。"},
			}
		}
		tool := coretools.ToolDefinition{
			Name:        part.name,
			Description: part.text + "走板块服务，不能改走 Tushare。",
			Parameters:  params,
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
	if reply, err := os.ReadFile(filepath.Join(dir, "reply.md")); err == nil {
		cfg.Reply = string(reply)
	}
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

var errOpeningJSONOnReply = errors.New("追问请用 markdown 展开分析，不要套开场摘要格式；请再问一次")

func replyAnswer(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", nil
	}
	// 追问误用开场 JSON → 硬拒，避免再展示「倾向+一句摘要」
	if speak, err := parseSpeak(text); err == nil {
		normalizeSpeak(&speak)
		if speak.Stance != "" || speak.Summary != "" || speak.Strength != "" {
			return "", errOpeningJSONOnReply
		}
		if strings.TrimSpace(speak.Text) != "" {
			return strings.TrimSpace(speak.Text), nil
		}
	}
	text = strings.TrimPrefix(text, "```markdown")
	text = strings.TrimPrefix(text, "```md")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	return strings.TrimSpace(text), nil
}

// sanitizeFlowGroupContext 追问时压缩群里旧资金流长文，避免模型照抄退回九步。
func sanitizeFlowGroupContext(cfg agentConfig, question string, lines []groupLine) []groupLine {
	if cfg.ID != "volume_flow" || strings.TrimSpace(question) == "" || len(lines) == 0 {
		return lines
	}
	out := make([]groupLine, len(lines))
	copy(out, lines)
	for i := range out {
		if out[i].Agent != "volume_flow" && !strings.Contains(out[i].Name, "资金流") {
			continue
		}
		out[i].Text = compactFlowReply(out[i].Text)
		if n := len([]rune(out[i].Text)); n > 280 {
			out[i].Text = string([]rune(out[i].Text)[:280]) + "…"
		}
	}
	return out
}

// compactFlowReply 拦住退回旧版九步长文：优先保留「结论/依据」，否则截到第一章之前。
func compactFlowReply(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !flowReplyLooksLikeLegacyReport(text) {
		return text
	}
	if out := extractFlowConclusionBlocks(text); out != "" {
		return out
	}
	for _, mark := range []string{
		"\n### 一、", "\n## 一、", "\n一、价格", "\n### 二、",
		"\n## 资金流分析", "\n# 资金流分析",
	} {
		if i := strings.Index(text, mark); i > 0 {
			if head := strings.TrimSpace(text[:i]); head != "" {
				return head
			}
		}
	}
	// 全文都是旧骨架且没有结论段：只留开头一段，避免整篇倾泻
	runes := []rune(text)
	if len(runes) > 400 {
		return string(runes[:400]) + "…"
	}
	return text
}

func flowReplyLooksLikeLegacyReport(text string) bool {
	// 任一强标记即视为旧体检表；弱标记需凑够 2 个
	for _, c := range []string{
		"## 资金流分析", "### 一、价格", "一、价格与成交",
		"### 八、结论", "### 九、纪律", "背离清单",
	} {
		if strings.Contains(text, c) {
			return true
		}
	}
	hits := 0
	for _, c := range []string{"### 二、技术", "### 三、成交", "### 四、", "### 五、", "### 六、", "### 七、"} {
		if strings.Contains(text, c) {
			hits++
		}
	}
	return hits >= 2
}

func extractFlowConclusionBlocks(text string) string {
	// 抓 **结论** … **依据** …（到下一个 ** 大标题或文末）
	type block struct{ title, body string }
	var parts []block
	lines := strings.Split(text, "\n")
	var cur *block
	flush := func() {
		if cur == nil {
			return
		}
		cur.body = strings.TrimSpace(cur.body)
		if cur.body != "" {
			parts = append(parts, *cur)
		}
		cur = nil
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		name := ""
		switch {
		case strings.HasPrefix(trim, "**结论**"), trim == "结论", strings.HasPrefix(trim, "## 结论"), strings.HasPrefix(trim, "### 结论"):
			name = "结论"
		case strings.HasPrefix(trim, "**依据**"), trim == "依据", strings.HasPrefix(trim, "## 依据"), strings.HasPrefix(trim, "### 依据"):
			name = "依据"
		case strings.HasPrefix(trim, "**还要注意**"), strings.HasPrefix(trim, "## 还要注意"), strings.HasPrefix(trim, "### 还要注意"):
			name = "还要注意"
		}
		if name != "" {
			flush()
			cur = &block{title: name}
			rest := trim
			for _, p := range []string{"**结论**", "**依据**", "**还要注意**", "## 结论", "### 结论", "## 依据", "### 依据", "## 还要注意", "### 还要注意", "结论", "依据"} {
				if strings.HasPrefix(rest, p) {
					rest = strings.TrimSpace(strings.TrimPrefix(rest, p))
					rest = strings.TrimLeft(rest, "：: ")
					break
				}
			}
			if rest != "" && rest != name {
				cur.body = rest + "\n"
			}
			continue
		}
		if cur != nil {
			// 碰到旧九步标题则结束
			if strings.HasPrefix(trim, "### 一") || strings.HasPrefix(trim, "一、价格") || strings.HasPrefix(trim, "## 资金流") {
				flush()
				break
			}
			cur.body += line + "\n"
		}
	}
	flush()
	var out strings.Builder
	want := map[string]bool{"结论": true, "依据": true, "还要注意": true}
	for _, p := range parts {
		if !want[p.title] {
			continue
		}
		fmt.Fprintf(&out, "**%s**：%s\n\n", p.title, strings.TrimSpace(p.body))
	}
	return strings.TrimSpace(out.String())
}

func normalizeSpeak(speak *analystSpeak) {
	if speak == nil {
		return
	}
	if strings.TrimSpace(speak.Text) == "" {
		speak.Text = strings.TrimSpace(speak.Summary)
	} else {
		speak.Text = strings.TrimSpace(speak.Text)
	}
	speak.Summary = strings.TrimSpace(speak.Summary)
	speak.Stance = strings.TrimSpace(speak.Stance)
	speak.Strength = strings.TrimSpace(speak.Strength)
	speak.Status = strings.TrimSpace(speak.Status)
	speak.StatusText = strings.TrimSpace(speak.StatusText)
	if speak.Status == "" && speak.Stance != "" {
		speak.Status = stanceToStatus(speak.Stance)
	}
	if speak.StatusText == "" {
		if label, ok := statusTextOf[speak.Status]; ok {
			speak.StatusText = label
		} else if speak.Stance != "" {
			speak.StatusText = speak.Stance
		}
	}
}

func stanceToStatus(stance string) string {
	switch stance {
	case "偏多", "support", "bullish":
		return "support"
	case "偏空", "warn", "bearish":
		return "warn"
	case "中性", "不明", "neutral":
		return "neutral"
	default:
		return ""
	}
}

func parseSpeak(raw string) (analystSpeak, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	text = strings.TrimSpace(text)
	candidates := []string{text}
	if soft := softenSpeakJSONPunctuation(text); soft != text {
		candidates = append(candidates, soft)
	}
	for _, cand := range candidates {
		if speak, ok := decodeSpeakObject(cand); ok {
			return speak, nil
		}
		// 顶层 text/summary 与 suggested_questions[].text 里的裸引号都会弄破 JSON（新闻/财报尤甚）
		if fixed := repairSpeakQuotes(cand); fixed != cand {
			if speak, ok := decodeSpeakObject(fixed); ok {
				return speak, nil
			}
			if trimmed := stripTrailingJSONCommas(fixed); trimmed != fixed {
				if speak, ok := decodeSpeakObject(trimmed); ok {
					return speak, nil
				}
			}
		}
		if trimmed := stripTrailingJSONCommas(cand); trimmed != cand {
			if speak, ok := decodeSpeakObject(trimmed); ok {
				return speak, nil
			}
		}
	}
	for _, cand := range candidates {
		if speak, ok := looseSpeak(cand); ok {
			return speak, nil
		}
		if fixed := repairSpeakQuotes(cand); fixed != cand {
			if speak, ok := looseSpeak(fixed); ok {
				return speak, nil
			}
		}
	}
	return analystSpeak{}, errors.New("没有 JSON 结论")
}

// softenSpeakJSONPunctuation 把弯引号/全角引号收成 ASCII，便于再走裸引号修复。
func softenSpeakJSONPunctuation(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '“', '”', '„', '‟', '＂':
			b.WriteByte('"')
		case '‘', '’', '＇':
			b.WriteByte('\'')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripTrailingJSONCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func decodeSpeakObject(text string) (analystSpeak, bool) {
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
		normalizeSpeak(&speak)
		if !looksLikeSpeak(speak) {
			continue
		}
		return speak, true
	}
	return analystSpeak{}, false
}

// looksLikeSpeak 区分开场结论与 suggested_questions 条目（后者常只有 text）。
func looksLikeSpeak(speak analystSpeak) bool {
	return speak.Stance != "" || speak.Status != "" || speak.Summary != "" || speak.FactIDs != nil
}

// repairSpeakQuotes 把开场 JSON 字符串字段里未转义的 " 改成合法 JSON。
// 顶层 text/summary 与 suggested_questions 里的 text/focus/question 都要修，否则整段解不开。
func repairSpeakQuotes(raw string) string {
	out := raw
	for round := 0; round < 8; round++ {
		prev := out
		for _, key := range []string{`"text"`, `"summary"`, `"focus"`, `"question"`} {
			out = repairJSONStringFieldAll(out, key)
		}
		if out == prev {
			break
		}
	}
	return out
}

func repairJSONStringFieldAll(raw, key string) string {
	searchFrom := 0
	for {
		rel := strings.Index(raw[searchFrom:], key)
		if rel < 0 {
			return raw
		}
		keyAt := searchFrom + rel
		rest := strings.TrimLeft(raw[keyAt+len(key):], " \t\r\n")
		if !strings.HasPrefix(rest, ":") {
			searchFrom = keyAt + len(key)
			continue
		}
		afterColon := strings.TrimLeft(rest[1:], " \t\r\n")
		if !strings.HasPrefix(afterColon, `"`) {
			searchFrom = keyAt + len(key)
			continue
		}
		valueStart := len(raw) - len(afterColon) // position of opening quote in raw
		body, end, ok := scanBrokenJSONString(raw[valueStart:])
		if !ok {
			searchFrom = keyAt + len(key)
			continue
		}
		orig := raw[valueStart : valueStart+end]
		// 字段本身已是合法 JSON 字符串时不要再 Marshal，否则会把 \" 叠成 \\\\
		var unused string
		if json.Unmarshal([]byte(orig), &unused) == nil {
			searchFrom = valueStart + end
			continue
		}
		escaped, _ := json.Marshal(body)
		raw = raw[:valueStart] + string(escaped) + raw[valueStart+end:]
		searchFrom = valueStart + len(escaped)
	}
}

func scanBrokenJSONString(s string) (body string, end int, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", 0, false
	}
	var b strings.Builder
	esc := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		if esc {
			b.WriteByte(c)
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			if speakFieldEnd(s[i+1:]) {
				return b.String(), i + 1, true
			}
			// 正文里的裸引号，先当内容留下，稍后整体 json.Marshal 再转义
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
	}
	return "", 0, false
}

func speakFieldEnd(rest string) bool {
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" || rest[0] == '}' {
		return true
	}
	if rest[0] != ',' {
		return false
	}
	rest = strings.TrimLeft(rest[1:], " \t\r\n")
	for _, key := range []string{
		`"fact_ids"`, `"suggested_questions"`, `"stance"`, `"strength"`,
		`"status"`, `"status_text"`, `"summary"`, `"text"`,
		`"tool"`, `"focus"`, `"points"`,
	} {
		if strings.HasPrefix(rest, key) {
			return true
		}
	}
	return false
}

func headlineFromSpeak(speak analystSpeak, catalog map[string]factRow, cfg agentConfig) (map[string]any, bool) {
	normalizeSpeak(&speak)
	if speak.Text == "" || forbiddenHit(speak.Text, cfg.Forbid) {
		return nil, false
	}
	label, known := statusTextOf[speak.Status]
	if !known {
		return nil, false
	}
	text := speak.Text
	// 开场 text 软截：资金流约 120 字、板块约 150 字、新闻约 100 字；优先在句号处截断
	if cfg.ID == "volume_flow" {
		text = trimSummaryRunes(text, 120)
	}
	if cfg.ID == "sector" {
		text = trimSummaryRunes(text, 150)
	}
	if cfg.ID == "news" {
		text = trimSummaryRunes(text, 100)
	}
	if cfg.ID == "fundamental" {
		text = trimSummaryRunes(text, 120)
	}
	if cfg.ID == "finance" {
		text = trimSummaryRunes(text, 100)
	}
	if cfg.ID == "market" {
		text = trimSummaryRunes(text, 100)
	}
	pack := func(facts []map[string]string) map[string]any {
		out := map[string]any{
			"text": text, "status": speak.Status, "status_text": label, "facts": facts,
		}
		if speak.Strength != "" {
			out["strength"] = speak.Strength
		}
		if len(speak.SuggestedQuestions) > 0 {
			out["suggested_questions"] = speak.SuggestedQuestions
		}
		return out
	}
	if len(catalog) == 0 {
		if len(speak.FactIDs) != 0 || !missingFactText(speak.Text) {
			return nil, false
		}
		return pack([]map[string]string{}), true
	}
	if len(speak.FactIDs) == 0 {
		if missingFactText(speak.Text) {
			return pack([]map[string]string{}), true
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
	return pack(facts), true
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

// trimSummaryRunes 把开场 summary 压到 max 字以内，优先在句末（。！？；）截断，
// 其次在逗号/顿号处；找不到可切点才硬截。避免「大单-0.49」这种半截。
func trimSummaryRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	head := runes[:max]
	cutAt := func(seps string) int {
		best := -1
		for i, r := range head {
			if strings.ContainsRune(seps, r) {
				best = i
			}
		}
		return best
	}
	if i := cutAt("。！？；"); i >= 12 { // 至少留一句完整结论；太短才放弃软截
		return strings.TrimSpace(string(head[:i+1]))
	}
	if i := cutAt("，、"); i >= 12 {
		return strings.TrimSpace(string(head[:i+1]))
	}
	return string(head)
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
		if text := assistantMessageText(msgs[i]); text != "" {
			return text
		}
	}
	return ""
}

func assistantMessageText(msg session.Message) string {
	if msg.Role != session.RoleAssistant {
		return ""
	}
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Kind == session.BlockText {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// lastParseableAssistantText 从后往前找第一条能 parseSpeak 的助手正文；
// 若都解不开，退回最后一条非空正文（给错误提示用）。
func lastParseableAssistantText(msgs []session.Message) string {
	fallback := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		text := assistantMessageText(msgs[i])
		if text == "" {
			continue
		}
		if fallback == "" {
			fallback = text
		}
		if _, err := parseSpeak(text); err == nil {
			return text
		}
	}
	return fallback
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
	for _, id := range append(append([]string{}, diagnosisDims...), "bull", "bear", "chief") {
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

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-harness/internal/notify"
)

var debateDimensions = map[string]string{
	"volume_flow": "资金流分析师",
	"market":      "大盘分析师",
	"news":        "新闻分析师",
	"finance":     "财报分析师",
	"fundamental": "基本面分析师",
	"sector":      "板块分析师",
}

type debatePoint struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Dimension string `json:"dimension"`
	Weight    int    `json:"weight"`
}

type debateReject struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type chiefDecision struct {
	Verdict  string         `json:"verdict"`
	Text     string         `json:"text"`
	Adopted  []string       `json:"adopted"`
	Rejected []debateReject `json:"rejected"`
	However  string         `json:"however"`
	IfWrong  string         `json:"if_wrong"`
	Actions  chiefActions   `json:"actions"`
}

type debateFile struct {
	Bull  []debatePoint  `json:"bull"`
	Bear  []debatePoint  `json:"bear"`
	Chief *chiefDecision `json:"chief,omitempty"`
}

func (s *Server) finishOneShot(cfg agentConfig, code, kind, question, raw, turnErr string) (map[string]any, error) {
	if kind == "reply" {
		text, err := replyAnswer(raw)
		if err != nil {
			return nil, err
		}
		if text == "" || forbiddenHit(text, cfg.Forbid) {
			if text == "" && turnErr != "" {
				return nil, errors.New(turnErr)
			}
			return emptyAnalystMessage(cfg, kind, question), nil
		}
		s.appendGroup(code, groupLine{Agent: cfg.ID, Name: cfg.Name, Kind: kind, Quote: question, Text: text})
		return analystMessage(cfg, kind, question, text, nil), nil
	}
	switch cfg.Speak {
	case "points":
		return s.finishPoints(cfg, code, raw, turnErr)
	case "chief":
		return s.finishChief(cfg, code, raw, turnErr)
	default:
		return emptyAnalystMessage(cfg, kind, question), nil
	}
}

func (s *Server) finishPoints(cfg agentConfig, code, raw, turnErr string) (map[string]any, error) {
	points, err := parsePoints(raw, cfg.ID, s.spokenDimensions(code), cfg.Forbid)
	if err != nil || len(points) == 0 {
		if turnErr != "" && err != nil {
			return nil, errors.New(turnErr)
		}
		msg := analystMessage(cfg, "analysis", "", "", nil)
		msg["points"] = []any{}
		msg["errors"] = []string{"没有能对上群里发言的论据"}
		return msg, nil
	}
	s.updateDebate(code, func(file *debateFile) {
		if cfg.ID == "bear" {
			file.Bear = points
		} else {
			file.Bull = points
		}
		file.Chief = nil
	})
	var spoken []string
	for _, point := range points {
		spoken = append(spoken, point.Text)
	}
	s.appendGroup(code, groupLine{Agent: cfg.ID, Name: cfg.Name, Kind: "analysis", Text: strings.Join(spoken, "；")})
	msg := analystMessage(cfg, "analysis", "", "", nil)
	msg["points"] = pointPayload(points, nil)
	msg["errors"] = []string{}
	return msg, nil
}

func (s *Server) finishChief(cfg agentConfig, code, raw, turnErr string) (map[string]any, error) {
	file := s.loadDebate(code)
	decision, err := parseChief(raw, file, cfg.Forbid)
	if err != nil {
		if turnErr != "" {
			return nil, errors.New(turnErr)
		}
		return emptyAnalystMessage(cfg, "analysis", ""), nil
	}
	file = s.updateDebate(code, func(current *debateFile) {
		current.Chief = decision
	})
	s.appendGroup(code, groupLine{
		Agent: cfg.ID, Name: cfg.Name, Kind: "analysis", Text: decision.Text, StatusText: decision.Verdict,
	})
	s.notifyChief(code, decision)
	return chiefPayload(cfg, decision, file), nil
}

func (s *Server) notifyChief(code string, decision *chiefDecision) {
	if decision == nil || strings.TrimSpace(decision.Text) == "" || strings.TrimSpace(s.cfg.WecomWebhook) == "" {
		return
	}
	title := code + " 主分析师 " + strings.TrimSpace(decision.Verdict)
	text := decision.Text
	webhook := s.cfg.WecomWebhook
	go notify.Emit(webhook, "chief:"+code, title, text)
}

func (s *Server) pointsAfterChief(code string, cfg agentConfig) (map[string]any, bool) {
	if !s.bullBearJudgedToday(code, diagnosisDay(time.Now())) {
		return nil, false
	}
	file := s.loadDebate(code)
	if file.Chief == nil {
		return nil, false
	}
	points := file.Bull
	if cfg.ID == "bear" {
		points = file.Bear
	}
	if len(points) == 0 {
		return nil, false
	}
	msg := analystMessage(cfg, "analysis", "", "", nil)
	msg["points"] = pointPayload(points, file.Chief)
	msg["errors"] = []string{}
	return msg, true
}

func (s *Server) sidePoints(code, id string) []debatePoint {
	file := s.loadDebate(code)
	if id == "bear" {
		return file.Bear
	}
	return file.Bull
}

func (s *Server) spokenDimensions(code string) map[string]bool {
	out := map[string]bool{}
	for _, line := range s.groupLines(code) {
		if line.Text == "" {
			continue
		}
		if _, ok := debateDimensions[line.Agent]; ok {
			out[line.Agent] = true
		}
	}
	return out
}

func parsePoints(raw, side string, spoken map[string]bool, forbid []string) ([]debatePoint, error) {
	var body struct {
		Points []struct {
			Text      string `json:"text"`
			Dimension string `json:"dimension"`
			Weight    int    `json:"weight"`
		} `json:"points"`
	}
	if err := decodeObject(raw, &body); err != nil {
		return nil, err
	}
	var out []debatePoint
	for _, item := range body.Points {
		text := strings.TrimSpace(item.Text)
		if text == "" || forbiddenHit(text, forbid) || !spoken[item.Dimension] {
			continue
		}
		if _, ok := debateDimensions[item.Dimension]; !ok {
			continue
		}
		weight := item.Weight
		if weight < 1 || weight > 3 {
			weight = 2
		}
		if len(out) == 3 {
			break
		}
		out = append(out, debatePoint{
			ID:        fmt.Sprintf("%s.%d", side, len(out)+1),
			Text:      text,
			Dimension: item.Dimension,
			Weight:    weight,
		})
	}
	if len(out) == 0 {
		return nil, errors.New("没有论据")
	}
	return out, nil
}

func parseChief(raw string, file *debateFile, forbid []string) (*chiefDecision, error) {
	var body chiefDecision
	if err := decodeObject(raw, &body); err != nil {
		return nil, err
	}
	body.Text = strings.TrimSpace(body.Text)
	body.Verdict = strings.TrimSpace(body.Verdict)
	switch body.Verdict {
	case "偏多", "偏空", "不明":
	case "support":
		body.Verdict = "偏多"
	case "warn":
		body.Verdict = "偏空"
	case "neutral":
		body.Verdict = "不明"
	default:
		return nil, errors.New("裁决方向无法识别")
	}
	if body.Text == "" || forbiddenHit(body.Text, forbid) {
		return nil, errors.New("没有裁决")
	}
	known := map[string]bool{}
	for _, point := range append(append([]debatePoint{}, file.Bull...), file.Bear...) {
		known[point.ID] = true
	}
	seen := map[string]bool{}
	var adopted []string
	for _, id := range body.Adopted {
		id = strings.TrimSpace(id)
		if !known[id] || seen[id] {
			continue
		}
		seen[id] = true
		adopted = append(adopted, id)
	}
	var rejected []debateReject
	for _, item := range body.Rejected {
		id := strings.TrimSpace(item.ID)
		if !known[id] || seen[id] {
			continue
		}
		seen[id] = true
		rejected = append(rejected, debateReject{ID: id, Reason: strings.TrimSpace(item.Reason)})
	}
	body.Adopted = adopted
	for i := range rejected {
		rejected[i].Reason = speakArgIDs(rejected[i].Reason)
	}
	body.Rejected = rejected
	body.Text = speakArgIDs(body.Text)
	// 开场裁决正文保持短结论；模型若把取舍/校准整段塞进 text，软截到约 180 字
	body.Text = trimSummaryRunes(body.Text, 180)
	body.However = speakArgIDs(strings.TrimSpace(body.However))
	body.IfWrong = speakArgIDs(strings.TrimSpace(body.IfWrong))
	body.Actions = body.Actions.clean()
	return &body, nil
}

func pointPayload(points []debatePoint, decision *chiefDecision) []map[string]any {
	out := make([]map[string]any, 0, len(points))
	for _, point := range points {
		item := map[string]any{
			"id":     point.ID,
			"text":   point.Text,
			"weight": point.Weight,
			"analyst": map[string]string{
				"dimension": point.Dimension,
				"name":      debateDimensions[point.Dimension],
			},
		}
		if chip := verdictChip(point.ID, decision); chip != nil {
			item["verdict"] = chip
		}
		out = append(out, item)
	}
	return out
}

func verdictChip(id string, decision *chiefDecision) map[string]any {
	if decision == nil {
		return nil
	}
	for _, adopted := range decision.Adopted {
		if adopted == id {
			return map[string]any{"adopt": true, "text": "采纳"}
		}
	}
	for _, rejected := range decision.Rejected {
		if rejected.ID == id {
			text := rejected.Reason
			if text == "" {
				text = "未采纳"
			}
			return map[string]any{"adopt": false, "text": text}
		}
	}
	return nil
}

func chiefPayload(cfg agentConfig, decision *chiefDecision, file *debateFile) map[string]any {
	bull, bear := 0, 0
	for _, id := range decision.Adopted {
		for _, point := range file.Bull {
			if point.ID == id {
				bull += point.Weight
			}
		}
		for _, point := range file.Bear {
			if point.ID == id {
				bear += point.Weight
			}
		}
	}
	spread := bull - bear
	return map[string]any{
		"role":     cfg.ID,
		"name":     cfg.Name,
		"kind":     "analysis",
		"quote":    "",
		"text":     decision.Text,
		"verdict":  decision.Verdict,
		"adopted":  adoptedLine(decision.Adopted),
		"rejected": rejectedLine(decision.Rejected),
		"score": map[string]any{
			"bull": bull, "bear": bear, "spread": spread,
			"text": fmt.Sprintf("%d:%d，分差 %d", bull, bear, spread),
		},
		"however": decision.However,
		"if_wrong": map[string]any{
			"text":  decision.IfWrong,
			"alert": nil,
		},
		"actions": decision.Actions.payload(),
	}
}

func adoptedLine(ids []string) string {
	var bull, bear string
	for _, id := range ids {
		mark := circled(id)
		if strings.HasPrefix(id, "bull.") {
			bull += mark
		} else if strings.HasPrefix(id, "bear.") {
			bear += mark
		}
	}
	var parts []string
	if bull != "" {
		parts = append(parts, "看多"+bull)
	}
	if bear != "" {
		parts = append(parts, "看空"+bear)
	}
	return strings.Join(parts, " · ")
}

func rejectedLine(items []debateReject) string {
	var parts []string
	for _, item := range items {
		label := "看空"
		if strings.HasPrefix(item.ID, "bull.") {
			label = "看多"
		}
		label += circled(item.ID)
		if item.Reason != "" {
			label += "（" + item.Reason + "）"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " · ")
}

func circled(id string) string {
	const marks = "①②③④⑤⑥⑦⑧⑨"
	number := id
	if dot := strings.LastIndex(id, "."); dot >= 0 {
		number = id[dot+1:]
	}
	n := 0
	for _, r := range number {
		if r < '0' || r > '9' {
			return number
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 || n > 9 {
		return number
	}
	rs := []rune(marks)
	return string(rs[n-1])
}

func formatPoints(points []debatePoint) string {
	if len(points) == 0 {
		return "（没有）\n"
	}
	var b strings.Builder
	for _, point := range points {
		fmt.Fprintf(&b, "- %s %s：%s\n", point.ID, debateDimensions[point.Dimension], point.Text)
	}
	return b.String()
}

func decodeObject(raw string, dest any) error {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	text = strings.TrimSpace(text)
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		if err := dec.Decode(dest); err == nil {
			return nil
		}
	}
	return errors.New("没有 JSON")
}

func (s *Server) loadDebate(code string) *debateFile {
	s.groupsMu.Lock()
	defer s.groupsMu.Unlock()
	if s.debates == nil {
		s.debates = map[string]*debateFile{}
	}
	if file, ok := s.debates[code]; ok && file != nil {
		return cloneDebate(file)
	}
	file := readDebate(s.debatePath(code))
	s.debates[code] = file
	return cloneDebate(file)
}

func (s *Server) updateDebate(code string, change func(*debateFile)) *debateFile {
	s.groupsMu.Lock()
	defer s.groupsMu.Unlock()
	if s.debates == nil {
		s.debates = map[string]*debateFile{}
	}
	file := s.debates[code]
	if file == nil {
		file = readDebate(s.debatePath(code))
	}
	change(file)
	s.debates[code] = cloneDebate(file)
	_ = writeDebate(s.debatePath(code), file)
	return cloneDebate(file)
}

func (s *Server) debatePath(code string) string {
	if strings.TrimSpace(s.cfg.DataDir) == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "groups", sanitizeCode(code)+".debate.json")
}

func cloneDebate(file *debateFile) *debateFile {
	if file == nil {
		return &debateFile{}
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return &debateFile{}
	}
	out := &debateFile{}
	_ = json.Unmarshal(raw, out)
	return out
}

func readDebate(path string) *debateFile {
	out := &debateFile{}
	if path == "" {
		return out
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, out)
	return out
}

func writeDebate(path string, file *debateFile) error {
	if path == "" || file == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

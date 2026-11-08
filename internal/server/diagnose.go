package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// diagnosisDims 是维度分析师（不含看多/看空/主分析师）。
var diagnosisDims = []string{"volume_flow", "market", "sector", "news", "finance", "fundamental"}

// diagnosisOrder 是页面点诊断和每晚任务共用的顺序：维度分析师，然后看多、看空，最后主分析师。
var diagnosisOrder = append(append([]string{}, diagnosisDims...), "bull", "bear", "chief")

var diagnosisLabel = map[string]string{
	"volume_flow": "资金流", "market": "大盘", "sector": "板块", "news": "新闻", "finance": "财报", "fundamental": "基本面",
}

func diagnosisDay(now time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return now.In(loc).Format("2006-01-02")
}

func (s *Server) lockCode(code string) func() {
	s.codeMu.Lock()
	if s.codes == nil {
		s.codes = map[string]*sync.Mutex{}
	}
	mu := s.codes[code]
	if mu == nil {
		mu = &sync.Mutex{}
		s.codes[code] = mu
	}
	s.codeMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// bullBearJudgedToday 看多、看空已经在今天的主分析师之前说过。
// 页面补采纳标记时可以直接用存档；换一天必须重新说，不能因为上一轮的裁决还在就跳过。
func (s *Server) bullBearJudgedToday(code, day string) bool {
	lastBull, lastBear := -1, -1
	var chiefs []int
	chiefDay := ""
	for i, line := range s.groupLines(code) {
		if line.Kind != "analysis" || strings.TrimSpace(line.Text) == "" {
			continue
		}
		switch line.Agent {
		case "bull":
			lastBull = i
		case "bear":
			lastBear = i
		case "chief":
			chiefs = append(chiefs, i)
			chiefDay = line.Day
		}
	}
	if len(chiefs) == 0 || lastBull < 0 || lastBear < 0 || chiefDay != day {
		return false
	}
	prev := -1
	if len(chiefs) > 1 {
		prev = chiefs[len(chiefs)-2]
	}
	lastChief := chiefs[len(chiefs)-1]
	return lastBull > prev && lastBear > prev && lastChief > lastBull && lastChief > lastBear
}

// roundFresh 今天各位维度分析师和主分析师都说过，并且看多、看空也在这场裁决之前。
func (s *Server) roundFresh(code, day string) bool {
	if !s.bullBearJudgedToday(code, day) {
		return false
	}
	have := map[string]bool{}
	for _, line := range s.groupLines(code) {
		if line.Kind == "analysis" && line.Day == day && strings.TrimSpace(line.Text) != "" {
			have[line.Agent] = true
		}
	}
	for _, id := range diagnosisDims {
		if !have[id] {
			return false
		}
	}
	return have["chief"]
}

func leanOf(verdict string) string {
	switch {
	case strings.Contains(verdict, "空"):
		return "bearish"
	case strings.Contains(verdict, "多"):
		return "bullish"
	default:
		return "neutral"
	}
}

func (s *Server) verdictHistory(code string) []map[string]any {
	var days []string
	byDay := map[string]map[string]any{}
	for _, line := range s.groupLines(code) {
		if line.Agent != "chief" || line.Kind != "analysis" || strings.TrimSpace(line.Text) == "" || line.Day == "" {
			continue
		}
		if _, ok := byDay[line.Day]; !ok {
			days = append(days, line.Day)
		}
		byDay[line.Day] = map[string]any{"trade_date": line.Day, "lean": leanOf(line.StatusText), "level": levelOfVerdict(line.StatusText)}
	}
	out := make([]map[string]any, 0, len(days))
	for _, day := range days {
		out = append(out, byDay[day])
	}
	return out
}

func (s *Server) diagnosisResult(code string, reused bool) map[string]any {
	file := s.loadDebate(code)
	verdict, text := "", ""
	if file.Chief != nil {
		verdict, text = file.Chief.Verdict, file.Chief.Text
	}
	return map[string]any{
		"id": code + ":" + diagnosisDay(time.Now()), "reused": reused, "name": code,
		"verdict": verdict, "text": text, "history": s.verdictHistory(code),
	}
}

// RunDiagnosis 跑和页面相同的一轮。今天已经诊过就直接返回存档。
func (s *Server) RunDiagnosis(ctx context.Context, code string, force bool) (map[string]any, error) {
	unlock := s.lockCode(code)
	defer unlock()
	day := diagnosisDay(time.Now())
	if !force && s.roundFresh(code, day) {
		return s.diagnosisResult(code, true), nil
	}
	for _, id := range diagnosisOrder {
		cfg, err := loadAgent(id)
		if err != nil {
			return nil, err
		}
		if _, err := s.runAnalyst(ctx, cfg, code, "analysis", ""); err != nil {
			return nil, err
		}
	}
	return s.diagnosisResult(code, false), nil
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	out, err := s.RunDiagnosis(r.Context(), code, body.Force || r.URL.Query().Get("force") == "1")
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func errBadCode(code string) error {
	return errString("股票代码无法识别: " + code)
}

type errString string

func (e errString) Error() string { return string(e) }

func (s *Server) handleChiefDetail(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	if !tsCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, errBadCode(code))
		return
	}
	writeJSON(w, http.StatusOK, s.chiefDetail(code))
}

func (s *Server) chiefDetail(code string) map[string]any {
	file := s.loadDebate(code)
	decision := file.Chief
	verdict, text, however, wrong := "不明", "", "", ""
	var score map[string]any
	if decision != nil {
		verdict, text, however, wrong = decision.Verdict, decision.Text, decision.However, decision.IfWrong
		payload := chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, decision, file)
		score, _ = payload["score"].(map[string]any)
	}
	day := ""
	for _, line := range s.groupLines(code) {
		if line.Agent == "chief" && line.Kind == "analysis" && line.Day != "" {
			day = line.Day
		}
	}
	levels := map[string]any{}
	if b := s.readBundle(code); b != nil {
		s.ensureLevels(b)
		levels["down"], levels["up"] = b.Levels["down"], b.Levels["up"]
	}
	return map[string]any{
		"status":     "ok",
		"updated_at": day,
		"levels":     levels,
		"direction":  map[string]any{"verdict": verdict, "text": text},
		"score":      score,
		"conflict":   however,
		"postmortem": wrong,
		"stance":     s.stance(code),
		"points": map[string]any{
			"bull": map[string]any{"points": detailPoints(file.Bull, decision)},
			"bear": map[string]any{"points": detailPoints(file.Bear, decision)},
		},
	}
}

func (s *Server) stance(code string) []map[string]any {
	spoken := map[string]groupLine{}
	for _, line := range latestGroupView(s.groupLines(code)) {
		if line.Kind == "analysis" && strings.TrimSpace(line.Text) != "" {
			spoken[line.Agent] = line
		}
	}
	out := make([]map[string]any, 0, len(diagnosisDims))
	for _, id := range diagnosisDims {
		line := spoken[id]
		direction := strings.TrimSpace(line.StatusText)
		out = append(out, map[string]any{
			"dimension": id, "label": diagnosisLabel[id], "direction": emptyAsNil(direction),
			"strength": "", "certainty": "", "text": line.Text, "done": direction != "",
		})
	}
	return out
}

func emptyAsNil(text string) any {
	if text == "" {
		return nil
	}
	return text
}

func detailPoints(points []debatePoint, decision *chiefDecision) []map[string]any {
	adopted := map[string]bool{}
	rejected := map[string]string{}
	if decision != nil {
		for _, id := range decision.Adopted {
			adopted[id] = true
		}
		for _, item := range decision.Rejected {
			rejected[item.ID] = item.Reason
		}
	}
	out := make([]map[string]any, 0, len(points))
	for _, point := range points {
		state, reason := "pending", ""
		if adopted[point.ID] {
			state = "adopted"
		}
		if text, ok := rejected[point.ID]; ok {
			state, reason = "rejected", text
		}
		out = append(out, map[string]any{
			"ref": point.ID, "mark": circled(point.ID), "claim": point.Text,
			"weight": point.Weight, "final_weight": point.Weight, "state": state, "reason": reason,
		})
	}
	return out
}

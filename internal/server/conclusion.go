package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-harness/internal/sched"
)

// bundleSteps is how many checkpoints one nightly job has: five analysts, bull, bear, chief,
// the two price lines, the bundle itself and the change check.
const bundleSteps = 12

// stepOutcome is what one speaking step (an analyst, the bull, the bear, the chief) came to.
type stepOutcome struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Label    string         `json:"label"`
	State    string         `json:"state"` // ok | failed
	Reason   string         `json:"reason,omitempty"`
	Headline map[string]any `json:"headline,omitempty"`
}

// conclusionBundle is the one conclusion for a stock: five analysts, the bull and bear arguments and
// the chief's verdict, produced together by the nightly task. The page, the group chat and the drawers
// all read this one file; clicking never makes a new one.
type conclusionBundle struct {
	ID             int64          `json:"id"`
	Code           string         `json:"ts_code"`
	TradeDate      string         `json:"trade_date"`
	Session        string         `json:"session"`
	GeneratedAt    string         `json:"generated_at"`
	GeneratedDay   string         `json:"generated_day"`
	Trigger        string         `json:"trigger,omitempty"` // what asked for it: schedule, or the reason of an on-demand run
	Source         string         `json:"source"`            // agent | none
	Model          string         `json:"model"`
	FallbackReason string         `json:"fallback_reason"`
	Analysts       []bundleRow    `json:"analysts"`
	Debate         map[string]any `json:"debate"`
	Chief          map[string]any `json:"chief"`
	// Levels are the price above and the price below that the conclusion is watched against. They are
	// worked out by rule from the data service, the same for everyone; the model does not pick them.
	Levels map[string]any `json:"levels,omitempty"`
}

type bundleRow struct {
	Dimension string         `json:"dimension"`
	Name      string         `json:"name"`
	Label     string         `json:"label"`
	State     string         `json:"state"`
	Reason    string         `json:"reason"`
	Headline  map[string]any `json:"headline"`
}

func (s *Server) bundlePath(code string) string {
	if strings.TrimSpace(s.cfg.DataDir) == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "bundles", sanitizeCode(code)+".json")
}

func (s *Server) readBundle(code string) *conclusionBundle {
	path := s.bundlePath(code)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var b conclusionBundle
	if json.Unmarshal(raw, &b) != nil || b.ID == 0 {
		return nil
	}
	return &b
}

func (s *Server) writeBundle(b *conclusionBundle) {
	path := s.bundlePath(b.Code)
	if path == "" {
		return
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

func sessionOf(cal *sched.Calendar, now time.Time) string {
	n := now.In(sched.Zone)
	if cal.IsTradingDay(n) && n.Hour() < 15 {
		return "intraday"
	}
	return "close"
}

// outcomesFromGroup rebuilds step outcomes from the latest analysis lines in the 研判群.
// Used after a live streamed diagnosis so the conclusion drawer can update without re-calling the model.
func (s *Server) outcomesFromGroup(code string) map[string]stepOutcome {
	out := map[string]stepOutcome{}
	for _, id := range diagnosisOrder {
		cfg, err := loadAgent(id)
		name := id
		if err == nil {
			name = cfg.Name
		}
		out[id] = stepOutcome{ID: id, Name: name, Label: diagnosisLabel[id], State: "missing", Reason: "还没有发言"}
	}
	for _, line := range latestGroupView(s.groupLines(code)) {
		if line.Kind != "analysis" || strings.TrimSpace(line.Text) == "" {
			continue
		}
		o, ok := out[line.Agent]
		if !ok {
			continue
		}
		o.State, o.Reason = "ok", ""
		if line.Name != "" {
			o.Name = line.Name
		}
		switch line.Agent {
		case "bull", "bear", "chief":
			// debate / chief payloads come from the debate file in buildBundle
		default:
			h := map[string]any{"text": line.Text}
			if line.Status != "" {
				h["status"] = line.Status
			}
			if line.StatusText != "" {
				h["status_text"] = line.StatusText
			}
			if len(line.SuggestedQuestions) > 0 {
				h["suggested_questions"] = line.SuggestedQuestions
			}
			o.Headline = h
		}
		out[line.Agent] = o
	}
	return out
}

// commitBundleFromGroup writes a conclusion bundle from the live group record (no new model calls).
func (s *Server) commitBundleFromGroup(code, trigger string) (*conclusionBundle, error) {
	outcomes := s.outcomesFromGroup(code)
	chief := outcomes["chief"]
	if chief.State != "ok" {
		return nil, fmt.Errorf("主分析师还没有裁决，无法落结论包")
	}
	missing := 0
	for _, id := range diagnosisDims {
		if outcomes[id].State != "ok" {
			missing++
		}
	}
	if missing == 5 {
		return nil, fmt.Errorf("还没有维度分析师结论")
	}
	now := s.now()
	b := s.buildBundle(code, now, outcomes, s.fetchLevels(code))
	if trigger != "" {
		b.Trigger = trigger
	} else {
		b.Trigger = "live"
	}
	s.writeBundle(b)
	return b, nil
}

// buildBundle assembles the bundle from what the steps said and what the debate file holds.
func (s *Server) buildBundle(code string, now time.Time, outcomes map[string]stepOutcome, levels map[string]any) *conclusionBundle {
	b := &conclusionBundle{
		ID: now.UnixMilli(), Code: code, TradeDate: sched.Day(s.cal.LastClose(now)), Session: sessionOf(s.cal, now),
		GeneratedAt: now.In(sched.Zone).Format("2006-01-02 15:04"), GeneratedDay: sched.Day(now), Model: s.cfg.Model, Source: "agent",
	}
	for _, id := range diagnosisDims {
		o := outcomes[id]
		row := bundleRow{Dimension: id, Name: o.Name, Label: diagnosisLabel[id], State: o.State, Reason: o.Reason, Headline: o.Headline}
		if row.State == "" {
			row.State = "missing"
		}
		b.Analysts = append(b.Analysts, row)
	}
	b.Levels = levels
	file := s.loadDebate(code)
	side := func(id string, points []debatePoint) map[string]any {
		o := outcomes[id]
		if o.State == "failed" {
			return map[string]any{"points": []any{}, "errors": []string{o.Reason}}
		}
		return map[string]any{"points": pointPayload(points, file.Chief), "errors": []string{}}
	}
	b.Debate = map[string]any{"bull": side("bull", file.Bull), "bear": side("bear", file.Bear)}
	if o := outcomes["chief"]; o.State != "failed" && file.Chief != nil {
		b.Chief = chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, file.Chief, file)
	} else {
		b.Source, b.FallbackReason = "none", outcomes["chief"].Reason
		if b.FallbackReason == "" {
			b.FallbackReason = "主分析师没有给出裁决"
		}
	}
	return b
}

// staleReason says why a bundle is not current; empty means it is.
func (s *Server) staleReason(b *conclusionBundle, now time.Time) string {
	if b.TradeDate < sched.Day(s.cal.LastClose(now)) {
		return "这份结论是 " + b.TradeDate + " 的，之后还没有更新"
	}
	return ""
}

func (s *Server) stockName(code string) string {
	if s.personal != nil {
		for _, w := range s.personal.WatchList() {
			if w.Code == code {
				return w.Name
			}
		}
		for _, t := range s.personal.Trades() {
			if t.Code == code && t.Name != "" {
				return t.Name
			}
		}
		for _, w := range s.personal.Wishes() {
			if w.Code == code && w.Name != "" {
				return w.Name
			}
		}
	}
	return code
}

// conclusionView is what GET /api/stocks/{code}/conclusion returns. With enqueue, a stock that has no
// bundle yet gets one queued.
func (s *Server) conclusionView(code string, enqueue bool) map[string]any {
	now := s.now()
	b := s.readBundle(code)
	job, active := sched.Job{}, false
	if s.sch != nil {
		job, active = s.sch.Store.ActiveJob(nightlyID, code)
	}
	if b == nil && !active && enqueue && s.sch != nil {
		_, _ = s.requestConclusion(code, "first_view", false)
		job, active = s.sch.Store.ActiveJob(nightlyID, code)
	}
	body := map[string]any{"ts_code": code, "stock_name": s.stockName(code), "bundle": nil, "job": s.jobProgress(job, active)}
	switch {
	case b == nil && active:
		body["state"] = "pending"
	case b == nil:
		body["state"] = "failed"
		body["job"].(map[string]any)["message"] = s.lastFailure(code)
	default:
		view := map[string]any{
			"id": b.ID, "trade_date": b.TradeDate, "session": b.Session, "generated_at": b.GeneratedAt, "source": b.Source, "model": b.Model,
			"fallback_reason": b.FallbackReason, "stale": s.staleReason(b, now), "analysts": b.Analysts, "debate": b.Debate, "chief": withDownLine(b.Chief, b.Levels),
		}
		body["bundle"] = view
		body["state"] = map[bool]string{true: "refreshing", false: "ready"}[active]
	}
	return body
}

func (s *Server) jobProgress(job sched.Job, active bool) map[string]any {
	out := map[string]any{"state": "none", "done": 0, "total": bundleSteps, "message": ""}
	if !active {
		return out
	}
	out["state"] = "queued"
	if job.State == "running" {
		out["state"] = "running"
	} else if job.State == "retrying" || job.State == "not_ready" {
		out["state"] = "retrying"
	}
	if job.ID != "" {
		out["done"] = len(s.sch.Store.StepNames(job.ID))
	}
	return out
}

func (s *Server) lastFailure(code string) string {
	if s.sch == nil {
		return "调度器没有启动"
	}
	for _, j := range s.sch.Store.JobsOf(nightlyID, code) {
		if j.State == "dead" && j.Error != nil {
			return j.Error.Message
		}
		break
	}
	return "还没有生成"
}

// requestConclusion queues one nightly job for a stock. Background triggers only queue when there is
// no bundle at all; force (the user's "重新分析") always queues. A job already waiting or running is reused.
func (s *Server) requestConclusion(code, reason string, force bool) (map[string]any, error) {
	if s.sch == nil {
		return nil, errNoScheduler
	}
	if _, active := s.sch.Store.ActiveJob(nightlyID, code); active {
		return map[string]any{"queued": false, "reason": "已经在跑"}, nil
	}
	if !force && s.readBundle(code) != nil {
		return map[string]any{"queued": false, "reason": "已经有结论，交给夜间任务更新"}, nil
	}
	run, err := s.sch.Manual(nightlyID, []sched.UnitSpec{{Key: code, Priority: 95}}, map[string]any{"on_demand": true, "force": force, "reason": reason})
	if err != nil {
		return nil, err
	}
	s.sch.Tick()
	return map[string]any{"queued": true, "run_id": run.ID}, nil
}

// now is the scheduler's clock, so tests and the real thing agree on what "today" is.
func (s *Server) now() time.Time {
	if s.sch != nil && s.sch.Clock != nil {
		return s.sch.Clock()
	}
	return time.Now()
}

// fetchLevels asks the data service for the price above and the price below. A failure gives nil: the
// conclusion is still good without them, and the detail view will try again.
func (s *Server) fetchLevels(code string) map[string]any {
	var body map[string]any
	if err := s.stockGetJSON("/api/v1/shared/levels/"+code, &body); err != nil || body["status"] != "ok" {
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{"price", "down", "up"} {
		if v, ok := body[k]; ok && v != nil {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ensureLevels fills in the price lines of a bundle made before they existed, once.
func (s *Server) ensureLevels(b *conclusionBundle) {
	if b == nil || b.Levels != nil {
		return
	}
	if lv := s.fetchLevels(b.Code); lv != nil {
		b.Levels = lv
		s.writeBundle(b)
	}
}

// withDownLine gives the chief's card the "if I am wrong" price: the line below, which the page offers as
// a reminder, and says it in the text too.
func withDownLine(chief map[string]any, levels map[string]any) map[string]any {
	if chief == nil {
		return nil
	}
	down, _ := levels["down"].(map[string]any)
	price, ok := down["price"].(float64)
	if !ok {
		return chief
	}
	out := map[string]any{}
	for k, v := range chief {
		out[k] = v
	}
	wrong := map[string]any{}
	if old, ok := chief["if_wrong"].(map[string]any); ok {
		for k, v := range old {
			wrong[k] = v
		}
	}
	text, _ := wrong["text"].(string)
	if !strings.Contains(text, fmt.Sprintf("%.2f", price)) {
		text = strings.TrimRight(strings.TrimSpace(text), "。．.；; ")
		if text != "" {
			text += "，并且"
		}
		text += fmt.Sprintf("收盘跌破 %.2f", price)
	}
	wrong["text"], wrong["alert"] = text, map[string]any{"price": price, "side": "down"}
	out["if_wrong"] = wrong
	return out
}

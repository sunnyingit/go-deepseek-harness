package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-harness/internal/custom"
	"go-harness/internal/inbox"
	"go-harness/internal/sched"
)

// The tasks the user makes up in the scan page. Each one is a task of its own in the scheduler ("custom.c1"), so it is
// planned, retried, caught up after sleep and reported with a receipt like the built-in ones.

func customID(id string) string { return "custom." + id }

// registerCustomTasks puts every saved custom task in the scheduler. A task that is switched off is registered
// and paused, so "run now" still works for it.
func (s *Server) registerCustomTasks() {
	if s.custom == nil || s.sch == nil {
		return
	}
	for _, t := range s.custom.All() {
		s.registerCustom(t)
	}
}

func (s *Server) registerCustom(t custom.Task) {
	queue := "data"
	if t.Kind != custom.Scan {
		queue = "llm"
	}
	id := t.ID
	trigger := sched.Trigger{Kind: "daily", At: t.At, WindowMin: 120, Days: t.Days}
	attempts, backoff, frequent, budget := 2, []time.Duration{5 * time.Minute, 20 * time.Minute}, false, 0
	if t.Every > 0 {
		// every N minutes: inside the market sessions, or around the clock. A slot that failed is not retried, the next one comes soon enough.
		trigger = sched.Trigger{Kind: "every", EveryMin: t.Every, Days: t.Days}
		if t.Days == "trading" {
			trigger.Kind = "interval"
		}
		attempts, backoff, frequent = 1, nil, true
		if t.Kind != custom.Scan {
			budget = 150 // model calls a day: a task set to run often must not run the bill up
		}
	}
	s.sch.Register(&sched.Spec{
		ID: customID(id), Title: t.Title, Queue: queue, Frequent: frequent, BudgetCalls: budget,
		Trigger:     trigger,
		MaxAttempts: attempts, Backoff: backoff, CatchupTTL: 6 * time.Hour, Timeout: 10 * time.Minute,
		Fanout: func(*sched.PlanCtx) []sched.Unit {
			cur, ok := s.custom.Get(id)
			if !ok {
				return nil
			}
			var out []sched.Unit
			for _, code := range s.customCodes(cur) {
				out = append(out, sched.Unit{Key: code, Priority: 50})
			}
			return out
		},
		Handler: s.customHandler,
	})
	s.sch.Store.SetPaused(customID(id), !t.Enabled)
}

func (s *Server) unregisterCustom(id string) {
	s.sch.Unregister(customID(id))
	s.sch.Store.SetPaused(customID(id), false)
}

// customCodes are the stocks a task covers right now. "我的…" follows the followed stocks as they change.
func (s *Server) customCodes(t custom.Task) []string {
	if t.Scope == "codes" {
		return append([]string(nil), t.Codes...)
	}
	var out []string
	if t.Scope == "watch" {
		for _, w := range s.personal.WatchList() {
			out = append(out, w.Code)
		}
		return out
	}
	for _, sub := range s.personal.Subjects() {
		if t.Scope == "all" || (t.Scope == "hold" && sub.Tier == "hold") || (t.Scope == "wish" && sub.Tier == "wish") {
			out = append(out, sub.Code)
		}
	}
	return out
}

func (s *Server) customHandler(c *sched.JobCtx) (any, error) {
	id := strings.TrimPrefix(c.Spec.ID, "custom.")
	t, ok := s.custom.Get(id)
	if !ok {
		return nil, sched.Permanent("这个任务已经删掉了")
	}
	switch t.Kind {
	case custom.Analyze:
		if len(t.Analysts) > 0 {
			return s.customDimensions(c, t)
		}
		c.Input()["force"] = true // the user asked for a fresh conclusion at this time
		return s.nightly(c)
	case custom.Ask:
		return s.customAsk(c, t)
	case custom.Scan:
		return s.customScan(c, t)
	}
	return nil, sched.Permanent("不认识的任务类型")
}

// askAnalyst asks one analyst a question about a stock. Tests replace it.
func (s *Server) askAnalyst(ctx context.Context, code, analyst, question string) (string, error) {
	if s.askFn != nil {
		return s.askFn(ctx, code, analyst, question)
	}
	if s.llm == nil {
		return "", sched.Permanent("没有可用的模型：请先配置模型密钥")
	}
	unlock := s.lockCode(code)
	defer unlock()
	cfg, err := loadAgent(analyst)
	if err != nil {
		return "", err
	}
	msg, err := s.runAnalyst(ctx, cfg, code, "reply", question)
	if err != nil {
		return "", err
	}
	text, _ := msg["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", sched.Transient(custom.AnalystLabel[analyst] + "没有给出回答")
	}
	return text, nil
}

func clipText(text string, n int) string {
	body := strings.TrimSpace(text)
	if r := []rune(body); len(r) > n {
		return string(r[:n]) + "…"
	}
	return body
}

func (s *Server) customAsk(c *sched.JobCtx, t custom.Task) (any, error) {
	code := c.Unit()
	who := t.Analysts
	if len(who) == 0 {
		who = []string{"chief"}
	}
	var parts []string
	for _, id := range who {
		id := id
		text, err := sched.Step(c, "ask."+id, func() (string, error) {
			c.AddCalls(1)
			return s.askAnalyst(c.Ctx, code, id, t.Question)
		})
		if err != nil {
			return nil, err
		}
		if len(who) == 1 {
			parts = append(parts, clipText(text, 600))
		} else {
			parts = append(parts, "【"+custom.AnalystLabel[id]+"】"+clipText(text, 400))
		}
	}
	_, _, perr := s.inbox.Publish("custom.answer", "stock", code, map[string]any{"task": t.Title, "name": s.stockName(code), "ts_code": code, "text": strings.Join(parts, "\n\n"), "question": t.Question},
		fmt.Sprintf("custom.answer:%s:%s:%s", t.ID, code, c.RunID()), inbox.Options{})
	if perr != nil {
		return nil, perr
	}
	return map[string]any{"ts_code": code, "answered": len(who)}, nil
}

// customDimensions analyses a stock again from only the chosen sides (money flow, sector, news ...). There is no
// chief and no verdict, so nothing is written down as a prediction: the headlines go to the inbox.
func (s *Server) customDimensions(c *sched.JobCtx, t custom.Task) (any, error) {
	code := c.Unit()
	if s.llm == nil && s.stepFn == nil {
		return nil, sched.Permanent("没有可用的模型：请先配置模型密钥")
	}
	unlock := s.lockCode(code)
	defer unlock()
	var lines []string
	okCount := 0
	for _, id := range t.Analysts {
		id := id
		o, err := sched.Step(c, "dim."+id, func() (stepOutcome, error) {
			out := s.speak(c.Ctx, id, code)
			if out.State == "ok" {
				c.AddCalls(1)
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		label := custom.AnalystLabel[id]
		if o.State != "ok" {
			lines = append(lines, label+"：这次没能发言（"+clipText(o.Reason, 60)+"）")
			continue
		}
		okCount++
		verdict, _ := o.Headline["status_text"].(string)
		text, _ := o.Headline["text"].(string)
		line := label + "："
		if verdict != "" {
			line += verdict + " — "
		}
		lines = append(lines, line+clipText(text, 160))
	}
	if okCount == 0 && !c.LastTry() {
		return nil, sched.Transient("几位分析师都没能发言")
	}
	_, _, err := s.inbox.Publish("custom.analysis", "stock", code, map[string]any{"task": t.Title, "name": s.stockName(code), "ts_code": code, "lines": lines},
		fmt.Sprintf("custom.analysis:%s:%s:%s", t.ID, code, c.RunID()), inbox.Options{})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ts_code": code, "analysed": okCount}, nil
}

// customFacts gathers what one stock looks like, fetching only what the task's conditions need.
func (s *Server) customFacts(t custom.Task, code string) custom.Facts {
	var f custom.Facts
	d := holdingsData{s}
	if t.Needs("change_up", "change_down", "price_below", "price_above") {
		if q, _ := s.quotes.Fetch([]string{code}); q[code].Price > 0 {
			f.Price, f.ChangePct, f.HasChange = q[code].Price, q[code].ChangePct, true
		}
	}
	if t.Needs("below_ma", "above_ma", "new_high", "new_low") {
		for _, b := range barsOf(s.shared("/closes/"+code, 30*time.Minute)) {
			f.Closes = append(f.Closes, custom.Bar{Day: b.Day, Close: b.Close})
		}
	}
	if t.Needs("flow_in_days", "flow_out_days") {
		if s.shared("/flow/"+code, 5*time.Minute) != nil {
			f.FlowIn, f.FlowOut = d.Flow(code)
			f.HasFlow = true
		}
	}
	if t.Needs("ai_bullish", "ai_bearish") {
		if b := s.readBundle(code); b != nil && b.Chief != nil {
			verdict, _ := b.Chief["verdict"].(string)
			switch leanOf(verdict) {
			case "bullish":
				f.AILean = "bullish"
			case "bearish":
				f.AILean = "bearish"
			default:
				f.AILean = "unclear"
			}
		}
	}
	if t.Needs("sector_is") {
		f.SectorVerd = d.Sector(code).Verdict
	}
	return f
}

func (s *Server) customScan(c *sched.JobCtx, t custom.Task) (any, error) {
	code := c.Unit()
	f := s.customFacts(t, code)
	hit, why, unknown := custom.Match(t, f)
	out := map[string]any{"ts_code": code, "hit": hit, "unknown": unknown}
	if !hit {
		return out, nil
	}
	lines := append([]string{"条件：" + t.RulesText()}, why...)
	_, created, err := s.inbox.Publish("custom.hit", "stock", code, map[string]any{"task": t.Title, "name": s.stockName(code), "ts_code": code, "why": lines},
		fmt.Sprintf("custom.hit:%s:%s:%s", t.ID, code, sched.Day(c.Now())), inbox.Options{})
	if err != nil {
		return nil, err
	}
	out["notified"] = created
	return out, nil
}

// ---- views and routes

func (s *Server) customView(t custom.Task) map[string]any {
	names := map[string]string{}
	for _, code := range t.Codes {
		names[code] = s.stockName(code)
	}
	codes := s.customCodes(t)
	v := map[string]any{
		"id": t.ID, "title": t.Title, "kind": t.Kind, "kind_text": custom.KindText(t.Kind), "scope": t.Scope, "codes": t.Codes, "scope_text": t.ScopeText(names),
		"at": t.At, "every": t.Every, "days": t.Days, "schedule_text": t.ScheduleText(), "question": t.Question, "rules": t.Rules, "match": t.Match, "rules_text": t.RulesText(),
		"analysts": t.Analysts, "analysts_text": t.AnalystsText(), "enabled": t.Enabled, "count": len(codes),
	}
	if sp, ok := s.sch.Spec(customID(t.ID)); ok && t.Enabled {
		if t.Every > 0 {
			if text := s.nextIntervalText(sp.Trigger, s.now()); text != "" {
				v["when_text"] = text
			}
		} else if at, ok := s.nextDaily(sp.Trigger, s.now()); ok {
			v["next"], v["when_text"] = at.Format("2006-01-02 15:04"), whenText(at, s.now())
		}
	}
	if runs := s.sch.Store.Runs(customID(t.ID), 1); len(runs) > 0 {
		r := runs[0]
		at := r.FinishedAt
		if at == 0 {
			at = r.StartedAt
		}
		if at == 0 {
			at = r.ScheduledFor
		}
		v["last"] = map[string]any{"state": r.State, "at": time.UnixMilli(at).In(sched.Zone).Format("01-02 15:04"), "counts": r.Counts, "trigger": r.Trigger}
	}
	return v
}

func (s *Server) registerCustomRoutes(mux *http.ServeMux) {
	const base = "/api/scan/custom"
	need := func(w http.ResponseWriter) bool {
		if s.custom == nil || s.sch == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "自定义任务还没有启动"})
			return false
		}
		return true
	}
	fail := func(w http.ResponseWriter, err error) {
		if _, ok := err.(custom.InputError); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
	}
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		tasks := []map[string]any{}
		for _, t := range s.custom.All() {
			tasks = append(tasks, s.customView(t))
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "kinds": custom.Kinds, "sector_verdicts": custom.SectorVerdicts, "limit": custom.MaxTasks, "model": s.llm != nil})
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var t custom.Task
		if err := readJSON(r, &t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求内容不对"})
			return
		}
		made, err := s.custom.Add(t)
		if err != nil {
			fail(w, err)
			return
		}
		s.registerCustom(made)
		writeJSON(w, http.StatusCreated, map[string]any{"task": s.customView(made)})
	})
	mux.HandleFunc("PUT "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var t custom.Task
		if err := readJSON(r, &t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求内容不对"})
			return
		}
		made, err := s.custom.Update(r.PathValue("id"), t)
		if err != nil {
			fail(w, err)
			return
		}
		s.registerCustom(made)
		writeJSON(w, http.StatusOK, map[string]any{"task": s.customView(made)})
	})
	mux.HandleFunc("DELETE "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		id := r.PathValue("id")
		if !s.custom.Remove(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这个任务"})
			return
		}
		s.unregisterCustom(id)
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("POST "+base+"/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		_ = readJSON(r, &body)
		t, ok := s.custom.SetEnabled(r.PathValue("id"), body.Enabled)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这个任务"})
			return
		}
		s.sch.Store.SetPaused(customID(t.ID), !t.Enabled)
		writeJSON(w, http.StatusOK, map[string]any{"task": s.customView(t)})
	})
	mux.HandleFunc("POST "+base+"/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !need(w) {
			return
		}
		t, ok := s.custom.Get(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "没有这个任务"})
			return
		}
		if t.Kind != custom.Scan && s.llm == nil && s.stepFn == nil && s.askFn == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有可用的模型：请先在设置里配置模型密钥"})
			return
		}
		var units []sched.UnitSpec
		for _, code := range s.customCodes(t) {
			if _, active := s.sch.Store.ActiveJob(customID(t.ID), code); !active {
				units = append(units, sched.UnitSpec{Key: code, Priority: 60})
			}
		}
		if len(units) == 0 {
			msg := "这个任务现在没有可跑的股票"
			if len(s.customCodes(t)) > 0 {
				msg = "已经在跑了"
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
			return
		}
		run, err := s.sch.Manual(customID(t.ID), units, map[string]any{"on_demand": true, "reason": "scan_now"}) // a task of the scan, not a diagnosis you asked for
		if err != nil {
			fail(w, err)
			return
		}
		s.sch.Tick()
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": len(units), "run_id": run.ID})
	})
}

// nextIntervalText says when an every-N-minutes task runs next. Slots are multiples of N minutes from midnight; a
// market-session task waits for the next session when the market is closed.
func (s *Server) nextIntervalText(tr sched.Trigger, now time.Time) string {
	local := now.In(sched.Zone)
	mins := local.Hour()*60 + local.Minute()
	nextSlot := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, sched.Zone).Add(time.Duration((mins/tr.EveryMin+1)*tr.EveryMin) * time.Minute)
	if tr.Kind != "interval" {
		return "下一次 " + whenText(nextSlot, now)
	}
	if in, start := s.sessionNow(now); !in {
		if start.IsZero() {
			return ""
		}
		return "下一次 " + whenText(start, now)
	}
	if sched.InSession(nextSlot) && nextSlot.Day() == local.Day() {
		return "下一次 " + whenText(nextSlot, now)
	}
	if _, start := s.sessionNow(nextSlot.Add(time.Minute)); !start.IsZero() {
		return "下一次 " + whenText(start, now)
	}
	return "下一次 " + whenText(nextSlot, now)
}

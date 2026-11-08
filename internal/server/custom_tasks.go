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
	s.sch.Register(&sched.Spec{
		ID: customID(id), Title: t.Title, Queue: queue,
		Trigger:     sched.Trigger{Kind: "daily", At: t.At, WindowMin: 120, Days: t.Days},
		MaxAttempts: 2, Backoff: []time.Duration{5 * time.Minute, 20 * time.Minute}, CatchupTTL: 6 * time.Hour, Timeout: 10 * time.Minute,
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
		c.Input()["force"] = true // the user asked for a fresh conclusion at this time
		return s.nightly(c)
	case custom.Ask:
		return s.customAsk(c, t)
	case custom.Scan:
		return s.customScan(c, t)
	}
	return nil, sched.Permanent("不认识的任务类型")
}

// askChief asks the chief analyst one question about a stock. Tests replace it.
func (s *Server) askChief(ctx context.Context, code, question string) (string, error) {
	if s.askFn != nil {
		return s.askFn(ctx, code, question)
	}
	if s.llm == nil {
		return "", sched.Permanent("没有可用的模型：请先配置模型密钥")
	}
	unlock := s.lockCode(code)
	defer unlock()
	cfg, err := loadAgent("chief")
	if err != nil {
		return "", err
	}
	msg, err := s.runAnalyst(ctx, cfg, code, "reply", question)
	if err != nil {
		return "", err
	}
	text, _ := msg["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", sched.Transient("主分析师没有给出回答")
	}
	return text, nil
}

func (s *Server) customAsk(c *sched.JobCtx, t custom.Task) (any, error) {
	code := c.Unit()
	text, err := sched.Step(c, "ask", func() (string, error) {
		c.AddCalls(1)
		return s.askChief(c.Ctx, code, t.Question)
	})
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(text)
	if r := []rune(body); len(r) > 600 {
		body = string(r[:600]) + "…"
	}
	_, _, perr := s.inbox.Publish("custom.answer", "stock", code, map[string]any{"task": t.Title, "name": s.stockName(code), "ts_code": code, "text": body, "question": t.Question},
		fmt.Sprintf("custom.answer:%s:%s:%s", t.ID, code, sched.Day(c.Now())), inbox.Options{})
	if perr != nil {
		return nil, perr
	}
	return map[string]any{"ts_code": code, "answered": true}, nil
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
		"at": t.At, "days": t.Days, "schedule_text": t.ScheduleText(), "question": t.Question, "rules": t.Rules, "match": t.Match, "rules_text": t.RulesText(),
		"enabled": t.Enabled, "count": len(codes),
	}
	if sp, ok := s.sch.Spec(customID(t.ID)); ok {
		if at, ok := s.nextDaily(sp.Trigger, s.now()); ok && t.Enabled {
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

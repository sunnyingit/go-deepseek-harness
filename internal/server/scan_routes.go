package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

// minSample is how many settled calls a breakdown row needs before its rate means anything.
const minSample = 20

func (s *Server) scanAccuracy(horizon, days int) map[string]any {
	preds := s.scan.All()
	acc := scan.Stats(preds, horizon, days, s.now())
	for i := range acc.Analysts {
		acc.Analysts[i].Label = dimLabel[acc.Analysts[i].Key]
		if acc.Analysts[i].Label == "" {
			acc.Analysts[i].Label = acc.Analysts[i].Key
		}
	}
	today := sched.Day(s.now())
	var dueToday scan.Rate
	key := strconv.Itoa(horizon)
	for _, p := range preds {
		if r := p.Results[key]; r != nil && r.Day == today && r.Outcome != "" {
			switch r.Outcome {
			case "hit":
				dueToday.Hit++
			case "miss":
				dueToday.Miss++
			default:
				dueToday.Flat++
			}
			dueToday.N++
		}
	}
	if d := dueToday.Hit + dueToday.Miss; d > 0 {
		dueToday.Pct = float64(dueToday.Hit) / float64(d)
	} else {
		dueToday.Pct = -1
	}
	current := ""
	if m := (holdingsData{s}).Market(); m != nil {
		current = scan.Market(m.Level)
	}
	// does the strong tier beat the weak one? the page says so, or says the strength means nothing
	var strong, weak *scan.Row
	for i := range acc.Level {
		switch acc.Level[i].Key {
		case "2", "3":
			if strong == nil || acc.Level[i].Rate.N > strong.Rate.N {
				strong = &acc.Level[i]
			}
		case "1":
			weak = &acc.Level[i]
		}
	}
	levelNote := ""
	if strong != nil && weak != nil && strong.Rate.N >= minSample && weak.Rate.N >= minSample && strong.Rate.Pct >= 0 && weak.Rate.Pct >= 0 {
		gap := (strong.Rate.Pct - weak.Rate.Pct) * 100
		if gap >= 2 {
			levelNote = fmt.Sprintf("强度「%s」比「%s」高 %.1f 个点：强度有区分度，说明 AI 不只是在猜方向。", strong.Label, weak.Label, gap)
		} else {
			levelNote = fmt.Sprintf("强度「%s」和「%s」只差 %.1f 个点：强度目前没有区分度，别拿它当把握大小用。", strong.Label, weak.Label, gap)
		}
	}
	return map[string]any{
		"horizon": horizon, "days": days, "overall": acc.Overall, "baseline": acc.Baseline, "edge": acc.Edge, "unclear_rate": acc.Unclear, "made": acc.Made,
		"by_day": acc.ByDay, "by_source": acc.Source, "by_level": acc.Level, "by_direction": acc.Dir, "by_market": acc.Market, "by_analyst": acc.Analysts,
		"due_today": dueToday, "current_market": current, "min_n": minSample, "level_note": levelNote, "first_day": s.firstSettleDay(),
	}
}

func (s *Server) firstSettleDay() string {
	first := ""
	for _, p := range s.scan.All() {
		for _, r := range p.Results {
			if r != nil && (first == "" || r.Day < first) {
				first = r.Day
			}
		}
	}
	return first
}

func (s *Server) registerScanRoutes(mux *http.ServeMux) {
	const base = "/api/scan"
	get := func(path string, fn func(r *http.Request) any) {
		mux.HandleFunc("GET "+base+path, func(w http.ResponseWriter, r *http.Request) {
			if s.scan == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "扫描还没有启动"})
				return
			}
			writeJSON(w, http.StatusOK, fn(r))
		})
	}
	get("/summary", func(r *http.Request) any { return s.scanSummary() })
	get("/upcoming", func(r *http.Request) any { return s.scanUpcoming() })
	get("/panel", func(r *http.Request) any { return s.scanDayPanel() })
	get("/today", func(r *http.Request) any { return s.scanToday(r.URL.Query().Get("source")) })
	get("/tracking", func(r *http.Request) any {
		q := r.URL.Query()
		return s.scanTracking(q.Get("due"), q.Get("mine") == "1", q.Get("source"))
	})
	get("/settled", func(r *http.Request) any { return s.scanSettled(settledQuery(r, s.now())) })
	get("/accuracy", func(r *http.Request) any {
		h, _ := strconv.Atoi(r.URL.Query().Get("horizon"))
		if h != 20 {
			h = 5
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days <= 0 || days > 365 {
			days = 60
		}
		return s.scanAccuracy(h, days)
	})
	mux.HandleFunc("POST "+base+"/run", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Task string `json:"task"`
		}
		_ = readJSON(r, &body)
		out, err := s.runScanNow(body.Task)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, out)
	})
	// hide / show a stock in the tracking list; the predictions themselves are never touched
	mux.HandleFunc("POST "+base+"/hide", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code string `json:"code"`
			On   bool   `json:"on"`
		}
		if err := readJSON(r, &body); err != nil || (body.Code == "" && body.On) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "要说明哪只股票"})
			return
		}
		s.hidden.Set(body.Code, body.On)
		writeJSON(w, http.StatusOK, map[string]any{"hidden": s.hidden.List()})
	})
	mux.HandleFunc("GET "+base+"/export.csv", func(w http.ResponseWriter, r *http.Request) {
		if s.scan == nil {
			http.Error(w, "扫描还没有启动", http.StatusServiceUnavailable)
			return
		}
		view := s.scanSettled(settledQuery(r, s.now()))
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="scan-settled.csv"`)
		_, _ = w.Write([]byte("\xef\xbb\xbf")) // so a spreadsheet opens the Chinese text correctly
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"结算日", "发出日", "来源", "代码", "名称", "预测", "板块", "天数", "本股涨幅%", "板块涨幅%", "超额%", "结果", "原因"})
		pct := func(v float64) string { return strconv.FormatFloat(v*100, 'f', 2, 64) }
		for _, day := range view["days"].([]map[string]any) {
			for _, row := range day["rows"].([]map[string]any) {
				_ = cw.Write([]string{day["day"].(string), fmt.Sprint(row["made_day"]), fmt.Sprint(row["source_text"]), fmt.Sprint(row["ts_code"]), fmt.Sprint(row["name"]),
					fmt.Sprint(row["verdict"]), fmt.Sprint(row["sector"]), fmt.Sprint(row["horizon"]), pct(row["stock_ret"].(float64)), pct(row["sector_ret"].(float64)),
					pct(row["excess"].(float64)), map[string]string{"hit": "对", "miss": "错", "flat": "持平"}[fmt.Sprint(row["outcome"])], fmt.Sprint(row["why"])})
			}
		}
		cw.Flush()
	})
}

// settledQuery reads the filters of the settled tab; "range" is a shortcut for a settle-day window ending today.
func settledQuery(r *http.Request, now time.Time) settledFilter {
	q := r.URL.Query()
	f := settledFilter{From: q.Get("from"), To: q.Get("to"), Source: q.Get("source"), Query: q.Get("q"), WrongOnly: q.Get("wrong") == "1"}
	f.Horizon, _ = strconv.Atoi(q.Get("horizon"))
	if f.From == "" {
		switch q.Get("range") {
		case "week":
			f.From = sched.Day(now.AddDate(0, 0, -7))
		case "month":
			f.From = sched.Day(now.AddDate(0, -1, 0))
		case "quarter":
			f.From = sched.Day(now.AddDate(0, -3, 0))
		case "year":
			f.From = fmt.Sprintf("%d-01-01", now.In(sched.Zone).Year())
		}
	}
	return f
}

// runScanNow starts one of the scan tasks right away instead of waiting for its time. The nightly run
// reuses today's conclusions (and writes them down as predictions); only stale ones are made again.
func (s *Server) runScanNow(task string) (map[string]any, error) {
	if s.sch == nil {
		return nil, errNoScheduler
	}
	switch task {
	case nightlyID:
		if s.llm == nil && s.stepFn == nil {
			return nil, fmt.Errorf("没有可用的模型：请先在设置里配置模型密钥")
		}
		var units []sched.UnitSpec
		for _, sub := range s.personal.Subjects() {
			if _, active := s.sch.Store.ActiveJob(nightlyID, sub.Code); active {
				continue
			}
			p := 50
			if sub.Tier == "hold" {
				p = 90
			}
			units = append(units, sched.UnitSpec{Key: sub.Code, Priority: p})
		}
		if len(units) == 0 {
			if len(s.personal.Subjects()) == 0 {
				return nil, fmt.Errorf("你还没有自选、持仓或想买的股票，没有东西可扫")
			}
			return map[string]any{"queued": 0, "reason": "都已经在跑"}, nil
		}
		run, err := s.sch.Manual(nightlyID, units, map[string]any{"on_demand": true, "reason": "scan_now"})
		if err != nil {
			return nil, err
		}
		s.sch.Tick()
		return map[string]any{"queued": len(units), "run_id": run.ID}, nil
	case "scan.settle":
		run, err := s.sch.Manual("scan.settle", nil, map[string]any{"on_demand": true})
		if err != nil {
			return nil, err
		}
		s.sch.Tick()
		return map[string]any{"queued": 1, "run_id": run.ID}, nil
	}
	return nil, fmt.Errorf("不认识的任务：%s", task)
}

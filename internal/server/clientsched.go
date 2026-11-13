package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-harness/internal/custom"
	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/quotes"
	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

const (
	nightlyID = "stock.nightly"
	// maxNightlyWaits is how often the nightly job waits for the stock service before giving up on it.
	moveThresholdPct = 7.0
)

var errNoScheduler = errors.New("调度器没有启动")

// initClientTasks sets up what runs on this machine: the scheduler, the inbox and the user's own data.
func (s *Server) initClientTasks(ctx context.Context) {
	dir := ""
	if strings.TrimSpace(s.cfg.DataDir) != "" {
		dir = filepath.Join(s.cfg.DataDir, "client")
	}
	file := func(name string) string {
		if dir == "" {
			return ""
		}
		return filepath.Join(dir, name)
	}
	calPath := ""
	if s.cfg.DataDir != "" {
		calPath = filepath.Join(s.cfg.DataDir, "calendar.json")
	}
	s.cal = sched.LoadCalendar(calPath)
	s.inbox = inbox.Open(file("inbox.json"), nil)
	s.inbox.AutoDispatch = true
	s.ensureWecomChannel()
	s.personal = personal.Open(file("personal.json"), nil)
	s.quotes = quotes.New()
	s.sch = sched.New(sched.OpenStore(file("sched.json")), s.cal, nil)
	s.relay = newRelayClient(file("relay.json"), time.Now)
	s.scan = scan.Open(file("predictions.json"), nil)
	s.hidden = openHidden(file("scan_hidden.json"))
	s.custom = custom.Open(file("custom_tasks.json"), nil)
	loadServiceSetting(file("service.json"))
	s.quotes.Via = s.backendQuotes
	s.registerTasks()
	s.sch.Listeners = append(s.sch.Listeners, s.onRunFinished)
	go s.sch.Run(ctx, 15*time.Second)
}

func (s *Server) registerTasks() {
	s.sch.AddGate("market_closed", func(day string, now time.Time) bool {
		if day < sched.Day(now) {
			return true
		}
		t, _ := time.ParseInLocation("2006-01-02 15:04", day+" 15:20", sched.Zone)
		return !now.Before(t)
	})
	s.sch.Register(&sched.Spec{
		ID: nightlyID, Title: "每晚个股结论", Category: sched.CatScan, Note: "给每只自选、持仓、想买各出一份结论；同一只票只保留最新一条在跟踪",
		Trace: map[string]any{
			"apis":    []any{"POST /api/v1/stocks/{code}/analyze", "本地结论落库"},
			"purpose": "对每只关注股票跑一轮分析并写出结论",
		},
		Queue: "llm", Gates: []string{"market_closed"},
		Trigger:     sched.Trigger{Kind: "daily", At: "20:30", WindowMin: 210, Days: "trading"},
		MaxAttempts: 3, Backoff: []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute}, Timeout: 15 * time.Minute,
		CatchupTTL: 12 * time.Hour, BudgetCalls: 500, Fanout: s.subjectUnits, Handler: s.nightly,
	})
	marketTrace := map[string]any{
		"apis":    []any{"POST /api/v1/market/refresh", "GET /api/v1/market/summary"},
		"purpose": "刷新股票服务市场缓存，再读大盘结论，写成简报通知",
	}
	s.sch.Register(&sched.Spec{
		ID: "market.pre", Title: "盘前简报", Category: sched.CatMarket, Note: "开盘前汇总大盘与隔夜要点",
		Trace: marketTrace,
		Queue: "data", Trigger: sched.Trigger{Kind: "daily", At: "08:30", WindowMin: 20, Days: "trading"},
		MaxAttempts: 3, Backoff: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}, CatchupTTL: 5 * time.Hour,
		Handler: func(c *sched.JobCtx) (any, error) { return s.marketBrief(c, "pre") },
	})
	s.sch.Register(&sched.Spec{
		ID: "market.close", Title: "收盘复盘", Category: sched.CatMarket, Note: "收盘后复盘当日行情",
		Trace: marketTrace,
		Queue: "data", Gates: []string{"market_closed"},
		Trigger:     sched.Trigger{Kind: "daily", At: "15:35", WindowMin: 20, Days: "trading"},
		MaxAttempts: 3, Backoff: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}, CatchupTTL: 16 * time.Hour,
		Handler: func(c *sched.JobCtx) (any, error) { return s.marketBrief(c, "close") },
	})
	alertTrace := map[string]any{
		"apis":    []any{"quotes.Fetch", "inbox.Publish(price.alert.fired|price.move)"},
		"purpose": "拉实时行情，核对到价提醒与持仓异动",
	}
	s.sch.Register(&sched.Spec{
		ID: "alerts.intraday", Title: "盘中价格提醒", Category: sched.CatMarket, Note: "交易时段每 3 分钟检查到价提醒",
		Trace: alertTrace,
		Queue: "light", Frequent: true, Trigger: sched.Trigger{Kind: "interval", EveryMin: 3, Days: "trading"},
		MaxAttempts: 1, Timeout: 90 * time.Second, Handler: func(c *sched.JobCtx) (any, error) { return s.evaluateAlerts(c, "touch") },
	})
	s.sch.Register(&sched.Spec{
		ID: "relay.sync", Title: "离线转发同步", Category: sched.CatNotify, Note: "把待投递的通知同步到外部渠道",
		Trace: map[string]any{
			"apis":    []any{"relay.syncAlerts", "relay.syncSubscriptions", "relay.pullEvents", "relay.pullSystemEvents"},
			"purpose": "与离线转发服务对账并拉回事件",
		},
		Queue: "light", Frequent: true, Trigger: sched.Trigger{Kind: "every", EveryMin: 2, Days: "any"},
		MaxAttempts: 1, Timeout: 60 * time.Second, Handler: s.relaySyncTask,
	})
	s.registerWatchTasks()
	s.registerScanTasks()
	s.registerCustomTasks()
	s.sch.Register(&sched.Spec{
		ID: "notify.dispatch", Title: "通知投递", Category: sched.CatNotify, Note: "按打扰策略投递收件箱里的通知",
		Trace: map[string]any{
			"apis":    []any{"inbox.Dispatch"},
			"purpose": "按打扰策略把收件箱待投递项发到桌面/外部渠道",
		},
		Queue: "light", Frequent: true, Trigger: sched.Trigger{Kind: "every", EveryMin: 1, Days: "any"},
		MaxAttempts: 1, Timeout: 60 * time.Second, Handler: func(c *sched.JobCtx) (any, error) {
			res := s.inbox.Dispatch()
			summary := fmt.Sprintf("投递结果：%v", res)
			out := map[string]any{"apis": []any{"inbox.Dispatch"}, "summary": summary, "detail": summary}
			for k, v := range res {
				out[k] = v
			}
			return out, nil
		},
	})
	for _, d := range []struct {
		id, title, at, days, label string
		hours                      int
	}{
		{"notify.digest.am", "早间摘要", "08:35", "any", "早间摘要", 16},
		{"notify.digest.pm", "盘后摘要", "15:40", "trading", "盘后摘要", 7},
	} {
		d := d
		s.sch.Register(&sched.Spec{
			ID: d.id, Title: d.title, Category: sched.CatNotify, Note: "把未单独打扰的通知折成一条摘要",
			Trace: map[string]any{
				"apis":         []any{"inbox.BuildDigest"},
				"purpose":      "把时间窗内未单独打扰的普通通知折成一条摘要",
				"label":        d.label,
				"window_hours": d.hours,
			},
			Queue: "light", Trigger: sched.Trigger{Kind: "daily", At: d.at, WindowMin: 60, Days: d.days}, MaxAttempts: 1, CatchupTTL: 3 * time.Hour,
			Handler: func(c *sched.JobCtx) (any, error) {
				since := c.Now().Add(-time.Duration(d.hours) * time.Hour)
				made, n := s.inbox.BuildDigest(d.label, since)
				out := map[string]any{
					"absorbed":     n,
					"label":        d.label,
					"window_hours": d.hours,
					"since":        since.In(sched.Zone).Format("2006-01-02 15:04"),
					"apis":         []any{"inbox.BuildDigest"},
				}
				if made == nil {
					out["summary"] = "这段时间没有可折进摘要的通知（或摘要开关未开）"
					out["detail"] = out["summary"]
					return out, nil
				}
				out["summary"] = made.Title
				out["detail"] = made.Body
				return out, nil
			},
		})
	}
	s.sch.Register(&sched.Spec{
		ID: "alerts.close", Title: "收盘提醒", Category: sched.CatMarket, Note: "收盘后按收盘价确认到价提醒",
		Trace: alertTrace,
		Queue: "light", Gates: []string{"market_closed"},
		Trigger:     sched.Trigger{Kind: "daily", At: "15:35", WindowMin: 60, Days: "trading"},
		MaxAttempts: 3, Backoff: []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute}, CatchupTTL: 24 * time.Hour,
		Handler: func(c *sched.JobCtx) (any, error) { return s.evaluateAlerts(c, "close") },
	})
}

// subjectUnits is the nightly fan-out: holdings first, then wishes, then the rest of the watchlist.
func (s *Server) subjectUnits(*sched.PlanCtx) []sched.Unit {
	var out []sched.Unit
	for _, sub := range s.personal.Subjects() {
		p := 50
		if sub.Tier == "hold" {
			p = 90
		}
		out = append(out, sched.Unit{Key: sub.Code, Priority: p, Input: map[string]any{"tier": sub.Tier}})
	}
	return out
}

// ---- the nightly conclusion

// speak runs one speaking step. The default asks the model through the analyst machinery; tests replace it.
func (s *Server) speak(ctx context.Context, id, code string) stepOutcome {
	if s.stepFn != nil {
		return s.stepFn(ctx, id, code)
	}
	cfg, err := loadAgent(id)
	out := stepOutcome{ID: id, Name: cfg.Name, Label: diagnosisLabel[id]}
	if err != nil {
		out.State, out.Reason = "failed", err.Error()
		return out
	}
	msg, err := s.runAnalyst(ctx, cfg, code, "analysis", "")
	if err != nil {
		out.State, out.Reason = "failed", err.Error()
		return out
	}
	switch id {
	case "bull", "bear":
		out.State = "ok" // an empty set of arguments is an honest answer, not a failure
	case "chief":
		if t, _ := msg["text"].(string); strings.TrimSpace(t) == "" {
			out.State, out.Reason = "failed", "主分析师没有给出裁决"
		} else {
			out.State = "ok"
		}
	default:
		h, _ := msg["headline"].(map[string]any)
		if t, _ := msg["text"].(string); h == nil || strings.TrimSpace(t) == "" {
			out.State, out.Reason = "failed", "没有给出可用的结论"
		} else {
			out.State, out.Headline = "ok", h
		}
	}
	return out
}

func (s *Server) nightly(c *sched.JobCtx) (any, error) {
	code := c.Unit()
	if s.llm == nil && s.stepFn == nil {
		return nil, sched.Permanent("没有可用的模型：请先配置模型密钥")
	}
	now := c.Now()
	force, _ := c.Input()["force"].(bool)
	if !force {
		if b := s.readBundle(code); b != nil && b.GeneratedDay == sched.Day(now) && b.TradeDate == sched.Day(s.cal.LastClose(now)) && b.Source == "agent" {
			// nothing to redo, but a conclusion made before predictions were kept still has to be written down
			made, err := sched.Step(c, "predict", func() (bool, error) { return s.recordPrediction(c, b) })
			return map[string]any{"ts_code": code, "reused": true, "predicted": made && err == nil}, nil
		}
	}
	unlock := s.lockCode(code)
	defer unlock()

	step := func(name, id string, mustSucceed bool) (stepOutcome, error) {
		return sched.Step(c, name, func() (stepOutcome, error) {
			o := s.speak(c.Ctx, id, code)
			if o.State == "failed" && mustSucceed && !c.LastTry() {
				return o, sched.Transient(fmt.Sprintf("%s没说成：%s", diagnosisLabel[id], o.Reason))
			}
			if o.State == "ok" {
				c.AddCalls(1)
			}
			return o, nil
		})
	}
	outcomes := map[string]stepOutcome{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errs []string
	for _, id := range diagnosisDims {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			o, err := step("analyst."+id, id, true)
			mu.Lock()
			defer mu.Unlock()
			o.ID = id
			outcomes[id] = o
			if err != nil {
				errs = append(errs, err.Error())
			}
		}(id)
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, sched.Transient(strings.Join(errs, "；"))
	}
	for _, id := range []string{"bull", "bear", "chief"} {
		o, err := step(id, id, id == "chief")
		if err != nil {
			return nil, err
		}
		o.ID = id
		outcomes[id] = o
	}
	levels, _ := sched.Step(c, "levels", func() (map[string]any, error) { return s.fetchLevels(code), nil }) // never fails the job
	made, err := sched.Step(c, "conclude", func() (map[string]any, error) {
		b := s.buildBundle(code, c.Now(), outcomes, levels)
		b.Trigger = triggerOf(c)
		s.writeBundle(b)
		return map[string]any{"id": b.ID, "source": b.Source}, nil
	})
	if err != nil {
		return nil, err
	}
	if _, err := sched.Step(c, "predict", func() (bool, error) { return s.recordPrediction(c, s.readBundle(code)) }); err != nil {
		return nil, err
	}
	changed, err := sched.Step(c, "diff", func() (bool, error) { return s.publishChange(c, code) }) // never fails the job
	if err != nil {
		return nil, err
	}
	return map[string]any{"ts_code": code, "bundle": made["id"], "source": made["source"], "changed": changed}, nil
}

// publishChange compares the last two days' verdicts and tells the user when the lean changed.
func (s *Server) publishChange(c *sched.JobCtx, code string) (bool, error) {
	hist := s.verdictHistory(code)
	if len(hist) < 2 {
		return false, nil
	}
	before, after := hist[len(hist)-2], hist[len(hist)-1]
	today := sched.Day(c.Now())
	if after["trade_date"] != today || before["lean"] == after["lean"] {
		return false, nil
	}
	names := map[string]string{"bullish": "偏多", "bearish": "偏空", "neutral": "不明"}
	headline := fmt.Sprintf("从%s变为%s", names[fmt.Sprint(before["lean"])], names[fmt.Sprint(after["lean"])])
	_, created, err := s.inbox.Publish("judgment.changed", "stock", code, map[string]any{
		"ts_code": code, "name": s.stockName(code), "trade_date": today, "headline": headline,
	}, fmt.Sprintf("%s:%s:%v", code, today, after["lean"]), inbox.Options{TradeDate: today})
	return created, err
}

// ---- market brief

func (s *Server) stockGetJSON(path string, out any) error {
	return stockGetJSONWithin(path, out, 60*time.Second)
}

// stockGetJSONWithin is stockGetJSON with the time the data service has to answer.
func stockGetJSONWithin(path string, out any, within time.Duration) error {
	client := &http.Client{Timeout: within}
	req, err := http.NewRequest(http.MethodGet, stockAPIBase()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-User-Id", clientUserID())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("股票服务返回 %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// stockPostRefresh asks the data service to drop its market bundle cache, so
// the summary read right after is freshly computed (the two scheduled points).
func (s *Server) stockPostRefresh() error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, stockAPIBase()+"/api/v1/market/refresh", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", clientUserID())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("股票服务返回 %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) marketBrief(c *sched.JobCtx, session string) (any, error) {
	apis := []any{"POST /api/v1/market/refresh", "GET /api/v1/market/summary"}
	// 先让数据服务丢弃缓存重算：定时任务点上的换档靠这一步，平时页面读到的可能是 5 分钟内的旧切片。
	if err := s.stockPostRefresh(); err != nil {
		return nil, sched.Transient("触发市场重算失败：" + err.Error())
	}
	var summary map[string]any
	if err := s.stockGetJSON("/api/v1/market/summary", &summary); err != nil {
		return nil, sched.Transient("取市场数据失败：" + err.Error())
	}
	title, text, headline := briefText(session, summary)
	if title == "" {
		return nil, sched.Permanent("市场数据里没有结论")
	}
	if c.Trigger() != "catchup" { // a brief that is hours late is noise
		day := c.TradeDate()
		if day == "" {
			day = sched.Day(c.Now())
		}
		if _, _, err := s.inbox.Publish("market.brief", "market", "market", map[string]any{"title": title, "text": text},
			"market:"+session+":"+day, inbox.Options{TradeDate: day}); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"session": session, "summary": headline, "title": title, "detail": text, "apis": apis,
	}, nil
}

func briefText(session string, summary map[string]any) (title, text, headline string) {
	con, _ := summary["conclusion"].(map[string]any)
	if con == nil {
		return "", "", ""
	}
	level, _ := con["level"].(string)
	tilt, _ := con["tilt"].(string)
	head := level + tilt
	var sigs []string
	if list, ok := con["signals"].([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				sigs = append(sigs, fmt.Sprintf("%v%v %v", m["n"], m["name"], m["label"]))
			}
		}
	}
	action, _ := con["action"].(string)
	text = fmt.Sprintf("档位 %s。%s。\n怎么做：%s", head, strings.Join(sigs, "；"), action)
	title = "市场 · 盘前简报"
	if session == "close" {
		title = "市场 · 收盘复盘"
	}
	return title, text, head
}

// ---- price alerts

type firedAlert struct {
	Alert personal.Alert
	Price float64
}

// reached tells which alerts the quotes have touched. confirm "touch" looks at alerts that fire on the
// live price, "close" at those that wait for the close.
func reached(alerts []personal.Alert, q map[string]quotes.Quote, confirm string) []firedAlert {
	var out []firedAlert
	for _, a := range alerts {
		if a.Confirm != confirm {
			continue
		}
		quote, ok := q[a.Code]
		if !ok {
			continue
		}
		if (a.Side == "down" && quote.Price <= a.Price) || (a.Side == "up" && quote.Price >= a.Price) {
			out = append(out, firedAlert{a, quote.Price})
		}
	}
	return out
}

func (s *Server) evaluateAlerts(c *sched.JobCtx, confirm string) (any, error) {
	alerts := s.personal.Alerts("")
	if s.relay != nil && s.relay.authoritative() {
		alerts = nil // the server evaluates price alerts while it answers, also when this computer is off
	}
	held := personal.Held(s.personal.Trades())
	codes := map[string]bool{}
	for _, a := range alerts {
		if a.Confirm == confirm {
			codes[a.Code] = true
		}
	}
	if confirm == "touch" {
		for code := range held {
			codes[code] = true
		}
	}
	apis := []any{"quotes.Fetch", "inbox.Publish(price.alert.fired|price.move)"}
	if len(codes) == 0 {
		return map[string]any{
			"watching": 0, "alerts": 0, "moves": 0, "apis": apis,
			"summary": "没有需要检查的提醒或持仓", "detail": "没有需要检查的提醒或持仓",
		}, nil
	}
	list := make([]string, 0, len(codes))
	for code := range codes {
		list = append(list, code)
	}
	q, err := sched.Step(c, "quotes", func() (map[string]quotes.Quote, error) {
		got, err := s.quotes.Fetch(list)
		if err != nil && len(got) == 0 {
			return nil, sched.Transient(err.Error())
		}
		return got, nil
	})
	if err != nil {
		return nil, err
	}
	day := sched.Day(c.Now())
	fired, moves := 0, 0
	for _, f := range reached(alerts, q, confirm) {
		name := f.Alert.Name
		if name == "" {
			name = s.stockName(f.Alert.Code)
		}
		_, created, err := s.inbox.Publish("price.alert.fired", "stock", f.Alert.Code, map[string]any{
			"alert_id": f.Alert.ID, "ts_code": f.Alert.Code, "name": s.stockName(f.Alert.Code), "side": f.Alert.Side,
			"trigger_price": f.Alert.Price, "observed_price": f.Price, "confirm": f.Alert.Confirm,
		}, fmt.Sprintf("alert:%d:%s", f.Alert.ID, day), inbox.Options{TradeDate: day})
		_ = name
		if err == nil && created {
			fired++
		}
	}
	if confirm == "touch" {
		for code := range held {
			quote, ok := q[code]
			if !ok || (quote.ChangePct < moveThresholdPct && quote.ChangePct > -moveThresholdPct) {
				continue
			}
			dir := "up"
			if quote.ChangePct < 0 {
				dir = "down"
			}
			_, created, err := s.inbox.Publish("price.move", "stock", code, map[string]any{
				"ts_code": code, "name": s.stockName(code), "pct": quote.ChangePct, "observed_price": quote.Price,
			}, fmt.Sprintf("move:%s:%s:%s", code, day, dir), inbox.Options{TradeDate: day})
			if err == nil && created {
				moves++
			}
		}
	}
	summary := fmt.Sprintf("盯 %d 只 · 到价 %d · 异动 %d", len(list), fired, moves)
	return map[string]any{
		"watching": len(list), "alerts": fired, "moves": moves, "apis": apis,
		"summary": summary, "detail": summary,
	}, nil
}

// ---- receipts

var stateText = map[string]string{"succeeded": "成功", "skipped": "已跳过", "expired": "已过期", "partial": "部分失败", "failed": "失败"}
var triggerText = map[string]string{"schedule": "定时", "catchup": "补跑", "manual": "手动"}

// onRunFinished writes the receipt of a finished run into the inbox, so the user can see that a task
// really ran. Failures are loud (a desktop pop-up); successes only go into the inbox.
func (s *Server) onRunFinished(run sched.Run, sp *sched.Spec) {
	if s.inbox == nil {
		return
	}
	jobs := s.sch.Store.Jobs(run.ID)
	switch run.State {
	case "failed", "partial":
		dead, first := 0, ""
		for _, j := range jobs {
			if j.State == "dead" {
				dead++
				if first == "" && j.Error != nil {
					first = j.Error.Message
				}
			}
		}
		_, _, _ = s.inbox.Publish("task.failed", "system", run.TaskID, map[string]any{
			"task_id": run.TaskID, "title": sp.Title, "run_id": run.ID, "dead": dead, "total": len(jobs), "first_error": first,
		}, run.ID, inbox.Options{})
	case "succeeded", "skipped", "expired":
		payload := map[string]any{
			"task_id": run.TaskID, "title": sp.Title, "run_id": run.ID, "state": run.State, "state_text": stateText[run.State],
			"trigger": run.Trigger, "trigger_text": triggerText[run.Trigger], "jobs": run.Counts["jobs"], "succeeded": run.Counts["succeeded"],
			"note": run.Note,
		}
		if run.StartedAt > 0 && run.FinishedAt > 0 {
			payload["duration_ms"] = run.FinishedAt - run.StartedAt
		}
		attachJobResult(payload, jobs, sp)
		key := "client:" + run.ID
		if sp.Frequent && (run.Trigger == "schedule" || run.Trigger == "catchup") {
			key = fmt.Sprintf("client:daily:%s:%s:%s", run.TaskID, sched.Day(time.UnixMilli(run.FinishedAt)), run.State)
		}
		_, _, _ = s.inbox.Publish("task.completed", "system", run.TaskID, payload, key, inbox.Options{})
	}
}

// attachJobResult copies human-readable result fields from job outputs into the receipt.
func attachJobResult(payload map[string]any, jobs []sched.Job, sp *sched.Spec) {
	if sp != nil && sp.Trace != nil {
		if apis, ok := sp.Trace["apis"]; ok {
			payload["apis"] = apis
		}
		if purpose, ok := sp.Trace["purpose"].(string); ok && purpose != "" {
			payload["purpose"] = purpose
		}
	}
	for _, j := range jobs {
		m, ok := j.Output.(map[string]any)
		if !ok || m == nil {
			continue
		}
		if _, have := payload["apis"]; !have {
			if apis, ok := m["apis"]; ok {
				payload["apis"] = apis
			}
		}
		for _, k := range []string{"detail", "summary", "text"} {
			if payload[k] != nil {
				continue
			}
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				payload[k] = v
			}
		}
		if payload["detail"] == nil {
			if title, _ := m["title"].(string); title != "" {
				if text, _ := m["text"].(string); text != "" {
					payload["detail"] = title + "\n" + text
				} else {
					payload["detail"] = title
				}
			}
		}
		if payload["detail"] == nil && payload["summary"] == nil {
			if syn := summarizeCounts(m); syn != "" {
				payload["summary"] = syn
			}
		}
		if payload["detail"] != nil || payload["summary"] != nil {
			break
		}
	}
}

func summarizeCounts(m map[string]any) string {
	parts := []string{}
	for _, k := range []string{"watching", "alerts", "moves", "absorbed", "events", "created", "n", "hit", "decided"} {
		if v, ok := fltAny(m[k]); ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func fltAny(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// ensureWecomChannel turns the WeCom webhook from the config file (the only outside channel before
// notification settings existed) into a channel, so what worked keeps working.
func (s *Server) ensureWecomChannel() {
	hook := strings.TrimSpace(s.cfg.WecomWebhook)
	if hook == "" {
		return
	}
	if _, ok := s.inbox.Channel("wecom"); ok {
		return
	}
	_, _ = s.inbox.PutChannel(inbox.Channel{ID: "wecom", Type: "wecom_webhook", Name: "企业微信群", Enabled: true, MinSeverity: "normal", Secret: hook, Relay: true})
}

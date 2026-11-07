package server

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

// What the scan page shows is built from the predictions the client writes down itself: every conclusion
// (the nightly run, or one you asked for) becomes one prediction, checked against its sector five and
// twenty trading days later. The prices and the sector's index come from the data service.

func (s *Server) registerScanTasks() {
	s.sch.Register(&sched.Spec{
		ID: "scan.settle", Title: "预测结算", Queue: "data", Gates: []string{"market_closed"},
		Trigger: sched.Trigger{Kind: "daily", At: "17:30", WindowMin: 240, Days: "trading"}, MaxAttempts: 3, Backoff: []time.Duration{10 * time.Minute, 30 * time.Minute, time.Hour},
		CatchupTTL: 20 * time.Hour, Timeout: 5 * time.Minute, Handler: s.settleScan,
	})
}

// triggerOf says what asked for a conclusion: the schedule, or the reason a run was queued for.
func triggerOf(c *sched.JobCtx) string {
	if on, _ := c.Input()["on_demand"].(bool); !on {
		return "schedule"
	}
	if reason, _ := c.Input()["reason"].(string); reason != "" {
		return reason
	}
	return "manual"
}

// sourceFromTrigger is the source a prediction is filed under. A conclusion you asked for (you pressed
// 重新分析, opened a stock that had none, or added it) is yours; the schedule's, the run-now button's and the
// background fill-ins are the scan's.
func sourceFromTrigger(trigger string) string {
	switch trigger {
	case "manual", "first_view", "watch_add":
		return "diagnose"
	}
	return "scan"
}

// sourceOf is the source for a conclusion that is being written down. The conclusion remembers what asked for
// it; one made before it remembered falls back to the run that is writing it down.
func sourceOf(c *sched.JobCtx, b *conclusionBundle) string {
	if b != nil && b.Trigger != "" {
		return sourceFromTrigger(b.Trigger)
	}
	return sourceFromTrigger(triggerOf(c))
}

func firstSentence(text string) string {
	text = strings.TrimSpace(strings.NewReplacer("**", "", "\n", " ").Replace(text))
	for _, sep := range []string{"。", "；", "！", "？"} {
		if i := strings.Index(text, sep); i > 0 {
			text = text[:i]
		}
	}
	r := []rune(text)
	if len(r) > 70 {
		return string(r[:70]) + "…"
	}
	return text
}

func leanFromStatus(status string) string {
	switch status {
	case "support":
		return "bullish"
	case "warn":
		return "bearish"
	}
	return "neutral"
}

// recordPrediction writes down the conclusion that was just made. It is idempotent per conclusion.
func (s *Server) recordPrediction(c *sched.JobCtx, b *conclusionBundle) (bool, error) {
	if s.scan == nil || b == nil || b.Chief == nil || b.Source != "agent" {
		return false, nil
	}
	verdict, _ := b.Chief["verdict"].(string)
	text, _ := b.Chief["text"].(string)
	// a conclusion that is being reused was made earlier: the bundle id is the moment it was made
	madeAt := c.Now()
	if b.ID > 1e12 && time.UnixMilli(b.ID).Before(madeAt) {
		madeAt = time.UnixMilli(b.ID)
	}
	dir := leanOf(verdict)
	if dir == "neutral" {
		dir = scan.Unclear
	}
	level := levelOfVerdict(verdict)
	if dir == scan.Unclear {
		level = 0
	}
	p := scan.Prediction{
		Code: b.Code, Name: s.stockName(b.Code), Source: sourceOf(c, b), Direction: dir, Level: level, Verdict: verdict, Evidence: firstSentence(text),
		MadeAt: madeAt.UnixMilli(), BaseDay: sched.Day(s.cal.LastClose(madeAt)), BundleID: b.ID,
	}
	if info := (holdingsData{s}).Sector(b.Code); info.Name != "" {
		p.Sector = info.Name
	}
	if m := (holdingsData{s}).Market(); m != nil {
		p.Market = scan.Market(m.Level)
	}
	for _, row := range b.Analysts {
		a := scan.Analyst{Dimension: row.Dimension, Name: row.Name, Lean: "neutral"}
		if row.Headline != nil {
			a.Lean = leanFromStatus(fmt.Sprint(row.Headline["status"]))
			a.Text = fmt.Sprint(row.Headline["text"])
		}
		if row.Name == "" {
			a.Name = row.Label
		}
		p.Analysts = append(p.Analysts, a)
	}
	for _, bar := range (holdingsData{s}).Closes(b.Code) {
		if bar.Day == p.BaseDay {
			p.BaseClose = bar.Close
		}
	}
	_, made := s.scan.Add(p)
	return made, nil
}

// ---- prices

type priced struct {
	stock, sector []scan.Bar
	sectorName    string
}

func barsOf(body map[string]any) []scan.Bar {
	var out []scan.Bar
	items, _ := body["items"].([]any)
	for _, it := range items {
		pair, _ := it.([]any)
		if len(pair) == 2 {
			day, _ := pair[0].(string)
			if cl, ok := pair[1].(float64); ok && day != "" {
				out = append(out, scan.Bar{Day: day, Close: cl})
			}
		}
	}
	return out
}

func (s *Server) pricesOf(code string) priced {
	stock := barsOf(s.shared("/closes/"+url.PathEscape(code), 30*time.Minute))
	sec := s.shared("/sector-series/"+url.PathEscape(code), 30*time.Minute)
	name, _ := sec["name"].(string)
	return priced{stock: stock, sector: barsOf(sec), sectorName: name}
}

// ---- settling

// settleScan checks every prediction that has come due and writes the result, once.
func (s *Server) settleScan(c *sched.JobCtx) (any, error) {
	preds := s.scan.All()
	want := sched.Day(s.cal.LastClose(c.Now()))
	type due struct {
		p scan.Prediction
		h int
	}
	var todo []due
	for _, p := range preds {
		if p.Direction == scan.Unclear {
			continue
		}
		for _, h := range scan.Horizons {
			if p.Result(h) == nil {
				todo = append(todo, due{p, h})
			}
		}
	}
	if len(todo) == 0 {
		return map[string]any{"settled": 0}, nil
	}
	cache := map[string]priced{}
	stale := false
	settled := map[int][]scan.Prediction{}
	counts := map[string]int{}
	var lines []string
	now := c.Now()
	for _, d := range todo {
		px, ok := cache[d.p.Code]
		if !ok {
			px = s.pricesOf(d.p.Code)
			cache[d.p.Code] = px
		}
		if n := len(px.stock); n == 0 || px.stock[n-1].Day < want {
			stale = true
			continue
		}
		t := scan.Track(d.p, px.stock, px.sector, d.h)
		if !t.Have || !t.Done {
			continue
		}
		r := scan.Settle(d.p, t, now)
		if s.scan.SetResult(d.p.ID, d.h, r) {
			counts[r.Outcome]++
			counts["n"]++
			d.p.Results = map[string]*scan.Result{fmt.Sprint(d.h): &r}
			settled[d.h] = append(settled[d.h], d.p)
		}
	}
	if counts["n"] == 0 && stale && c.Waits() < 4 {
		c.BumpWait()
		return nil, sched.NotReady("日线还没有最新一天", 20*time.Minute)
	}
	if counts["n"] > 0 {
		related := s.relatedCodes()
		for _, h := range scan.Horizons {
			items := settled[h]
			if len(items) == 0 {
				continue
			}
			var hit, miss, flat int
			var mine []string
			for _, p := range items {
				r := p.Result(h)
				switch r.Outcome {
				case "hit":
					hit++
				case "miss":
					miss++
				default:
					flat++
				}
				if related[p.Code] && len(mine) < 5 {
					mark := map[string]string{"hit": "✓", "miss": "✗", "flat": "="}[r.Outcome]
					mine = append(mine, fmt.Sprintf("%s %s %s", p.Name, word(p.Direction), mark))
				}
			}
			line := fmt.Sprintf("%d 天：对 %d · 错 %d · 持平 %d", h, hit, miss, flat)
			lines = append(lines, line)
			if len(mine) > 0 {
				lines = append(lines, "和你有关的："+strings.Join(mine, "；"))
			}
		}
		_, _, _ = s.inbox.Publish("scan.settled", "system", "scan", map[string]any{"n": counts["n"], "hit": counts["hit"], "decided": counts["hit"] + counts["miss"], "lines": lines},
			"scan:"+sched.Day(now), inbox.Options{TradeDate: want})
	}
	return map[string]any{"settled": counts["n"], "hit": counts["hit"], "miss": counts["miss"], "flat": counts["flat"]}, nil
}

func word(direction string) string {
	if direction == scan.Bullish {
		return "偏多"
	}
	if direction == scan.Bearish {
		return "偏空"
	}
	return "不明"
}

// relatedCodes are the stocks the user holds, wishes for or follows.
func (s *Server) relatedCodes() map[string]bool {
	out := map[string]bool{}
	for _, sub := range s.personal.Subjects() {
		out[sub.Code] = true
	}
	return out
}

// dueDay estimates the day a horizon falls on: n trading days after the base day, by the stock's own
// bars where they reach, otherwise counting weekdays.
func (s *Server) dueDay(base string, n int, bars []scan.Bar) string {
	for i, b := range bars {
		if b.Day == base && i+n < len(bars) {
			return bars[i+n].Day
		}
	}
	d, err := time.ParseInLocation("2006-01-02", base, sched.Zone)
	if err != nil {
		return ""
	}
	for left := n; left > 0; {
		d = d.AddDate(0, 0, 1)
		if s.cal.IsTradingDay(d) {
			left--
		}
	}
	return sched.Day(d)
}

var dimLabel = map[string]string{"volume_flow": "资金流", "sector": "板块", "news": "新闻", "finance": "财报", "fundamental": "基本面"}

// ---- views

func (s *Server) mineOf(code string, related map[string]bool) map[string]any {
	held := personal.Held(s.personal.Trades())[code] != nil
	wish := false
	for _, w := range s.personal.Wishes() {
		wish = wish || w.Code == code
	}
	return map[string]any{"held": held, "wish": wish, "related": related[code]}
}

func localClock(ms int64) time.Time { return time.UnixMilli(ms).In(sched.Zone) }

func (s *Server) predRow(p scan.Prediction, px *priced, related map[string]bool) map[string]any {
	var bars []scan.Bar
	if px != nil {
		bars = px.stock
	}
	row := map[string]any{
		"id": p.ID, "source": p.Source, "source_text": map[string]string{"scan": "定时扫描", "diagnose": "你诊断"}[p.Source], "ts_code": p.Code, "name": p.Name,
		"sector": p.Sector, "made_day": localClock(p.MadeAt).Format("2006-01-02"), "made_at": localClock(p.MadeAt).Format("15:04"), "base_day": p.BaseDay,
		"base_close": p.BaseClose, "verdict": p.Verdict, "direction": p.Direction, "level": p.Level, "evidence": p.Evidence, "mine": s.mineOf(p.Code, related),
		"due": map[string]any{"5": s.dueDay(p.BaseDay, 5, bars), "20": s.dueDay(p.BaseDay, 20, bars)},
	}
	return row
}

func (s *Server) madeDays(preds []scan.Prediction) []string {
	seen := map[string]bool{}
	var days []string
	for _, p := range preds {
		d := localClock(p.MadeAt).Format("2006-01-02")
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Strings(days)
	return days
}

// scanSummary is the head of the page: the four numbers and what the scan covered.
func (s *Server) scanSummary() map[string]any {
	preds := s.scan.All()
	days := s.madeDays(preds)
	today := sched.Day(s.now())
	shown := today
	if len(days) > 0 && days[len(days)-1] < today {
		shown = days[len(days)-1]
	}
	var made, bull, bear, unclear, scanN, diagN int
	tracking, settledToday, hitToday := 0, 0, 0
	for _, p := range preds {
		if localClock(p.MadeAt).Format("2006-01-02") == shown {
			made++
			switch p.Direction {
			case scan.Bullish:
				bull++
			case scan.Bearish:
				bear++
			default:
				unclear++
			}
			if p.Source == "scan" {
				scanN++
			} else {
				diagN++
			}
		}
		if p.Direction != scan.Unclear {
			for _, h := range scan.Horizons {
				if p.Result(h) == nil {
					tracking++
				}
			}
		}
		if r := p.Result(5); r != nil && r.Day == today && r.Outcome != "" {
			settledToday++
			if r.Outcome == "hit" {
				hitToday++
			}
		}
	}
	acc := scan.Stats(preds, 5, 60, s.now())
	var pct any
	if acc.Overall.Pct >= 0 {
		pct = acc.Overall.Pct
	}
	var market any
	if m := (holdingsData{s}).Market(); m != nil {
		market = map[string]any{"level": m.Level, "tilt": m.Tilt, "text": m.Text}
	}
	return map[string]any{
		"day": shown, "today": today, "market": market, "next_run": s.nextRunText(),
		"made": made, "tracking": tracking, "settled_today": map[string]any{"hit": hitToday, "n": settledToday}, "accuracy": pct, "accuracy_n": acc.Overall.N,
		"funnel": map[string]any{"subjects": len(s.personal.Subjects()), "researched": made, "bullish": bull, "bearish": bear, "unclear": unclear, "scan": scanN, "diagnose": diagN},
		"empty":  len(preds) == 0,
	}
}

func (s *Server) scanToday(source string) map[string]any {
	preds := s.scan.All()
	sum := s.scanSummary()
	day := sum["day"].(string)
	related := s.relatedCodes()
	rows := []map[string]any{}
	for i := len(preds) - 1; i >= 0; i-- {
		p := preds[i]
		if localClock(p.MadeAt).Format("2006-01-02") != day || (source != "" && source != "all" && p.Source != source) {
			continue
		}
		px := s.pricesOf(p.Code)
		rows = append(rows, s.predRow(p, &px, related))
	}
	return map[string]any{"day": day, "rows": rows, "total": len(rows)}
}

func (s *Server) trackingRow(p scan.Prediction, h int, px priced, related map[string]bool) map[string]any {
	t := scan.Track(p, px.stock, px.sector, h)
	row := s.predRow(p, &px, related)
	row["horizon"], row["day"], row["have"] = h, t.Day, t.Have
	if t.Have {
		row["stock_ret"], row["sector_ret"], row["excess"] = t.StockRet, t.SectorRet, t.Excess
		o := scan.Judge(p.Direction, t.Excess)
		row["now"] = o
		row["close_call"] = o != "" && (t.Excess < 0.01 && t.Excess > -0.01)
	}
	row["settle_day"] = s.dueDay(p.BaseDay, h, px.stock)
	return row
}

func (s *Server) scanTracking(due string, mineOnly bool, source string) map[string]any {
	preds := s.scan.All()
	related := s.relatedCodes()
	cache := map[string]priced{}
	rows := []map[string]any{}
	hit, close5 := 0, 0
	byHorizon := map[int]int{}
	tomorrow := 0
	mineCount := 0
	today := sched.Day(s.now())
	tomorrowDay := sched.Day(s.now().AddDate(0, 0, 1))
	for _, p := range preds {
		if p.Direction == scan.Unclear {
			continue
		}
		px, ok := cache[p.Code]
		if !ok {
			px = s.pricesOf(p.Code)
			cache[p.Code] = px
		}
		for _, h := range scan.Horizons {
			if p.Result(h) != nil {
				continue
			}
			row := s.trackingRow(p, h, px, related)
			byHorizon[h]++
			if row["now"] == "hit" {
				hit++
			}
			if sd, _ := row["settle_day"].(string); sd <= tomorrowDay && sd >= today && h == 5 {
				tomorrow++
			}
			if row["mine"].(map[string]any)["related"] == true {
				mineCount++
			}
			if mineOnly && row["mine"].(map[string]any)["related"] != true {
				continue
			}
			if source != "" && source != "all" && p.Source != source {
				continue
			}
			if !dueMatch(due, row["settle_day"].(string), s.now()) {
				continue
			}
			rows = append(rows, row)
		}
	}
	_ = close5
	sort.SliceStable(rows, func(a, b int) bool { return rows[a]["settle_day"].(string) < rows[b]["settle_day"].(string) })
	total := byHorizon[5] + byHorizon[20]
	return map[string]any{"rows": rows, "total": total, "by_horizon": map[string]int{"5": byHorizon[5], "20": byHorizon[20]}, "now_right": hit, "tomorrow": tomorrow, "mine": mineCount}
}

// dueMatch filters tracking rows by when they settle.
func dueMatch(filter, day string, now time.Time) bool {
	switch filter {
	case "", "all":
		return true
	case "tomorrow":
		return day == sched.Day(now.AddDate(0, 0, 1)) || day == sched.Day(now)
	case "week":
		end := now.AddDate(0, 0, 7-int((now.Weekday()+6)%7))
		return day <= sched.Day(end)
	case "next":
		start := now.AddDate(0, 0, 8-int((now.Weekday()+6)%7))
		return day >= sched.Day(start) && day <= sched.Day(start.AddDate(0, 0, 6))
	}
	return true
}

type settledFilter struct {
	From, To, Source, Query string
	Horizon                 int
	WrongOnly               bool
}

func (s *Server) scanSettled(f settledFilter) map[string]any {
	preds := s.scan.All()
	related := s.relatedCodes()
	type item struct {
		p scan.Prediction
		h int
		r *scan.Result
	}
	var items []item
	for _, p := range preds {
		for _, h := range scan.Horizons {
			r := p.Result(h)
			if r == nil || r.Outcome == "" || (f.Horizon != 0 && h != f.Horizon) {
				continue
			}
			if f.From != "" && r.Day < f.From || f.To != "" && r.Day > f.To {
				continue
			}
			if f.Source != "" && f.Source != "all" && p.Source != f.Source {
				continue
			}
			if f.WrongOnly && r.Outcome != "miss" {
				continue
			}
			if q := strings.TrimSpace(f.Query); q != "" && !strings.Contains(p.Name, q) && !strings.Contains(strings.ToLower(p.Code), strings.ToLower(q)) {
				continue
			}
			items = append(items, item{p, h, r})
		}
	}
	groups := map[string][]item{}
	var days []string
	for _, it := range items {
		if _, ok := groups[it.r.Day]; !ok {
			days = append(days, it.r.Day)
		}
		groups[it.r.Day] = append(groups[it.r.Day], it)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	var total scan.Rate
	out := []map[string]any{}
	for _, d := range days {
		var rate scan.Rate
		rows := []map[string]any{}
		reasons := map[string]int{}
		mine := []map[string]any{}
		for _, it := range groups[d] {
			switch it.r.Outcome {
			case "hit":
				rate.Hit++
			case "miss":
				rate.Miss++
			default:
				rate.Flat++
			}
			rate.N++
			total.N++
			switch it.r.Outcome {
			case "hit":
				total.Hit++
			case "miss":
				total.Miss++
			default:
				total.Flat++
			}
			row := map[string]any{"id": it.p.ID, "horizon": it.h, "source": it.p.Source, "source_text": map[string]string{"scan": "扫描", "diagnose": "你诊断"}[it.p.Source], "ts_code": it.p.Code, "name": it.p.Name,
				"verdict": it.p.Verdict, "direction": it.p.Direction, "sector": it.p.Sector, "stock_ret": it.r.StockRet, "sector_ret": it.r.SectorRet, "excess": it.r.Excess,
				"outcome": it.r.Outcome, "why": it.r.Why, "made_day": it.p.BaseDay, "mine": s.mineOf(it.p.Code, related)}
			rows = append(rows, row)
			if it.r.Outcome == "miss" {
				label := "没有明确依据"
				if it.r.WhyDim != "" {
					label = dimLabel[it.r.WhyDim] + "分析师的判断"
				}
				reasons[label]++
			}
			if related[it.p.Code] {
				mine = append(mine, row)
			}
		}
		if rate.Hit+rate.Miss > 0 {
			rate.Pct = float64(rate.Hit) / float64(rate.Hit+rate.Miss)
		} else {
			rate.Pct = -1
		}
		type kv struct {
			k string
			n int
		}
		var rs []kv
		for k, n := range reasons {
			rs = append(rs, kv{k, n})
		}
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].n > rs[b].n })
		var reasonRows []map[string]any
		for _, r := range rs {
			reasonRows = append(reasonRows, map[string]any{"label": r.k, "count": r.n})
		}
		out = append(out, map[string]any{"day": d, "rate": rate, "rows": rows, "mine": mine, "wrong_reasons": reasonRows})
	}
	if total.Hit+total.Miss > 0 {
		total.Pct = float64(total.Hit) / float64(total.Hit+total.Miss)
	} else {
		total.Pct = -1
	}
	return map[string]any{"days": out, "total": total, "n_days": len(out)}
}

func firstOf(days []string) string {
	if len(days) == 0 {
		return ""
	}
	return days[0]
}

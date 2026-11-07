package server

import (
	"fmt"
	"math"
	"sort"
	"time"

	"go-harness/internal/personal"
	"go-harness/internal/sched"
)

var weekdayName = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// taskNotes says what the tasks that matter to the scan page do, in plain words.
var taskNotes = map[string]string{
	nightlyID:     "给每只自选、持仓、想买各出一份结论，并把结论记成一条预测",
	"scan.settle": "结算到期的预测：第 5、20 个交易日对账，写出对错和原因，并发一条战绩通知",
}

func scheduleText(t sched.Trigger) string {
	switch t.Kind {
	case "daily":
		if t.Days == "trading" {
			return "每个交易日 " + t.At
		}
		return "每天 " + t.At
	case "interval":
		return fmt.Sprintf("交易时段每 %d 分钟", t.EveryMin)
	case "every":
		return fmt.Sprintf("每 %d 分钟", t.EveryMin)
	}
	return "手动"
}

// nextDaily is the next moment a daily task is due: today if its time is still ahead, otherwise the next eligible day.
func (s *Server) nextDaily(t sched.Trigger, now time.Time) (time.Time, bool) {
	if t.Kind != "daily" {
		return time.Time{}, false
	}
	hhmm, err := time.Parse("15:04", t.At)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(sched.Zone)
	for i := 0; i < 15; i++ {
		d := local.AddDate(0, 0, i)
		at := time.Date(d.Year(), d.Month(), d.Day(), hhmm.Hour(), hhmm.Minute(), 0, 0, sched.Zone)
		if !at.After(now) || (t.Days == "trading" && !s.cal.IsTradingDay(at)) {
			continue
		}
		return at, true
	}
	return time.Time{}, false
}

// whenText turns a moment into "周一 11-09 20:30 · 2 天后".
func whenText(at, now time.Time) string {
	n := now.In(sched.Zone)
	days := int(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, sched.Zone).Sub(time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, sched.Zone)).Hours() / 24)
	rel := map[int]string{0: "今天", 1: "明天"}[days]
	if rel == "" {
		rel = fmt.Sprintf("%d 天后", days)
	}
	return fmt.Sprintf("%s %s %s · %s", weekdayName[at.Weekday()], at.Format("01-02"), at.Format("15:04"), rel)
}

var tierText = map[string]string{"hold": "持仓", "wish": "想买", "other": "自选"}

func (s *Server) scanUpcoming() map[string]any {
	now := s.now()
	var scanTasks, others []map[string]any
	var nightlyNext time.Time
	for _, sp := range s.sch.Specs() {
		if sp.Frequent {
			continue
		}
		item := map[string]any{"task_id": sp.ID, "title": sp.Title, "trigger_text": scheduleText(sp.Trigger), "note": taskNotes[sp.ID], "paused": s.sch.Store.Paused(sp.ID)}
		if at, ok := s.nextDaily(sp.Trigger, now); ok {
			item["next"], item["when_text"], item["sort"] = at.Format("2006-01-02 15:04"), whenText(at, now), at.Unix()
			if sp.ID == nightlyID {
				nightlyNext = at
			}
		}
		if runs := s.sch.Store.Runs(sp.ID, 1); len(runs) > 0 {
			r := runs[0]
			at := r.FinishedAt
			if at == 0 {
				at = r.StartedAt
			}
			if at == 0 {
				at = r.ScheduledFor
			}
			item["last"] = map[string]any{"state": r.State, "at": time.UnixMilli(at).In(sched.Zone).Format("01-02 15:04"), "counts": r.Counts}
		}
		if taskNotes[sp.ID] != "" {
			scanTasks = append(scanTasks, item)
		} else {
			others = append(others, item)
		}
	}
	byTime := func(list []map[string]any) {
		sort.SliceStable(list, func(a, b int) bool {
			x, _ := list[a]["sort"].(int64)
			y, _ := list[b]["sort"].(int64)
			if x == 0 || y == 0 {
				return y == 0 && x != 0
			}
			return x < y
		})
	}
	byTime(others)
	// the scan tasks keep the order they matter in: conclusions first, then settling
	sort.SliceStable(scanTasks, func(a, b int) bool {
		return scanTasks[a]["task_id"] == nightlyID && scanTasks[b]["task_id"] != nightlyID
	})

	var subjects []map[string]any
	for _, sub := range s.personal.Subjects() {
		row := map[string]any{"ts_code": sub.Code, "name": s.stockName(sub.Code), "tier": sub.Tier, "tier_text": tierText[sub.Tier]}
		if b := s.readBundle(sub.Code); b != nil {
			row["last_at"], row["last_verdict"], row["last_source"] = b.GeneratedAt, b.Chief["verdict"], b.Source
		}
		subjects = append(subjects, row)
	}
	nightly := map[string]any{"subjects": subjects, "count": len(subjects)}
	if !nightlyNext.IsZero() {
		nightly["next"] = nightlyNext.Format("2006-01-02 15:04")
	}
	return map[string]any{"now": now.In(sched.Zone).Format("2006-01-02 15:04"), "scan_tasks": scanTasks, "others": others, "nightly": nightly, "settle": s.settlePlan(), "watch": s.watchedPrices(now)}
}

// settlePlan counts the predictions that come due on each coming day.
func (s *Server) settlePlan() []map[string]any {
	type bucket struct {
		n     int
		names []string
	}
	by := map[string]*bucket{}
	for _, p := range s.scan.All() {
		if p.Direction == "unclear" {
			continue
		}
		px := s.pricesOf(p.Code)
		for _, h := range []int{5, 20} {
			if p.Result(h) != nil {
				continue
			}
			day := s.dueDay(p.BaseDay, h, px.stock)
			if day == "" {
				continue
			}
			b := by[day]
			if b == nil {
				b = &bucket{}
				by[day] = b
			}
			b.n++
			label := fmt.Sprintf("%s %d 天", p.Name, h)
			if len(b.names) < 4 {
				b.names = append(b.names, label)
			}
		}
	}
	days := []string{}
	for d := range by {
		days = append(days, d)
	}
	sort.Strings(days)
	out := []map[string]any{}
	for _, d := range days {
		out = append(out, map[string]any{"day": d, "count": by[d].n, "names": by[d].names})
		if len(out) == 6 {
			break
		}
	}
	return out
}

// ---- the prices being watched

// sessionNow says whether the market is in a trading session; otherwise the next session start.
func (s *Server) sessionNow(now time.Time) (bool, time.Time) {
	local := now.In(sched.Zone)
	at := func(d time.Time, h, m int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, sched.Zone)
	}
	for i := 0; i < 15; i++ {
		d := local.AddDate(0, 0, i)
		if !s.cal.IsTradingDay(d) {
			continue
		}
		for _, w := range [][4]int{{9, 30, 11, 30}, {13, 0, 15, 0}} {
			start, end := at(d, w[0], w[1]), at(d, w[2], w[3])
			if !now.Before(start) && now.Before(end) {
				return true, time.Time{}
			}
			if start.After(now) {
				return false, start
			}
		}
	}
	return false, time.Time{}
}

// watchedPrices lists the price alerts that are armed, with how far each is from the last price.
func (s *Server) watchedPrices(now time.Time) map[string]any {
	alerts := s.personal.Alerts("")
	rows := []map[string]any{}
	prices := map[string]float64{}
	if len(alerts) > 0 {
		seen := map[string]bool{}
		var codes []string
		for _, a := range alerts {
			if !seen[a.Code] {
				seen[a.Code] = true
				codes = append(codes, a.Code)
			}
		}
		if q, err := s.quotes.Fetch(codes); err == nil || len(q) > 0 {
			for code, v := range q {
				prices[code] = v.Price
			}
		}
	}
	in, start := s.sessionNow(now)
	touchNext := "盘中每 3 分钟检查"
	if !in && !start.IsZero() {
		touchNext = "下一次 " + whenText(start, now)
	}
	closeNext := ""
	if sp, ok := s.sch.Spec("alerts.close"); ok {
		if at, ok := s.nextDaily(sp.Trigger, now); ok {
			closeNext = "收盘后核对，下一次 " + whenText(at, now)
		}
	}
	for _, a := range alerts {
		name := a.Name
		if n := s.stockName(a.Code); n != a.Code {
			name = n
		}
		row := map[string]any{"id": a.ID, "ts_code": a.Code, "name": name, "side": a.Side, "price": a.Price, "confirm": a.Confirm, "check": touchNext}
		if a.Confirm == "close" {
			row["check"] = closeNext
		}
		if p := prices[a.Code]; p > 0 {
			row["now"] = p
			row["gap_pct"] = (a.Price - p) / p
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		gi, oki := rows[i]["gap_pct"].(float64)
		gj, okj := rows[j]["gap_pct"].(float64)
		if oki != okj {
			return oki
		}
		return math.Abs(gi) < math.Abs(gj)
	})
	held := len(personal.Held(s.personal.Trades()))
	return map[string]any{"alerts": rows, "held": held, "offline": s.relay != nil && s.relay.authoritative(), "in_session": in}
}

package server

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/sched"
)

// The other kinds of notification, besides alerts and the nightly conclusion. What everyone sees the same
// (announcements, limit moves, new highs and lows, the calendar) is detected by the data service and read
// from its shared endpoints; what depends on the user's holdings (is my reason still true, did a wish come
// true, how did my calls turn out) is worked out here.

// tierSeverity says how loud an announcement of each importance is for a stock the user cares about
// to each degree. Holdings hear the most, the plain watchlist only the gravest.
var tierSeverity = map[string]map[string]string{
	"hold":  {"P0": "urgent", "P1": "normal", "P2": "info"},
	"wish":  {"P0": "urgent", "P1": "normal"},
	"other": {"P0": "urgent"},
}

var importanceRank = map[string]int{"P0": 0, "P1": 1, "P2": 2}

const dailyNormalCap = 5 // per stock per day; more than this only goes to the inbox

func (s *Server) registerWatchTasks() {
	s.sch.Register(&sched.Spec{
		ID: "news.watch", Title: "公告监控", Queue: "data", Frequent: true, Fanout: s.subjectUnits,
		Trigger: sched.Trigger{Kind: "every", EveryMin: 10, Days: "any"}, MaxAttempts: 2, Backoff: []time.Duration{time.Minute, 5 * time.Minute}, Timeout: 90 * time.Second,
		Handler: s.watchAnnouncements,
	})
	s.sch.Register(&sched.Spec{
		ID: "signals.close", Title: "收盘信号", Queue: "data", Gates: []string{"market_closed"}, Fanout: s.subjectUnits,
		Trigger: sched.Trigger{Kind: "daily", At: "15:45", WindowMin: 120, Days: "trading"}, MaxAttempts: 3, Backoff: []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute},
		CatchupTTL: 20 * time.Hour, Timeout: 90 * time.Second, Handler: s.closeSignals,
	})
	s.sch.Register(&sched.Spec{
		ID: "calendar.morning", Title: "日历提醒", Queue: "data",
		Trigger: sched.Trigger{Kind: "daily", At: "08:40", WindowMin: 120, Days: "any"}, MaxAttempts: 2, Backoff: []time.Duration{5 * time.Minute, 15 * time.Minute},
		CatchupTTL: 6 * time.Hour, Timeout: 90 * time.Second, Handler: s.morningCalendar,
	})
	s.sch.Register(&sched.Spec{
		ID: "holdings.watch", Title: "持仓复查", Queue: "data", Gates: []string{"market_closed"},
		Trigger: sched.Trigger{Kind: "daily", At: "16:00", WindowMin: 180, Days: "trading"}, MaxAttempts: 2, Backoff: []time.Duration{10 * time.Minute, 30 * time.Minute},
		CatchupTTL: 20 * time.Hour, Timeout: 3 * time.Minute, Handler: s.watchHoldings,
	})
}

func (s *Server) tierOf(c *sched.JobCtx) string {
	if t, _ := c.Input()["tier"].(string); t != "" {
		return t
	}
	return "other"
}

func asMapList(v any) []map[string]any {
	var out []map[string]any
	if list, ok := v.([]any); ok {
		for _, it := range list {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func annKey(it map[string]any) string {
	return fmt.Sprint(it["published_at"]) + "|" + fmt.Sprint(it["item_id"])
}

func (s *Server) normalToday(code, day string) int {
	n := 0
	for _, it := range s.inbox.List("stock", false, 300) {
		if it.Type == "announcement.published" && it.SubjectID == code && it.Severity == "normal" && it.TradeDate == day {
			n++
		}
	}
	return n
}

// watchAnnouncements reads the newest announcements of one stock and tells the user about the ones that
// arrived since last time. The first run only remembers where it is, so old announcements are not
// announced as news.
func (s *Server) watchAnnouncements(c *sched.JobCtx) (any, error) {
	if h := c.Now().In(sched.Zone).Hour(); h < 7 || h >= 22 {
		return map[string]any{"skipped": "night"}, nil
	}
	code, tier := c.Unit(), s.tierOf(c)
	var body map[string]any
	if err := s.stockGetJSON("/api/v1/shared/announcements/"+url.PathEscape(code), &body); err != nil {
		return nil, sched.Transient("取公告失败：" + err.Error())
	}
	items := asMapList(body["items"])
	sort.SliceStable(items, func(a, b int) bool { return annKey(items[a]) < annKey(items[b]) })
	store, key := c.Store(), "ann:"+code
	var cursor string
	if !store.KVGet(key, &cursor) {
		last := ""
		if len(items) > 0 {
			last = annKey(items[len(items)-1])
		}
		store.KVSet(key, last)
		return map[string]any{"ts_code": code, "initialized": true}, nil
	}
	var fresh []map[string]any
	for _, it := range items {
		if annKey(it) > cursor {
			fresh = append(fresh, it)
		}
	}
	if len(fresh) == 0 {
		return map[string]any{"ts_code": code, "new": 0}, nil
	}
	day := sched.Day(c.Now())
	buckets := map[string][]map[string]any{}
	for _, it := range fresh {
		if sev := tierSeverity[tier][fmt.Sprint(it["importance"])]; sev != "" {
			buckets[sev] = append(buckets[sev], it)
		}
	}
	events := 0
	for sev, group := range buckets {
		sort.SliceStable(group, func(a, b int) bool {
			ra, rb := importanceRank[fmt.Sprint(group[a]["importance"])], importanceRank[fmt.Sprint(group[b]["importance"])]
			if ra != rb {
				return ra < rb
			}
			return fmt.Sprint(group[a]["published_at"]) < fmt.Sprint(group[b]["published_at"])
		})
		lead := group[0]
		if sev == "normal" && s.normalToday(code, day) >= dailyNormalCap {
			sev = "info"
		}
		payload := map[string]any{"ts_code": code, "name": s.stockName(code), "extra": len(group) - 1}
		for _, k := range []string{"item_id", "title", "url", "source", "source_grade", "published_at", "category", "topic", "importance", "direction_hint", "matched"} {
			payload[k] = lead[k]
		}
		var others []string
		for _, g := range group[1:] {
			if len(others) < 2 {
				others = append(others, fmt.Sprint(g["title"]))
			}
		}
		payload["others"] = others
		if _, made, err := s.inbox.Publish("announcement.published", "stock", code, payload, "ann:"+fmt.Sprint(lead["item_id"]), inbox.Options{TradeDate: day, Severity: sev}); err == nil && made {
			events++
		}
	}
	store.KVSet(key, annKey(items[len(items)-1]))
	return map[string]any{"ts_code": code, "new": len(fresh), "events": events}, nil
}

// closeSignals reports, after the close, what happened to one stock today: a limit move, a new high or
// low, large money leaving for as many days as the user asked to be told about.
func (s *Server) closeSignals(c *sched.JobCtx) (any, error) {
	code, tier := c.Unit(), s.tierOf(c)
	var sig map[string]any
	if err := s.stockGetJSON("/api/v1/shared/signals/"+url.PathEscape(code), &sig); err != nil {
		return nil, sched.Transient("取收盘信号失败：" + err.Error())
	}
	want := sched.Day(s.cal.LastClose(c.Now()))
	day, _ := sig["trade_date"].(string)
	if day != want {
		return nil, sched.NotReady("日线还没有最新一天", 10*time.Minute)
	}
	name := s.stockName(code)
	out := map[string]any{"ts_code": code}
	if lim, _ := sig["limit"].(map[string]any); lim != nil {
		sev := "normal"
		if tier == "other" {
			sev = "info"
		}
		kind := fmt.Sprint(lim["kind"])
		_, made, _ := s.inbox.Publish("limit.hit", "stock", code, map[string]any{"ts_code": code, "name": name, "kind": kind, "close": lim["close"], "trade_date": day},
			fmt.Sprintf("limit:%s:%s:%s", code, day, kind), inbox.Options{TradeDate: day, Severity: sev})
		out["limit"] = made
	}
	if ex, _ := sig["extreme"].(map[string]any); ex != nil {
		kind, days := fmt.Sprint(ex["kind"]), fmt.Sprint(ex["window_days"])
		_, made, _ := s.inbox.Publish("price.extreme", "stock", code, map[string]any{"ts_code": code, "name": name, "kind": kind, "window_days": ex["window_days"], "close": ex["close"]},
			fmt.Sprintf("extreme:%s:%s:%s:%s", code, day, kind, days), inbox.Options{TradeDate: day})
		out["extreme"] = made
	}
	if rules := s.personal.RulesOf(code); len(rules) > 0 {
		var flow map[string]any
		if err := s.stockGetJSON("/api/v1/shared/flow/"+url.PathEscape(code), &flow); err == nil {
			outDays, _ := flow["out"].(float64)
			for _, r := range rules {
				if r.Kind == "flow_out" && int(outDays) == int(r.Value) {
					_, made, _ := s.inbox.Publish("flow.streak", "stock", code, map[string]any{"ts_code": code, "name": name, "direction": "out", "days": int(r.Value)},
						fmt.Sprintf("flow:%s:%d:%s", code, int(r.Value), day), inbox.Options{TradeDate: day})
					out["flow"] = made
				}
			}
		}
	}
	return out, nil
}

// morningCalendar tells about reports and lock-up expiries coming in the next few days.
func (s *Server) morningCalendar(c *sched.JobCtx) (any, error) {
	subs := s.personal.Subjects()
	if len(subs) == 0 {
		return map[string]any{"watching": 0}, nil
	}
	tiers := map[string]string{}
	var parts []string
	for _, sub := range subs {
		tiers[sub.Code] = sub.Tier
		parts = append(parts, sub.Code+":"+s.stockName(sub.Code))
	}
	var body map[string]any
	if err := s.stockGetJSON("/api/v1/shared/calendar?codes="+url.QueryEscape(strings.Join(parts, ",")), &body); err != nil {
		return nil, sched.Transient("取日历失败：" + err.Error())
	}
	now := c.Now()
	today, limit := sched.Day(now), sched.Day(now.AddDate(0, 0, 3))
	made := 0
	for _, row := range asMapList(body["items"]) {
		date, text := fmt.Sprint(row["date"]), fmt.Sprint(row["text"])
		if date < today || date > limit {
			continue
		}
		for _, sub := range subs {
			if n := s.stockName(sub.Code); n == sub.Code || !strings.Contains(text, n) {
				continue
			}
			sev := "normal"
			if sub.Tier == "other" {
				sev = "info"
			}
			if _, ok, _ := s.inbox.Publish("calendar.upcoming", "stock", sub.Code, map[string]any{"ts_code": sub.Code, "name": s.stockName(sub.Code), "date": date, "text": strings.TrimSpace(strings.TrimPrefix(text, s.stockName(sub.Code)))},
				"cal:"+date+":"+sub.Code+":"+text, inbox.Options{TradeDate: today, Severity: sev}); ok {
				made++
			}
			break
		}
	}
	return map[string]any{"events": made}, nil
}

// watchHoldings checks what depends on the user's own positions: whether a reason still holds, whether a
// wish has come true, which past calls have been settled.
func (s *Server) watchHoldings(c *sched.JobCtx) (any, error) {
	ctx := s.holdingsCtx()
	day := sched.Day(c.Now())
	store := c.Store()
	out := map[string]int{"reasons": 0, "wishes": 0, "settled": 0}
	for _, st := range ctx.ReasonStates() {
		key := "reason:" + st.Code
		var last string
		known := store.KVGet(key, &last)
		store.KVSet(key, st.Status)
		if !known || last == st.Status || st.Status == "none" {
			continue
		}
		if _, made, _ := s.inbox.Publish("reason.status_changed", "stock", st.Code, map[string]any{"ts_code": st.Code, "name": st.Name, "to": st.StatusText, "line": st.Line},
			fmt.Sprintf("reason:%s:%s:%s", st.Code, day, st.Status), inbox.Options{TradeDate: day}); made {
			out["reasons"]++
		}
	}
	for _, w := range ctx.WishesReady() {
		if _, made, _ := s.inbox.Publish("wish.condition_met", "stock", w.Wish.Code, map[string]any{"ts_code": w.Wish.Code, "name": w.Wish.Name, "met": w.Met},
			fmt.Sprintf("wish:%d", w.Wish.ID), inbox.Options{TradeDate: day}); made {
			out["wishes"]++
		}
	}
	for _, st := range ctx.SettledEntries(9) {
		if _, made, _ := s.inbox.Publish("outcome.settled", "stock", st.Code, map[string]any{"ts_code": st.Code, "name": st.Name, "entry_date": st.Date, "horizon": 5, "hit": st.Hit, "ret": st.Ret},
			fmt.Sprintf("outcome:%s:%s", st.Code, st.Date), inbox.Options{TradeDate: day}); made {
			out["settled"]++
		}
	}
	return out, nil
}

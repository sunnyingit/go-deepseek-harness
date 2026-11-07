package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-harness/internal/personal"
	"go-harness/internal/quotes"
	"go-harness/internal/sched"
)

// dataService plays the shared endpoints these checks read; the maps can be changed between runs.
type dataService struct {
	mu            sync.Mutex
	announcements map[string][]map[string]any
	signals       map[string]map[string]any
	flow          map[string]map[string]any
	calendar      []map[string]any
	closes        map[string][][2]any // stock closes by code
	sectors       map[string][][2]any // sector index closes by stock code
}

func (d *dataService) start(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		p := r.URL.Path
		var out any
		switch {
		case strings.HasPrefix(p, "/api/v1/shared/announcements/"):
			out = map[string]any{"items": d.announcements[strings.TrimPrefix(p, "/api/v1/shared/announcements/")]}
		case strings.HasPrefix(p, "/api/v1/shared/signals/"):
			out = d.signals[strings.TrimPrefix(p, "/api/v1/shared/signals/")]
		case strings.HasPrefix(p, "/api/v1/shared/flow/"):
			out = d.flow[strings.TrimPrefix(p, "/api/v1/shared/flow/")]
		case strings.HasPrefix(p, "/api/v1/shared/closes/"):
			out = map[string]any{"items": d.closes[strings.TrimPrefix(p, "/api/v1/shared/closes/")]}
		case strings.HasPrefix(p, "/api/v1/shared/sector-series/"):
			out = map[string]any{"name": "银行", "items": d.sectors[strings.TrimPrefix(p, "/api/v1/shared/sector-series/")]}
		case strings.HasPrefix(p, "/api/v1/shared/sector/"):
			out = map[string]any{"name": "银行"}
		case p == "/api/v1/shared/calendar":
			out = map[string]any{"items": d.calendar}
		case strings.HasPrefix(p, "/api/v1/shared/"):
			out = map[string]any{"items": []any{}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(api.Close)
	t.Setenv("STOCK_API_BASE", api.URL)
}

func ann(id, title, importance, at string) map[string]any {
	return map[string]any{"item_id": id, "title": title, "importance": importance, "published_at": at, "source": "交易所公告", "category": "经营", "topic": "订单", "matched": []string{"订单"}}
}

type unitSpec = sched.UnitSpec

func (s *Server) runTask(t *testing.T, task string, units ...string) {
	t.Helper()
	var us []unitSpec
	for _, u := range units {
		us = append(us, unitSpec{Key: u})
	}
	if _, err := s.sch.Manual(task, us, map[string]any{"on_demand": true}); err != nil {
		t.Fatal(err)
	}
	s.sch.TickSync()
}

func quotesFresh(s *Server) *quotes.Client {
	c := quotes.New()
	c.Get = s.quotes.Get
	return c
}

func TestAnnouncementsFirstRunOnlyRemembersThenEachTierGetsItsOwnLoudness(t *testing.T) {
	ds := &dataService{announcements: map[string][]map[string]any{}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "10:00"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	ds.announcements["000001.SZ"] = []map[string]any{ann("a1", "旧公告", "P1", "2026-11-01 09:00:00")}
	ds.announcements["600036.SH"] = []map[string]any{ann("b1", "旧公告", "P1", "2026-11-01 09:00:00")}
	s.runTask(t, "news.watch", "000001.SZ", "600036.SH")
	if got := s.inbox.List("stock", false, 20); len(got) != 0 {
		t.Fatalf("history was announced as news: %+v", got)
	}
	c.advance(10 * time.Minute)
	ds.mu.Lock()
	ds.announcements["000001.SZ"] = append(ds.announcements["000001.SZ"], ann("a2", "签订重大订单", "P1", "2026-11-05 10:05:00"), ann("a3", "召开说明会", "P2", "2026-11-05 10:06:00"), ann("a4", "实控人被立案调查", "P0", "2026-11-05 10:07:00"))
	ds.announcements["600036.SH"] = append(ds.announcements["600036.SH"], ann("b2", "签订重大订单", "P1", "2026-11-05 10:05:00"))
	ds.mu.Unlock()
	// the watchlist stock (tier other) hears only P0, the holding hears P0 loudly and P1 normally, P2 quietly
	s.sch.Manual("news.watch", []unitSpec{{Key: "000001.SZ", Input: map[string]any{"tier": "hold"}}, {Key: "600036.SH", Input: map[string]any{"tier": "other"}}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	bySeverity := map[string]int{}
	for _, it := range s.inbox.List("stock", false, 20) {
		if it.Type == "announcement.published" {
			bySeverity[it.SubjectID+":"+it.Severity]++
		}
	}
	if bySeverity["000001.SZ:urgent"] != 1 || bySeverity["000001.SZ:normal"] != 1 || bySeverity["000001.SZ:info"] != 1 || bySeverity["600036.SH:urgent"] != 0 || bySeverity["600036.SH:normal"] != 0 {
		t.Fatalf("by severity = %v", bySeverity)
	}
	before := len(s.inbox.List("stock", false, 50))
	s.sch.Manual("news.watch", []unitSpec{{Key: "000001.SZ", Input: map[string]any{"tier": "hold"}}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	if len(s.inbox.List("stock", false, 50)) != before {
		t.Fatal("the same announcements were announced twice")
	}
}

func TestAnnouncementsStopBeingLoudAfterFiveNormalOnesAStockADay(t *testing.T) {
	ds := &dataService{announcements: map[string][]map[string]any{"000001.SZ": {ann("a0", "旧", "P1", "2026-11-01 09:00:00")}}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "10:00"))
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	run := func() {
		s.sch.Manual("news.watch", []unitSpec{{Key: "000001.SZ", Input: map[string]any{"tier": "hold"}}}, map[string]any{"on_demand": true})
		s.sch.TickSync()
	}
	run()
	for i := 0; i < 7; i++ {
		c.advance(10 * time.Minute)
		ds.mu.Lock()
		ds.announcements["000001.SZ"] = append(ds.announcements["000001.SZ"], ann("n"+string(rune('a'+i)), "公告", "P1", c.now().Format("2006-01-02 15:04:05")))
		ds.mu.Unlock()
		run()
	}
	counts := map[string]int{}
	for _, it := range s.inbox.List("stock", false, 50) {
		counts[it.Severity]++
	}
	if counts["normal"] != 5 || counts["info"] != 2 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestCloseSignalsLimitExtremeAndTheFlowStreakTheUserAskedFor(t *testing.T) {
	ds := &dataService{signals: map[string]map[string]any{}, flow: map[string]map[string]any{}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "16:00"))
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.personal.WatchAdd("600036.SH", "招商银行")
	s.personal.AddRule("000001.SZ", "flow_out", 3)
	ds.signals["000001.SZ"] = map[string]any{"trade_date": "2026-11-05", "limit": map[string]any{"kind": "limit_up", "close": 11.0}, "extreme": map[string]any{"kind": "high", "window_days": 60.0, "close": 11.0}}
	ds.signals["600036.SH"] = map[string]any{"trade_date": "2026-11-05", "limit": map[string]any{"kind": "limit_down", "close": 9.0}}
	ds.flow["000001.SZ"] = map[string]any{"in": 0.0, "out": 3.0}
	s.sch.Manual("signals.close", []unitSpec{{Key: "000001.SZ", Input: map[string]any{"tier": "hold"}}, {Key: "600036.SH", Input: map[string]any{"tier": "other"}}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	got := map[string]string{}
	for _, it := range s.inbox.List("stock", false, 20) {
		got[it.Type+":"+it.SubjectID] = it.Severity
	}
	if got["limit.hit:000001.SZ"] != "normal" || got["price.extreme:000001.SZ"] != "info" || got["flow.streak:000001.SZ"] != "normal" || got["limit.hit:600036.SH"] != "info" {
		t.Fatalf("got = %v", got)
	}
	before := len(s.inbox.List("stock", false, 50))
	s.sch.Manual("signals.close", []unitSpec{{Key: "000001.SZ", Input: map[string]any{"tier": "hold"}}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	if len(s.inbox.List("stock", false, 50)) != before {
		t.Fatal("tonight's signals were announced twice")
	}
	// a stale last bar means the data is not ready yet: wait, do not report yesterday as today
	ds.mu.Lock()
	ds.signals["000001.SZ"]["trade_date"] = "2026-11-04"
	ds.mu.Unlock()
	c.advance(time.Hour)
	run, _ := s.sch.Manual("signals.close", []unitSpec{{Key: "000001.SZ"}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	jobs := s.sch.Store.Jobs(run.ID)
	if len(jobs) != 1 || jobs[0].State != "not_ready" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestCalendarTellsAboutTheNextThreeDaysOnceAndMatchesTheStockByName(t *testing.T) {
	ds := &dataService{calendar: []map[string]any{
		{"date": "2026-11-06", "text": "杭氧股份 三季报披露", "tag": "涉及自选"},
		{"date": "2026-11-20", "text": "杭氧股份 解禁", "tag": ""},
		{"date": "2026-11-06", "text": "别的公司 三季报披露", "tag": ""},
	}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-05", "08:41"))
	s.personal.WatchAdd("002430.SZ", "杭氧股份")
	s.runTask(t, "calendar.morning")
	s.runTask(t, "calendar.morning")
	var items []string
	for _, it := range s.inbox.List("stock", false, 10) {
		if it.Type == "calendar.upcoming" {
			items = append(items, it.Title)
		}
	}
	if len(items) != 1 || !strings.Contains(items[0], "三季报披露") {
		t.Fatalf("items = %v", items)
	}
}

func TestHoldingsWatchReportsAReasonThatChangedAWishThatCameTrueAndSettledCalls(t *testing.T) {
	fakeShared(t)
	s, _ := newClientServer(t, zoneTime("2026-11-05", "16:05"))
	s.quotes.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~40.00~39.00~1";`, nil }
	if _, err := s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 38, Qty: 100, Date: "2026-11-01", Reason: "高股息",
		Invalid: []personal.Item{{"kind": "close_below", "value": 35.0}}}); err != nil {
		t.Fatal(err)
	}
	s.runTask(t, "holdings.watch")
	if n := countType(s, "reason.status_changed"); n != 0 {
		t.Fatalf("the first look only remembers: %d", n)
	}
	// the price falls through the line the user drew: the reason no longer holds
	s.quotes.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~34.00~39.00~1";`, nil }
	s.quotes = quotesFresh(s)
	s.sch.Manual("holdings.watch", nil, map[string]any{"on_demand": true})
	s.sch.TickSync()
	items := s.inbox.List("stock", false, 10)
	var found bool
	for _, it := range items {
		if it.Type == "reason.status_changed" && strings.Contains(it.Title, "不成立") {
			found = true
		}
	}
	if !found {
		t.Fatalf("items = %+v", items)
	}
	s.quotes = quotesFresh(s)
	s.quotes.Get = func(string) (string, error) {
		return `v_sh600036="1~招商银行~600036~34.00~39.00~1"; v_sz002430="1~杭氧股份~002430~26.00~25.00~1";`, nil
	}
	if _, err := s.personal.WishAdd(personal.Wish{Code: "002430.SZ", Name: "杭氧股份", Reason: "x", Conditions: []personal.Item{{"kind": "price_below", "value": 30.0}}}); err != nil {
		t.Fatal(err)
	}
	s.sch.Manual("holdings.watch", nil, map[string]any{"on_demand": true})
	s.sch.TickSync()
	if countType(s, "wish.condition_met") != 1 {
		t.Fatalf("wish not reported: %+v", s.inbox.List("stock", false, 20))
	}
	s.sch.Manual("holdings.watch", nil, map[string]any{"on_demand": true})
	s.sch.TickSync()
	if countType(s, "wish.condition_met") != 1 || countType(s, "reason.status_changed") != 1 {
		t.Fatal("the same thing was reported twice")
	}
}

func countType(s *Server, typ string) int {
	n := 0
	for _, it := range s.inbox.List("", false, 100) {
		if it.Type == typ {
			n++
		}
	}
	return n
}

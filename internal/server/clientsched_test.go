package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-harness/internal/appconfig"
	"go-harness/internal/custom"
	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/quotes"
	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func zoneTime(day, hhmm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, sched.Zone)
	return t
}

// newClientServer builds a server with the client pieces on a fake clock. 2026-11-05 is a Thursday.
func newClientServer(t *testing.T, start time.Time) (*Server, *testClock) {
	clock := &testClock{t: start}
	dir := t.TempDir()
	s := &Server{cfg: appconfig.Config{DataDir: dir, Model: "test-model"}, groups: map[string][]groupLine{}, debates: map[string]*debateFile{}}
	s.cal = sched.LoadCalendar("")
	s.inbox = inbox.Open(dir+"/inbox.json", clock.now)
	s.personal = personal.Open(dir+"/personal.json", clock.now)
	s.quotes = quotes.New()
	s.scan = scan.Open(dir+"/predictions.json", clock.now)
	s.custom = custom.Open(dir+"/custom.json", clock.now)
	s.hidden = openHidden(dir + "/scan_hidden.json")
	s.sch = sched.New(sched.OpenStore(""), s.cal, clock.now)
	s.registerTasks()
	s.sch.Listeners = append(s.sch.Listeners, s.onRunFinished)
	t.Cleanup(s.sch.Wait) // jobs a test queued but did not wait for must not outlive its temp directory
	return s, clock
}

// due ticks once at 20:00 (the install moment) and moves the clock to 20:35 so the 20:30 task is due.
func due(s *Server, c *testClock) {
	s.sch.Tick()
	c.advance(35 * time.Minute)
}

type speaker struct {
	mu    sync.Mutex
	calls map[string]int
	fail  map[string]int // how many times each step fails before it works
}

func (sp *speaker) fn(_ context.Context, id, code string) stepOutcome {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.calls == nil {
		sp.calls = map[string]int{}
	}
	sp.calls[id]++
	out := stepOutcome{ID: id, Name: id + "分析师", Label: diagnosisLabel[id], State: "ok"}
	if sp.fail[id] > 0 {
		sp.fail[id]--
		out.State, out.Reason = "failed", "模型超时"
		return out
	}
	if id != "bull" && id != "bear" && id != "chief" {
		out.Headline = map[string]any{"text": id + " 的结论", "status": "support", "status_text": "偏多", "facts": []any{}}
	}
	return out
}

func seedDebate(s *Server, code string) {
	s.updateDebate(code, func(f *debateFile) {
		f.Bull = []debatePoint{{ID: "bull.1", Text: "板块强", Dimension: "sector", Weight: 3}}
		f.Bear = []debatePoint{{ID: "bear.1", Text: "位置高", Dimension: "volume_flow", Weight: 2}}
		f.Chief = &chiefDecision{Verdict: "偏多 · 稍微", Text: "偏多但位置偏高", Adopted: []string{"bull.1"}}
	})
}

func TestNightlyMakesOneBundlePerStockAndLeavesAReceipt(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	sp := &speaker{}
	s.stepFn = sp.fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	seedDebate(s, "600036.SH")
	seedDebate(s, "000001.SZ")
	due(s, c)
	s.sch.TickSync()
	run := s.sch.Store.Runs(nightlyID, 1)[0]
	if run.State != "succeeded" || run.Counts["succeeded"] != 2 {
		t.Fatalf("run = %+v", run)
	}
	b := s.readBundle("600036.SH")
	if b == nil || b.TradeDate != "2026-11-05" || b.Source != "agent" || len(b.Analysts) != 5 || b.Chief["verdict"] != "偏多 · 稍微" {
		t.Fatalf("bundle = %+v", b)
	}
	for _, a := range b.Analysts {
		if a.State != "ok" || a.Headline["text"] == nil {
			t.Fatalf("analyst = %+v", a)
		}
	}
	if pts := b.Debate["bull"].(map[string]any)["points"].([]any); len(pts) != 1 || pts[0].(map[string]any)["text"] != "板块强" {
		t.Fatalf("bull = %+v", b.Debate["bull"])
	}
	var receipts []inbox.Item
	for _, it := range s.inbox.List("system", false, 10) {
		if it.SubjectID == nightlyID {
			receipts = append(receipts, it)
		}
	}
	if len(receipts) != 1 || receipts[0].Type != "task.completed" || !strings.Contains(receipts[0].Body, "单元 2/2") {
		t.Fatalf("receipts = %+v", receipts)
	}
	if len(s.inbox.DesktopPending()) != 0 {
		t.Fatal("a success popped up on the desktop")
	}
}

func TestRetryOnlyRedoesTheAnalystThatFailedAndAFreshBundleIsNotRedone(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	sp := &speaker{fail: map[string]int{"news": 1}}
	s.stepFn = sp.fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	due(s, c)
	s.sch.TickSync()
	if s.readBundle("600036.SH") != nil {
		t.Fatal("a bundle was written while an analyst still had retries left")
	}
	c.advance(3 * time.Minute)
	s.sch.TickSync()
	if sp.calls["news"] != 2 || sp.calls["finance"] != 1 || sp.calls["chief"] != 1 {
		t.Fatalf("calls = %v", sp.calls)
	}
	if s.readBundle("600036.SH") == nil {
		t.Fatal("no bundle after the retry")
	}
	before := sp.calls["chief"]
	s.sch.Manual(nightlyID, []sched.UnitSpec{{Key: "600036.SH"}}, map[string]any{"on_demand": true})
	s.sch.TickSync()
	if sp.calls["chief"] != before {
		t.Fatal("a scheduled-style run redid a bundle made today")
	}
	s.sch.Manual(nightlyID, []sched.UnitSpec{{Key: "600036.SH"}}, map[string]any{"on_demand": true, "force": true})
	s.sch.TickSync()
	if sp.calls["chief"] != before+1 {
		t.Fatalf("force did not redo the bundle: %v", sp.calls)
	}
}

func TestAnalystThatNeverWorksIsShownAsNoConclusionNotMadeUp(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	sp := &speaker{fail: map[string]int{"finance": 99}}
	s.stepFn = sp.fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	due(s, c)
	for i := 0; i < 4; i++ {
		s.sch.TickSync()
		c.advance(31 * time.Minute)
	}
	b := s.readBundle("600036.SH")
	if b == nil {
		t.Fatal("no bundle after the last attempt")
	}
	for _, a := range b.Analysts {
		if a.Dimension == "finance" && (a.State != "failed" || a.Headline != nil || a.Reason == "") {
			t.Fatalf("finance = %+v", a)
		}
	}
	if s.sch.Store.Runs(nightlyID, 1)[0].State != "succeeded" {
		t.Fatalf("run = %+v", s.sch.Store.Runs(nightlyID, 1)[0])
	}
}

func TestWithoutAModelTheJobFailsAtOnceAndPopsUp(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	due(s, c)
	s.sch.TickSync()
	run := s.sch.Store.Runs(nightlyID, 1)[0]
	if run.State != "failed" {
		t.Fatalf("run = %+v", run)
	}
	pending := s.inbox.DesktopPending()
	if len(pending) != 1 || !strings.Contains(pending[0]["title"].(string), "每晚个股结论") {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestConclusionViewQueuesTheFirstOneAndReportsProgressThenReady(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-07", "10:00")) // Saturday: the nightly gate is closed, an on-demand job ignores it
	sp := &speaker{}
	s.stepFn = sp.fn
	seedDebate(s, "600036.SH")
	body := s.conclusionView("600036.SH", true)
	if body["state"] != "pending" || body["bundle"] != nil {
		t.Fatalf("body = %+v", body)
	}
	if again := s.conclusionView("600036.SH", true); again["state"] != "pending" || len(s.sch.Store.Runs(nightlyID, 0)) != 1 {
		t.Fatal("a second view queued a second job")
	}
	s.sch.TickSync()
	ready := s.conclusionView("600036.SH", true)
	if ready["state"] != "ready" {
		t.Fatalf("ready = %+v", ready)
	}
	bundle := ready["bundle"].(map[string]any)
	if bundle["trade_date"] != "2026-11-06" || bundle["stale"] != "" {
		t.Fatalf("bundle = %+v", bundle)
	}
	c.advance(5 * 24 * time.Hour)
	if stale := s.conclusionView("600036.SH", false)["bundle"].(map[string]any)["stale"]; !strings.Contains(stale.(string), "2026-11-06") {
		t.Fatalf("stale = %v", stale)
	}
	if out, _ := s.requestConclusion("600036.SH", "holdings", false); out["queued"] != false {
		t.Fatalf("a background trigger requeued an existing bundle: %v", out)
	}
	if out, _ := s.requestConclusion("600036.SH", "manual", true); out["queued"] != true {
		t.Fatalf("manual did not queue: %v", out)
	}
}

func TestAlertsFireOncePerDayAndHoldingsGetAMoveAlert(t *testing.T) {
	s, c := newClientServer(t, zoneTime("2026-11-05", "09:00"))
	s.quotes.Get = func(string) (string, error) {
		return `v_sh600036="1~招商银行~600036~29.00~30.00~1"; v_sz000001="1~平安银行~000001~10.80~10.00~1";`, nil
	}
	s.personal.AlertAdd("600036.SH", "down", 29.5, "touch", "招商银行")
	s.personal.AlertAdd("600036.SH", "down", 28, "close", "招商银行")
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.sch.Tick()
	c.advance(31 * time.Minute) // 09:31, inside the session
	s.sch.TickSync()
	c.advance(3 * time.Minute)
	s.sch.TickSync()
	var alerts, moves int
	for _, it := range s.inbox.List("stock", false, 20) {
		switch it.Type {
		case "price.alert.fired":
			alerts++
			if it.Severity != "urgent" || it.Desktop != "pending" {
				t.Fatalf("alert = %+v", it)
			}
		case "price.move":
			moves++
		}
	}
	if alerts != 1 || moves != 1 {
		t.Fatalf("alerts=%d moves=%d (the close alert must wait for the close task)", alerts, moves)
	}
	var receipts int
	for _, it := range s.inbox.List("system", false, 20) {
		if it.Type == "task.completed" && strings.Contains(it.Title, "盘中价格提醒") {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("a task that runs every 3 minutes left %d receipts, want the first of the day only", receipts)
	}
}

func TestMarketBriefGoesToTheInboxOnceAndNotWhenCaughtUp(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"conclusion":{"level":"偏强","tilt":"震荡","action":"控制仓位","signals":[{"n":1,"name":"量能","label":"放大"}]}}`))
	}))
	defer api.Close()
	t.Setenv("STOCK_API_BASE", api.URL)
	s, c := newClientServer(t, zoneTime("2026-11-05", "08:00"))
	s.sch.Tick()
	c.advance(31 * time.Minute)
	s.sch.TickSync()
	items := s.inbox.List("market", false, 5)
	if len(items) != 1 || items[0].Title != "市场 · 盘前简报" || !strings.Contains(items[0].Body, "偏强震荡") || !strings.Contains(items[0].Body, "1量能 放大") {
		t.Fatalf("items = %+v", items)
	}
}

func TestClientRoutesServeInboxWatchlistAndConclusion(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.quotes.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~39.50~39.20~1";`, nil }
	s.inbox.Publish("market.brief", "market", "market", map[string]any{"title": "x", "text": "y"}, "k", inbox.Options{})
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	call := func(method, path, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, body := call("GET", "/notify/inbox?group=market", ""); code != 200 || len(body["items"].([]any)) != 1 || body["unread"].(float64) != 1 {
		t.Fatalf("inbox = %d %v", code, body)
	}
	if _, body := call("POST", "/notify/inbox/read", `{"all":true}`); body["unread"].(float64) != 0 {
		t.Fatalf("read = %v", body)
	}
	if code, body := call("POST", "/api/v1/watchlist", `{"q":"600036.sh"}`); code != 201 || body["added"] != true {
		t.Fatalf("add = %d %v", code, body)
	}
	code, body := call("GET", "/api/v1/watchlist", "")
	items := body["items"].([]any)
	first := items[0].(map[string]any)
	if code != 200 || first["ts_code"] != "600036.SH" || first["price"].(float64) != 39.5 || first["subtitle"].(map[string]any)["kind"] != "empty" {
		t.Fatalf("watchlist = %d %v", code, body)
	}
	if code, body := call("POST", "/api/v1/stocks/600036.SH/alerts", `{"side":"down","price":36.5}`); code != 201 || body["alert"] == nil {
		t.Fatalf("alert = %d %v", code, body)
	}
	if code, _ := call("POST", "/api/v1/stocks/600036.SH/alerts", `{"side":"sideways","price":36.5}`); code != 400 {
		t.Fatal("accepted a bad side")
	}
	if code, body := call("GET", "/api/stocks/600036.SH/conclusion?enqueue=0", ""); code != 200 || body["state"] == "ready" {
		t.Fatalf("conclusion = %d %v", code, body)
	}
	if code, _ := call("GET", "/api/stocks/bad/conclusion", ""); code != 400 {
		t.Fatal("accepted a bad code")
	}
	// Hong Kong listings (five digits) are followed too: the diagnose page must be able to read them
	if code, body := call("GET", "/api/stocks/02050.HK/conclusion?enqueue=0", ""); code != 200 || body["ts_code"] != "02050.HK" {
		t.Fatalf("HK conclusion = %d %v", code, body)
	}
}

func TestTheTwoPriceLinesComeFromTheDataServiceAndReachTheDetailAndTheChiefCard(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/shared/levels/") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","price":40.0,"down":{"price":36.5,"dist":-0.0875,"name":"20 日均线","condition":"连续 2 日收盘跌破","consequence":"→ 不明","verified":true},"up":{"price":44.0,"dist":0.1,"name":"前高","condition":"收盘站上","consequence":"→ 更强","verified":true}}`))
	}))
	defer api.Close()
	t.Setenv("STOCK_API_BASE", api.URL)
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.stepFn = (&speaker{}).fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	due(s, c)
	s.sch.TickSync()
	b := s.readBundle("600036.SH")
	if b == nil || b.Levels["down"] == nil || b.Levels["up"] == nil {
		t.Fatalf("bundle levels = %+v", b)
	}
	detail := s.chiefDetail("600036.SH")
	lv := detail["levels"].(map[string]any)
	if lv["down"].(map[string]any)["price"].(float64) != 36.5 || lv["up"].(map[string]any)["name"] != "前高" {
		t.Fatalf("detail levels = %v", lv)
	}
	card := s.conclusionView("600036.SH", false)["bundle"].(map[string]any)["chief"].(map[string]any)
	wrong := card["if_wrong"].(map[string]any)
	if wrong["alert"].(map[string]any)["price"].(float64) != 36.5 || !strings.Contains(wrong["text"].(string), "收盘跌破 36.50") {
		t.Fatalf("if_wrong = %v", wrong)
	}
	// a bundle made before the lines existed gets them the first time the detail is opened
	b.Levels = nil
	s.writeBundle(b)
	if lv := s.chiefDetail("600036.SH")["levels"].(map[string]any); lv["down"] == nil {
		t.Fatalf("old bundle did not get its lines: %v", lv)
	}
	if s.readBundle("600036.SH").Levels == nil {
		t.Fatal("the lines were not saved")
	}
	// the data service being down must not fail the conclusion
	api.Close()
	t.Setenv("STOCK_API_BASE", "http://127.0.0.1:1")
	if got := s.fetchLevels("600036.SH"); got != nil {
		t.Fatalf("levels from a dead service = %v", got)
	}
}

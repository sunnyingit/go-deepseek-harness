package server

import (
	"fmt"
	"testing"
	"time"

	"go-harness/internal/personal"
	"go-harness/internal/scan"
	"go-harness/internal/sched"
)

// series builds consecutive weekday closes starting at start, rising by step each day.
func series(start string, n int, first, step float64) [][2]any {
	d, _ := time.ParseInLocation("2006-01-02", start, sched.Zone)
	var out [][2]any
	for len(out) < n {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			out = append(out, [2]any{sched.Day(d), first + step*float64(len(out))})
		}
		d = d.AddDate(0, 0, 1)
	}
	return out
}

func TestEveryConclusionBecomesOnePredictionAndAskedOnesAreMarked(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1)}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.stepFn = (&speaker{}).fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	due(s, c)
	s.sch.TickSync()
	all := s.scan.All()
	if len(all) != 1 {
		t.Fatalf("predictions = %+v", all)
	}
	p := all[0]
	if p.Source != "scan" || p.Code != "600036.SH" || p.Direction != scan.Bullish || p.Verdict != "偏多 · 稍微" || p.Sector != "银行" || len(p.Analysts) != 6 {
		t.Fatalf("prediction = %+v", p)
	}
	if p.BaseDay != "2026-11-05" || p.BaseClose < 10.79 || p.BaseClose > 10.81 || p.Evidence == "" || p.BundleID == 0 {
		t.Fatalf("base = %+v", p)
	}
	// the same night's bundle is not written down twice
	s.runTask(t, "stock.nightly", "600036.SH")
	if n := len(s.scan.All()); n != 1 {
		t.Fatalf("second run made %d", n)
	}
}

func TestSettlingFillsFiveDayResultsOnceAndTellsTheUser(t *testing.T) {
	stock := series("2026-10-26", 8, 10, 0.1) // 10/26 .. 11/04 ; base 10/26 close 10.0; day 5 = 11/02 (10.5)
	sector := series("2026-10-26", 8, 100, 0.2)
	ds := &dataService{closes: map[string][][2]any{"600036.SH": stock}, sectors: map[string][][2]any{"600036.SH": sector}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-04", "17:40"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	s.scan.Add(scan.Prediction{Code: "600036.SH", Name: "招商银行", Source: "scan", Direction: scan.Bullish, Level: 2, Verdict: "偏多 · 稍微", BaseDay: "2026-10-26",
		BaseClose: 10, MadeAt: zoneTime("2026-10-26", "20:40").UnixMilli(), BundleID: 1, Analysts: []scan.Analyst{{Dimension: "news", Name: "新闻分析师", Lean: "bullish"}}})
	// unclear on another stock: same-code unclear would supersede the bullish call under tracking rules
	s.scan.Add(scan.Prediction{Code: "000001.SZ", Name: "平安银行", Source: "scan", Direction: scan.Unclear, Verdict: "不明", BaseDay: "2026-10-26", MadeAt: zoneTime("2026-10-26", "20:41").UnixMilli(), BundleID: 2})
	_ = c
	s.runTask(t, "scan.settle")
	got := s.scan.All()
	var bull, unclear *scan.Prediction
	for i := range got {
		switch got[i].Code {
		case "600036.SH":
			bull = &got[i]
		case "000001.SZ":
			unclear = &got[i]
		}
	}
	if bull == nil || unclear == nil {
		t.Fatalf("predictions = %+v", got)
	}
	r := bull.Result(5)
	if r == nil || r.Outcome != "hit" || r.StockRet < 0.049 || r.StockRet > 0.051 {
		t.Fatalf("result = %+v", r)
	}
	if bull.Result(20) != nil || unclear.Result(5) != nil {
		t.Fatalf("a 20-day or an unclear prediction was settled: bull=%+v unclear=%+v", bull, unclear)
	}
	found := 0
	for _, it := range s.inbox.List("system", false, 20) {
		if it.Type == "scan.settled" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("settle notices = %d", found)
	}
	// nothing is settled twice and no second notice is made
	s.runTask(t, "scan.settle")
	for _, it := range s.inbox.List("system", false, 20) {
		if it.Type == "scan.settled" {
			found--
		}
	}
	if found != 0 {
		t.Fatalf("second settle changed notices (%d)", found)
	}
}

func TestSettlingWaitsWhenTheDailyBarsAreOlderThanTheLastClose(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 4, 10, 0.1)}, sectors: map[string][][2]any{"600036.SH": series("2026-10-26", 4, 100, 0.2)}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-04", "17:40"))
	s.scan.Add(scan.Prediction{Code: "600036.SH", Name: "招商银行", Source: "scan", Direction: scan.Bullish, Level: 2, BaseDay: "2026-10-26", BaseClose: 10, MadeAt: 1, BundleID: 1})
	s.runTask(t, "scan.settle")
	if s.scan.All()[0].Result(5) != nil {
		t.Fatal("settled on stale data")
	}
}

func TestScanViewsSplitTodayTrackingAndSettled(t *testing.T) {
	stock := series("2026-10-26", 8, 10, 0.1)
	sector := series("2026-10-26", 8, 100, 2) // sector outruns the stock: a bullish call misses
	ds := &dataService{closes: map[string][][2]any{"600036.SH": stock, "000001.SZ": stock}, sectors: map[string][][2]any{"600036.SH": sector, "000001.SZ": sector}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-04", "17:40"))
	s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.personal.WishAdd(personal.Wish{Code: "000001.SZ", Name: "平安银行", Reason: "想买测试", PlanPct: 5})
	add := func(code, source string, dir string, day string, id int64) {
		s.scan.Add(scan.Prediction{Code: code, Name: code, Source: source, Direction: dir, Level: 2, Verdict: "x", BaseDay: day, BaseClose: 10, MadeAt: zoneTime(day, "20:40").UnixMilli(), BundleID: id})
	}
	add("600036.SH", "scan", scan.Bullish, "2026-10-26", 1)
	add("000001.SZ", "diagnose", scan.Bullish, "2026-11-03", 2)
	add("000001.SZ", "scan", scan.Unclear, "2026-11-03", 3)
	s.runTask(t, "scan.settle")
	sum := s.scanSummary()
	if sum["made"] != 2 || sum["tracking"] != 1 {
		t.Fatalf("summary = %+v (newer call for 000001 supersedes the open one; only 600036's 20-day left)", sum)
	}
	today := s.scanToday("diagnose", "")
	if today["total"] != 1 {
		t.Fatalf("today diagnose = %+v", today)
	}
	set := s.scanSettled(settledFilter{Horizon: 5})
	if set["n_days"] != 1 {
		t.Fatalf("settled = %+v", set)
	}
	days := set["days"].([]map[string]any)
	rows := days[0]["rows"].([]map[string]any)
	if rows[0]["outcome"] != "miss" || rows[0]["why"] == "" || rows[0]["mine"].(map[string]any)["held"] != true {
		t.Fatalf("row = %+v", rows[0])
	}
	if wrong := s.scanSettled(settledFilter{WrongOnly: true}); wrong["n_days"] != 1 {
		t.Fatalf("wrong only = %+v", wrong)
	}
	if none := s.scanSettled(settledFilter{Query: "不存在"}); none["n_days"] != 0 {
		t.Fatalf("query = %+v", none)
	}
	tr := s.scanTracking("all", "", "")
	if tr["total"].(int) != 1 {
		t.Fatalf("tracking = %+v", tr)
	}
	_ = fmt.Sprint
}

func TestUpcomingListsTheNextRunsAndWhatTheyWillScan(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1)}, sectors: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 100, 0.2)}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-07", "10:00")) // a Saturday
	s.personal.WatchAdd("002050.SZ", "三花智控")
	s.personal.WatchAdd("000021.SZ", "深科技")
	s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.scan.Add(scan.Prediction{Code: "600036.SH", Name: "招商银行", Source: "scan", Direction: scan.Bullish, Level: 1, BaseDay: "2026-11-05", BaseClose: 10, MadeAt: 1, BundleID: 9})
	up := s.scanUpcoming()
	tasks := up["scan_tasks"].([]map[string]any)
	if len(tasks) != 2 || tasks[0]["task_id"] != nightlyID || tasks[1]["task_id"] != "scan.settle" {
		t.Fatalf("scan tasks = %+v", tasks)
	}
	if tasks[0]["next"] != "2026-11-09 20:30" || tasks[1]["next"] != "2026-11-09 17:30" {
		t.Fatalf("a weekend must skip to Monday: %+v %+v", tasks[0]["next"], tasks[1]["next"])
	}
	nightly := up["nightly"].(map[string]any)
	subs := nightly["subjects"].([]map[string]any)
	if len(subs) != 3 || subs[0]["ts_code"] != "600036.SH" || subs[0]["tier_text"] != "持仓" || subs[2]["tier_text"] != "自选" {
		t.Fatalf("subjects = %+v", subs)
	}
	plan := up["settle"].([]map[string]any)
	if len(plan) == 0 || plan[0]["count"] == 0 {
		t.Fatalf("settle plan = %+v", plan)
	}
	// on a weekday morning the same task is due today
	s2, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	if got := s2.scanUpcoming()["scan_tasks"].([]map[string]any)[0]["when_text"]; got != "周一 11-09 20:30 · 今天" {
		t.Fatalf("when = %v", got)
	}
}

func TestRunNowReusesTodaysConclusionsAndWritesThemDown(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1)}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	sp := &speaker{}
	s.stepFn = sp.fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	due(s, c)
	s.sch.TickSync()
	calls := sp.calls["chief"]
	made := s.readBundle("600036.SH").ID
	// predictions were not being kept when that conclusion was made
	s.scan = scan.Open("", c.now)
	c.advance(30 * time.Minute)
	out, err := s.runScanNow(nightlyID)
	if err != nil || out["queued"] != 1 {
		t.Fatalf("run now = %+v %v", out, err)
	}
	s.sch.TickSync()
	all := s.scan.All()
	if len(all) != 1 || all[0].Source != "scan" || all[0].MadeAt != made {
		t.Fatalf("predictions = %+v (bundle %d)", all, made)
	}
	if sp.calls["chief"] != calls {
		t.Fatalf("a fresh conclusion was made again: %d -> %d", calls, sp.calls["chief"])
	}
	// asking twice writes nothing twice
	_, _ = s.runScanNow(nightlyID)
	s.sch.TickSync()
	if n := len(s.scan.All()); n != 1 {
		t.Fatalf("second run wrote %d", n)
	}
	if _, err := s.runScanNow("nope"); err == nil {
		t.Fatal("an unknown task was accepted")
	}
}

func TestUpcomingShowsTheArmedPriceAlertsAndWhenTheyAreChecked(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00")) // a Monday, in the morning session
	s.quotes.Get = func(string) (string, error) { return `v_sz002050="1~三花智控~002050~58.00~57.00~1";`, nil }
	if _, err := s.personal.AlertAdd("002050.SZ", "down", 56.5, "touch", "三花智控"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.personal.AlertAdd("002050.SZ", "up", 62, "close", "三花智控"); err != nil {
		t.Fatal(err)
	}
	w := s.scanUpcoming()["watch"].(map[string]any)
	rows := w["alerts"].([]map[string]any)
	if len(rows) != 2 || w["in_session"] != true {
		t.Fatalf("watch = %+v", w)
	}
	// the nearer one first; the gap is measured from the last price
	if rows[0]["price"] != 56.5 || rows[0]["check"] != "盘中每 3 分钟检查" {
		t.Fatalf("first = %+v", rows[0])
	}
	if gap := rows[0]["gap_pct"].(float64); gap > -0.025 || gap < -0.027 {
		t.Fatalf("gap = %v", gap)
	}
	if rows[1]["check"] != "收盘后核对，下一次 周一 11-09 15:35 · 今天" {
		t.Fatalf("close check = %v", rows[1]["check"])
	}
	// at night the touch check waits for the next session
	s2, _ := newClientServer(t, zoneTime("2026-11-07", "10:00")) // Saturday
	s2.quotes.Get = s.quotes.Get
	_, _ = s2.personal.AlertAdd("002050.SZ", "down", 56.5, "touch", "三花智控")
	r := s2.scanUpcoming()["watch"].(map[string]any)["alerts"].([]map[string]any)
	if r[0]["check"] != "下一次 周一 11-09 09:30 · 2 天后" {
		t.Fatalf("weekend check = %v", r[0]["check"])
	}
}

func TestDayPanelSummarisesTheSettleDayAndTomorrow(t *testing.T) {
	stock := series("2026-10-26", 8, 10, 0.1)
	sector := series("2026-10-26", 8, 100, 2)
	ds := &dataService{closes: map[string][][2]any{"600036.SH": stock, "000001.SZ": stock}, sectors: map[string][][2]any{"600036.SH": sector, "000001.SZ": sector}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-04", "17:40"))
	s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.scan.Add(scan.Prediction{Code: "600036.SH", Name: "招商银行", Source: "scan", Direction: scan.Bullish, Level: 2, Verdict: "偏多 · 比较明显", BaseDay: "2026-10-26", BaseClose: 10, MadeAt: 1, BundleID: 1,
		Analysts: []scan.Analyst{{Dimension: "volume_flow", Name: "资金流分析师", Lean: "bullish", Text: "大资金流入"}}})
	s.runTask(t, "scan.settle")
	p := s.scanDayPanel()
	if p["has"] != true || p["day"] != "2026-11-02" && p["day"] != "2026-11-04" {
		t.Fatalf("panel = %+v", p)
	}
	rate := p["rate"].(scan.Rate)
	if rate.N != 1 || rate.Miss != 1 {
		t.Fatalf("rate = %+v", rate)
	}
	if mine := p["mine"].([]map[string]any); len(mine) != 1 || mine[0]["name"] != "招商银行" {
		t.Fatalf("mine = %+v", p["mine"])
	}
	if r := p["wrong_reasons"].([]map[string]any); len(r) != 1 || r[0]["count"] != 1 {
		t.Fatalf("reasons = %+v", p["wrong_reasons"])
	}
	if tm := p["tomorrow"].(map[string]any); tm["day"] != "2026-11-05" {
		t.Fatalf("tomorrow = %+v", tm)
	}
}

func TestAPredictionIsFiledByWhatAskedForTheConclusion(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1)}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.stepFn = (&speaker{}).fn
	for _, code := range []string{"600036.SH", "000001.SZ", "300750.SZ"} {
		s.personal.WatchAdd(code, code)
		seedDebate(s, code)
	}
	due(s, c) // the schedule makes all three
	s.sch.TickSync()
	for _, p := range s.scan.All() {
		if p.Source != "scan" {
			t.Fatalf("the schedule's conclusion was filed as %q", p.Source)
		}
	}
	// pressing 重新分析 makes a new conclusion that is yours, and the same stock gets a second prediction
	if _, err := s.requestConclusion("600036.SH", "manual", true); err != nil {
		t.Fatal(err)
	}
	s.sch.TickSync()
	got := map[string]int{}
	for _, p := range s.scan.All() {
		got[p.Code+":"+p.Source]++
	}
	if got["600036.SH:scan"] != 1 || got["600036.SH:diagnose"] != 1 {
		t.Fatalf("predictions = %v", got)
	}
	// the run-now button only writes down conclusions that are already there: each keeps what asked for it
	s.scan = scan.Open("", c.now)
	b := s.readBundle("600036.SH")
	if b.Trigger != "manual" {
		t.Fatalf("trigger = %q", b.Trigger)
	}
	if _, err := s.runScanNow(nightlyID); err != nil {
		t.Fatal(err)
	}
	s.sch.TickSync()
	by := map[string]string{}
	for _, p := range s.scan.All() {
		by[p.Code] = p.Source
	}
	if by["600036.SH"] != "diagnose" || by["000001.SZ"] != "scan" {
		t.Fatalf("sources after run-now = %v", by)
	}
	if sourceFromTrigger("watch_add") != "diagnose" || sourceFromTrigger("first_view") != "diagnose" || sourceFromTrigger("holdings") != "scan" || sourceFromTrigger("schedule") != "scan" {
		t.Fatal("trigger mapping changed")
	}
}

func TestTodayPicksOutWhatTheUserShouldReadAndHidingOnlyChangesTheTrackingList(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1), "000001.SZ": series("2026-10-26", 9, 10, 0.1), "600519.SH": series("2026-10-26", 9, 10, 0.1)}}
	ds.start(t)
	s, c := newClientServer(t, zoneTime("2026-11-05", "17:40"))
	_ = c
	s.personal.TradeAdd(personal.Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	s.personal.WatchAdd("000001.SZ", "平安银行")
	s.personal.WatchAdd("600519.SH", "贵州茅台")
	add := func(code, source, dir, day string, id int64) {
		s.scan.Add(scan.Prediction{Code: code, Name: code, Source: source, Direction: dir, Level: 2, Verdict: word(dir), Evidence: "理由。", BaseDay: day, BaseClose: 10, MadeAt: zoneTime(day, "20:40").UnixMilli(), BundleID: id})
	}
	add("000001.SZ", "scan", scan.Bullish, "2026-11-03", 1) // yesterday bullish, today bearish: turned round
	add("600036.SH", "scan", scan.Bearish, "2026-11-04", 2) // held, bearish today: against the user's plan
	add("000001.SZ", "scan", scan.Bearish, "2026-11-04", 3)
	add("600519.SH", "scan", scan.Bullish, "2026-11-04", 4) // nothing special
	today := s.scanToday("", "")
	rows := today["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["ts_code"] != "600036.SH" {
		t.Fatalf("today rows = %+v (自选不进战绩，只留持仓)", rows)
	}
	if mine := rows[0]["mine"].(map[string]any); mine["held"] != true {
		t.Fatalf("mine = %+v", mine)
	}
	if today["total"].(int) != 1 {
		t.Fatalf("today total = %v", today["total"])
	}
	if wishOnly := s.scanToday("", "wish"); wishOnly["total"].(int) != 0 {
		t.Fatalf("kind=wish should be empty = %+v", wishOnly)
	}

	// hiding a held stock takes it out of the tracking list and its count, not out of the record
	before := s.scanTracking("all", "", "")["total"].(int)
	if before != 1 {
		t.Fatalf("tracking before hide = %d, want 1 held stock", before)
	}
	a := newCustomAPI(t, s)
	if code, _ := a.call("POST", "/api/scan/hide", `{"code":"600036.SH","on":true}`); code != 200 {
		t.Fatalf("hide = %d", code)
	}
	tr := s.scanTracking("all", "", "")
	if tr["total"].(int) >= before || len(tr["hidden"].([]map[string]any)) != 1 || s.scanSummary()["tracking"].(int) >= before {
		t.Fatalf("after hiding = %+v (before %d)", tr, before)
	}
	if len(s.scan.All()) != 4 {
		t.Fatal("hiding changed the predictions")
	}
	if code, _ := a.call("POST", "/api/scan/hide", `{"code":"","on":true}`); code != 400 {
		t.Fatalf("hide nothing = %d", code)
	}
	a.call("POST", "/api/scan/hide", `{"code":"600036.SH","on":false}`)
	if got := s.scanTracking("all", "", "")["total"].(int); got != before {
		t.Fatalf("after showing again = %d, want %d", got, before)
	}
}

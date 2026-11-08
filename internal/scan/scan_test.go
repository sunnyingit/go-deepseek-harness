package scan

import (
	"math"
	"strings"
	"testing"
	"time"
)

func bars(start string, closes ...float64) []Bar {
	day, _ := time.Parse("2006-01-02", start)
	var out []Bar
	for i, c := range closes {
		out = append(out, Bar{day.AddDate(0, 0, i).Format("2006-01-02"), c})
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTrackMeasuresStockAgainstItsSectorFromTheBaseDayAndKnowsWhenItIsDone(t *testing.T) {
	stock := bars("2026-10-26", 10, 10.1, 10.2, 10.3, 10.4, 10.5, 10.6) // base day 10-26
	sector := bars("2026-10-26", 100, 100.5, 101, 101, 101, 101, 101)
	p := Prediction{BaseDay: "2026-10-26"}
	mid := Track(p, stock[:4], sector[:4], 5)
	if !mid.Have || mid.Day != 3 || mid.Done || !near(mid.StockRet, 10.3/10-1) || !near(mid.SectorRet, 0.01) || !near(mid.Excess, 10.3/10-1-0.01) {
		t.Fatalf("mid = %+v", mid)
	}
	done := Track(p, stock, sector, 5)
	if !done.Done || done.Day != 5 || done.EndDay != "2026-10-31" || !near(done.StockRet, 0.05) {
		t.Fatalf("done = %+v", done)
	}
	if lag := Track(p, stock, sector[:3], 5); lag.Day != 2 || lag.Done {
		t.Fatalf("a sector that is behind the stock must not be mixed in: %+v", lag)
	}
	if none := Track(Prediction{BaseDay: "2026-10-20"}, stock, sector, 5); none.Have {
		t.Fatalf("no data before the base day: %+v", none)
	}
	if nosector := Track(p, stock, bars("2026-10-27", 1, 2, 3), 5); nosector.Have {
		t.Fatalf("a sector without the base day means nothing: %+v", nosector)
	}
}

func TestJudgeFollowsTheDesignRules(t *testing.T) {
	cases := []struct {
		dir    string
		excess float64
		want   string
	}{
		{Bullish, 0.02, "hit"}, {Bullish, -0.02, "miss"}, {Bearish, -0.02, "hit"}, {Bearish, 0.02, "miss"},
		{Bullish, 0.004, "flat"}, {Bearish, -0.0049, "flat"}, {Bullish, 0.005, "hit"}, {Unclear, 0.05, ""},
	}
	for _, c := range cases {
		if got := Judge(c.dir, c.excess); got != c.want {
			t.Errorf("Judge(%s, %v) = %q want %q", c.dir, c.excess, got, c.want)
		}
	}
}

func TestSettleJudgesEachAnalystAndSaysWhyAWrongCallWasWrong(t *testing.T) {
	p := Prediction{Direction: Bullish, Analysts: []Analyst{
		{Dimension: "volume_flow", Name: "资金流", Lean: "support", Text: "大资金连续流入，九成来自两笔超大单"},
		{Dimension: "sector", Name: "板块", Lean: "warn", Text: "板块转弱"},
		{Dimension: "news", Name: "新闻", Lean: "neutral"},
	}}
	r := Settle(p, Tracking{Day: 5, EndDay: "2026-11-02", StockRet: -0.031, SectorRet: -0.004, Excess: -0.027, Done: true, Have: true}, time.Unix(1_800_000_000, 0))
	if r.Outcome != "miss" || r.Analysts["volume_flow"] != "miss" || r.Analysts["sector"] != "hit" || r.Analysts["news"] != "" {
		t.Fatalf("result = %+v", r)
	}
	if r.WhyDim != "volume_flow" || !strings.Contains(r.Why, "资金流分析师偏多") || !strings.Contains(r.Why, "跑输板块 2.7 个点") {
		t.Fatalf("why = %q (%s)", r.Why, r.WhyDim)
	}
	if hit := Settle(p, Tracking{Day: 5, Excess: 0.02, Done: true}, time.Now()); hit.Why != "" || hit.Outcome != "hit" {
		t.Fatalf("a right call needs no excuse: %+v", hit)
	}
}

func TestStoreNeverChangesAWrittenPredictionOrResultAndDedupesByConclusion(t *testing.T) {
	path := t.TempDir() + "/p.json"
	st := Open(path, func() time.Time { return time.Unix(1_800_000_000, 0) })
	a, made := st.Add(Prediction{Code: "A", Source: "scan", Direction: Bullish, BundleID: 7})
	if !made || a.ID != "p1" || a.MadeAt == 0 {
		t.Fatalf("a = %+v", a)
	}
	if _, made := st.Add(Prediction{Code: "A", Source: "scan", Direction: Bearish, BundleID: 7}); made {
		t.Fatal("the same conclusion was written twice")
	}
	if _, made := st.Add(Prediction{Code: "A", Source: "diagnose", Direction: Bullish, BundleID: 7}); !made {
		t.Fatal("the same conclusion for another source is its own record")
	}
	if !st.SetResult("p1", 5, Result{Outcome: "hit"}) || st.SetResult("p1", 5, Result{Outcome: "miss"}) {
		t.Fatal("a result can be written once and only once")
	}
	if got := Open(path, nil).All()[0].Result(5); got == nil || got.Outcome != "hit" {
		t.Fatalf("after restart = %+v", got)
	}
}

func TestStatsOverallBaselineBreakdownsAndTheRollingCurve(t *testing.T) {
	today := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	made := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC).UnixMilli()
	mk := func(src, dir string, level int, market string, day string, outcome string, excess float64, dims map[string]string) Prediction {
		return Prediction{Source: src, Direction: dir, Level: level, Market: market, MadeAt: made,
			Results: map[string]*Result{"5": {Day: day, Outcome: outcome, Excess: excess, Analysts: dims}}}
	}
	preds := []Prediction{
		mk("scan", Bullish, 2, "震荡市", "2026-11-02", "hit", 0.03, map[string]string{"volume_flow": "hit"}),
		mk("scan", Bullish, 1, "震荡市", "2026-11-02", "miss", -0.02, map[string]string{"volume_flow": "miss"}),
		mk("scan", Bearish, 2, "震荡市", "2026-11-02", "hit", -0.04, nil),
		mk("diagnose", Bearish, 1, "上涨市", "2026-11-03", "miss", 0.02, nil),
		mk("scan", Bullish, 1, "震荡市", "2026-11-03", "flat", 0.001, nil),
		{Source: "scan", Direction: Unclear, MadeAt: made},
		{Source: "scan", Direction: Bullish, MadeAt: made}, // not settled yet
	}
	a := Stats(preds, 5, 60, today)
	if a.Overall.Hit != 2 || a.Overall.Miss != 2 || a.Overall.Flat != 1 || !near(a.Overall.Pct, 0.5) {
		t.Fatalf("overall = %+v", a.Overall)
	}
	// excess signs among decided calls: +,-,-,+ → half up: the baseline is 0.5
	if !near(a.Baseline, 0.5) || !near(a.Edge, 0) {
		t.Fatalf("baseline = %v edge = %v", a.Baseline, a.Edge)
	}
	if a.Made != 7 || !near(a.Unclear, 1.0/7) {
		t.Fatalf("made = %d unclear = %v", a.Made, a.Unclear)
	}
	byLevel := map[string]Rate{}
	for _, r := range a.Level {
		byLevel[r.Label] = r.Rate
	}
	if byLevel["比较明显"].Hit != 2 || byLevel["稍微"].Miss != 2 {
		t.Fatalf("by level = %+v", a.Level)
	}
	if a.Source[0].Key != "diagnose" || a.Source[1].Rate.N != 4 {
		t.Fatalf("by source = %+v", a.Source)
	}
	if len(a.Analysts) != 1 || a.Analysts[0].Rate.Hit != 1 || a.Analysts[0].Rate.Miss != 1 {
		t.Fatalf("by analyst = %+v", a.Analysts)
	}
	if len(a.ByDay) != 2 || !near(a.ByDay[0].Rate.Pct, 2.0/3) || !near(a.ByDay[1].Rolling, 0.5) {
		t.Fatalf("by day = %+v", a.ByDay)
	}
	if none := Stats(nil, 20, 60, today); none.Overall.Pct != -1 || none.Baseline != -1 {
		t.Fatalf("empty stats = %+v", none)
	}
	if Market("顺风") != "上涨市" || Market("逆风") != "下跌市" || Market("中性") != "震荡市" || Market("没有") != "" {
		t.Fatal("market words")
	}
}

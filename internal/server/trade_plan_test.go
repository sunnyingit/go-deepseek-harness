package server

import (
	"strings"
	"testing"

	"go-harness/internal/personal"
	"go-harness/internal/scan"
)

func TestMovingAveragesStartWhereTheyCanBeComputedAndTheChartCarriesTheTwoPriceLines(t *testing.T) {
	var bars []scan.Bar
	for i := 0; i < 300; i++ {
		bars = append(bars, scan.Bar{Day: series("2025-01-02", 300, 10, 0.1)[i][0].(string), Close: float64(i + 1)})
	}
	ma := maSeries(bars[:10], 7)
	if ma[5] != nil || ma[6] == nil || *ma[6] != 4 || *ma[9] != 7 { // (1..7)/7 = 4, (4..10)/7 = 7
		t.Fatalf("ma7 = %v %v %v", ma[5], ma[6], ma[9])
	}
	levels := map[string]any{
		"down": map[string]any{"price": 280.0, "name": "20 日均线", "verified": true},
		"up":   map[string]any{"price": 320.0, "name": "前高"},
	}
	trend := chartTrend(bars, levels)
	if trend["window"] != chartDays || len(trend["bars"].([]map[string]any)) != chartDays {
		t.Fatalf("window = %v", trend["window"])
	}
	m := trend["ma"].(map[string]any)
	if m["ma7"] != 297.0 || m["ma20"] != 290.5 || m["ma60"] != 270.5 { // last closes 294..300 / 281..300 / 241..300
		t.Fatalf("latest averages = %v", m)
	}
	lines := trend["lines"].(map[string]any)
	if lines["up"].(map[string]any)["label"] != "突破价位" || lines["down"].(map[string]any)["price"] != 280.0 {
		t.Fatalf("lines = %v", lines)
	}
	if len(trend["x_ticks"].([]string)) != 6 || chartTrend(bars[:1], nil) != nil {
		t.Fatal("ticks or the too-short case")
	}
}

func planFor(verdict string, mod func(*planInput)) tradePlan {
	in := planInput{
		Code: "603738.SH", Name: "泰晶科技", Verdict: verdict, Day: "2026-11-08", Stamp: "2026-11-08 12:00", BundleID: 7,
		Down: &planLevel{Price: 23.33, Name: "20 日均线", Condition: "连续 2 日收盘跌破", Consequent: "这轮判断不成立"},
		Up:   &planLevel{Price: 27.93, Name: "前高", Condition: "收盘站上"},
	}
	if mod != nil {
		mod(&in)
	}
	return buildTradePlan(in)
}

func section(p tradePlan, key string) planSection {
	for _, s := range p.Sections {
		if s.Key == key {
			return s
		}
	}
	return planSection{}
}

func TestTheTradePlanIsConditionalAndNeverOrdersOrNamesATarget(t *testing.T) {
	p := planFor("偏空", func(in *planInput) { in.Held, in.Cost = true, 25 })
	hold, buy := section(p, "hold"), section(p, "buy")
	if !strings.Contains(hold.Lines[0].Text, "不加仓") || !strings.Contains(hold.Lines[1].Text, "转弱价 23.33（20 日均线）：连续 2 日收盘跌破，这轮判断不成立") || !strings.Contains(hold.Lines[1].Text, "相对你的成本 25.00 是 -6.7%") {
		t.Fatalf("hold = %+v", hold.Lines)
	}
	if !strings.Contains(buy.Lines[0].Text, "先等") || !strings.Contains(buy.Lines[1].Text, "收盘站上 27.93") || !strings.Contains(buy.Lines[2].Text, "放弃这一轮") {
		t.Fatalf("buy = %+v", buy.Lines)
	}
	if !p.Held || p.Window != "5～20 个交易日" || !strings.Contains(p.Note, "不是确定的买卖指令") {
		t.Fatalf("plan = %+v", p)
	}
	for _, s := range p.Sections {
		for _, l := range s.Lines {
			if forbiddenHit(l.Text, actionBanned) {
				t.Fatalf("a banned word in %q", l.Text)
			}
		}
	}
	// the chief's own reminders replace the default first line; a bullish verdict allows probing, with a stop
	q := planFor("偏多", func(in *planInput) {
		in.HoldNote, in.BuyNote = "继续拿，板块转弱再看", "可以先试探"
		in.Wish, in.WishPct = true, 5
	})
	if section(q, "hold").Lines[0].Text != "继续拿，板块转弱再看" || section(q, "buy").Lines[0].Text != "可以先试探" {
		t.Fatalf("notes = %+v", q.Sections)
	}
	if bl := section(q, "buy").Lines; !strings.Contains(bl[1].Text, "收盘跌破 23.33 就停止试探") || !strings.Contains(bl[3].Text, "占总资金 5%") {
		t.Fatalf("buy lines = %+v", bl)
	}
	// no price lines: no watch lines, still a plan
	r := planFor("不明", func(in *planInput) { in.Down, in.Up = nil, nil })
	if len(section(r, "hold").Lines) != 2 || !strings.Contains(section(r, "buy").Lines[1].Text, "转为偏多") {
		t.Fatalf("no lines: %+v", r.Sections)
	}
}

func TestThePlanIsMadeWhenAConclusionArrivesAndAgainOnNewerOnesOrOnRequest(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"603738.SH": series("2026-10-26", 9, 24, 0.1)}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.personal.TradeAdd(personal.Trade{Code: "603738.SH", Name: "泰晶科技", Side: "buy", Price: 25, Qty: 100, Date: "2026-10-01"})
	a := newCustomAPI(t, s)
	if code, out := a.call("GET", "/api/stocks/603738.SH/trade-plan", ""); code != 200 || out["status"] != "empty" {
		t.Fatalf("before any conclusion: %d %v", code, out)
	}
	bundle := func(id int64, verdict string) {
		s.writeBundle(&conclusionBundle{
			ID: id, Code: "603738.SH", Source: "agent", GeneratedDay: "2026-11-09",
			Levels: map[string]any{"down": map[string]any{"price": 23.33, "name": "20 日均线", "condition": "收盘跌破"}},
			Chief:  chiefPayload(agentConfig{ID: "chief", Name: "主分析师"}, &chiefDecision{Verdict: verdict, Text: verdict, Actions: chiefActions{Hold: "现在不加仓"}}, &debateFile{}),
		})
	}
	bundle(1, "偏空")
	_, out := a.call("GET", "/api/stocks/603738.SH/trade-plan", "")
	plan := out["plan"].(map[string]any)
	if out["status"] != "ok" || plan["verdict"] != "偏空" || plan["held"] != true || plan["bundle_id"] != float64(1) {
		t.Fatalf("plan = %v", out)
	}
	// the same conclusion: the saved plan is given back, with its own time
	first := plan["generated_at"]
	_, out = a.call("GET", "/api/stocks/603738.SH/trade-plan", "")
	if out["plan"].(map[string]any)["generated_at"] != first {
		t.Fatal("the plan was made again for the same conclusion")
	}
	// a newer conclusion: a new plan, with no click
	bundle(2, "偏多")
	_, out = a.call("GET", "/api/stocks/603738.SH/trade-plan", "")
	if p := out["plan"].(map[string]any); p["verdict"] != "偏多" || p["bundle_id"] != float64(2) {
		t.Fatalf("after a newer conclusion: %v", p)
	}
	if code, out := a.call("POST", "/api/stocks/603738.SH/trade-plan", ""); code != 200 || out["status"] != "ok" {
		t.Fatalf("regenerate = %d %v", code, out)
	}
	// the chart route gives a year of closes (here nine) with the lines from the conclusion
	_, out = a.call("GET", "/api/stocks/603738.SH/chart", "")
	tr, _ := out["trend"].(map[string]any)
	if out["status"] != "ok" || tr == nil || tr["window"] != float64(9) || tr["lines"].(map[string]any)["down"] == nil {
		t.Fatalf("chart = %v", out)
	}
}

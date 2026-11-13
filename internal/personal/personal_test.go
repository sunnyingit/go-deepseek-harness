package personal

import (
	"math"
	"strings"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPositionsUseAMovingWeightedCostAndLabelEachTrade(t *testing.T) {
	trades := []Trade{
		{ID: 1, Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"},
		{ID: 2, Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 12, Qty: 100, Date: "2026-10-02"},
		{ID: 3, Code: "600036.SH", Name: "招商银行", Side: "sell", Price: 13, Qty: 50, Date: "2026-10-03"},
		{ID: 4, Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 5, Qty: 100, Date: "2026-10-01"},
		{ID: 5, Code: "000001.SZ", Name: "平安银行", Side: "sell", Price: 6, Qty: 100, Date: "2026-10-04"},
	}
	held, notes := Positions(trades)
	pos := held["600036.SH"]
	if pos == nil || pos.Qty != 150 || !near(pos.Avg, 11) || pos.Since != "2026-10-01" || !near(pos.Realized, 100) {
		t.Fatalf("pos = %+v", pos)
	}
	if _, still := held["000001.SZ"]; still {
		t.Fatal("a cleared position is still held")
	}
	if notes[1].Label != "建仓" || notes[2].Label != "加仓" || notes[3].Label != "减仓" || notes[5].Label != "清仓" {
		t.Fatalf("labels = %+v", notes)
	}
	if !near(notes[3].PnL, 100) || !near(notes[3].PnLPct, 13.0/11-1) {
		t.Fatalf("sell note = %+v", notes[3])
	}
}

func TestValuationMarksMissingQuotesAndTotalsAddUp(t *testing.T) {
	held := map[string]*Position{
		"A": {Code: "A", Name: "甲", Qty: 100, Avg: 10, Since: "2026-10-01"},
		"B": {Code: "B", Name: "乙", Qty: 200, Avg: 5},
	}
	rows := Valuation(held, map[string]Quote{"A": {Price: 11, Change: 0.5, ChangePct: 4.76}}, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	a, b := rows[0], rows[1]
	if !a.Live || !near(a.MV, 1100) || !near(a.PnL, 100) || !near(a.DayPnL, 50) || a.Days == nil || *a.Days != 35 {
		t.Fatalf("a = %+v", a)
	}
	if b.Live || b.ChangePct != nil || !near(b.MV, 1000) || !near(b.PnL, 0) || !near(b.DayPnL, 0) {
		t.Fatalf("b (no quote) = %+v", b)
	}
	tot := SumTotals(rows)
	if !near(tot.MV, 2100) || !near(tot.PnL, 100) || !near(tot.Cost, 2000) || !near(tot.PnLPct, 0.05) || !near(a.Weight+b.Weight, 1) {
		t.Fatalf("totals = %+v", tot)
	}
}

func TestStoreDedupesAlertsOrdersSubjectsAndSurvivesARestart(t *testing.T) {
	path := t.TempDir() + "/personal.json"
	st := Open(path, func() time.Time { return time.Unix(1_800_000_000, 0) })
	st.WatchAdd("300750.SZ", "宁德时代")
	st.WatchAdd("600036.SH", "招商银行")
	if st.WatchAdd("300750.SZ", "宁德时代") {
		t.Fatal("added a duplicate")
	}
	first, _ := st.AlertAdd("600036.SH", "down", 30, "close", "招商银行")
	again, _ := st.AlertAdd("600036.SH", "down", 30.001, "touch", "招商银行")
	if first.ID != again.ID || len(st.Alerts("")) != 1 {
		t.Fatal("same alert stored twice")
	}
	if _, err := st.AlertAdd("600036.SH", "sideways", 30, "close", ""); err == nil {
		t.Fatal("accepted a bad side")
	}
	st.TradeAdd(Trade{Code: "601318.SH", Name: "中国平安", Side: "buy", Price: 40, Qty: 100, Date: "2026-10-01"})
	st.WishAdd(Wish{Code: "000858.SZ", Name: "五粮液", Reason: "高端白酒"})
	got := []string{}
	for _, s := range st.Subjects() {
		got = append(got, s.Code+":"+s.Tier)
	}
	want := []string{"601318.SH:hold", "000858.SZ:wish", "300750.SZ:other", "600036.SH:other"}
	if len(got) != len(want) {
		t.Fatalf("subjects = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("subjects = %v", got)
		}
	}
	re := Open(path, nil)
	if len(re.Trades()) != 1 || len(re.WatchList()) != 2 || !re.AlertRemove(first.ID) || len(re.Alerts("")) != 0 {
		t.Fatal("state lost across restart")
	}
}

func TestTradeValidationMirrorsWhatTheUserCanFixAndRefusesImpossibleSales(t *testing.T) {
	st := Open("", func() time.Time { return time.Unix(1_800_000_000, 0) })
	bad := func(tr Trade, want string) {
		t.Helper()
		_, err := st.TradeAdd(tr)
		if _, ok := err.(InputError); !ok || !strings.Contains(err.Error(), want) {
			t.Fatalf("%+v: err = %v, want %q", tr, err, want)
		}
	}
	bad(Trade{Code: "A", Side: "hold", Price: 1, Qty: 1, Date: "2026-11-02"}, "方向")
	bad(Trade{Code: "A", Side: "buy", Price: 0, Qty: 1, Date: "2026-11-02"}, "价格")
	bad(Trade{Code: "A", Side: "buy", Price: 1, Qty: 0, Date: "2026-11-02"}, "数量")
	bad(Trade{Code: "A", Side: "buy", Price: 1, Qty: 1, Date: "11-02"}, "日期")
	bad(Trade{Code: "A", Side: "sell", Price: 1, Qty: 100, Date: "2026-11-02"}, "卖出")
	buy, _ := st.TradeAdd(Trade{Code: "A", Name: "甲", Side: "buy", Price: 10, Qty: 100, Date: "2026-11-01"})
	sell, _ := st.TradeAdd(Trade{Code: "A", Name: "甲", Side: "sell", Price: 11, Qty: 100, Date: "2026-11-02"})
	if ok, err := st.TradeRemove(buy.ID); ok || err == nil {
		t.Fatal("deleted the buy a later sale depends on")
	}
	if ok, err := st.TradeRemove(sell.ID); !ok || err != nil {
		t.Fatalf("remove sell: %v %v", ok, err)
	}
	if ok, _ := st.TradeRemove(buy.ID); !ok {
		t.Fatal("could not remove the buy after the sale was gone")
	}
}

func TestWishReasonRulesCheckupsAndKV(t *testing.T) {
	st := Open("", func() time.Time { return time.Unix(1_800_000_000, 0) })
	if _, err := st.WishAdd(Wish{Code: "A", Name: "甲"}); err == nil {
		t.Fatal("a wish without a reason was accepted")
	}
	w, err := st.WishAdd(Wish{Code: "A", Name: "甲", Reason: "x", PlanPct: 10, Source: "weird"})
	if err != nil || w.Source != "manual" || w.Match != "all" || w.Status != "open" {
		t.Fatalf("wish = %+v %v", w, err)
	}
	if _, err := st.WishAdd(Wish{Code: "A", Name: "甲", Reason: "again"}); err == nil {
		t.Fatal("added the same stock twice")
	}
	if !st.WishCloseWith(w.ID, "bought") || len(st.Wishes()) != 0 || st.WishCloseWith(w.ID, "dropped") {
		t.Fatal("close semantics wrong")
	}
	if _, err := st.SetReason("A", "", nil, ""); err == nil {
		t.Fatal("empty reason accepted")
	}
	r, _ := st.SetReason("A", "高股息", []Item{{"kind": "close_below", "value": 36.5}}, "")
	if r.WrittenAt == "" {
		t.Fatalf("reason = %+v", r)
	}
	st.DropReason("A")
	if _, ok := st.ReasonOf("A"); ok {
		t.Fatal("reason not dropped")
	}
	rule, _ := st.AddRule("A", "flow_out", 3)
	again, _ := st.AddRule("A", "flow_out", 3)
	if rule.ID != again.ID || len(st.RulesOf("A")) != 1 || !st.RemoveRule(rule.ID) {
		t.Fatal("rules wrong")
	}
	if _, err := st.AddRule("A", "other", 3); err == nil {
		t.Fatal("unknown rule kind accepted")
	}
	st.SaveCheckup(Item{"n": 1})
	st.SaveCheckup(Item{"n": 2})
	if got := st.Checkups(1); len(got) != 1 || got[0].Data["n"].(int) != 2 {
		t.Fatalf("checkups = %+v", got)
	}
	st.KVSet("seen", []string{"a"})
	var seen []string
	if !st.KVGet("seen", &seen) || len(seen) != 1 {
		t.Fatal("kv lost")
	}
	st.AlertAdd("A", "down", 30, "close", "理由不成立")
	st.AlertAdd("A", "down", 25, "close", "我自己设的")
	if st.DropAlertsByName("A", "理由不成立") != 1 || len(st.Alerts("A")) != 1 {
		t.Fatal("tagged alert drop wrong")
	}
}

func TestAdjustAndEditTradesKeepTheLedgerConsistent(t *testing.T) {
	st := Open("", func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) })
	if _, err := st.TradeAdd(Trade{Code: "600036.SH", Name: "招商银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-11-01"}); err != nil {
		t.Fatal(err)
	}
	sell, err := st.TradeAdd(Trade{Code: "600036.SH", Name: "招商银行", Side: "sell", Price: 11, Qty: 60, Date: "2026-11-03"})
	if err != nil {
		t.Fatal(err)
	}
	// the buy cannot be cut below what the later sale needs
	buyID := st.TradesOf("600036.SH")[0].ID
	if _, err := st.TradeUpdate(buyID, 10, 50, "2026-11-01"); err == nil {
		t.Fatal("a buy smaller than the later sale was accepted")
	}
	if _, err := st.TradeUpdate(buyID, 10.5, 120, "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TradeUpdate(sell.ID, 0, 60, "2026-11-03"); err == nil {
		t.Fatal("a zero price was accepted")
	}
	held := Held(st.Trades())["600036.SH"]
	if held.Qty != 60 || held.Avg != 10.5 {
		t.Fatalf("held = %+v", held)
	}
	// a correction sets quantity and cost; later trades build on it
	if _, err := st.TradeAdjust("600036.SH", "招商银行", 200, 9.5, "2026-11-04"); err != nil {
		t.Fatal(err)
	}
	held = Held(st.Trades())["600036.SH"]
	if held.Qty != 200 || held.Avg != 9.5 {
		t.Fatalf("after correction %+v", held)
	}
	if _, err := st.TradeAdd(Trade{Code: "600036.SH", Name: "招商银行", Side: "sell", Price: 12, Qty: 250, Date: "2026-11-05"}); err == nil {
		t.Fatal("selling more than the corrected quantity was accepted")
	}
	// a correction to zero closes the position; one that would break a later sale is refused
	if _, err := st.TradeAdjust("600036.SH", "招商银行", 30, 9, "2026-11-02"); err == nil {
		t.Fatal("a correction below a later sale was accepted")
	}
	if _, err := st.TradeAdjust("600036.SH", "招商银行", 0, 0, "2026-11-05"); err != nil {
		t.Fatal(err)
	}
	if len(Held(st.Trades())) != 0 {
		t.Fatalf("still held: %v", Held(st.Trades()))
	}
}

func TestCapitalIsKeptAndWishesCanBeRewritten(t *testing.T) {
	st := Open("", func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) })
	if st.Capital() != 0 {
		t.Fatal("capital should start unset")
	}
	if err := st.SetCapital(-1); err == nil {
		t.Fatal("negative capital accepted")
	}
	_ = st.SetCapital(500000)
	if st.Capital() != 500000 {
		t.Fatalf("capital = %v", st.Capital())
	}
	w, err := st.WishAdd(Wish{Code: "000001.SZ", Name: "平安银行", Reason: "便宜", PlanPct: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WishUpdate(w.ID, Wish{Reason: " ", PlanPct: 5}); err == nil {
		t.Fatal("an empty reason was accepted")
	}
	got, err := st.WishUpdate(w.ID, Wish{Reason: "更便宜了", PlanPct: 12, Match: "any", Invalid: []Item{{"kind": "sector_out"}}})
	if err != nil || got.Reason != "更便宜了" || got.PlanPct != 12 || got.Match != "any" || len(got.Invalid) != 1 || got.Conditions == nil {
		t.Fatalf("wish = %+v %v", got, err)
	}
	if _, err := st.WishUpdate(w.ID+99, Wish{Reason: "x"}); err == nil {
		t.Fatal("an unknown wish was updated")
	}
}

func TestPlanLifecycle(t *testing.T) {
	st := Open("", func() time.Time { return time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) })
	if _, err := st.PlanAdd(PlanItem{Code: "600036.SH", Name: "招商银行", Side: "buy", Date: "2026-11-10"}); err == nil {
		t.Fatal("buy without reason accepted")
	}
	p, err := st.PlanAdd(PlanItem{Code: "600036.SH", Name: "招商银行", Side: "buy", Date: "2026-11-10", Reason: "跌到支撑", Price: 38, Qty: 500, PlanPct: 5})
	if err != nil || p.Status != "planned" {
		t.Fatalf("add = %+v %v", p, err)
	}
	if n := len(st.PlansOf("2026-11-10")); n != 1 {
		t.Fatalf("plans = %d", n)
	}
	got, err := st.PlanUpdate(p.ID, "2026-11-11", "add", 37.5, 800, 8, "加仓一点", "")
	if err != nil || got.Side != "add" || got.Price != 37.5 || got.Qty != 800 || got.Date != "2026-11-11" {
		t.Fatalf("update = %+v %v", got, err)
	}
	got, err = st.PlanSetStatus(p.ID, "done")
	if err != nil || got.Status != "done" {
		t.Fatalf("status = %+v %v", got, err)
	}
	if !st.PlanRemove(p.ID) || st.PlanRemove(p.ID) {
		t.Fatal("remove")
	}
}

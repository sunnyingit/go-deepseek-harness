package holdings

import (
	"strings"
	"testing"
	"time"

	"go-harness/internal/personal"
)

var today = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

func line(start string, closes ...float64) Series {
	day, _ := time.Parse("2006-01-02", start)
	var out Series
	for i, c := range closes {
		out = append(out, Bar{day.AddDate(0, 0, i).Format("2006-01-02"), c})
	}
	return out
}

func rep(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func fp(v float64) *float64 { return &v }

var aiBull = AI{Has: true, Lean: "bullish", Level: 2, Label: "偏多 · 比较明显", Position: "适中", Headline: "偏多：财报、基本面都在推", Watch: []string{"板块弱势可能结束"}, JudgedAt: "2026-11-02 09:58", TradeDate: "2026-11-02"}

type fake struct {
	quotes    map[string]personal.Quote
	ai        map[string]AI
	sector    map[string]SectorInfo
	closes    map[string]Series
	index     Series
	flows     map[string][2]int
	judgments map[string][]Judgment
	requested []string
	market    *Market
	since     string
	cal       []CalRow
	names     map[string][2]string
}

func newFake() *fake {
	flip := aiBull
	flip.Lean, flip.Level, flip.Label, flip.Position, flip.Change = "unclear", 0, "不明", "偏高", "从偏多变为不明"
	flip.Watch = []string{"大资金连续 2 天流入", "出现订单公告"}
	bear := aiBull
	bear.Lean, bear.Label = "bearish", "偏空 · 位置偏高"
	var idx []float64
	for i := 0; i < 200; i++ {
		idx = append(idx, 100+float64(i)*0.3+float64(i%5))
	}
	return &fake{
		quotes: map[string]personal.Quote{
			"002050.SZ": {Price: 26.8, Change: 0.16, ChangePct: 0.4}, "601689.SH": {Price: 58.2, Change: -1.4, ChangePct: -2.4},
			"603738.SH": {Price: 22.41, Change: 0.68, ChangePct: 3.1}, "600036.SH": {Price: 40.0, Change: 0.1, ChangePct: 0.25},
		},
		ai: map[string]AI{"002050.SZ": flip, "601689.SH": bear, "603738.SH": aiBull},
		sector: map[string]SectorInfo{
			"002050.SZ": {Name: "汽车零部件", Verdict: "只热不增", Flow: fp(-3.3), Heat: fp(1.6), Source: "估算"},
			"601689.SH": {Name: "汽车零部件", Verdict: "只热不增", Flow: fp(-3.3), Heat: fp(1.6), Source: "估算"},
			"603738.SH": {Name: "通信元件", Verdict: "三层同向", Flow: fp(2.0), Heat: fp(1.8), Source: "估算"},
			"600036.SH": {Name: "银行", Verdict: "三层同向", Flow: fp(8.2), Heat: fp(1.5), Tracked: true, Source: "ETF"},
		},
		closes: map[string]Series{}, index: line("2026-01-01", idx...), flows: map[string][2]int{}, judgments: map[string][]Judgment{},
		market: &Market{Level: "中性", Tilt: "偏弱", Action: "x", Text: "中性偏弱 · 不加仓", Stance: "不加仓"}, since: "2026-10-26",
		names: map[string][2]string{"600036": {"600036.SH", "招商银行"}, "002050": {"002050.SZ", "三花智控"}, "601689": {"601689.SH", "拓普集团"},
			"603738": {"603738.SH", "泰晶科技"}, "002430": {"002430.SZ", "杭氧股份"}},
	}
}

func (f *fake) Quotes(names map[string]string) map[string]personal.Quote {
	out := map[string]personal.Quote{}
	for c := range names {
		if q, ok := f.quotes[c]; ok {
			out[c] = q
		}
	}
	return out
}
func (f *fake) Closes(code string) Series        { return f.closes[code] }
func (f *fake) IndexCloses() Series              { return f.index }
func (f *fake) Flow(code string) (int, int)      { v := f.flows[code]; return v[0], v[1] }
func (f *fake) AI(code string) AI                { return f.ai[code] }
func (f *fake) Judgments(code string) []Judgment { return f.judgments[code] }
func (f *fake) RequestJudgment(code string)      { f.requested = append(f.requested, code) }
func (f *fake) Candidates() []Candidate {
	return []Candidate{{"600519.SH", "贵州茅台", "偏多 · 稍微", "2026-11-02"}, {"600036.SH", "招商银行", "偏多 · 比较明显", "2026-11-02"}}
}
func (f *fake) Market() *Market                     { return f.market }
func (f *fake) MarketSince() string                 { return f.since }
func (f *fake) Calendar(map[string]string) []CalRow { return f.cal }
func (f *fake) TrackedSectors() []SectorRow {
	return []SectorRow{{Name: "银行", Verdict: "三层同向", Flow: fp(8.2), Heat: fp(1.5), Tracked: true, Source: "ETF"}, {Name: "芯片", Verdict: "只热不增", Flow: fp(-9.6), Heat: fp(2.1), Tracked: true, Source: "ETF"}}
}
func (f *fake) Sector(code string) SectorInfo { return f.sector[code] }
func (f *fake) Resolve(q string) (string, string, bool) {
	key := strings.Split(q, ".")[0]
	if n, ok := f.names[key]; ok {
		return n[0], n[1], true
	}
	return "", "", false
}

type env struct {
	t     *testing.T
	store *personal.Store
	data  *fake
}

func newEnv(t *testing.T) *env {
	return &env{t, personal.Open("", func() time.Time { return today }), newFake()}
}

func (e *env) ctx() *Ctx { return NewCtx(e.store, e.data, today, nil) }

func (e *env) add(code, name, side string, price float64, qty int, day, reason string, invalid []personal.Item, context personal.Item) {
	e.t.Helper()
	if _, err := e.store.TradeAdd(personal.Trade{Code: code, Name: name, Side: side, Price: price, Qty: qty, Date: day, Reason: reason, Invalid: invalid, Context: context}); err != nil {
		e.t.Fatal(err)
	}
}

func aiCtx(lean string, level int, label string) personal.Item {
	return personal.Item{"ai": Map{"lean": lean, "level": level, "label": label}}
}

func (e *env) seed() {
	ctx := personal.Item{"market": Map{"level": "中性", "tilt": "偏弱", "stance": "不加仓"}, "ai": Map{"lean": "bullish", "level": 1, "label": "偏多 · 稍微"}}
	e.add("002050.SZ", "三花智控", "buy", 29.35, 1000, "2026-10-13", "机器人订单会在四季度落地", []personal.Item{{"kind": "sector_out"}},
		personal.Item{"ai": Map{"lean": "bullish", "level": 2, "label": "偏多 · 比较明显"}, "market": Map{"level": "中性", "tilt": "", "stance": ""}})
	e.add("002050.SZ", "三花智控", "buy", 26.8, 500, "2026-10-28", "", nil, ctx)
	e.add("601689.SH", "拓普集团", "buy", 52.0, 1000, "2026-09-18", "Optimus 供应链", []personal.Item{{"kind": "close_below", "value": 56.5}}, aiCtx("bullish", 2, "偏多 · 比较明显"))
	e.add("603738.SH", "泰晶科技", "buy", 19.8, 2000, "2026-10-20", "板块主升", []personal.Item{{"kind": "close_below", "value": 20.85}, {"kind": "flow_out", "days": 2}}, aiCtx("bullish", 2, "偏多 · 比较明显"))
	e.store.SetReason("002050.SZ", "机器人订单会在四季度落地", []personal.Item{{"kind": "sector_out"}}, "2026-10-13")
	e.store.SetReason("601689.SH", "Optimus 供应链", []personal.Item{{"kind": "close_below", "value": 56.5}}, "2026-09-18")
	e.store.SetReason("603738.SH", "板块主升", []personal.Item{{"kind": "close_below", "value": 20.85}, {"kind": "flow_out", "days": 2}}, "2026-10-20")
	e.store.AlertAdd("601689.SH", "down", 56.5, "close", AlertTag)
	e.store.AlertAdd("603738.SH", "down", 20.85, "close", AlertTag)
	e.data.flows["603738.SH"] = [2]int{0, 1}
}

func rows(m Map) []Map { return m["rows"].([]Map) }

func TestSummaryHeaderOrderAndReasonStatus(t *testing.T) {
	e := newEnv(t)
	e.seed()
	out := e.ctx().Summary()
	header := out["header"].(Map)
	wantMV := 1500*26.8 + 1000*58.2 + 2000*22.41
	if d := header["mv"].(float64) - wantMV; d > 1e-6 || d < -1e-6 {
		t.Fatalf("mv = %v want %v", header["mv"], wantMV)
	}
	if out["tabs"].(Map)["now"] != 3 {
		t.Fatalf("tabs = %v", out["tabs"])
	}
	r := rows(out)
	var names []string
	for _, row := range r {
		names = append(names, row["name"].(string))
	}
	if strings.Join(names, ",") != "拓普集团,三花智控,泰晶科技" {
		t.Fatalf("order = %v", names)
	}
	if tag := r[0]["tag"].(Map); tag["kind"] != "alert" || tag["text"] != "接近你设的提醒价" {
		t.Fatalf("tag = %v", r[0]["tag"])
	}
	if !strings.Contains(r[0]["flip"].(string), "距提醒价还差 2.9%") || r[1]["tag"].(Map)["kind"] != "change" {
		t.Fatalf("flip = %v / %v", r[0]["flip"], r[1]["tag"])
	}
	if f := r[1]["flip"].(string); !strings.Contains(f, "从偏多变为不明") || !strings.Contains(f, "还差 2 条") {
		t.Fatalf("flip = %v", f)
	}
	if r[1]["ai"].(Map)["label"] != "不明 · 位置偏高" {
		t.Fatalf("ai = %v", r[1]["ai"])
	}
	for _, row := range r {
		if row["reason"].(Map)["status"] != "doubt" {
			t.Fatalf("%v reason = %v", row["name"], row["reason"])
		}
	}
	if !strings.Contains(r[2]["reason"].(Map)["line"].(string), "已出现 1 天") {
		t.Fatalf("reason = %v", r[2]["reason"])
	}
	sum := 0.0
	for _, row := range r {
		sum += row["weight"].(float64)
	}
	if sum < 0.999999 || sum > 1.000001 {
		t.Fatalf("weights = %v", sum)
	}
}

func TestSummaryEmptyAndMissingQuote(t *testing.T) {
	e := newEnv(t)
	if out := e.ctx().Summary(); out["empty"] != true || len(rows(out)) != 0 || out["header"].(Map)["mv"] != 0.0 {
		t.Fatalf("empty = %v", out)
	}
	e.seed()
	delete(e.data.quotes, "601689.SH")
	for _, row := range rows(e.ctx().Summary()) {
		if row["name"] == "拓普集团" && (row["live"] != false || row["day_pnl"] != 0.0) {
			t.Fatalf("row = %v", row)
		}
	}
}

func TestWishCardConditionsAndImpact(t *testing.T) {
	e := newEnv(t)
	e.seed()
	_, err := e.store.WishAdd(personal.Wish{Code: "600036.SH", Name: "招商银行", Reason: "银行三层同向，高股息托底", PlanPct: 10, Source: "scan",
		Conditions: []personal.Item{{"kind": "price_below", "value": 37.0}, {"kind": "etf_inflow"}, {"kind": "flow_in_days", "days": 3}}})
	if err != nil {
		t.Fatal(err)
	}
	card := e.ctx().TabBuy()["items"].([]Map)[0]
	if card["name"] != "招商银行" || strings.Split(card["source"].(string), " · ")[0] != "来自扫描" {
		t.Fatalf("card = %v", card)
	}
	conds := map[string]Map{}
	for _, c := range card["conditions"].([]Map) {
		conds[c["kind"].(string)] = c
	}
	if conds["price_below"]["met"] != false || !strings.Contains(conds["price_below"]["detail"].(string), "还差 7.5%") || conds["etf_inflow"]["met"] != true || conds["flow_in_days"]["detail"] != "0 / 3 天" {
		t.Fatalf("conds = %v", conds)
	}
	if card["status"].(Map)["text"] != "等条件" {
		t.Fatalf("status = %v", card["status"])
	}
	first := card["impact"].(Map)["lines"].([]Map)[0]
	if first["buying"] != true || first["sector"] != "银行" || first["before"].(float64) != 0 {
		t.Fatalf("impact = %v", first)
	}
}

func TestCandidatesMarkTakenAndAddWishRules(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.store.WishAdd(personal.Wish{Code: "600036.SH", Name: "招商银行", Reason: "x"})
	e.store.WatchAdd("600845.SH", "宝信软件")
	out := e.ctx().Candidates()
	taken := map[string]bool{}
	for _, it := range out["scan"].([]Map) {
		taken[it["ts_code"].(string)] = it["taken"].(bool)
	}
	if taken["600519.SH"] || !taken["600036.SH"] || out["watch"].([]Map)[0]["name"] != "宝信软件" {
		t.Fatalf("candidates = %v", out)
	}
	e2 := newEnv(t)
	e2.seed()
	for _, body := range []Map{{"ts_code": "002050.SZ", "reason": "x"}, {"ts_code": "999999.SH", "reason": "x"}} {
		if _, err := e2.ctx().AddWish(body); err == nil {
			t.Fatalf("accepted %v", body)
		}
	}
	w, err := e2.ctx().AddWish(Map{"ts_code": "600036", "reason": "高股息", "plan_pct": 10.0, "conditions": []any{
		map[string]any{"kind": "price_below", "value": "38"}, map[string]any{"kind": "bad"}, map[string]any{"kind": "custom", "text": ""}}})
	if err != nil || len(w.Conditions) != 1 || w.Conditions[0]["value"] != 38.0 || e2.data.requested[0] != "600036.SH" {
		t.Fatalf("wish = %+v %v", w, err)
	}
	if _, err := e2.ctx().AddWish(Map{"ts_code": "600036.SH", "reason": "again"}); err == nil {
		t.Fatal("added the same wish twice")
	}
}

func TestCheckStructureReasonsStressAndFit(t *testing.T) {
	e := newEnv(t)
	e.seed()
	out := e.ctx().TabCheck(false)
	if out["empty"] != false {
		t.Fatal("empty")
	}
	top := out["structure"].(Map)["sectors"].([]Map)[0]
	if top["name"] != "汽车零部件" {
		t.Fatalf("top = %v", top)
	}
	found := false
	for _, f := range out["structure"].(Map)["flags"].([]Map) {
		found = found || strings.Contains(f["title"].(string), "同一个板块")
	}
	if !found {
		t.Fatal("no same-sector flag")
	}
	for _, r := range out["reasons"].([]Map) {
		if r["name"] == "拓普集团" && r["status"] != "doubt" {
			t.Fatalf("reason = %v", r)
		}
	}
	var keys []string
	for _, s := range out["stress"].([]Map) {
		keys = append(keys, s["key"].(string))
	}
	if strings.Join(keys, ",") != "market,sector,own" || !strings.Contains(out["stress"].([]Map)[2]["text"].(string), "拓普集团跌破 56.50") {
		t.Fatalf("stress = %v", out["stress"])
	}
	fit := out["fit"].(Map)
	var title, text string
	for _, l := range fit["lines"].([]Map) {
		if strings.Contains(l["title"].(string), "之后你买入") {
			title, text = l["title"].(string), l["text"].(string)
		}
	}
	if fit["known"] != true || title != "10-26 之后你买入了 1 次" || !strings.Contains(text, "10-28 加仓三花智控（浮亏时加仓）") || !strings.Contains(text, "越跌越买") {
		t.Fatalf("fit = %v", fit)
	}
	if out["attention"].(int) < 3 {
		t.Fatalf("attention = %v", out["attention"])
	}
}

func TestHotGroupsPairWithHoldingsAndCheckupsCompare(t *testing.T) {
	e := newEnv(t)
	e.seed()
	groups := e.ctx().TabCheck(false)["hot"].([]Map)
	names := map[string]Map{}
	hasReal := false
	for _, g := range groups {
		hasReal = hasReal || strings.HasPrefix(g["title"].(string), "有钱又热")
		for _, r := range g["rows"].([]Map) {
			names[r["name"].(string)] = r
		}
	}
	if !hasReal || names["通信元件"]["mine"].([]Map)[0]["name"] != "泰晶科技" || len(names["银行"]["mine"].([]Map)) != 0 {
		t.Fatalf("hot = %v", groups)
	}
	first := e.ctx().TabCheck(true)
	if first["compare"] != nil {
		t.Fatal("first checkup compared with nothing")
	}
	second := e.ctx().TabCheck(true)
	titles := map[string]bool{}
	for _, l := range second["compare"].(Map)["lines"].([]Map) {
		titles[l["title"].(string)] = true
	}
	if !titles["汽车零部件占比"] || len(e.store.Checkups(5)) != 2 {
		t.Fatalf("compare = %v", second["compare"])
	}
	if e2 := newEnv(t); e2.ctx().TabCheck(false)["empty"] != true || e2.ctx().Summary()["tabs"].(Map)["chk"] != 0 {
		t.Fatal("empty checkup wrong")
	}
}

func TestReviewRowsHabitsAndImportedAreNotReviewed(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.add("600036.SH", "招商银行", "buy", 35.0, 1000, "2026-09-22", "高股息", nil, aiCtx("bullish", 2, "偏多 · 比较明显"))
	e.add("600036.SH", "招商银行", "sell", 41.6, 1000, "2026-10-15", "", nil, nil)
	e.data.closes["600036.SH"] = line("2026-10-15", append([]float64{41.6}, rep(43.2, 30)...)...)
	out := e.ctx().TabReview()
	if out["empty"] != false || out["buys"] != 5 || out["adds"] != 1 || out["sells"] != 1 {
		t.Fatalf("review = %v", out)
	}
	sold := out["sold"].([]Map)[0]
	if sold["name"] != "招商银行" || sold["relation"] != "跟 AI 一致" || sold["after_done"] != true {
		t.Fatalf("sold = %v", sold)
	}
	if d := sold["pnl_pct"].(float64) - (41.6/35.0 - 1); d > 1e-9 || d < -1e-9 {
		t.Fatalf("pnl_pct = %v", sold["pnl_pct"])
	}
	habits := map[string]Map{}
	for _, h := range out["habits"].([]Map) {
		habits[h["key"].(string)] = h
	}
	if habits["dip"]["count"] != 1 || !strings.Contains(habits["dip"]["detail"].(string), "三花智控 10-28 加仓") {
		t.Fatalf("habits = %v", habits)
	}
	e2 := newEnv(t)
	e2.add("600036.SH", "招商银行", "buy", 35.0, 1000, "2026-10-30", "", nil, personal.Item{"imported": true})
	if e2.ctx().TabReview()["empty"] != true || e2.ctx().Summary()["tabs"].(Map)["rev"] != 0 {
		t.Fatal("imported trade was reviewed")
	}
}

func TestHeldDetailEntriesOutcomesRulesAndUnknownStock(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.data.judgments["002050.SZ"] = []Judgment{{"2026-10-13", "bullish", 2}, {"2026-10-20", "bullish", 2}, {"2026-11-02", "unclear", 0}}
	e.data.closes["002050.SZ"] = line("2026-10-13", rep(29.35, 25)...)
	out := e.ctx().StockDetail("002050.SZ")
	held := out["held"].(Map)
	if held["qty"] != 1500 || out["reason"].(Map)["status"] != "doubt" || out["needs_price"] != true {
		t.Fatalf("detail = %v", out)
	}
	if d := held["avg"].(float64) - (29.35*1000+26.8*500)/1500; d > 1e-9 || d < -1e-9 {
		t.Fatalf("avg = %v", held["avg"])
	}
	trades := out["trades"].([]Map)
	if trades[0]["label"] != "加仓" || trades[1]["label"] != "建仓" || trades[0]["conflict"] != true || !strings.Contains(trades[0]["market_text"].(string), "不加仓") {
		t.Fatalf("trades = %v", trades)
	}
	if items := out["flip_items"].([]string); len(items) != 2 || items[0] != "大资金连续 2 天流入" {
		t.Fatalf("flip_items = %v", items)
	}
	outcomes := map[string]Map{}
	for _, en := range out["entries"].([]Map) {
		outcomes[en["text"].(string)] = en["outcome"].(Map)
	}
	if outcomes["你买入时 · AI 偏多 · 比较明显"] == nil || outcomes["偏多 · 比较明显 → 不明"]["state"] != "skip" {
		t.Fatalf("entries = %v", out["entries"])
	}
	e.store.AddRule("603738.SH", "flow_out", 2)
	p := e.ctx().StockDetail("603738.SH")
	rules := p["rules"].([]Map)
	if rules[0]["text"] != "大资金连续 2 天流出" || rules[0]["detail"] != "你设的 · 已出现 1 天" || p["alerts"].([]Map)[0]["text"] != "收盘跌破 20.85" || p["needs_price"] != false {
		t.Fatalf("detail = %v", p)
	}
	if e.ctx().StockDetail("000001.SZ") != nil {
		t.Fatal("unknown stock has a detail")
	}
}

func TestOutcomeSettlesAfterFiveDays(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.data.closes["603738.SH"] = line("2026-10-20", 19.8, 20, 20.5, 21, 21.5, 22.0, 22.5)
	for _, en := range e.ctx().StockDetail("603738.SH")["entries"].([]Map) {
		if strings.HasPrefix(en["text"].(string), "你买入时") {
			o := en["outcome"].(Map)
			if o["state"] != "hit" || !strings.Contains(o["text"].(string), "T+5 方向对了 +11.1%") {
				t.Fatalf("outcome = %v", o)
			}
			return
		}
	}
	t.Fatal("no buy entry")
}

func TestBuyingSetsReasonAlertsRulesAndClosesTheWish(t *testing.T) {
	e := newEnv(t)
	e.store.WishAdd(personal.Wish{Code: "600036.SH", Name: "招商银行", Reason: "银行三层同向", PlanPct: 10})
	out, err := e.ctx().RecordTrade(Map{"ts_code": "600036.SH", "side": "buy", "price": 40.0, "qty": 1000.0, "date": "2026-11-02", "reason": "高股息托底",
		"invalid": []any{map[string]any{"kind": "close_below", "value": "36.5"}, map[string]any{"kind": "flow_out", "days": 3.0},
			map[string]any{"kind": "custom", "text": "三季报逻辑被证伪"}, map[string]any{"kind": "junk"}}})
	if err != nil || out["label"] != "建仓" {
		t.Fatalf("out = %v err = %v", out, err)
	}
	r, _ := e.store.ReasonOf("600036.SH")
	if r.Text != "高股息托底" || len(r.Invalid) != 3 {
		t.Fatalf("reason = %+v", r)
	}
	alerts := e.store.Alerts("600036.SH")
	if len(alerts) != 1 || alerts[0].Price != 36.5 || alerts[0].Name != AlertTag {
		t.Fatalf("alerts = %+v", alerts)
	}
	if rules := e.store.RulesOf("600036.SH"); len(rules) != 1 || rules[0].Kind != "flow_out" || rules[0].Value != 3 {
		t.Fatalf("rules = %+v", rules)
	}
	trades := e.store.Trades()
	if len(e.store.Wishes()) != 0 || trades[0].WishID == nil || e.data.requested[0] != "600036.SH" || trades[0].Context["market"].(Map)["stance"] != "不加仓" {
		t.Fatalf("trade = %+v wishes = %v", trades[0], e.store.Wishes())
	}
}

func TestAddingKeepsTheReasonSellingEverythingClearsItAndValidation(t *testing.T) {
	e := newEnv(t)
	e.seed()
	if _, err := e.ctx().RecordTrade(Map{"ts_code": "002050.SZ", "side": "buy", "price": 25.0, "qty": 100.0, "date": "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.store.ReasonOf("002050.SZ"); r.Text != "机器人订单会在四季度落地" {
		t.Fatalf("reason = %+v", r)
	}
	if _, err := e.ctx().RecordTrade(Map{"ts_code": "601689.SH", "side": "sell", "price": 58.2, "qty": 1000.0, "date": "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.store.ReasonOf("601689.SH"); ok || len(e.store.Alerts("601689.SH")) != 0 {
		t.Fatal("selling everything left the reason or reminders behind")
	}
	for _, tc := range []struct {
		body Map
		word string
	}{
		{Map{"ts_code": "999999.SH", "side": "buy", "price": 1.0, "qty": 1.0}, "没有找到"},
		{Map{"ts_code": "600036.SH", "side": "buy", "price": "x", "qty": 1.0}, "数字"},
		{Map{"ts_code": "600036.SH", "side": "sell", "price": 1.0, "qty": 100.0}, "卖出"},
	} {
		if _, err := e.ctx().RecordTrade(tc.body); err == nil || !strings.Contains(err.Error(), tc.word) {
			t.Fatalf("%v: err = %v", tc.body, err)
		}
	}
}

func TestSetReasonKeepsTheWrittenDateAndPriceLineReplacesTheOldOne(t *testing.T) {
	e := newEnv(t)
	e.seed()
	if _, err := e.ctx().SetReason("002050.SZ", "改成：T 链核心", []personal.Item{{"kind": "close_below", "value": 24.0}}); err != nil {
		t.Fatal(err)
	}
	r, _ := e.store.ReasonOf("002050.SZ")
	if r.Text != "改成：T 链核心" || r.WrittenAt != "2026-10-13" || len(e.store.Alerts("002050.SZ")) != 1 || e.store.Alerts("002050.SZ")[0].Price != 24 {
		t.Fatalf("reason = %+v alerts = %+v", r, e.store.Alerts("002050.SZ"))
	}
	if err := e.ctx().SetPriceLine("002050.SZ", 25.5); err != nil || e.store.Alerts("002050.SZ")[0].Price != 25.5 || len(e.store.Alerts("002050.SZ")) != 1 {
		t.Fatalf("price line: %v %+v", err, e.store.Alerts("002050.SZ"))
	}
	if err := e.ctx().SetPriceLine("600036.SH", 30); err == nil {
		t.Fatal("set a line on a stock that is not held")
	}
}

func TestPreviewFlagsBuyingAgainstTheMarket(t *testing.T) {
	e := newEnv(t)
	e.seed()
	out := e.ctx().TradePreview(Map{"ts_code": "600036.SH", "side": "buy", "price": 40.0, "qty": 500.0})
	want := 20000 / (1500*26.8 + 1000*58.2 + 2000*22.41) * 100
	if out["found"] != true || out["conflict"] != true || out["pct"].(float64)-want > 1e-9 || out["pct"].(float64)-want < -1e-9 || out["impact"].(Map)["lines"].([]Map)[0]["sector"] != "银行" {
		t.Fatalf("preview = %v", out)
	}
	if e.ctx().TradePreview(Map{"ts_code": "999999.SH"})["found"] != false {
		t.Fatal("unknown stock found")
	}
}

func TestImportParsing(t *testing.T) {
	rowsOut, skipped := ParseImport("证券代码,证券名称,持仓数量,成本价\n600036,招商银行,1000,35.20\n002050.SZ 三花智控 1,500 26.8\n说明文字\n600519,贵州茅台,abc,1500")
	if len(rowsOut) != 1 || rowsOut[0].Code != "600036" || rowsOut[0].Qty != 1000 || rowsOut[0].Cost != 35.2 || len(skipped) != 3 {
		t.Fatalf("rows = %+v skipped = %v", rowsOut, skipped)
	}
	r2, _ := ParseImport("股票代码\t成本价\t股份余额\n600036\t35.2\t1000")
	if len(r2) != 1 || r2[0].Qty != 1000 || r2[0].Cost != 35.2 {
		t.Fatalf("rows = %+v", r2)
	}
	e := newEnv(t)
	e.seed()
	out := e.ctx().ImportPositions("600036 1000 35.2\n002050 500 30")
	added, skip := out["added"].([]Map), out["skipped"].([]Map)
	if len(added) != 1 || added[0]["name"] != "招商银行" || skip[0]["why"] != "已经有持仓，没有重复导入" {
		t.Fatalf("import = %v", out)
	}
	if tr := e.store.TradesOf("600036.SH"); len(tr) != 1 || tr[0].Context["imported"] != true {
		t.Fatalf("trades = %+v", tr)
	}
}

func TestNotificationsItemsAndReadMarks(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.data.quotes["601689.SH"] = personal.Quote{Price: 56.42, Change: -1.4, ChangePct: -2.4}
	e.data.cal = []CalRow{{"2026-11-03", "杭氧股份 三季报披露", "涉及自选"}}
	data := e.ctx().Notifications()
	kinds := map[string]bool{}
	var alertText string
	for _, it := range data["items"].([]Map) {
		kinds[it["kind"].(string)] = true
		if it["kind"] == "alert" {
			alertText = it["text"].(string)
		}
	}
	if !kinds["alert"] || !kinds["change"] || !kinds["upcoming"] || alertText != "拓普集团 跌到 56.50" || data["unread"] != len(data["items"].([]Map)) {
		t.Fatalf("data = %v", data)
	}
	e.ctx().MarkRead()
	if e.ctx().Notifications()["unread"] != 0 {
		t.Fatal("still unread after marking read")
	}
}

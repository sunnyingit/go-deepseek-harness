package holdings

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/personal"
)

var (
	leanWord   = map[string]string{"bullish": "偏多", "bearish": "偏空", "unclear": "不明"}
	levelWord  = map[int]string{0: "", 1: "稍微", 2: "比较明显", 3: "明显"}
	toneOf     = map[string]string{"三层同向": "good", "有钱没热度": "good", "只热不增": "warn", "在流出": "bad"}
	reasonWord = map[string]string{"ok": "还成立", "doubt": "存疑", "broken": "不成立", "none": "没写理由"}
	sourceWord = map[string]string{"scan": "来自扫描", "sold": "来自卖出后", "diagnose": "来自你诊断", "manual": "自己加的"}
)

// AlertTag names the price alerts that were written as "what would prove me wrong", so they can be told
// apart from alerts the user set by hand.
const AlertTag = "理由不成立"

var conditionKinds = map[string]bool{"price_below": true, "etf_inflow": true, "flow_in_days": true, "ai_bullish": true, "market_ok": true, "custom": true}
var invalidKinds = map[string]bool{"close_below": true, "sector_out": true, "flow_out": true, "custom": true}

// AI is what the latest conclusion says about a stock.
type AI struct {
	Has       bool
	Lean      string
	Level     int
	Label     string
	Position  string
	Headline  string
	Watch     []string
	Change    string
	JudgedAt  string
	Stale     bool
	TradeDate string
}

// SectorInfo is the sector a stock sits in and its money/heat verdict.
type SectorInfo struct {
	Name    string
	Verdict string
	Flow    *float64
	Heat    *float64
	Source  string
	Tracked bool
}

// Judgment is one day's verdict in a stock's history.
type Judgment struct {
	TradeDate string
	Lean      string
	Level     int
}

// Candidate is a stock the AI leaned bullish on.
type Candidate struct {
	Code, Name, Label, TradeDate string
}

// CalRow is something coming up for a followed stock.
type CalRow struct {
	Date, Text, Tag string
}

// Data is everything the page needs from outside: prices, the AI's conclusions, the shared market data.
// Each method swallows its own failure and returns an empty value, so one stock never breaks the page.
type Data interface {
	Quotes(names map[string]string) map[string]personal.Quote
	Closes(code string) Series
	IndexCloses() Series
	Flow(code string) (in, out int)
	AI(code string) AI
	Judgments(code string) []Judgment
	RequestJudgment(code string)
	Candidates() []Candidate
	Market() *Market
	MarketSince() string
	TrackedSectors() []SectorRow
	Sector(code string) SectorInfo
	Calendar(names map[string]string) []CalRow
	Resolve(query string) (code, name string, ok bool)
}

// Ctx is the state of one request: the trades, the positions worked out from them, prices and the AI
// and sector of each stock, each fetched once.
type Ctx struct {
	Store *personal.Store
	Data  Data
	Today time.Time
	Inbox *inbox.Inbox
	memo  map[string]any
}

func NewCtx(store *personal.Store, data Data, today time.Time, in *inbox.Inbox) *Ctx {
	return &Ctx{Store: store, Data: data, Today: today, Inbox: in, memo: map[string]any{}}
}

func (c *Ctx) reset() { c.memo = map[string]any{} }

func memo[T any](c *Ctx, key string, make func() T) T {
	if v, ok := c.memo[key]; ok {
		return v.(T)
	}
	v := make()
	c.memo[key] = v
	return v
}

func (c *Ctx) todayISO() string { return c.Today.Format("2006-01-02") }

func (c *Ctx) Trades() []personal.Trade { return memo(c, "trades", c.Store.Trades) }

type ledger struct {
	held  map[string]*personal.Position
	notes map[int]personal.TradeNote
}

func (c *Ctx) ledger() ledger {
	return memo(c, "ledger", func() ledger {
		h, n := personal.Positions(c.Trades())
		return ledger{h, n}
	})
}

func (c *Ctx) Held() map[string]*personal.Position { return c.ledger().held }
func (c *Ctx) Notes() map[int]personal.TradeNote   { return c.ledger().notes }
func (c *Ctx) Wishes() []personal.Wish             { return memo(c, "wishes", c.Store.Wishes) }

func (c *Ctx) Names() map[string]string {
	return memo(c, "names", func() map[string]string {
		out := map[string]string{}
		for _, w := range c.Wishes() {
			out[w.Code] = w.Name
		}
		for code, p := range c.Held() {
			out[code] = p.Name
		}
		return out
	})
}

func (c *Ctx) sortedNames() []string {
	var codes []string
	for code := range c.Names() {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func (c *Ctx) Quotes() map[string]personal.Quote {
	return memo(c, "quotes", func() map[string]personal.Quote {
		if len(c.Names()) == 0 {
			return map[string]personal.Quote{}
		}
		return c.Data.Quotes(c.Names())
	})
}

func (c *Ctx) Rows() []personal.Row {
	return memo(c, "rows", func() []personal.Row { return personal.Valuation(c.Held(), c.Quotes(), c.Today) })
}

func (c *Ctx) Total() personal.Totals {
	return memo(c, "total", func() personal.Totals { return personal.SumTotals(c.Rows()) })
}

func (c *Ctx) Market() *Market { return memo(c, "market", c.Data.Market) }

func (c *Ctx) Row(code string) *personal.Row {
	for _, r := range c.Rows() {
		if r.Code == code {
			r := r
			return &r
		}
	}
	return nil
}

func (c *Ctx) Price(code string) float64 {
	if q, ok := c.Quotes()[code]; ok && q.Price > 0 {
		return q.Price
	}
	if r := c.Row(code); r != nil {
		return r.Price
	}
	return 0
}

func (c *Ctx) AI(code string) AI { return memo(c, "ai:"+code, func() AI { return c.Data.AI(code) }) }
func (c *Ctx) Sector(code string) SectorInfo {
	return memo(c, "sector:"+code, func() SectorInfo { return c.Data.Sector(code) })
}
func (c *Ctx) Closes(code string) Series {
	return memo(c, "closes:"+code, func() Series { return c.Data.Closes(code) })
}
func (c *Ctx) Alerts(code string) []personal.Alert {
	return c.Store.Alerts(code)
}

type flowRun struct{ in, out int }

func (c *Ctx) Flow(code string) flowRun {
	return memo(c, "flow:"+code, func() flowRun { i, o := c.Data.Flow(code); return flowRun{i, o} })
}

// ---- small helpers

func judgeLabel(lean string, level int) string {
	word := leanWord[lean]
	if word == "" {
		word = "不明"
	}
	if s := levelWord[level]; s != "" && lean != "unclear" {
		return word + " · " + s
	}
	return word
}

func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func marketBlock(m *Market) any {
	if m == nil {
		return nil
	}
	return Map{"level": m.Level, "tilt": m.Tilt, "text": m.Text, "stance": m.Stance, "action": m.Action}
}

func sectorBlock(s SectorInfo) Map {
	return Map{"name": s.Name, "verdict": nz(s.Verdict), "tone": orDefault(toneOf[s.Verdict], "flat"), "flow": fptr(s.Flow), "heat": fptr(s.Heat), "source": s.Source, "tracked": s.Tracked}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func aiBlock(a AI) any {
	if !a.Has {
		return nil
	}
	label := orDefault(a.Label, "不明")
	if (a.Position == "偏高" || a.Position == "偏低") && a.Lean != "bullish" {
		label += " · 位置" + a.Position
	}
	return Map{"lean": a.Lean, "level": a.Level, "label": label, "change": a.Change, "stale": a.Stale, "judged_at": nz(a.JudgedAt)}
}

func flipText(a AI) string {
	if !a.Has || len(a.Watch) == 0 {
		return ""
	}
	word := "转偏多"
	if a.Lean == "bullish" {
		word = "改判"
	}
	return fmt.Sprintf("%s还差 %d 条", word, len(a.Watch))
}

func alertView(a personal.Alert, price float64) Map {
	var gap any
	reached := false
	g := 0.0
	if price > 0 {
		g = (price - a.Price) / price
		gap = g
		reached = price <= a.Price
	}
	text := fmt.Sprintf("涨到 %.2f", a.Price)
	if a.Side == "down" {
		text = fmt.Sprintf("收盘跌破 %.2f", a.Price)
	}
	detail := ""
	if gap != nil {
		if reached {
			detail = "已到价"
		} else {
			detail = fmt.Sprintf("还差 %.1f%%", g*100)
		}
	}
	return Map{"id": a.ID, "price": a.Price, "side": a.Side, "gap": gap, "reached": reached, "tag": a.Name, "text": text, "detail": detail}
}

func (c *Ctx) nearAlert(code string) Map {
	price := c.Price(code)
	var best Map
	for _, a := range c.Alerts(code) {
		if a.Side != "down" {
			continue
		}
		v := alertView(a, price)
		gap, hasGap := v["gap"].(float64)
		if v["reached"] == true || (hasGap && gap <= NearGap) {
			if best == nil || less(v, best) {
				best = v
			}
		}
	}
	return best
}

func less(a, b Map) bool {
	ar, br := a["reached"] != true, b["reached"] != true
	if ar != br {
		return !ar
	}
	ag, _ := a["gap"].(float64)
	bg, _ := b["gap"].(float64)
	return ag < bg
}

// ---- the reason for holding

func (c *Ctx) Reason(code string) Map {
	return memo(c, "reason:"+code, func() Map { return c.reason(code) })
}

func (c *Ctx) reason(code string) Map {
	pos := c.Held()[code]
	text, written := "", ""
	var invalid []personal.Item
	var first *personal.Trade
	if pos != nil && len(pos.Buys) > 0 {
		first = &pos.Buys[0]
	}
	if stored, ok := c.Store.ReasonOf(code); ok {
		text, invalid, written = stored.Text, stored.Invalid, stored.WrittenAt
	} else if first != nil && (first.Reason != "" || len(first.Invalid) > 0) {
		text, invalid, written = first.Reason, first.Invalid, first.Date
	}
	if text == "" && len(invalid) == 0 {
		return Map{"text": "", "invalid": []personal.Item{}, "written_at": "", "status": "none", "status_text": reasonWord["none"], "line": "还没写下为什么买"}
	}
	ai, sector := c.AI(code), c.Sector(code)
	flowOut := 0
	for _, it := range invalid {
		if str(it["kind"]) == "flow_out" {
			flowOut = c.Flow(code).out
		}
	}
	buyLean := ""
	if first != nil {
		buyLean = ctxLean(*first)
	}
	status, line := ReasonCheck(invalid, ReasonFacts{Price: c.Price(code), AILean: ai.Lean, AILabel: ai.Label, HasAI: ai.Has, BuyLean: buyLean, Verdict: sector.Verdict, FlowOut: flowOut})
	if invalid == nil {
		invalid = []personal.Item{}
	}
	return Map{"text": text, "invalid": invalid, "written_at": written, "status": status, "status_text": reasonWord[status], "line": line}
}

// triggers is, per holding, the nearest "close below" price under the current price.
func (c *Ctx) triggers() map[string]float64 {
	out := map[string]float64{}
	for _, r := range c.Rows() {
		best := 0.0
		for _, a := range c.Alerts(r.Code) {
			if a.Side == "down" && a.Price < r.Price && a.Price > best {
				best = a.Price
			}
		}
		if best > 0 {
			out[r.Code] = best
		}
	}
	return out
}

// ---- 持有

func (c *Ctx) holdRow(r personal.Row) (Map, [2]float64) {
	ai, sector, reason := c.AI(r.Code), c.Sector(r.Code), c.Reason(r.Code)
	near := c.nearAlert(r.Code)
	var tag any
	switch {
	case near != nil && near["reached"] == true:
		tag = Map{"kind": "alert", "text": "已到你设的提醒价"}
	case near != nil:
		tag = Map{"kind": "alert", "text": "接近你设的提醒价"}
	case ai.Change != "":
		tag = Map{"kind": "change", "text": "AI 今天改判"}
	}
	var flip string
	switch {
	case near != nil && near["reached"] != true:
		flip = fmt.Sprintf("距提醒价还差 %.1f%%", near["gap"].(float64)*100)
	case near != nil:
		flip = "已到提醒价"
	default:
		flip = flipText(ai)
	}
	if ai.Change != "" && near == nil {
		if flip != "" {
			flip = ai.Change + "；" + flip
		} else {
			flip = ai.Change
		}
	}
	order := 4.0
	switch {
	case near != nil:
		order = 0
	case ai.Change != "":
		order = 1
	case reason["status"] == "broken":
		order = 2
	case reason["status"] == "doubt":
		order = 3
	}
	var daysV any
	if r.Days != nil {
		daysV = *r.Days
	}
	row := Map{
		"ts_code": r.Code, "name": r.Name, "qty": r.Qty, "avg": r.Avg, "price": r.Price, "live": r.Live, "change_pct": fptr(r.ChangePct),
		"day_pnl": r.DayPnL, "pnl": r.PnL, "pnl_pct": r.PnLPct, "weight": r.Weight, "since": r.Since, "days": daysV,
		"sector": sectorBlock(sector), "ai": aiBlock(ai), "flip": flip, "tag": tag,
		"reason": Map{"status": reason["status"], "text": reason["status_text"], "line": reason["line"]},
	}
	return row, [2]float64{order, -r.Weight}
}

func (c *Ctx) reviewTrades(days int) []personal.Trade {
	since := c.Today.AddDate(0, 0, -days).Format("2006-01-02")
	var out []personal.Trade
	for _, t := range c.Trades() {
		if t.Date >= since && !imported(t) {
			out = append(out, t)
		}
	}
	return out
}

func imported(t personal.Trade) bool { v, _ := t.Context["imported"].(bool); return v }

func (c *Ctx) Summary() Map {
	type scored struct {
		row Map
		key [2]float64
	}
	var rows []scored
	for _, r := range c.Rows() {
		row, key := c.holdRow(r)
		rows = append(rows, scored{row, key})
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].key[0] != rows[b].key[0] {
			return rows[a].key[0] < rows[b].key[0]
		}
		return rows[a].key[1] < rows[b].key[1]
	})
	out := make([]Map, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.row)
	}
	t := c.Total()
	return Map{
		"empty":  len(c.Rows()) == 0 && len(c.Wishes()) == 0 && len(c.Trades()) == 0,
		"header": Map{"mv": t.MV, "day_pnl": t.DayPnL, "day_pct": t.DayPct, "pnl": t.PnL, "pnl_pct": t.PnLPct, "count": len(out)},
		"tabs":   Map{"buy": len(c.Wishes()), "now": len(out), "chk": c.attention(), "rev": len(c.reviewTrades(ReviewDays)), "rev_days": ReviewDays},
		"market": marketBlock(c.Market()),
		"rows":   out,
	}
}

// ---- 想买

func (c *Ctx) wishFacts(w personal.Wish) WishFacts {
	ai, sector := c.AI(w.Code), c.Sector(w.Code)
	f := WishFacts{Price: c.Price(w.Code), AILean: ai.Lean, Sector: sector.Name, SectorFlow: sector.Flow, Market: c.Market()}
	if ai.Has {
		f.AILabel = ai.Label
	}
	for _, cond := range w.Conditions {
		if str(cond["kind"]) == "flow_in_days" {
			f.FlowIn = c.Flow(w.Code).in
		}
	}
	return f
}

func (c *Ctx) sectorOf() map[string]string {
	out := map[string]string{}
	for _, r := range c.Rows() {
		out[r.Code] = orDefault(c.Sector(r.Code).Name, "其他")
	}
	return out
}

func (c *Ctx) impactView(code string, planPct float64) Map {
	newSector := orDefault(c.Sector(code).Name, "其他")
	impact := PlanImpact(c.Rows(), c.sectorOf(), newSector, planPct)
	lines, _ := impact["lines"].([]Map)
	var out []Map
	for _, l := range lines {
		out = append(out, Map{"sector": l["sector"], "before": l["before"], "after": l["after"], "buying": l["buying"]})
	}
	var target Map
	var others []Map
	for _, l := range lines {
		if l["buying"] == true && target == nil {
			target = l
		} else if l["buying"] != true {
			others = append(others, l)
		}
	}
	var notes []string
	if len(others) > 0 && target != nil && target["before"].(float64) < 0.15 {
		all := true
		for _, o := range others {
			if !(o["after"].(float64) < o["before"].(float64)) {
				all = false
			}
		}
		if all {
			over := false
			for _, l := range lines {
				if l["after"].(float64) > SectorLimit {
					over = true
				}
			}
			if !over {
				notes = append(notes, "组合更均衡")
			}
		}
	}
	if target != nil && target["after"].(float64) > SectorLimit {
		notes = append(notes, fmt.Sprintf("买入后%s会到 %.1f%%，超过 %.0f%% 的参考线", target["sector"], target["after"].(float64)*100, SectorLimit*100))
	}
	if a, b, v, ok := c.sameDirection(code); ok {
		notes = append(notes, fmt.Sprintf("⚠ %s和你手里的%s走势高度同向（相关 %.2f）：买入后这类仓位会更集中", a, b, v))
	}
	if out == nil {
		out = []Map{}
	}
	if notes == nil {
		notes = []string{}
	}
	return Map{"lines": out, "amount": impact["amount"], "total": impact["total"], "notes": notes}
}

func (c *Ctx) sameDirection(code string) (string, string, float64, bool) {
	if len(c.Rows()) == 0 {
		return "", "", 0, false
	}
	mine := c.Closes(code)
	var bestName string
	bv, found := 0.0, false
	for _, r := range c.Rows() {
		if v := Corr(mine, c.Closes(r.Code), 60); v != nil && *v >= HighCorr && (!found || *v > bv) {
			bestName, bv, found = r.Name, *v, true
		}
	}
	name := c.Names()[code]
	if name == "" {
		name = code
	}
	return name, bestName, bv, found
}

func (c *Ctx) wishCard(w personal.Wish) Map {
	f := c.wishFacts(w)
	state := WishState(w, f)
	created := time.UnixMilli(w.CreatedAt).In(c.Today.Location()).Format("01-02")
	var impact any
	if w.PlanPct > 0 {
		impact = c.impactView(w.Code, w.PlanPct)
	}
	var price any
	if f.Price > 0 {
		price = f.Price
	}
	return Map{
		"id": w.ID, "ts_code": w.Code, "name": w.Name, "price": price, "sector": sectorBlock(c.Sector(w.Code)),
		"source": fmt.Sprintf("%s · %s 加入", orDefault(sourceWord[w.Source], "自己加的"), created), "status": Map{"text": state["status"], "tone": state["tone"]},
		"reason": w.Reason, "match": w.Match, "conditions": state["conditions"], "met": state["met"], "auto": state["auto"],
		"ai": aiBlock(c.AI(w.Code)), "plan_pct": w.PlanPct, "impact": impact, "invalid": w.Invalid,
	}
}

func (c *Ctx) TabBuy() Map {
	items := []Map{}
	for _, w := range c.Wishes() {
		items = append(items, c.wishCard(w))
	}
	return Map{"market": marketBlock(c.Market()), "items": items}
}

func (c *Ctx) Candidates() Map {
	taken := map[string]bool{}
	for code := range c.Held() {
		taken[code] = true
	}
	for _, w := range c.Wishes() {
		taken[w.Code] = true
	}
	scan := []Map{}
	for i, cand := range c.Data.Candidates() {
		if i >= 12 {
			break
		}
		why := "已在想买"
		if _, held := c.Held()[cand.Code]; held {
			why = "已持有"
		}
		scan = append(scan, Map{"ts_code": cand.Code, "name": cand.Name, "label": cand.Label, "trade_date": cand.TradeDate, "sector": sectorBlock(c.Sector(cand.Code)), "taken": taken[cand.Code], "taken_text": why})
	}
	mine := []Map{}
	for _, w := range c.Store.WatchList() {
		if taken[w.Code] {
			continue
		}
		if len(mine) >= 12 {
			break
		}
		mine = append(mine, Map{"ts_code": w.Code, "name": w.Name, "sector": sectorBlock(c.Sector(w.Code)), "ai": aiBlock(c.AI(w.Code))})
	}
	hot := []string{}
	if groups := c.hot(); len(groups) > 0 {
		for _, r := range groups[0].Rows {
			hot = append(hot, r.Name)
		}
	}
	return Map{"scan": scan, "watch": mine, "hot": hot}
}

func (c *Ctx) WishPreview(query string, planPct float64) Map {
	code, name, ok := c.Data.Resolve(query)
	if !ok {
		code, name = query, query
	}
	var price float64
	if q, ok := c.Data.Quotes(map[string]string{code: name})[code]; ok {
		price = q.Price
	}
	ai, sector := c.AI(code), c.Sector(code)
	closes := c.Closes(code)
	var ma20, gap any
	if len(closes) >= 20 {
		sum := 0.0
		for _, b := range closes[len(closes)-20:] {
			sum += b.Close
		}
		m := sum / 20
		ma20 = m
		if price > 0 {
			gap = price/m - 1
		}
	}
	var impact any
	if planPct > 0 {
		impact = c.impactView(code, planPct)
	}
	wished := false
	for _, w := range c.Wishes() {
		if w.Code == code {
			wished = true
		}
	}
	_, held := c.Held()[code]
	total := c.Total().MV
	amount := 0.0
	if planPct > 0 {
		amount = total * planPct / 100
	}
	var priceV any
	if price > 0 {
		priceV = price
	}
	return Map{
		"ts_code": code, "name": name, "price": priceV, "sector": sectorBlock(sector), "ai": aiBlock(ai), "market": marketBlock(c.Market()),
		"ma20": ma20, "ma20_gap": gap, "sector_flow": fptr(sector.Flow), "impact": impact, "total": total, "amount": amount,
		"evidence": evidence(ai, sector), "held": held, "wished": wished,
	}
}

func evidence(ai AI, sector SectorInfo) []string {
	out := []string{}
	if ai.Has && ai.Headline != "" {
		out = append(out, strings.TrimRight(ai.Headline, "。"))
	}
	if sector.Name != "" && sector.Verdict != "" {
		line := sector.Name + sector.Verdict
		if sector.Flow != nil {
			line += fmt.Sprintf("（近期资金 %+.1f 亿）", *sector.Flow)
		}
		out = append(out, line)
	}
	return out
}

// ---- 体检

func (c *Ctx) hot() []HotGroup {
	return memo(c, "hot", func() []HotGroup {
		sectors := append([]SectorRow(nil), c.Data.TrackedSectors()...)
		known := map[string]bool{}
		for _, s := range sectors {
			known[s.Name] = true
		}
		for _, code := range c.sortedNames() {
			s := c.Sector(code)
			if s.Name != "" && !known[s.Name] && s.Verdict != "" {
				known[s.Name] = true
				sectors = append(sectors, SectorRow{Name: s.Name, Verdict: s.Verdict, Flow: s.Flow, Heat: s.Heat, Source: s.Source})
			}
		}
		held := map[string][]Mine{}
		for _, r := range c.Rows() {
			n := orDefault(c.Sector(r.Code).Name, "其他")
			held[n] = append(held[n], Mine{r.Name, r.Weight})
		}
		return HotGroups(sectors, held)
	})
}

func (c *Ctx) reasons() (map[string]Map, []string) {
	out := map[string]Map{}
	var order []string
	for _, r := range c.Rows() {
		rs := c.Reason(r.Code)
		out[r.Code] = Map{"name": r.Name, "text": rs["text"], "status": rs["status"], "status_text": rs["status_text"], "line": rs["line"]}
		order = append(order, r.Code)
	}
	return out, order
}

func (c *Ctx) structure(withCorr bool) (Conc, map[string]string) {
	sectorOf := c.sectorOf()
	verdicts := map[string]string{}
	for _, r := range c.Rows() {
		verdicts[sectorOf[r.Code]] = c.Sector(r.Code).Verdict
	}
	corr := map[[2]string]float64{}
	rows := c.Rows()
	if withCorr && len(rows) > 1 {
		for i, a := range rows {
			for _, b := range rows[i+1:] {
				if sectorOf[a.Code] == sectorOf[b.Code] {
					if v := Corr(c.Closes(a.Code), c.Closes(b.Code), 60); v != nil {
						corr[[2]string{a.Code, b.Code}] = *v
					}
				}
			}
		}
	}
	return Concentration(rows, sectorOf, verdicts, corr), sectorOf
}

func (c *Ctx) fit() Map {
	m := c.Market()
	if m == nil {
		return Map{"known": false, "lines": []Map{{"level": "提示", "title": "没有取到大盘结论", "text": "市场页的结论暂时没取到，这一项先不判断"}}}
	}
	since := c.Data.MarketSince()
	if since == "" {
		since = c.Today.AddDate(0, 0, -14).Format("2006-01-02")
	}
	var lines []Map
	var buys []personal.Trade
	for _, t := range c.Trades() {
		if t.Side == "buy" && t.Date >= since && !imported(t) {
			buys = append(buys, t)
		}
	}
	if NoAdd(m) {
		if len(buys) > 0 {
			var bits []string
			dip := false
			for _, t := range buys {
				note := c.Notes()[t.ID]
				loss := note.QtyBefore > 0 && t.Price < note.AvgBefore
				dip = dip || loss
				bit := fmt.Sprintf("%s %s%s", dayText(t.Date), note.Label, t.Name)
				if loss {
					bit += "（浮亏时加仓）"
				}
				bits = append(bits, bit)
			}
			text := strings.Join(bits, "、") + "。这和大盘结论不一致"
			if dip {
				text += "，也是你操作习惯里「越跌越买」那一条"
			}
			lines = append(lines, Map{"level": "注意", "title": fmt.Sprintf("%s 之后你买入了 %d 次", dayText(since), len(buys)), "text": text})
		} else {
			lines = append(lines, Map{"level": "正常", "title": "没有逆着大盘买", "text": fmt.Sprintf("大盘说「%s」，%s 之后你没有新买入", m.Stance, dayText(since))})
		}
		for _, r := range c.Rows() {
			if c.Sector(r.Code).Verdict == "三层同向" {
				lines = append(lines, Map{"level": "正常", "title": r.Name + "在有新钱的板块", "text": "和大盘偏防守的方向不冲突"})
			}
		}
	} else {
		lines = append(lines, Map{"level": "正常", "title": "和大盘没有冲突", "text": fmt.Sprintf("大盘现在%s，%s 起没变过，你的操作没有逆着它", m.Text, dayText(since))})
	}
	return Map{"known": true, "since": since, "market": m.Text, "lines": lines}
}

func (c *Ctx) attention() int {
	if len(c.Rows()) == 0 {
		return 0
	}
	conc, _ := c.structure(false)
	n := 0
	for _, f := range conc.Flags {
		if f["level"] == "注意" {
			n++
		}
	}
	reasons, _ := c.reasons()
	for _, r := range reasons {
		if s := str(r["status"]); s == "doubt" || s == "broken" {
			n++
			break
		}
	}
	for _, l := range c.fit()["lines"].([]Map) {
		if l["level"] == "注意" {
			n++
			break
		}
	}
	return n
}

func structureView(conc Conc) Map {
	sectors := []Map{}
	for _, s := range conc.Sectors {
		sectors = append(sectors, Map{"name": s.Name, "weight": s.Weight, "verdict": nz(s.Verdict), "tone": orDefault(toneOf[s.Verdict], "flat"), "stocks": s.Stocks})
	}
	flags := conc.Flags
	if flags == nil {
		flags = []Map{}
	}
	return Map{"sectors": sectors, "flags": flags}
}

func hotView(groups []HotGroup) []Map {
	out := []Map{}
	for _, g := range groups {
		rows := []Map{}
		for _, r := range g.Rows {
			mine := []Map{}
			for _, m := range r.Mine {
				mine = append(mine, Map{"name": m.Name, "weight": m.Weight})
			}
			rows = append(rows, Map{"name": r.Name, "flow": fptr(r.Flow), "heat": fptr(r.Heat), "verdict": r.Verdict, "tone": orDefault(toneOf[r.Verdict], "flat"), "source": r.Source, "tracked": r.Tracked, "mine": mine})
		}
		out = append(out, Map{"title": g.Title, "rows": rows})
	}
	return out
}

func (c *Ctx) TabCheck(save bool) Map {
	now := c.Today
	if len(c.Rows()) == 0 {
		return Map{"empty": true, "text": "还没有持仓，体检没有可看的。先「记一笔」，或导入券商持仓。"}
	}
	conc, sectorOf := c.structure(true)
	reasons, order := c.reasons()
	index := c.Data.IndexCloses()
	betas := map[string]*float64{}
	for _, r := range c.Rows() {
		betas[r.Code] = Beta(c.Closes(r.Code), index, 250)
	}
	triggers := c.triggers()
	stress := Stress(c.Rows(), sectorOf, betas, triggers)
	fit := c.fit()
	hot := c.hot()
	alerts := map[string]int{}
	for _, r := range c.Rows() {
		for _, a := range c.Alerts(r.Code) {
			if a.Side == "down" {
				alerts[r.Code]++
			}
		}
	}
	heldSectors := map[string]bool{}
	for _, n := range sectorOf {
		heldSectors[n] = true
	}
	suggestions := Suggestions(c.Rows(), conc, reasons, order, triggers, alerts, hot, heldSectors)
	var topV any
	if len(conc.Sectors) > 0 {
		topV = Map{"name": conc.Sectors[0].Name, "weight": conc.Sectors[0].Weight}
	}
	var weak []string
	for _, code := range order {
		if s := str(reasons[code]["status"]); s == "doubt" || s == "broken" {
			weak = append(weak, str(reasons[code]["name"]))
		}
	}
	var issues []string
	for _, f := range conc.Flags {
		if f["level"] == "注意" && strings.Contains(str(f["title"]), "同一个板块") {
			issues = append(issues, "板块集中")
			break
		}
	}
	for _, f := range conc.Flags {
		if f["title"] == "单只偏重" {
			issues = append(issues, "单只偏重")
			break
		}
	}
	for _, r := range c.Rows() {
		if alerts[r.Code] == 0 && triggers[r.Code] == 0 {
			issues = append(issues, "未设价格条件")
			break
		}
	}
	if weak == nil {
		weak = []string{}
	}
	if issues == nil {
		issues = []string{}
	}
	snapshot := Map{"top_sector": topV, "weak": len(weak), "weak_names": weak, "issues": issues}
	prev := c.Store.Checkups(1)
	var compare any
	compareDay := ""
	if len(prev) > 0 {
		if cmp := CompareCheckups(snapshot, prev[0].Data); cmp != nil {
			compare = cmp
		}
		compareDay = time.UnixMilli(prev[0].CreatedAt).In(now.Location()).Format("01-02")
	}
	stamp := now
	if save {
		saved := c.Store.SaveCheckup(snapshot)
		stamp = time.UnixMilli(saved.CreatedAt).In(now.Location())
	}
	reasonList := []Map{}
	for _, code := range order {
		r := reasons[code]
		reasonList = append(reasonList, Map{"ts_code": code, "name": r["name"], "text": r["text"], "status": r["status"], "status_text": r["status_text"], "line": r["line"]})
	}
	if suggestions == nil {
		suggestions = []Map{}
	}
	return Map{
		"empty": false, "generated_at": stamp.Format("01-02 15:04"), "attention": c.attention(), "structure": structureView(conc), "reasons": reasonList,
		"stress": stress, "fit": fit, "hot": hotView(hot), "suggestions": suggestions, "compare": compare, "compare_day": compareDay,
	}
}

// ---- 复盘

func (c *Ctx) TabReview() Map {
	var real []personal.Trade
	for _, t := range c.Trades() {
		if !imported(t) {
			real = append(real, t)
		}
	}
	if len(real) == 0 {
		return Map{"empty": true, "text": "还没有买卖记录。记几笔以后，这里会把你买入时 AI 怎么说、卖出后又怎样并排放给你看。"}
	}
	closes := map[string]Series{}
	last := map[string]float64{}
	for _, t := range real {
		if _, ok := closes[t.Code]; !ok {
			closes[t.Code] = c.Closes(t.Code)
		}
	}
	for code, rows := range closes {
		if len(rows) > 0 {
			last[code] = rows[len(rows)-1].Close
		} else if p := c.Price(code); p > 0 {
			last[code] = p
		}
	}
	for code := range c.Held() {
		if p := c.Price(code); p > 0 {
			last[code] = p
		}
	}
	out := Review(real, c.Notes(), closes, last, c.Today, ReviewDays)
	reasons := map[string]Map{}
	for _, r := range c.Rows() {
		reasons[r.Code] = Map{"name": r.Name, "status": c.Reason(r.Code)["status"]}
	}
	habits := Habits(real, c.Notes(), closes, last, reasons, c.Today, ReviewDays)
	out["empty"] = false
	out["habits"] = habits
	return out
}

// ---- one stock

func (c *Ctx) entries(code string) []Map {
	closes := c.Closes(code)
	pos := c.Held()[code]
	firstDay := c.Today.AddDate(0, 0, -60).Format("2006-01-02")
	var buys []personal.Trade
	if pos != nil {
		firstDay, buys = pos.Since, pos.Buys
	}
	type item struct {
		date, text, lean string
		base             float64
	}
	var items []item
	for _, t := range buys {
		ai := ctxAI(t)
		if lean := str(ai["lean"]); lean != "" {
			verb := "加仓"
			if c.Notes()[t.ID].Label == "建仓" {
				verb = "买入"
			}
			label := str(ai["label"])
			if label == "" {
				label = orDefault(leanWord[lean], "不明")
			}
			items = append(items, item{t.Date, fmt.Sprintf("你%s时 · AI %s", verb, label), lean, t.Price})
		}
	}
	var prev *[2]any
	for _, p := range c.Data.Judgments(code) {
		if p.TradeDate < firstDay {
			continue
		}
		key := [2]any{p.Lean, p.Level}
		if prev != nil && *prev != key {
			base := 0.0
			for _, b := range closes {
				if b.Day == p.TradeDate {
					base = b.Close
				}
			}
			items = append(items, item{p.TradeDate, judgeLabel(prev[0].(string), prev[1].(int)) + " → " + judgeLabel(p.Lean, p.Level), p.Lean, base})
		}
		k := key
		prev = &k
	}
	out := []Map{}
	for _, it := range items {
		outcome := Map{"state": "pending", "text": "待回溯"}
		if it.lean == "unclear" {
			outcome = Map{"state": "skip", "text": "不明，不统计"}
		} else if it.base > 0 {
			ret, passed := RetAfter(closes, it.date, it.base, 5)
			if ret != nil && passed >= 5 {
				hit := *ret < 0
				if it.lean == "bullish" {
					hit = *ret > 0
				}
				word := "错了"
				state := "miss"
				if hit {
					word, state = "对了", "hit"
				}
				outcome = Map{"state": state, "text": fmt.Sprintf("T+5 方向%s %+.1f%%", word, *ret*100), "hit": hit, "ret": *ret}
			} else if passed > 0 {
				outcome = Map{"state": "pending", "text": fmt.Sprintf("待回溯 · 第 %d/5 天", passed)}
			}
		}
		out = append(out, Map{"date": it.date, "text": it.text, "lean": it.lean, "outcome": outcome})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a]["date"].(string) < out[b]["date"].(string) })
	return out
}

func (c *Ctx) StockDetail(code string) Map {
	row := c.Row(code)
	name := c.Names()[code]
	if name == "" {
		for _, t := range c.Trades() {
			if t.Code == code {
				name = t.Name
				break
			}
		}
	}
	if row == nil && name == "" {
		return nil
	}
	ai, sector := c.AI(code), c.Sector(code)
	var reason any
	if row != nil {
		reason = c.Reason(code)
	}
	price := c.Price(code)
	alerts := []Map{}
	hasPrice := false
	for _, a := range c.Alerts(code) {
		alerts = append(alerts, alertView(a, price))
		if a.Side == "down" {
			hasPrice = true
		}
	}
	rules := []Map{}
	for _, r := range c.Store.RulesOf(code) {
		days := int(r.Value)
		out := c.Flow(code).out
		if out > days {
			out = days
		}
		rules = append(rules, Map{"id": r.ID, "text": fmt.Sprintf("大资金连续 %d 天流出", days), "detail": fmt.Sprintf("你设的 · 已出现 %d 天", out)})
	}
	trades := []Map{}
	all := c.Trades()
	for i := len(all) - 1; i >= 0; i-- {
		t := all[i]
		if t.Code != code {
			continue
		}
		note := c.Notes()[t.ID]
		conflict, mtext := TradeFlags(t)
		trades = append(trades, Map{"id": t.ID, "date": t.Date, "label": note.Label, "side": t.Side, "price": t.Price, "qty": t.Qty, "market_text": mtext,
			"ai_label": str(ctxAI(t)["label"]), "conflict": conflict, "note": t.Note, "imported": imported(t)})
	}
	var held any
	if row != nil {
		var daysV any
		if row.Days != nil {
			daysV = *row.Days
		}
		held = Map{"avg": row.Avg, "qty": row.Qty, "weight": row.Weight, "since": row.Since, "days": daysV, "price": row.Price, "pnl": row.PnL, "pnl_pct": row.PnLPct, "mv": row.MV}
	}
	var priceV any
	if price > 0 {
		priceV = price
	}
	watch := ai.Watch
	if watch == nil {
		watch = []string{}
	}
	var wishV any
	for _, w := range c.Wishes() {
		if w.Code == code {
			wishV = c.wishCard(w)
		}
	}
	return Map{"wish": wishV,
		"ts_code": code, "name": name, "price": priceV, "held": held, "sector": sectorBlock(sector), "ai": aiBlock(ai), "flip": flipText(ai), "flip_items": watch,
		"reason": reason, "alerts": alerts, "rules": rules, "needs_price": row != nil && !hasPrice, "trades": trades, "entries": c.entries(code), "market": marketBlock(c.Market()),
	}
}

// ---- writing

func cleanInvalid(items []personal.Item) []personal.Item {
	out := []personal.Item{}
	for _, it := range items {
		kind := str(it["kind"])
		if !invalidKinds[kind] {
			continue
		}
		switch kind {
		case "close_below":
			if v, ok := number(it["value"]); ok && v > 0 {
				out = append(out, personal.Item{"kind": kind, "value": v})
			}
		case "flow_out":
			days := asInt(it["days"], 3)
			out = append(out, personal.Item{"kind": kind, "days": clamp(days, 1, 20)})
		case "custom":
			if text := strings.TrimSpace(str(it["text"])); text != "" {
				out = append(out, personal.Item{"kind": kind, "text": truncate(text, 80)})
			}
		default:
			out = append(out, personal.Item{"kind": kind})
		}
	}
	return out
}

func cleanConditions(items []personal.Item) []personal.Item {
	out := []personal.Item{}
	for _, it := range items {
		kind := str(it["kind"])
		if !conditionKinds[kind] {
			continue
		}
		switch kind {
		case "price_below":
			if v, ok := number(it["value"]); ok && v > 0 {
				out = append(out, personal.Item{"kind": kind, "value": v})
			}
		case "flow_in_days":
			out = append(out, personal.Item{"kind": kind, "days": clamp(asInt(it["days"], 3), 1, 20)})
		case "custom":
			if text := strings.TrimSpace(str(it["text"])); text != "" {
				out = append(out, personal.Item{"kind": kind, "text": truncate(text, 80)})
			}
		default:
			out = append(out, personal.Item{"kind": kind})
		}
	}
	return out
}

// number reads a number that may have been typed as text.
func number(v any) (float64, bool) {
	if f, ok := asFloat(v); ok {
		return f, true
	}
	if s, ok := v.(string); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, err == nil
	}
	return 0, false
}

func clamp(v, lo, hi int) int { return int(math.Max(float64(lo), math.Min(float64(hi), float64(v)))) }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (c *Ctx) tradeContext(code string) personal.Item {
	out := personal.Item{}
	if m := c.Market(); m != nil {
		out["market"] = Map{"level": m.Level, "tilt": m.Tilt, "stance": m.Stance}
	}
	if ai := c.AI(code); ai.Has {
		out["ai"] = Map{"lean": ai.Lean, "level": ai.Level, "label": ai.Label, "position": nz(ai.Position)}
	}
	return out
}

// syncInvalid turns "what would prove me wrong" into alerts: a price becomes a price alert, large money
// leaving becomes a rule.
func (c *Ctx) syncInvalid(code string, invalid []personal.Item) {
	c.Store.DropAlertsByName(code, AlertTag)
	for _, r := range c.Store.RulesOf(code) {
		c.Store.RemoveRule(r.ID)
	}
	name := c.Names()[code]
	for _, it := range invalid {
		switch str(it["kind"]) {
		case "close_below":
			v, _ := asFloat(it["value"])
			c.Store.AlertAdd(code, "down", v, "close", AlertTag)
		case "flow_out":
			c.Store.AddRule(code, "flow_out", float64(asInt(it["days"], 3)))
		}
	}
	_ = name
}

func fail(format string, args ...any) error { return personal.InputError(fmt.Sprintf(format, args...)) }

// RecordTrade writes one trade and what follows from it: the reason, the alerts, closing a wish.
func (c *Ctx) RecordTrade(body Map) (Map, error) {
	query := strings.TrimSpace(firstStr(body["ts_code"], body["q"]))
	code, name, ok := "", "", false
	if query != "" {
		code, name, ok = c.Data.Resolve(query)
	}
	if !ok {
		if query == "" {
			query = "没填"
		}
		return nil, fail("没有找到股票：%s", query)
	}
	side := str(body["side"])
	price, ok1 := number(body["price"])
	qtyF, ok2 := number(body["qty"])
	if !ok1 || !ok2 || qtyF != math.Trunc(qtyF) {
		return nil, fail("价格和数量要填数字")
	}
	day := firstStr(body["date"])
	if day == "" {
		day = c.todayISO()
	}
	reasonText := strings.TrimSpace(str(body["reason"]))
	invalid := cleanInvalid(items(body["invalid"]))
	var wish *personal.Wish
	for _, w := range c.Wishes() {
		if w.Code == code {
			w := w
			wish = &w
		}
	}
	trade := personal.Trade{Code: code, Name: name, Side: side, Price: price, Qty: int(qtyF), Date: day, Reason: reasonText, Invalid: invalid, Context: c.tradeContext(code)}
	if wish != nil && side == "buy" {
		id := wish.ID
		trade.WishID = &id
	}
	saved, err := c.Store.TradeAdd(trade)
	if err != nil {
		return nil, err
	}
	c.reset()
	held, notes := c.Held(), c.Notes()
	label := notes[saved.ID].Label
	if side == "buy" {
		if reasonText != "" || len(invalid) > 0 {
			existing, has := c.Store.ReasonOf(code)
			written := day
			if label != "建仓" && has {
				written = existing.WrittenAt
			}
			merged := invalid
			if len(merged) == 0 && has {
				merged = existing.Invalid
			}
			text := reasonText
			if text == "" && has {
				text = existing.Text
			}
			if _, err := c.Store.SetReason(code, text, merged, written); err == nil {
				c.syncInvalid(code, merged)
			}
		}
		if wish != nil {
			c.Store.WishCloseWith(wish.ID, "bought")
		}
	} else if _, still := held[code]; !still {
		c.Store.DropReason(code)
		c.syncInvalid(code, nil)
	}
	c.Data.RequestJudgment(code)
	c.reset()
	_, still := c.Held()[code]
	return Map{"trade": saved, "label": label, "closed": !still}, nil
}

func firstStr(vs ...any) string {
	for _, v := range vs {
		if s := strings.TrimSpace(str(v)); s != "" {
			return s
		}
	}
	return ""
}

func items(v any) []personal.Item {
	var out []personal.Item
	switch x := v.(type) {
	case []personal.Item:
		return x
	case []any:
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// TradePreview is a look before writing: what the market says, what the AI says, how the shares move.
func (c *Ctx) TradePreview(body Map) Map {
	query := strings.TrimSpace(firstStr(body["ts_code"], body["q"]))
	code, name, ok := "", "", false
	if query != "" {
		code, name, ok = c.Data.Resolve(query)
	}
	if !ok {
		return Map{"found": false}
	}
	side := str(body["side"])
	price, _ := number(body["price"])
	qty, _ := number(body["qty"])
	ai, sector := c.AI(code), c.Sector(code)
	quote := c.Data.Quotes(map[string]string{code: name})[code]
	total := c.Total().MV
	var pctV any
	pctF := 0.0
	if total > 0 && price > 0 && qty > 0 {
		pctF = price * qty / total * 100
		pctV = pctF
	}
	var qprice any
	if quote.Price > 0 {
		qprice = quote.Price
	}
	heldQty := 0
	pos, held := c.Held()[code]
	if held {
		heldQty = pos.Qty
	}
	out := Map{"found": true, "ts_code": code, "name": name, "price": qprice, "market": marketBlock(c.Market()), "ai": aiBlock(ai), "sector": sectorBlock(sector),
		"side": side, "pct": pctV, "held": held, "held_qty": heldQty, "conflict": side == "buy" && NoAdd(c.Market()), "impact": nil}
	if side == "buy" && pctF > 0 {
		out["impact"] = c.impactView(code, pctF)
	}
	return out
}

// AddWish stores a stock the user wants to buy.
func (c *Ctx) AddWish(body Map) (personal.Wish, error) {
	query := strings.TrimSpace(firstStr(body["ts_code"], body["q"]))
	code, name, ok := "", "", false
	if query != "" {
		code, name, ok = c.Data.Resolve(query)
	}
	if !ok {
		if query == "" {
			query = "没填"
		}
		return personal.Wish{}, fail("没有找到股票：%s", query)
	}
	if _, held := c.Held()[code]; held {
		return personal.Wish{}, fail("已经持有这只了，不能重复加到想买")
	}
	plan := 0.0
	if v, present := body["plan_pct"]; present && v != nil && v != "" {
		f, ok := number(v)
		if !ok {
			return personal.Wish{}, fail("仓位要填数字")
		}
		plan = f
	}
	match := firstStr(body["match"])
	if match == "" {
		match = "all"
	}
	w, err := c.Store.WishAdd(personal.Wish{Code: code, Name: name, Reason: str(body["reason"]), Conditions: cleanConditions(items(body["conditions"])), Match: match,
		PlanPct: plan, Invalid: cleanInvalid(items(body["invalid"])), Source: firstStr(body["source"])})
	if err != nil {
		return personal.Wish{}, err
	}
	c.Data.RequestJudgment(code)
	c.reset()
	return w, nil
}

var codePattern = regexp.MustCompile(`(?i)^(\d{6})(?:\.(?:SH|SZ|BJ))?$`)

// ImportRow is one position read from pasted text.
type ImportRow struct {
	Code string
	Qty  int
	Cost float64
	Line int
}

var splitCells = regexp.MustCompile(`[,\t;，]+|\s{2,}`)
var hasSep = regexp.MustCompile(`[,\t;，]|\s{2,}`)

// ParseImport reads one position per line: code, quantity, cost (the name is optional). A header row
// names the columns; without one the order is code, quantity, cost.
func ParseImport(text string) ([]ImportRow, []Map) {
	var rows []ImportRow
	skipped := []Map{}
	var qtyCol, costCol *int
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var cells []string
		if hasSep.MatchString(line) {
			for _, cell := range splitCells.Split(line, -1) {
				if cell = strings.TrimSpace(cell); cell != "" {
					cells = append(cells, cell)
				}
			}
		} else {
			cells = strings.Fields(line)
		}
		code := ""
		for _, cell := range cells {
			if codePattern.MatchString(cell) {
				code = cell
				break
			}
		}
		if code == "" {
			if strings.Contains(line, "数量") || strings.Contains(line, "持仓") || strings.Contains(line, "成本") {
				qtyCol, costCol = nil, nil
				for i, cell := range cells {
					if qtyCol == nil && (strings.Contains(cell, "数量") || strings.Contains(cell, "股份余额") || strings.Contains(cell, "持仓")) {
						i := i
						qtyCol = &i
					}
					if costCol == nil && (strings.Contains(cell, "成本") || strings.Contains(cell, "买入均价")) {
						i := i
						costCol = &i
					}
				}
			} else {
				skipped = append(skipped, Map{"line": n + 1, "text": line, "why": "没有找到 6 位股票代码"})
			}
			continue
		}
		type num struct {
			i int
			v float64
		}
		var nums []num
		for i, cell := range cells {
			if cell == code {
				continue
			}
			if v, err := strconv.ParseFloat(strings.ReplaceAll(cell, ",", ""), 64); err == nil {
				nums = append(nums, num{i, v})
			}
		}
		var qty, cost float64
		have := false
		if qtyCol != nil && costCol != nil && len(cells) > *qtyCol && len(cells) > *costCol {
			q, e1 := strconv.ParseFloat(strings.ReplaceAll(cells[*qtyCol], ",", ""), 64)
			cs, e2 := strconv.ParseFloat(strings.ReplaceAll(cells[*costCol], ",", ""), 64)
			if e1 == nil && e2 == nil {
				qty, cost, have = q, cs, true
			}
		}
		if !have && len(nums) >= 2 {
			qty, cost, have = nums[0].v, nums[1].v, true
		}
		if !have || qty <= 0 || cost <= 0 || qty != math.Trunc(qty) {
			skipped = append(skipped, Map{"line": n + 1, "text": line, "why": "数量或成本价没有读出来"})
			continue
		}
		rows = append(rows, ImportRow{Code: codePattern.FindStringSubmatch(code)[1], Qty: int(qty), Cost: cost, Line: n + 1})
	}
	return rows, skipped
}

// ImportPositions adds pasted positions as imported buys (they are not reviewed as the user's own moves).
func (c *Ctx) ImportPositions(text string) Map {
	parsed, skipped := ParseImport(text)
	added := []Map{}
	for _, it := range parsed {
		code, name, ok := c.Data.Resolve(it.Code)
		if !ok {
			skipped = append(skipped, Map{"line": it.Line, "text": it.Code, "why": "没有找到这只股票"})
			continue
		}
		if _, held := c.Held()[code]; held {
			skipped = append(skipped, Map{"line": it.Line, "text": name, "why": "已经有持仓，没有重复导入"})
			continue
		}
		if _, err := c.Store.TradeAdd(personal.Trade{Code: code, Name: name, Side: "buy", Price: it.Cost, Qty: it.Qty, Date: c.todayISO(), Context: personal.Item{"imported": true}}); err != nil {
			skipped = append(skipped, Map{"line": it.Line, "text": name, "why": err.Error()})
			continue
		}
		c.Data.RequestJudgment(code)
		c.reset()
		added = append(added, Map{"ts_code": code, "name": name, "qty": it.Qty, "cost": it.Cost})
	}
	return Map{"added": added, "skipped": skipped}
}

// SetReason rewrites why a stock is held; the date it was first written stays.
func (c *Ctx) SetReason(code, text string, invalid []personal.Item) (personal.Reason, error) {
	if _, held := c.Held()[code]; !held {
		return personal.Reason{}, fail("手里没有这只，没法改买入理由")
	}
	cleaned := cleanInvalid(invalid)
	written, _ := c.Reason(code)["written_at"].(string)
	r, err := c.Store.SetReason(code, text, cleaned, written)
	if err != nil {
		return personal.Reason{}, err
	}
	c.syncInvalid(code, cleaned)
	c.reset()
	return r, nil
}

// SetPriceLine adds a "close below" line to a holding, both as an alert and in its reason.
func (c *Ctx) SetPriceLine(code string, price float64) error {
	if _, held := c.Held()[code]; !held {
		return fail("手里没有这只")
	}
	if price <= 0 {
		return fail("价格要大于 0")
	}
	reason := c.Reason(code)
	var invalid []personal.Item
	if old, ok := reason["invalid"].([]personal.Item); ok {
		for _, it := range old {
			if str(it["kind"]) != "close_below" {
				invalid = append(invalid, it)
			}
		}
	}
	invalid = append(invalid, personal.Item{"kind": "close_below", "value": price})
	written, _ := reason["written_at"].(string)
	if _, err := c.Store.SetReason(code, str(reason["text"]), invalid, written); err != nil {
		return err
	}
	c.syncInvalid(code, invalid)
	c.reset()
	return nil
}

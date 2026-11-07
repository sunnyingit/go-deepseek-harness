// Package holdings is the holdings page: checkups, wish conditions, reviews and habits. It only works out
// what the user's own trades and the shared market data say; it never tells anyone to buy or sell.
package holdings

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go-harness/internal/personal"
)

const (
	SingleLimit = 0.30 // one stock above this share of the holdings is heavy
	SectorLimit = 0.40 // one sector above this share is a pile-up
	HighCorr    = 0.7  // two stocks whose 60-day returns correlate above this move together
	ChaseGain   = 0.20
	ChaseDays   = 15
	EarlySell   = 0.10
	AfterDays   = 20
	ReviewDays  = 60
	MarketDrop  = 0.05
	SectorDrop  = 0.10
	NearGap     = 0.05
	HotTop      = 6
)

// Map is a JSON object; the page builders return these so the shape is exactly what the page reads.
type Map = map[string]any

// Market is the market page's conclusion in the words the holdings page uses.
type Market struct {
	Level  string `json:"level"`
	Tilt   string `json:"tilt"`
	Action string `json:"action"`
	Text   string `json:"text"`
	Stance string `json:"stance"`
}

var stances = map[[2]string]string{
	{"顺风", ""}: "顺势", {"逆风", ""}: "以减仓为主", {"中性", "偏强"}: "可以持有", {"中性", "偏弱"}: "不加仓", {"中性", "均衡"}: "新仓小一点",
}

func StanceOf(level, tilt string) string { return stances[[2]string{level, tilt}] }

func MarketText(level, tilt string) string {
	if level == "" || level == "没有" {
		return ""
	}
	text := level + tilt
	if s := StanceOf(level, tilt); s != "" {
		text += " · " + s
	}
	return text
}

// NoAdd is true when the market conclusion says "do not add": headwind, or neutral and weak.
func NoAdd(m *Market) bool {
	return m != nil && (m.Level == "逆风" || (m.Level == "中性" && m.Tilt == "偏弱"))
}

// ---- price series

// Bar is one day's close.
type Bar struct {
	Day   string
	Close float64
}

type Series []Bar

func indexOn(rows Series, day string) int {
	found := -1
	for i, b := range rows {
		if b.Day <= day {
			found = i
		} else {
			break
		}
	}
	return found
}

// RetAfter is the move n trading days after day relative to base (or to the latest bar when there are
// fewer), and how many trading days have passed.
func RetAfter(rows Series, day string, base float64, n int) (*float64, int) {
	i := indexOn(rows, day)
	if i < 0 || len(rows) == 0 || base == 0 {
		return nil, 0
	}
	last := i + n
	if last > len(rows)-1 {
		last = len(rows) - 1
	}
	if last <= i {
		return nil, 0
	}
	v := rows[last].Close/base - 1
	return &v, last - i
}

// RunUp is the move over the n trading days up to day.
func RunUp(rows Series, day string, n int) *float64 {
	i := indexOn(rows, day)
	if i < 0 || i < n || rows[i-n].Close == 0 {
		return nil
	}
	v := rows[i].Close/rows[i-n].Close - 1
	return &v
}

func returns(rows Series, days int) map[string]float64 {
	tail := rows
	if len(tail) > days+1 {
		tail = tail[len(tail)-(days+1):]
	}
	out := map[string]float64{}
	for i := 1; i < len(tail); i++ {
		if tail[i-1].Close != 0 {
			out[tail[i].Day] = tail[i].Close/tail[i-1].Close - 1
		}
	}
	return out
}

func commonDays(a, b map[string]float64) []string {
	var keys []string
	for k := range a {
		if _, ok := b[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Beta is the slope of the stock's daily returns on the index's over a year, kept between 0.3 and 2 so a
// short sample cannot give an absurd number.
func Beta(stock, index Series, days int) *float64 {
	a, b := returns(stock, days), returns(index, days)
	keys := commonDays(a, b)
	if len(keys) < 40 {
		return nil
	}
	var mx, my float64
	for _, k := range keys {
		mx += b[k]
		my += a[k]
	}
	mx /= float64(len(keys))
	my /= float64(len(keys))
	var variance, cov float64
	for _, k := range keys {
		variance += (b[k] - mx) * (b[k] - mx)
		cov += (b[k] - mx) * (a[k] - my)
	}
	if variance == 0 {
		return nil
	}
	v := math.Max(0.3, math.Min(2.0, cov/variance))
	return &v
}

// Corr is the correlation of two stocks' daily returns over days.
func Corr(first, second Series, days int) *float64 {
	a, b := returns(first, days), returns(second, days)
	keys := commonDays(a, b)
	if len(keys) < 30 {
		return nil
	}
	var mx, my float64
	for _, k := range keys {
		mx += a[k]
		my += b[k]
	}
	mx /= float64(len(keys))
	my /= float64(len(keys))
	var sx, sy, cov float64
	for _, k := range keys {
		sx += (a[k] - mx) * (a[k] - mx)
		sy += (b[k] - my) * (b[k] - my)
		cov += (a[k] - mx) * (b[k] - my)
	}
	if sx == 0 || sy == 0 {
		return nil
	}
	v := cov / (math.Sqrt(sx) * math.Sqrt(sy))
	return &v
}

// ---- structure: where the money sits

var verdictNote = map[string]string{
	"三层同向": "钱、热度、杠杆同向", "有钱没热度": "钱在进，热度或杠杆没跟上", "只热不增": "换手高，但没有新钱进来", "在流出": "增量为负，钱在往外走",
}

// SectorSlot is one sector's share of the holdings.
type SectorSlot struct {
	Name    string
	Weight  float64
	MV      float64
	Stocks  []string
	Verdict string
}

// Conc is the structure of the holdings.
type Conc struct {
	Sectors   []*SectorSlot
	Flags     []Map
	GoodShare float64
}

func pct(v float64, digits int) string { return fmt.Sprintf("%.*f", digits, v*100) }

func sectorName(m map[string]string, code string) string {
	if n := m[code]; n != "" {
		return n
	}
	return "其他"
}

// Concentration says how the holdings spread over sectors and whether one stock or sector is too heavy.
// verdicts maps a sector name to its money/heat verdict; correlations is keyed by the two stock codes.
func Concentration(rows []personal.Row, sectorOf map[string]string, verdicts map[string]string, correlations map[[2]string]float64) Conc {
	by := map[string]*SectorSlot{}
	var order []string
	for _, r := range rows {
		name := sectorName(sectorOf, r.Code)
		slot := by[name]
		if slot == nil {
			slot = &SectorSlot{Name: name, Verdict: verdicts[name]}
			by[name] = slot
			order = append(order, name)
		}
		slot.Weight += r.Weight
		slot.MV += r.MV
		slot.Stocks = append(slot.Stocks, r.Name)
	}
	sectors := make([]*SectorSlot, 0, len(by))
	for _, n := range order {
		sectors = append(sectors, by[n])
	}
	sort.SliceStable(sectors, func(a, b int) bool { return sectors[a].Weight > sectors[b].Weight })
	var flags []Map
	for _, s := range sectors {
		if s.Weight < SectorLimit {
			continue
		}
		var text string
		if len(s.Stocks) > 1 {
			text = fmt.Sprintf("%s都在%s，合计 %s%%", strings.Join(s.Stocks, "、"), s.Name, pct(s.Weight, 1))
		} else {
			text = fmt.Sprintf("%s所在的%s占 %s%%", s.Stocks[0], s.Name, pct(s.Weight, 1))
		}
		if verdictNote[s.Verdict] != "" && (s.Verdict == "只热不增" || s.Verdict == "在流出") {
			text += fmt.Sprintf("；这个板块「%s」：%s", s.Verdict, verdictNote[s.Verdict])
		}
		if a, b, v, ok := highPair(s.Stocks, rows, correlations); ok {
			text += fmt.Sprintf("。%s、%s近 60 天走势高度同向（相关 %.2f），跌的时候会一起跌", a, b, v)
		}
		flags = append(flags, Map{"level": "注意", "title": fmt.Sprintf("%s%% 的仓位压在同一个板块", pct(s.Weight, 0)), "text": text})
	}
	byWeight := append([]personal.Row(nil), rows...)
	sort.SliceStable(byWeight, func(a, b int) bool { return byWeight[a].Weight > byWeight[b].Weight })
	for _, r := range byWeight {
		if r.Weight >= SingleLimit {
			flags = append(flags, Map{"level": "注意", "title": "单只偏重", "text": fmt.Sprintf("%s占 %s%%，超过单只 %s%% 的参考线", r.Name, pct(r.Weight, 1), pct(SingleLimit, 0))})
		}
	}
	var good []*SectorSlot
	share := 0.0
	var goodStocks []string
	for _, s := range sectors {
		if s.Verdict == "三层同向" {
			good = append(good, s)
			share += s.Weight
			goodStocks = append(goodStocks, s.Stocks...)
		}
	}
	if len(good) > 0 {
		flags = append(flags, Map{"level": "正常", "title": "有新钱进的板块", "text": fmt.Sprintf("有新钱进的板块（三层同向）占 %s%%：%s", pct(share, 1), strings.Join(goodStocks, "、"))})
	} else if len(rows) > 0 {
		flags = append(flags, Map{"level": "提示", "title": "没有持仓在有新钱的板块里", "text": "手里的票所在板块都不是「三层同向」，上涨更多要靠个股自己"})
	}
	return Conc{Sectors: sectors, Flags: flags, GoodShare: share}
}

func highPair(names []string, rows []personal.Row, corr map[[2]string]float64) (string, string, float64, bool) {
	if len(corr) == 0 {
		return "", "", 0, false
	}
	codeOf := map[string]string{}
	for _, r := range rows {
		codeOf[r.Name] = r.Code
	}
	var best [2]string
	bv, found := 0.0, false
	for i, a := range names {
		for _, b := range names[i+1:] {
			v, ok := corr[[2]string{codeOf[a], codeOf[b]}]
			if !ok {
				v, ok = corr[[2]string{codeOf[b], codeOf[a]}]
			}
			if ok && v >= HighCorr && (!found || v > bv) {
				best, bv, found = [2]string{a, b}, v, true
			}
		}
	}
	return best[0], best[1], bv, found
}

// ---- the worst that could happen

// Stress prices three situations. Betas that could not be worked out count as 1; triggers are the "close
// below" prices the user set. It is not a forecast.
func Stress(rows []personal.Row, sectorOf map[string]string, betas map[string]*float64, triggers map[string]float64) []Map {
	total := 0.0
	for _, r := range rows {
		total += r.MV
	}
	if total == 0 {
		return nil
	}
	loss := map[string]float64{}
	sumLoss := 0.0
	for _, r := range rows {
		b := 1.0
		if v := betas[r.Code]; v != nil {
			b = *v
		}
		loss[r.Code] = r.MV * b * MarketDrop
		sumLoss += loss[r.Code]
	}
	ranked := append([]personal.Row(nil), rows...)
	sort.SliceStable(ranked, func(a, b int) bool { return loss[ranked[a].Code] > loss[ranked[b].Code] })
	top := ranked
	if len(top) > 2 {
		top = top[:2]
	}
	var names []string
	for _, r := range top {
		names = append(names, r.Name)
	}
	text := strings.Join(names, "、") + "跌得最多（联动最强）"
	if len(ranked) > 2 {
		text += "，" + ranked[len(ranked)-1].Name + "相对抗跌"
	}
	out := []Map{{"key": "market", "title": fmt.Sprintf("大盘再跌 %s%%（转逆风）", pct(MarketDrop, 0)), "text": text, "loss": sumLoss, "pct": sumLoss / total}}

	mv, stocks := map[string]float64{}, map[string][]string{}
	var sectors []string
	for _, r := range rows {
		n := sectorName(sectorOf, r.Code)
		if _, ok := mv[n]; !ok {
			sectors = append(sectors, n)
		}
		mv[n] += r.MV
		stocks[n] = append(stocks[n], r.Name)
	}
	heavy := sectors[0]
	for _, n := range sectors {
		if mv[n] > mv[heavy] {
			heavy = n
		}
	}
	share := mv[heavy] / total
	stext := "只影响" + strings.Join(stocks[heavy], "、")
	if share >= 0.25 {
		stext += fmt.Sprintf("，但它们占了%s%%", pct(share, 0))
	}
	out = append(out, Map{"key": "sector", "title": fmt.Sprintf("%s板块跌 %s%%", heavy, pct(SectorDrop, 0)), "text": stext, "loss": mv[heavy] * SectorDrop, "pct": mv[heavy] * SectorDrop / total})

	var hit, unset []string
	cost := 0.0
	for _, r := range rows {
		level, ok := triggers[r.Code]
		if !ok || level == 0 {
			unset = append(unset, r.Name)
			continue
		}
		if level < r.Price {
			hit = append(hit, fmt.Sprintf("%s跌破 %.2f", r.Name, level))
			cost += (r.Price - level) * float64(r.Qty)
		}
	}
	otext := "还没有设价格条件"
	if len(hit) > 0 {
		otext = strings.Join(hit, "、")
		if len(unset) > 0 {
			otext += "；" + strings.Join(unset, "、") + "没设价格条件"
		}
	}
	out = append(out, Map{"key": "own", "title": "碰到你自己设的价格条件就走", "text": otext, "loss": cost, "pct": cost / total})
	return out
}

// ---- is the reason still true

// ReasonFacts are what the reason is checked against.
type ReasonFacts struct {
	Price   float64
	AILean  string
	AILabel string
	HasAI   bool
	BuyLean string
	Verdict string
	FlowOut int
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func asInt(v any, def int) int {
	if f, ok := asFloat(v); ok {
		return int(f)
	}
	return def
}

func str(v any) string { s, _ := v.(string); return s }

// ReasonCheck compares "what would prove me wrong" with the facts. It does not read the reason text;
// it checks what can be checked: the price, the sector, large money, the AI's direction.
func ReasonCheck(invalid []personal.Item, f ReasonFacts) (status, line string) {
	var hits, partial []string
	for _, item := range invalid {
		switch str(item["kind"]) {
		case "close_below":
			if v, ok := asFloat(item["value"]); ok && f.Price > 0 && f.Price <= v {
				hits = append(hits, fmt.Sprintf("已跌破你设的 %.2f（现价 %.2f）", v, f.Price))
			}
		case "sector_out":
			if f.Verdict == "在流出" {
				hits = append(hits, "所在板块已转为在流出")
			}
		case "flow_out":
			days := asInt(item["days"], 3)
			if f.FlowOut >= days {
				hits = append(hits, fmt.Sprintf("大资金已连续 %d 天流出", f.FlowOut))
			} else if f.FlowOut > 0 {
				partial = append(partial, fmt.Sprintf("大资金流出已出现 %d 天（设的是 %d 天）", f.FlowOut, days))
			}
		}
	}
	if len(hits) > 0 {
		return "broken", strings.Join(hits, "；")
	}
	doubts := append([]string(nil), partial...)
	switch {
	case f.AILean == "bearish":
		label := f.AILabel
		if label == "" {
			label = "偏空"
		}
		doubts = append(doubts, "AI 现在"+label)
	case f.BuyLean == "bullish" && (f.AILean == "unclear" || f.AILean == "bearish"):
		doubts = append(doubts, "AI 已经不再偏多")
	}
	if f.Verdict == "在流出" {
		doubts = append(doubts, "所在板块在流出")
	} else if f.Verdict == "只热不增" {
		doubts = append(doubts, "所在板块只热不增，涨的主要靠情绪")
	}
	if len(doubts) > 0 {
		return "doubt", strings.Join(doubts, "；")
	}
	var bits []string
	if f.HasAI {
		label := f.AILabel
		if label == "" {
			label = "不明"
		}
		bits = append(bits, "AI "+label)
	}
	if f.Verdict != "" {
		bits = append(bits, "板块「"+f.Verdict+"」")
	}
	text := "没有发现和理由矛盾的事实"
	if len(bits) > 0 {
		text = strings.Join(bits, "；")
	}
	for _, item := range invalid {
		if str(item["kind"]) == "custom" {
			text += "；你写的自定义条件要自己盯"
			break
		}
	}
	return "ok", text
}

// ---- wishes: which conditions are met

// WishFacts are what the wish conditions are checked against.
type WishFacts struct {
	Price      float64
	AILean     string
	AILabel    string
	Sector     string
	SectorFlow *float64
	FlowIn     int
	Market     *Market
}

// WishState reads each condition and says whether the wish as a whole is ready.
func WishState(w personal.Wish, f WishFacts) Map {
	var rows []Map
	var auto []Map
	for _, cond := range w.Conditions {
		kind := str(cond["kind"])
		row := Map{"kind": kind, "label": str(cond["label"]), "met": nil, "detail": "", "gap": nil}
		label := func(def string) {
			if row["label"] == "" {
				row["label"] = def
			}
		}
		switch kind {
		case "price_below":
			level, _ := asFloat(cond["value"])
			label(fmt.Sprintf("回踩到 %.2f", level))
			if f.Price > 0 {
				met := f.Price <= level
				gap := math.Max(0, (f.Price-level)/f.Price)
				row["met"], row["gap"] = met, gap
				if met {
					row["detail"] = "已到"
				} else {
					row["detail"] = fmt.Sprintf("还差 %s%%", pct(gap, 1))
				}
			}
		case "etf_inflow":
			sector := f.Sector
			if sector == "" {
				sector = "板块"
			}
			label(sector + " ETF 继续净申购")
			if f.SectorFlow != nil {
				met := *f.SectorFlow > 0
				row["met"] = met
				if met {
					row["detail"] = "已满足"
				} else {
					row["detail"] = fmt.Sprintf("近 5 日 %+.1f 亿，还没转正", *f.SectorFlow)
				}
			}
		case "flow_in_days":
			days := asInt(cond["days"], 3)
			label(fmt.Sprintf("大资金连续 %d 天流入", days))
			row["met"] = f.FlowIn >= days
			m := f.FlowIn
			if days < m {
				m = days
			}
			row["detail"] = fmt.Sprintf("%d / %d 天", m, days)
		case "ai_bullish":
			label("AI 转为「偏多」")
			row["met"] = f.AILean == "bullish"
			now := f.AILabel
			if now == "" {
				now = "还没有判断"
			}
			row["detail"] = "现在" + now
		case "market_ok":
			label("大盘不再是「不加仓」")
			if f.Market != nil {
				row["met"] = !NoAdd(f.Market)
				t := MarketText(f.Market.Level, f.Market.Tilt)
				if t == "" {
					t = "没有结论"
				}
				row["detail"] = "现在" + t
			}
		default:
			text := str(cond["text"])
			if text == "" {
				text = "自己盯的条件"
			}
			label(text)
			row["detail"] = "自己盯"
		}
		if row["met"] != nil {
			auto = append(auto, row)
		}
		rows = append(rows, row)
	}
	met := 0
	for _, r := range auto {
		if r["met"] == true {
			met++
		}
	}
	done := false
	if len(auto) > 0 {
		if w.Match == "any" {
			done = met >= 1
		} else {
			done = met == len(auto)
		}
	}
	near := false
	minGap := math.Inf(1)
	for _, r := range auto {
		if r["kind"] == "price_below" && r["met"] == false {
			if g, ok := r["gap"].(float64); ok && g < minGap {
				minGap = g
			}
		}
	}
	near = minGap <= NearGap
	status, tone := "等条件", "flat"
	switch {
	case f.AILean == "bearish":
		status, tone = "AI 和你方向相反", "bad"
	case done:
		status, tone = "条件到了", "good"
	case near || (len(auto) > 1 && met >= len(auto)-1 && met > 0):
		status, tone = "条件快到了", "warn"
	}
	if rows == nil {
		rows = []Map{}
	}
	return Map{"conditions": rows, "met": met, "auto": len(auto), "done": done, "status": status, "tone": tone}
}

// PlanImpact is how the sector shares move if plan percent of the holdings' value is bought.
func PlanImpact(rows []personal.Row, sectorOf map[string]string, newSector string, planPct float64) Map {
	total := 0.0
	for _, r := range rows {
		total += r.MV
	}
	if total == 0 || planPct == 0 {
		return Map{"lines": []Map{}, "total": total, "amount": 0.0}
	}
	amount := total * planPct / 100
	after := total + amount
	weights := map[string]float64{}
	for _, r := range rows {
		weights[sectorName(sectorOf, r.Code)] += r.MV
	}
	target := newSector
	if target == "" {
		target = "其他"
	}
	lines := []Map{{"sector": target, "before": weights[target] / total, "after": (weights[target] + amount) / after, "buying": true}}
	type kv struct {
		name string
		mv   float64
	}
	var others []kv
	for n, mv := range weights {
		if n != target {
			others = append(others, kv{n, mv})
		}
	}
	sort.SliceStable(others, func(a, b int) bool {
		if others[a].mv != others[b].mv {
			return others[a].mv > others[b].mv
		}
		return others[a].name < others[b].name
	})
	if len(others) > 2 {
		others = others[:2]
	}
	for _, o := range others {
		lines = append(lines, Map{"sector": o.name, "before": o.mv / total, "after": o.mv / after, "buying": false})
	}
	return Map{"lines": lines, "total": total, "amount": amount}
}

// TradeFlags marks a buy made while the market conclusion said "do not add".
func TradeFlags(t personal.Trade) (conflict bool, marketText string) {
	m, _ := t.Context["market"].(map[string]any)
	if m == nil {
		return false, ""
	}
	mk := &Market{Level: str(m["level"]), Tilt: str(m["tilt"])}
	return t.Side == "buy" && NoAdd(mk), MarketText(mk.Level, mk.Tilt)
}

func ctxLean(t personal.Trade) string {
	ai, _ := t.Context["ai"].(map[string]any)
	return str(ai["lean"])
}

func ctxAI(t personal.Trade) map[string]any {
	ai, _ := t.Context["ai"].(map[string]any)
	return ai
}

// ---- review

func fptr(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// Review summarises the trades of the last days: how many, how they sat with the AI, and what happened after each sale.
func Review(trades []personal.Trade, notes map[int]personal.TradeNote, closes map[string]Series, last map[string]float64, today time.Time, days int) Map {
	since := today.AddDate(0, 0, -days).Format("2006-01-02")
	var recent, buys, sells, adds, aligned, risky, unclear []personal.Trade
	for _, t := range trades {
		if t.Date >= since {
			recent = append(recent, t)
		}
	}
	for _, t := range recent {
		if t.Side == "buy" {
			buys = append(buys, t)
			if notes[t.ID].Label == "加仓" {
				adds = append(adds, t)
			}
			switch ctxLean(t) {
			case "bullish":
				aligned = append(aligned, t)
			case "bearish":
				risky = append(risky, t)
			case "unclear":
				unclear = append(unclear, t)
			}
		} else {
			sells = append(sells, t)
		}
	}
	riskRows := []Map{}
	for _, t := range risky {
		var result any
		if now := last[t.Code]; now > 0 {
			result = now/t.Price - 1
		}
		riskRows = append(riskRows, Map{"name": t.Name, "ts_code": t.Code, "date": t.Date, "result": result})
	}
	sold := []Map{}
	for i := len(sells) - 1; i >= 0; i-- {
		t := sells[i]
		note := notes[t.ID]
		var entry personal.Trade
		if note.Entry != nil {
			entry = *note.Entry
		}
		after, passed := RetAfter(closes[t.Code], t.Date, t.Price, AfterDays)
		ai := ctxAI(entry)
		relation := ""
		switch str(ai["lean"]) {
		case "bullish":
			relation = "跟 AI 一致"
		case "bearish":
			relation = "AI 提示风险后仍买"
		}
		buyDate := note.Since
		if buyDate == "" {
			buyDate = entry.Date
		}
		sold = append(sold, Map{
			"id": t.ID, "ts_code": t.Code, "name": t.Name, "buy_date": buyDate, "sell_date": t.Date, "pnl_pct": note.PnLPct, "pnl": note.PnL,
			"after": fptr(after), "after_days": passed, "after_done": passed >= AfterDays, "reason": entry.Reason, "ai_label": str(ai["label"]),
			"ai_lean": ai["lean"], "relation": relation, "note": t.Note, "verdict": SoldVerdict(note.PnLPct, after, passed >= AfterDays),
		})
	}
	return Map{"buys": len(buys), "adds": len(adds), "sells": len(sells), "total": len(recent), "aligned": len(aligned), "unclear": len(unclear),
		"risky": len(risky), "risk_rows": riskRows, "sold": sold, "days": days}
}

func SoldVerdict(pnl float64, after *float64, done bool) string {
	if after == nil {
		return "卖出不久，还没有可比的后续走势"
	}
	span := "卖后至今"
	if done {
		span = "卖后 20 个交易日"
	}
	a := *after
	if pnl >= 0 {
		switch {
		case a >= EarlySell:
			return fmt.Sprintf("卖早了：赚了 %.1f%% 卖出，%s又涨了 %.1f%%，超过 %s%%", pnl*100, span, a*100, pct(EarlySell, 0))
		case a >= 0:
			return fmt.Sprintf("兑现：赚了 %.1f%% 卖出，%s又涨了 %.1f%%，没到「卖早了」的程度", pnl*100, span, a*100)
		}
		return fmt.Sprintf("兑现：赚了 %.1f%% 卖出，%s跌了 %.1f%%，卖在了高位附近", pnl*100, span, math.Abs(a)*100)
	}
	switch {
	case a >= EarlySell:
		return fmt.Sprintf("没兑现：亏 %.1f%% 卖出，%s又涨了 %.1f%%，可能卖在了低位", math.Abs(pnl)*100, span, a*100)
	case a <= 0:
		return fmt.Sprintf("止损有效：亏 %.1f%% 卖出，%s又跌了 %.1f%%，及时卖出少亏了一点", math.Abs(pnl)*100, span, math.Abs(a)*100)
	}
	return fmt.Sprintf("没兑现：亏 %.1f%% 卖出，%s基本持平", math.Abs(pnl)*100, span)
}

func dayText(d string) string {
	if len(d) >= 10 {
		return d[5:]
	}
	return d
}

// Habits counts four operating habits over the last days. The AI can be wrong too: this does not judge
// whether the user listened to it.
func Habits(trades []personal.Trade, notes map[int]personal.TradeNote, closes map[string]Series, last map[string]float64, reasons map[string]Map, today time.Time, days int) []Map {
	since := today.AddDate(0, 0, -days).Format("2006-01-02")
	type chase struct {
		t    personal.Trade
		gain float64
	}
	var chases []chase
	var dips []personal.Trade
	var earlies []chase
	for _, t := range trades {
		if t.Date < since {
			continue
		}
		rows := closes[t.Code]
		note := notes[t.ID]
		if t.Side == "buy" {
			if g := RunUp(rows, t.Date, ChaseDays); g != nil && *g >= ChaseGain {
				chases = append(chases, chase{t, *g})
			}
			if note.QtyBefore > 0 && t.Price < note.AvgBefore {
				dips = append(dips, t)
			}
		} else if a, passed := RetAfter(rows, t.Date, t.Price, AfterDays); a != nil && passed >= AfterDays && *a >= EarlySell {
			earlies = append(earlies, chase{t, *a})
		}
	}
	avgAfter := func(items []personal.Trade) string {
		var vals []float64
		pending := false
		for _, t := range items {
			v, passed := RetAfter(closes[t.Code], t.Date, t.Price, AfterDays)
			if v == nil {
				continue
			}
			if passed >= AfterDays {
				vals = append(vals, *v)
			} else {
				pending = true
			}
		}
		if len(vals) > 0 {
			sum := 0.0
			for _, v := range vals {
				sum += v
			}
			return fmt.Sprintf("%+.1f%%", sum/float64(len(vals))*100)
		}
		if pending {
			return "进行中"
		}
		return "—"
	}
	join := func(parts []string, n int) string {
		if len(parts) > n {
			parts = parts[:n]
		}
		return strings.Join(parts, "；")
	}
	var chaseT, earlyT []personal.Trade
	var chaseD, earlyD, dipD []string
	for _, c := range chases {
		chaseT = append(chaseT, c.t)
		chaseD = append(chaseD, fmt.Sprintf("%s %s 买入前已涨 %.0f%%", c.t.Name, dayText(c.t.Date), c.gain*100))
	}
	for _, c := range earlies {
		earlyT = append(earlyT, c.t)
		earlyD = append(earlyD, fmt.Sprintf("%s %s 卖出后涨了 %.0f%%", c.t.Name, dayText(c.t.Date), c.gain*100))
	}
	for _, t := range dips {
		now := last[t.Code]
		if now == 0 {
			now = t.Price
		}
		dipD = append(dipD, fmt.Sprintf("%s %s %s，目前 %+.1f%%", t.Name, dayText(t.Date), notes[t.ID].Label, (now/t.Price-1)*100))
	}
	var broken, doubt []string
	codes := make([]string, 0, len(reasons))
	for c := range reasons {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		switch str(reasons[c]["status"]) {
		case "broken":
			broken = append(broken, str(reasons[c]["name"]))
		case "doubt":
			doubt = append(doubt, str(reasons[c]["name"]))
		}
	}
	var stuck string
	if len(broken) == 0 {
		var ps []string
		for _, n := range doubt {
			ps = append(ps, n+"的理由目前「存疑」，还没到证伪")
		}
		stuck = join(ps, 2)
	} else {
		var ps []string
		for _, n := range broken {
			ps = append(ps, n+"的理由已不成立，仍在持有")
		}
		stuck = join(ps, 2)
	}
	return []Map{
		{"key": "chase", "title": fmt.Sprintf("追高：买入时 %d 天已涨超过 %s%%", ChaseDays, pct(ChaseGain, 0)), "count": len(chases), "after": avgAfter(chaseT), "detail": join(chaseD, 3)},
		{"key": "early", "title": fmt.Sprintf("卖早了：卖出后 %d 天又涨超过 %s%%", AfterDays, pct(EarlySell, 0)), "count": len(earlies), "after": avgAfter(earlyT), "detail": join(earlyD, 3)},
		{"key": "dip", "title": "越跌越买：浮亏时继续买入", "count": len(dips), "after": avgAfter(dips), "detail": join(dipD, 3)},
		{"key": "stuck", "title": "理由不成立了还拿着", "count": len(broken), "after": "—", "detail": stuck},
	}
}

// ---- hot sectors, suggestions, comparing checkups

// SectorRow is one sector of the market page's tracking.
type SectorRow struct {
	Name    string   `json:"name"`
	Verdict string   `json:"verdict"`
	Flow    *float64 `json:"flow"`
	Heat    *float64 `json:"heat"`
	Source  string   `json:"source"`
	Tracked bool     `json:"tracked"`
}

// Mine is a held stock sitting in a sector, with its weight.
type Mine struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

// HotGroup is a group of sectors with the same story.
type HotGroup struct {
	Title string
	Rows  []HotRow
}

type HotRow struct {
	SectorRow
	Mine []Mine
}

// HotGroups sorts sectors into three groups by whether there is new money and whether it is hot, and
// pairs them with what the user holds.
func HotGroups(sectors []SectorRow, held map[string][]Mine) []HotGroup {
	spec := []struct {
		title string
		pick  func(SectorRow) bool
	}{
		{"有钱又热 · 真热点", func(r SectorRow) bool { return r.Verdict == "三层同向" }},
		{"钱在进、还不热 · 可能是下一个", func(r SectorRow) bool { return r.Verdict == "有钱没热度" }},
		{"看着热、没新钱 · 别当热点追", func(r SectorRow) bool { return r.Verdict == "只热不增" || r.Verdict == "在流出" }},
	}
	var out []HotGroup
	for _, sp := range spec {
		var rows []SectorRow
		for _, r := range sectors {
			if sp.pick(r) {
				rows = append(rows, r)
			}
		}
		if len(rows) == 0 {
			continue
		}
		flow := func(r SectorRow) float64 {
			if r.Flow == nil {
				return -1e9
			}
			return *r.Flow
		}
		sort.SliceStable(rows, func(a, b int) bool { return flow(rows[a]) > flow(rows[b]) })
		top := rows
		if len(top) > HotTop {
			top = top[:HotTop]
		}
		shown := append([]SectorRow(nil), top...)
		for _, r := range rows {
			if len(held[r.Name]) == 0 {
				continue
			}
			in := false
			for _, s := range top {
				if s.Name == r.Name {
					in = true
				}
			}
			if !in {
				shown = append(shown, r)
			}
		}
		group := HotGroup{Title: sp.title}
		for _, r := range shown {
			group.Rows = append(group.Rows, HotRow{SectorRow: r, Mine: held[r.Name]})
		}
		out = append(out, group)
	}
	return out
}

// Suggestions are directions worth a look, as conditions, never as stocks. At most four.
func Suggestions(rows []personal.Row, conc Conc, reasons map[string]Map, order []string, triggers map[string]float64, alerts map[string]int, hot []HotGroup, heldSectors map[string]bool) []Map {
	var out []Map
	for _, code := range order {
		item := reasons[code]
		st := str(item["status"])
		if st == "doubt" || st == "broken" {
			word := "存疑"
			if st == "broken" {
				word = "不成立"
			}
			out = append(out, Map{"key": "recheck", "ts_code": code, "title": "先复核" + str(item["name"]), "text": fmt.Sprintf("理由%s：%s。去诊断看看最新情况。", word, str(item["line"])), "action": "diagnose"})
			break
		}
	}
	var unset []string
	for _, r := range rows {
		if triggers[r.Code] == 0 && alerts[r.Code] == 0 {
			unset = append(unset, r.Name)
		}
	}
	if len(unset) > 0 {
		out = append(out, Map{"key": "alert", "title": fmt.Sprintf("给没设价格条件的%d只补上", len(unset)), "text": strings.Join(unset, "、") + "现在跌多少都不会提醒你。可以按买入理由设一个「理由不成立」的价格。", "action": "alert", "codes": unset})
	}
	for _, f := range conc.Flags {
		if f["level"] == "注意" && strings.Contains(str(f["title"]), "同一个板块") {
			out = append(out, Map{"key": "sector", "title": "新钱先别再放进同一个板块", "text": str(f["title"]) + "。想加仓时，优先看别的板块里有新钱的方向，而不是在这个板块里再买一只。", "action": "market"})
			break
		}
	}
	var newHot []string
	for i, g := range hot {
		if i >= 2 {
			break
		}
		for _, r := range g.Rows {
			if len(r.Mine) == 0 && !heldSectors[r.Name] {
				newHot = append(newHot, r.Name)
			}
		}
	}
	if len(newHot) > 0 {
		show := newHot
		if len(show) > 3 {
			show = show[:3]
		}
		out = append(out, Map{"key": "hot", "title": "有新钱的板块你还没有", "text": strings.Join(show, "、") + "。只指到板块，不推荐具体的票；想找票就去扫描这个板块，挑中的再诊断一遍。", "action": "scan", "sectors": show})
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

// CompareCheckups sets this checkup against the last one: the heaviest sector, how many reasons are in
// doubt, and how many of the problems raised last time were fixed.
func CompareCheckups(now, before Map) Map {
	if before == nil {
		return nil
	}
	var lines []Map
	pt, _ := before["top_sector"].(map[string]any)
	nt, _ := now["top_sector"].(map[string]any)
	if str(pt["name"]) != "" && str(nt["name"]) != "" {
		pw, _ := asFloat(pt["weight"])
		nw, _ := asFloat(nt["weight"])
		note := "没变化"
		if nw < pw-0.005 {
			note = "下降了"
		} else if nw > pw+0.005 {
			note = "上升了"
		}
		if str(pt["name"]) == str(nt["name"]) {
			lines = append(lines, Map{"title": str(nt["name"]) + "占比", "text": fmt.Sprintf("%.1f%% → %.1f%%", pw*100, nw*100), "note": note})
		} else {
			lines = append(lines, Map{"title": "最重的板块", "text": fmt.Sprintf("%s %.1f%% → %s %.1f%%", str(pt["name"]), pw*100, str(nt["name"]), nw*100), "note": note})
		}
	}
	strs := func(v any) []string {
		var out []string
		switch x := v.(type) {
		case []string:
			return x
		case []any:
			for _, i := range x {
				out = append(out, str(i))
			}
		}
		return out
	}
	nowWeak, beforeWeak := strs(now["weak_names"]), strs(before["weak_names"])
	known := map[string]bool{}
	for _, n := range beforeWeak {
		known[n] = true
	}
	var added []string
	for _, n := range nowWeak {
		if !known[n] {
			added = append(added, n)
		}
	}
	sort.Strings(added)
	note := ""
	if len(added) > 0 {
		note = "新增：" + strings.Join(added, "、")
	}
	bw, _ := asFloat(before["weak"])
	nw, _ := asFloat(now["weak"])
	lines = append(lines, Map{"title": "理由存疑 / 不成立", "text": fmt.Sprintf("%d → %d", int(bw), int(nw)), "note": note})
	bi, ni := map[string]bool{}, map[string]bool{}
	for _, i := range strs(before["issues"]) {
		bi[i] = true
	}
	for _, i := range strs(now["issues"]) {
		ni[i] = true
	}
	solved := 0
	var still []string
	for i := range bi {
		if ni[i] {
			still = append(still, i)
		} else {
			solved++
		}
	}
	sort.Strings(still)
	stillNote := ""
	if len(still) > 0 {
		stillNote = "没解决：" + strings.Join(still, "、")
	}
	lines = append(lines, Map{"title": "上次提的问题", "text": fmt.Sprintf("%d 个 · 解决 %d", len(bi), solved), "note": stillNote})
	return Map{"lines": lines}
}

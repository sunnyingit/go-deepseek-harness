package personal

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidDay reports whether text looks like 2026-11-02.
func ValidDay(text string) bool { return dayPattern.MatchString(text) }

func tradeLess(a, b Trade) bool {
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return a.ID < b.ID
}

// heldAfter walks the trades in date order and returns what is left, or a negative number as soon as
// the position would go below zero.
func heldAfter(trades []Trade) int {
	sorted := append([]Trade(nil), trades...)
	sort.SliceStable(sorted, func(a, b int) bool { return tradeLess(sorted[a], sorted[b]) })
	held := 0
	for _, t := range sorted {
		switch t.Side {
		case "adjust": // 校正: this is what is held from here on
			held = t.Qty
		case "buy":
			held += t.Qty
		default:
			held -= t.Qty
		}
		if held < 0 {
			return held
		}
	}
	return held
}

func (st *Store) tradesOf(code string) []Trade {
	var out []Trade
	for _, t := range st.d.Trades {
		if code == "" || t.Code == code {
			out = append(out, t)
		}
	}
	return out
}

// TradeAdd records one trade. A sale larger than what was held at that date is refused.
func (st *Store) TradeAdd(t Trade) (Trade, error) {
	if t.Side != "buy" && t.Side != "sell" {
		return Trade{}, InputError("方向只能是买入或卖出")
	}
	if t.Price <= 0 {
		return Trade{}, InputError("价格要大于 0")
	}
	if t.Qty <= 0 {
		return Trade{}, InputError("数量要是大于 0 的整数")
	}
	if t.Code == "" || !ValidDay(t.Date) {
		return Trade{}, InputError("日期要写成 2026-11-02 这样")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	probe := append(st.tradesOf(t.Code), Trade{Side: t.Side, Qty: t.Qty, Date: t.Date, ID: 1 << 40})
	if heldAfter(probe) < 0 {
		return Trade{}, InputError("卖出的数量比当时手里有的多")
	}
	t.Reason = strings.TrimSpace(t.Reason)
	t.ID, t.CreatedAt = st.id(), st.now().UnixMilli()
	st.d.Trades = append(st.d.Trades, t)
	st.save()
	return t, nil
}

func (st *Store) Trades() []Trade {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]Trade(nil), st.d.Trades...)
}

func (st *Store) TradesOf(code string) []Trade {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.tradesOf(code)
}

func (st *Store) TradeByID(id int) (Trade, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, t := range st.d.Trades {
		if t.ID == id {
			return t, true
		}
	}
	return Trade{}, false
}

// TradeRemove deletes a trade unless a later sale would then exceed what was held.
func (st *Store) TradeRemove(id int) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	idx := -1
	for i, t := range st.d.Trades {
		if t.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return false, nil
	}
	var rest []Trade
	for _, t := range st.tradesOf(st.d.Trades[idx].Code) {
		if t.ID != id {
			rest = append(rest, t)
		}
	}
	if heldAfter(rest) < 0 {
		return false, InputError("删掉这一笔之后，后面的卖出会比手里有的多；先删后面的卖出")
	}
	st.d.Trades = append(st.d.Trades[:idx], st.d.Trades[idx+1:]...)
	st.save()
	return true, nil
}

func (st *Store) SetTradeNote(id int, note string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.d.Trades {
		if st.d.Trades[i].ID == id {
			st.d.Trades[i].Note = strings.TrimSpace(note)
			st.save()
			return true
		}
	}
	return false
}

// ---- wishes

// WishAdd stores a stock the user wants to buy, with the conditions that should be met first.
func (st *Store) WishAdd(w Wish) (Wish, error) {
	w.Reason = strings.TrimSpace(w.Reason)
	if w.Reason == "" {
		return Wish{}, InputError("先写一句为什么想买")
	}
	if w.Match == "" {
		w.Match = "all"
	}
	if w.Match != "all" && w.Match != "any" {
		return Wish{}, InputError("条件只能选全部满足或任一满足")
	}
	if w.PlanPct < 0 || w.PlanPct > 100 {
		return Wish{}, InputError("打算买的仓位要在 0 到 100 之间")
	}
	switch w.Source {
	case "scan", "sold", "diagnose", "manual":
	default:
		w.Source = "manual"
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, x := range st.d.Wishes {
		if x.Code == w.Code && x.Status == "open" {
			return Wish{}, InputError("已经在想买里了")
		}
	}
	if w.Conditions == nil {
		w.Conditions = []Item{}
	}
	if w.Invalid == nil {
		w.Invalid = []Item{}
	}
	w.ID, w.Status, w.CreatedAt = st.id(), "open", st.now().UnixMilli()
	st.d.Wishes = append(st.d.Wishes, w)
	st.save()
	return w, nil
}

// Wishes lists the wishes with a status ("" = all).
func (st *Store) WishesWith(status string) []Wish {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Wish
	for _, w := range st.d.Wishes {
		if status == "" || w.Status == status {
			out = append(out, w)
		}
	}
	return out
}

// Wishes lists the open wishes.
func (st *Store) Wishes() []Wish { return st.WishesWith("open") }

func (st *Store) WishByID(id int) (Wish, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, w := range st.d.Wishes {
		if w.ID == id {
			return w, true
		}
	}
	return Wish{}, false
}

// WishCloseWith closes an open wish as bought or dropped.
func (st *Store) WishCloseWith(id int, status string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.d.Wishes {
		if st.d.Wishes[i].ID == id && st.d.Wishes[i].Status == "open" {
			st.d.Wishes[i].Status, st.d.Wishes[i].ClosedAt = status, st.now().UnixMilli()
			st.save()
			return true
		}
	}
	return false
}

// WishClose drops an open wish.
func (st *Store) WishClose(id int) bool { return st.WishCloseWith(id, "dropped") }

// ---- daily trading plans

var planSides = map[string]bool{"buy": true, "sell": true, "add": true, "reduce": true, "watch": true}
var planStatuses = map[string]bool{"planned": true, "done": true, "skipped": true, "cancelled": true}

// PlanAdd stores one line in a day's trading plan.
func (st *Store) PlanAdd(p PlanItem) (PlanItem, error) {
	p.Reason = strings.TrimSpace(p.Reason)
	p.Note = strings.TrimSpace(p.Note)
	if p.Code == "" || p.Name == "" {
		return PlanItem{}, InputError("先选一只股票")
	}
	if !ValidDay(p.Date) {
		return PlanItem{}, InputError("日期要写成 2026-11-02 这样")
	}
	if !planSides[p.Side] {
		return PlanItem{}, InputError("方向只能是买入、卖出、加仓、减仓或盯着")
	}
	if p.Price < 0 {
		return PlanItem{}, InputError("价格不能是负数")
	}
	if p.Qty < 0 {
		return PlanItem{}, InputError("数量不能是负数")
	}
	if p.PlanPct < 0 || p.PlanPct > 100 {
		return PlanItem{}, InputError("仓位要在 0 到 100 之间")
	}
	if p.Side != "watch" && p.Reason == "" {
		return PlanItem{}, InputError("先写一句今天为什么想做")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	now := st.now().UnixMilli()
	p.ID, p.Status, p.CreatedAt, p.UpdatedAt = st.id(), "planned", now, now
	st.d.Plans = append(st.d.Plans, p)
	st.save()
	return p, nil
}

// PlansOf lists plan items for one day, oldest first.
func (st *Store) PlansOf(day string) []PlanItem {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []PlanItem
	for _, p := range st.d.Plans {
		if p.Date == day {
			out = append(out, p)
		}
	}
	return out
}

// PlansFrom lists plan items on or after day, oldest first.
func (st *Store) PlansFrom(day string) []PlanItem {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []PlanItem
	for _, p := range st.d.Plans {
		if p.Date >= day {
			out = append(out, p)
		}
	}
	return out
}

func (st *Store) PlanByID(id int) (PlanItem, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, p := range st.d.Plans {
		if p.ID == id {
			return p, true
		}
	}
	return PlanItem{}, false
}

// PlanUpdate replaces the editable fields of a plan item.
func (st *Store) PlanUpdate(id int, day, side string, price float64, qty int, planPct float64, reason, note string) (PlanItem, error) {
	reason, note = strings.TrimSpace(reason), strings.TrimSpace(note)
	if day != "" && !ValidDay(day) {
		return PlanItem{}, InputError("日期要写成 2026-11-02 这样")
	}
	if !planSides[side] {
		return PlanItem{}, InputError("方向只能是买入、卖出、加仓、减仓或盯着")
	}
	if price < 0 {
		return PlanItem{}, InputError("价格不能是负数")
	}
	if qty < 0 {
		return PlanItem{}, InputError("数量不能是负数")
	}
	if planPct < 0 || planPct > 100 {
		return PlanItem{}, InputError("仓位要在 0 到 100 之间")
	}
	if side != "watch" && reason == "" {
		return PlanItem{}, InputError("先写一句今天为什么想做")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.d.Plans {
		if st.d.Plans[i].ID != id {
			continue
		}
		p := &st.d.Plans[i]
		if day != "" {
			p.Date = day
		}
		p.Side, p.Price, p.Qty, p.PlanPct, p.Reason, p.Note = side, price, qty, planPct, reason, note
		p.UpdatedAt = st.now().UnixMilli()
		st.save()
		return *p, nil
	}
	return PlanItem{}, InputError("没有这条计划")
}

// PlanSetStatus marks a plan item done / skipped / cancelled / planned again.
func (st *Store) PlanSetStatus(id int, status string) (PlanItem, error) {
	if !planStatuses[status] {
		return PlanItem{}, InputError("状态只能是计划中、已做、没做或取消")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.d.Plans {
		if st.d.Plans[i].ID != id {
			continue
		}
		st.d.Plans[i].Status = status
		st.d.Plans[i].UpdatedAt = st.now().UnixMilli()
		st.save()
		return st.d.Plans[i], nil
	}
	return PlanItem{}, InputError("没有这条计划")
}

// PlanRemove deletes a plan item.
func (st *Store) PlanRemove(id int) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, p := range st.d.Plans {
		if p.ID == id {
			st.d.Plans = append(st.d.Plans[:i], st.d.Plans[i+1:]...)
			st.save()
			return true
		}
	}
	return false
}

// ---- reasons

func (st *Store) ReasonOf(code string) (Reason, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r, ok := st.d.Reasons[code]
	return r, ok
}

// SetReason keeps why a stock was bought. writtenAt "" means today.
func (st *Store) SetReason(code, text string, invalid []Item, writtenAt string) (Reason, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(invalid) == 0 {
		return Reason{}, InputError("理由不能空着")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.d.Reasons == nil {
		st.d.Reasons = map[string]Reason{}
	}
	if writtenAt == "" {
		writtenAt = st.now().In(zone()).Format("2006-01-02")
	}
	if invalid == nil {
		invalid = []Item{}
	}
	r := Reason{Code: code, Text: text, Invalid: invalid, WrittenAt: writtenAt, UpdatedAt: st.now().UnixMilli()}
	st.d.Reasons[code] = r
	st.save()
	return r, nil
}

func (st *Store) DropReason(code string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.d.Reasons[code]; ok {
		delete(st.d.Reasons, code)
		st.save()
	}
}

// ---- rules ("large money has left for N days in a row")

func (st *Store) RulesOf(code string) []Rule {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Rule
	for _, r := range st.d.Rules {
		if code == "" || r.Code == code {
			out = append(out, r)
		}
	}
	return out
}

func (st *Store) AddRule(code, kind string, value float64) (Rule, error) {
	if kind != "flow_out" {
		return Rule{}, InputError("不认识的提醒类型")
	}
	if value <= 0 {
		return Rule{}, InputError("天数要大于 0")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.d.Rules {
		if r.Code == code && r.Kind == kind && r.Value == value {
			return r, nil
		}
	}
	r := Rule{ID: st.id(), Code: code, Kind: kind, Value: value}
	st.d.Rules = append(st.d.Rules, r)
	st.save()
	return r, nil
}

func (st *Store) RemoveRule(id int) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, r := range st.d.Rules {
		if r.ID == id {
			st.d.Rules = append(st.d.Rules[:i], st.d.Rules[i+1:]...)
			st.save()
			return true
		}
	}
	return false
}

// ---- checkups

func (st *Store) SaveCheckup(data Item) Checkup {
	st.mu.Lock()
	defer st.mu.Unlock()
	c := Checkup{ID: st.id(), CreatedAt: st.now().UnixMilli(), Data: data}
	st.d.Checkups = append(st.d.Checkups, c)
	if len(st.d.Checkups) > 50 {
		st.d.Checkups = st.d.Checkups[len(st.d.Checkups)-50:]
	}
	st.save()
	return c
}

// Checkups returns the latest ones, newest first.
func (st *Store) Checkups(limit int) []Checkup {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Checkup
	for i := len(st.d.Checkups) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, st.d.Checkups[i])
	}
	return out
}

// ---- small values (which notifications were seen)

func (st *Store) KVGet(key string, out any) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	raw, ok := st.d.KV[key]
	return ok && json.Unmarshal(raw, out) == nil
}

func (st *Store) KVSet(key string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.d.KV == nil {
		st.d.KV = map[string]json.RawMessage{}
	}
	st.d.KV[key] = raw
	st.save()
}

// ---- alerts tagged by name (the "reason no longer holds" price lines)

// AlertsByName removes the alerts of one stock that carry a tag, and returns how many went.
func (st *Store) DropAlertsByName(code, tag string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	kept := st.d.Alerts[:0]
	n := 0
	for _, a := range st.d.Alerts {
		if a.Code == code && a.Name == tag {
			n++
			continue
		}
		kept = append(kept, a)
	}
	st.d.Alerts = kept
	if n > 0 {
		st.save()
	}
	return n
}

// Export is the whole personal data, as the one-time import from the old Python databases reads it.
type Export struct {
	Trades   []Trade   `json:"trades"`
	Wishes   []Wish    `json:"wishes"`
	Reasons  []Reason  `json:"reasons"`
	Rules    []Rule    `json:"rules"`
	Checkups []Checkup `json:"checkups"`
	Watch    []Watch   `json:"watch"`
	Alerts   []Alert   `json:"alerts"`
}

// Import loads an export, but only into an empty store, so running it twice cannot double the trades.
func (st *Store) Import(e Export) (bool, map[string]int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.d.Trades)+len(st.d.Wishes)+len(st.d.Watch)+len(st.d.Alerts) > 0 {
		return false, nil
	}
	st.d.Trades, st.d.Wishes, st.d.Rules, st.d.Watch, st.d.Alerts = e.Trades, e.Wishes, e.Rules, e.Watch, e.Alerts
	st.d.Checkups = e.Checkups
	st.d.Reasons = map[string]Reason{}
	for _, r := range e.Reasons {
		st.d.Reasons[r.Code] = r
	}
	maxID := 0
	for _, t := range e.Trades {
		maxID = max(maxID, t.ID)
	}
	for _, w := range e.Wishes {
		maxID = max(maxID, w.ID)
	}
	for _, r := range e.Rules {
		maxID = max(maxID, r.ID)
	}
	for _, a := range e.Alerts {
		maxID = max(maxID, a.ID)
	}
	for _, c := range e.Checkups {
		maxID = max(maxID, c.ID)
	}
	if maxID > st.d.Seq {
		st.d.Seq = maxID
	}
	st.save()
	return true, map[string]int{"trades": len(e.Trades), "wishes": len(e.Wishes), "reasons": len(e.Reasons), "rules": len(e.Rules), "checkups": len(e.Checkups), "watch": len(e.Watch), "alerts": len(e.Alerts)}
}

// TradeAdjust corrects what is held in a stock: from that date on it is qty shares at an average cost of avg. It is
// a line in the trade list of its own kind, so the buys and sells before it stay where they are.
func (st *Store) TradeAdjust(code, name string, qty int, avg float64, date string) (Trade, error) {
	if code == "" || !ValidDay(date) {
		return Trade{}, InputError("日期要写成 2026-11-02 这样")
	}
	if qty < 0 {
		return Trade{}, InputError("数量不能是负数")
	}
	if qty > 0 && avg <= 0 {
		return Trade{}, InputError("成本价要大于 0")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	t := Trade{Code: code, Name: name, Side: "adjust", Price: avg, Qty: qty, Date: date}
	if heldAfter(append(st.tradesOf(code), Trade{Side: "adjust", Qty: qty, Date: date, ID: 1 << 40})) < 0 {
		return Trade{}, InputError("校正之后，后面的卖出会比手里有的多；先改后面的卖出")
	}
	t.ID, t.CreatedAt = st.id(), st.now().UnixMilli()
	st.d.Trades = append(st.d.Trades, t)
	st.save()
	return t, nil
}

// TradeUpdate changes the price, quantity and date of a trade that was already written down. The change is refused
// when a later sale would then be larger than what was held.
func (st *Store) TradeUpdate(id int, price float64, qty int, date string) (Trade, error) {
	if !ValidDay(date) {
		return Trade{}, InputError("日期要写成 2026-11-02 这样")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	idx := -1
	for i, t := range st.d.Trades {
		if t.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return Trade{}, InputError("没有这笔记录")
	}
	cur := st.d.Trades[idx]
	if cur.Side == "adjust" {
		if qty < 0 {
			return Trade{}, InputError("数量不能是负数")
		}
		if qty > 0 && price <= 0 {
			return Trade{}, InputError("成本价要大于 0")
		}
	} else {
		if price <= 0 {
			return Trade{}, InputError("价格要大于 0")
		}
		if qty <= 0 {
			return Trade{}, InputError("数量要是大于 0 的整数")
		}
	}
	next := cur
	next.Price, next.Qty, next.Date = price, qty, date
	var probe []Trade
	for _, t := range st.tradesOf(cur.Code) {
		if t.ID == id {
			t = next
		}
		probe = append(probe, t)
	}
	if heldAfter(probe) < 0 {
		return Trade{}, InputError("改成这样之后，后面的卖出会比手里有的多")
	}
	st.d.Trades[idx] = next
	st.save()
	return next, nil
}

// WishUpdate rewrites a wish that is still open: why, the conditions, what would prove it wrong and how much.
func (st *Store) WishUpdate(id int, w Wish) (Wish, error) {
	w.Reason = strings.TrimSpace(w.Reason)
	if w.Reason == "" {
		return Wish{}, InputError("先写一句为什么想买")
	}
	if w.Match == "" {
		w.Match = "all"
	}
	if w.Match != "all" && w.Match != "any" {
		return Wish{}, InputError("条件只能选全部满足或任一满足")
	}
	if w.PlanPct < 0 || w.PlanPct > 100 {
		return Wish{}, InputError("打算买的仓位要在 0 到 100 之间")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.d.Wishes {
		x := &st.d.Wishes[i]
		if x.ID != id || x.Status != "open" {
			continue
		}
		x.Reason, x.Match, x.PlanPct = w.Reason, w.Match, w.PlanPct
		x.Conditions, x.Invalid = w.Conditions, w.Invalid
		if x.Conditions == nil {
			x.Conditions = []Item{}
		}
		if x.Invalid == nil {
			x.Invalid = []Item{}
		}
		st.save()
		return *x, nil
	}
	return Wish{}, InputError("没有这条想买")
}

// Capital is the money the user sets aside for stocks, held and not held; 0 means it was never set.
func (st *Store) Capital() float64 {
	var v float64
	if !st.KVGet("capital", &v) || v < 0 {
		return 0
	}
	return v
}

func (st *Store) SetCapital(v float64) error {
	if v < 0 {
		return InputError("总资金不能是负数")
	}
	st.KVSet("capital", v)
	return nil
}

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
		if t.Side == "buy" {
			held += t.Qty
		} else {
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

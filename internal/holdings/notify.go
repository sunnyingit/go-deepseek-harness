package holdings

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go-harness/internal/inbox"
)

// inboxKinds maps an inbox event type to the drawer's category.
var inboxKinds = map[string]string{
	"price.alert.fired": "alert", "price.move": "move", "limit.hit": "move", "price.extreme": "move", "flow.streak": "move",
	"judgment.changed": "change", "reason.status_changed": "change", "wish.condition_met": "change",
	"announcement.published": "ann", "news.item": "ann", "calendar.upcoming": "upcoming", "outcome.settled": "settle", "scan.settled": "settle", "custom.hit": "alert", "custom.answer": "ann", "custom.analysis": "ann", "quality.redline": "alert", "quality.picks": "ann",
	"task.failed": "system", "task.overdue": "system", "task.completed": "system", "digest.summary": "system",
	"market.brief": "system",
}

func isSystem(t string) bool {
	return t == "task.failed" || t == "task.overdue" || t == "task.completed" || t == "digest.summary" || t == "market.brief"
}

const inboxPrefix = "inbox:"

func (c *Ctx) groupOf(t time.Time) string {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, c.Today.Location())
	today := time.Date(c.Today.Year(), c.Today.Month(), c.Today.Day(), 0, 0, 0, 0, c.Today.Location())
	days := int(today.Sub(day).Hours() / 24)
	switch {
	case days <= 0:
		return "今天"
	case days == 1:
		return "昨天"
	}
	return "更早"
}

// inboxRows are the inbox items the holdings drawer shows: what concerns the followed stocks, plus system notices.
func (c *Ctx) inboxRows() []inbox.Item {
	if c.Inbox == nil {
		return nil
	}
	names := c.Names()
	for _, w := range c.Store.WatchList() { // a stock that is only on the watchlist is followed too
		if names[w.Code] == "" {
			names[w.Code] = w.Name
		}
	}
	var out []inbox.Item
	for _, it := range c.Inbox.List("", false, 120) {
		kind, ok := inboxKinds[it.Type]
		if !ok {
			continue
		}
		// system + scan 战绩汇总 always shown; custom/quality too; other stock events only for followed codes
		if kind == "system" || kind == "settle" || strings.HasPrefix(it.Type, "custom.") || strings.HasPrefix(it.Type, "quality.") || (it.Group == "stock" && names[it.SubjectID] != "") {
			out = append(out, it)
		}
	}
	return out
}

func (c *Ctx) inboxView(it inbox.Item) Map {
	created := time.UnixMilli(it.CreatedAt).In(c.Today.Location())
	group := c.groupOf(created)
	lines := strings.Split(it.Body, "\n")
	if it.Type == "task.completed" || it.Type == "task.failed" {
		// Notification drawer: conclusion only — drop "调用：…" run-input lines from older receipts too.
		filtered := lines[:0]
		for _, ln := range lines {
			if strings.HasPrefix(ln, "调用：") {
				continue
			}
			filtered = append(filtered, ln)
		}
		lines = filtered
	}
	sub := ""
	if len(lines) > 1 {
		end := len(lines)
		max := 3
		if isSystem(it.Type) || inboxKinds[it.Type] == "settle" {
			max = 8 // task receipts / digests / market briefs / settle lines carry the result
		}
		if end > max {
			end = max
		}
		sub = strings.Join(lines[1:end], "\n")
	}
	when := created.Format("01-02 15:04")
	if group == "今天" {
		when = created.Format("15:04")
	}
	code := it.SubjectID
	if isSystem(it.Type) || it.Type == "scan.settled" {
		code = ""
	}
	tab := ""
	if it.Link != nil {
		tab = it.Link["tab"]
	}
	return Map{"id": inboxPrefix + it.ID, "kind": inboxKinds[it.Type], "group": group, "title": it.Title, "text": lines[0], "sub": sub, "tone": it.Tone, "code": code,
		"day": created.Format("2006-01-02"), "when": when, "time": created.Format("15:04"), "unread": it.ReadAt == 0, "link": it.Link, "tab": tab, "severity": it.Severity}
}

// Notifications is what the holdings drawer shows: what the page works out itself (a reached alert, a
// changed judgment, a settled call, something coming up) plus the inbox. The inbox wins when both say
// the same thing.
func (c *Ctx) Notifications() Map {
	var items []Map
	today := c.todayISO()
	for _, code := range c.sortedNames() {
		name := c.Names()[code]
		price := c.Price(code)
		for _, a := range c.Alerts(code) {
			if v := alertView(a, price); v["reached"] == true {
				items = append(items, Map{"id": fmt.Sprintf("alert:%d:%s", a.ID, today), "kind": "alert", "group": "今天", "title": "到点提醒", "text": fmt.Sprintf("%s 跌到 %.2f", name, a.Price),
					"sub": fmt.Sprintf("你设的提醒价已触发 · 现价 %.2f", price), "code": code, "day": today, "when": "今天"})
			}
		}
		if ai := c.AI(code); ai.Change != "" {
			day := orDefault(ai.TradeDate, today)
			when := ai.JudgedAt
			if len(when) > 5 {
				when = when[len(when)-5:]
			}
			items = append(items, Map{"id": fmt.Sprintf("change:%s:%s", code, ai.TradeDate), "kind": "change", "group": "今天", "title": "判断变化", "text": name + " · " + ai.Change,
				"sub": ai.Headline, "code": code, "day": day, "when": when})
		}
		cut := c.Today.AddDate(0, 0, -9).Format("2006-01-02")
		for _, e := range c.entries(code) {
			o := e["outcome"].(Map)
			if (o["state"] == "hit" || o["state"] == "miss") && e["date"].(string) >= cut {
				d := e["date"].(string)
				items = append(items, Map{"id": fmt.Sprintf("settle:%s:%s", code, d), "kind": "settle", "group": "昨天", "title": "战绩结算", "text": name + " · 研判到期", "sub": o["text"],
					"code": code, "day": d, "when": dayText(d)})
			}
		}
	}
	for _, row := range c.Data.Calendar(c.Names()) {
		items = append(items, Map{"id": fmt.Sprintf("cal:%s:%s", row.Date, row.Text), "kind": "upcoming", "group": "即将发生", "title": "即将发生", "text": row.Text, "sub": row.Tag,
			"code": "", "day": row.Date, "when": dayText(row.Date)})
	}
	var seenList []string
	c.Store.KVGet("notif_seen", &seenList)
	seen := map[string]bool{}
	for _, id := range seenList {
		seen[id] = true
	}
	for _, it := range items {
		it["unread"] = !seen[it["id"].(string)]
	}
	items = c.mergeInbox(items)
	order := map[string]int{"今天": 0, "即将发生": 1, "昨天": 2, "更早": 3}
	sort.SliceStable(items, func(a, b int) bool {
		ga, gb := order[items[a]["group"].(string)], order[items[b]["group"].(string)]
		if ga != gb {
			return ga < gb
		}
		if items[a]["group"] == "即将发生" {
			return str(items[a]["day"]) < str(items[b]["day"])
		}
		return false
	})
	unread := 0
	for _, it := range items {
		if it["unread"] == true {
			unread++
		}
	}
	if items == nil {
		items = []Map{}
	}
	return Map{"items": items, "unread": unread}
}

func (c *Ctx) mergeInbox(items []Map) []Map {
	rows := c.inboxRows()
	if len(rows) == 0 {
		return items
	}
	fired, changed := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		switch r.Type {
		case "price.alert.fired":
			fired[fmt.Sprint(r.Payload["alert_id"])] = true
		case "judgment.changed":
			changed[r.SubjectID+"|"+fmt.Sprint(r.Payload["trade_date"])] = true
		}
	}
	var kept []Map
	for _, it := range items {
		id := it["id"].(string)
		parts := strings.Split(id, ":")
		if it["kind"] == "alert" && len(parts) > 1 && fired[parts[1]] {
			continue
		}
		if it["kind"] == "change" && changed[str(it["code"])+"|"+parts[len(parts)-1]] {
			continue
		}
		kept = append(kept, it)
	}
	for _, r := range rows {
		kept = append(kept, c.inboxView(r))
	}
	return kept
}

// MarkRead marks notifications as seen. Empty ids means everything currently listed;
// otherwise only those drawer ids (page-local or "inbox:…") are marked.
func (c *Ctx) MarkRead(ids []string) int {
	if len(ids) == 0 {
		data := c.Notifications()
		items, _ := data["items"].([]Map)
		all := make([]string, 0, len(items))
		for _, it := range items {
			if id, _ := it["id"].(string); id != "" {
				all = append(all, id)
			}
		}
		return c.markReadIDs(all)
	}
	return c.markReadIDs(ids)
}

func (c *Ctx) markReadIDs(ids []string) int {
	if len(ids) == 0 {
		return 0
	}
	var own, inboxIDs []string
	for _, id := range ids {
		if strings.HasPrefix(id, inboxPrefix) {
			inboxIDs = append(inboxIDs, strings.TrimPrefix(id, inboxPrefix))
		} else {
			own = append(own, id)
		}
	}
	n := 0
	if len(own) > 0 {
		var seen []string
		c.Store.KVGet("notif_seen", &seen)
		set := map[string]bool{}
		for _, id := range append(seen, own...) {
			set[id] = true
		}
		merged := make([]string, 0, len(set))
		for id := range set {
			merged = append(merged, id)
		}
		sort.Strings(merged)
		if len(merged) > 300 {
			merged = merged[len(merged)-300:]
		}
		c.Store.KVSet("notif_seen", merged)
		n += len(own)
	}
	if c.Inbox != nil && len(inboxIDs) > 0 {
		n += c.Inbox.MarkRead(inboxIDs, false)
	}
	return n
}

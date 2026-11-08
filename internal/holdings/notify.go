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
	"announcement.published": "ann", "news.item": "ann", "calendar.upcoming": "upcoming", "outcome.settled": "settle", "scan.settled": "settle", "custom.hit": "alert", "custom.answer": "ann",
	"task.failed": "system", "task.overdue": "system", "task.completed": "system", "digest.summary": "system",
}

func isSystem(t string) bool {
	return t == "task.failed" || t == "task.overdue" || t == "task.completed" || t == "scan.settled"
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
	var out []inbox.Item
	for _, it := range c.Inbox.List("", false, 120) {
		if _, ok := inboxKinds[it.Type]; !ok {
			continue
		}
		if isSystem(it.Type) || (it.Group == "stock" && names[it.SubjectID] != "") {
			out = append(out, it)
		}
	}
	return out
}

func (c *Ctx) inboxView(it inbox.Item) Map {
	created := time.UnixMilli(it.CreatedAt).In(c.Today.Location())
	group := c.groupOf(created)
	lines := strings.Split(it.Body, "\n")
	sub := ""
	if len(lines) > 1 {
		end := len(lines)
		if end > 3 {
			end = 3
		}
		sub = strings.Join(lines[1:end], "\n")
	}
	when := created.Format("01-02 15:04")
	if group == "今天" {
		when = created.Format("15:04")
	}
	code := it.SubjectID
	if isSystem(it.Type) {
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

// MarkRead marks everything seen: the page's own items by remembering their ids, the inbox items directly.
func (c *Ctx) MarkRead() int {
	data := c.Notifications()
	items, _ := data["items"].([]Map)
	var own []string
	for _, it := range items {
		if id := it["id"].(string); !strings.HasPrefix(id, inboxPrefix) {
			own = append(own, id)
		}
	}
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
	if c.Inbox != nil {
		var ids []string
		for _, r := range c.inboxRows() {
			ids = append(ids, r.ID)
		}
		c.Inbox.MarkRead(ids, false)
	}
	return len(items)
}

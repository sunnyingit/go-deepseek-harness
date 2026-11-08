// Package inbox is the client's notification inbox: every notable thing (a task receipt, a changed
// judgment, a price alert) becomes one item here. Items are de-duplicated by key, shown in the pages'
// notification drawers, and the ones that deserve it are handed to the desktop shell to pop up.
package inbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Item is one notification.
type Item struct {
	ID           string            `json:"notif_id"`
	Type         string            `json:"type"`
	Group        string            `json:"group"` // stock | market | system
	Severity     string            `json:"severity"`
	Tone         string            `json:"tone"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	SubjectKind  string            `json:"subject_kind"`
	SubjectID    string            `json:"subject_id"`
	Link         map[string]string `json:"link,omitempty"`
	Payload      map[string]any    `json:"payload,omitempty"`
	DedupeKey    string            `json:"dedupe_key"`
	TradeDate    string            `json:"trade_date,omitempty"`
	CreatedAt    int64             `json:"created_at"`
	ReadAt       int64             `json:"read_at,omitempty"`
	Desktop      string            `json:"desktop,omitempty"` // pending | shown
	DesktopAfter int64             `json:"desktop_after,omitempty"`
	Suppressed   string            `json:"suppressed,omitempty"` // why it did not bother anyone: info_only, collapsed, digest, expired, disabled, quiet…
	DigestID     string            `json:"digest_id,omitempty"`
}

// Type describes one kind of event: how it looks and how loud it is.
type Type struct {
	Severity string // info | normal | urgent
	Tone     string // good | warn | bad | flat
	Group    string
	Render   func(map[string]any) (title, body string)
	Link     func(map[string]any) map[string]string
	ToneFn   func(map[string]any) string
	TTL      time.Duration // an event older than this is not pushed any more (it stays in the inbox)
}

// Registry maps an event type to its description. Adding a notification = adding a row here.
var Registry = map[string]Type{}

func str(p map[string]any, k string) string {
	switch v := p[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// flt reads a number however it got into the payload (an int from Go code, a float64 after a JSON round trip).
func flt(p map[string]any, k string) (float64, bool) {
	switch v := p[k].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func num(p map[string]any, k string) string {
	if v, ok := flt(p, k); ok {
		return fmt.Sprintf("%.2f", v)
	}
	return "-"
}

func name(p map[string]any) string {
	if v := str(p, "name"); v != "" {
		return v
	}
	return str(p, "ts_code")
}

func stockLink(page, tab string) func(map[string]any) map[string]string {
	return func(p map[string]any) map[string]string {
		return map[string]string{"page": page, "tab": tab, "code": str(p, "ts_code")}
	}
}

func init() {
	Registry["judgment.changed"] = Type{TTL: 7 * 24 * time.Hour, Severity: "normal", Tone: "warn", Group: "stock",
		Render: func(p map[string]any) (string, string) {
			return name(p) + " 判断变了", strings.TrimRight(name(p)+"："+str(p, "headline"), "：")
		}, Link: stockLink("diagnose", "")}
	Registry["price.alert.fired"] = Type{TTL: 24 * time.Hour, Severity: "urgent", Tone: "warn", Group: "stock",
		Render: func(p map[string]any) (string, string) {
			side := "涨到"
			if str(p, "side") == "down" {
				side = "跌到"
			}
			how := "现价"
			if str(p, "confirm") == "close" {
				how = "收盘价"
			}
			body := fmt.Sprintf("%s（%s）%s %s，已%s设定的 %s。", name(p), str(p, "ts_code"), how, num(p, "observed_price"), side, num(p, "trigger_price"))
			if note := str(p, "note"); note != "" {
				body += "\n备注：" + note
			}
			return fmt.Sprintf("%s %s %s", name(p), side, num(p, "trigger_price")), body
		}, Link: stockLink("holdings", "now")}
	Registry["price.move"] = Type{TTL: 24 * time.Hour, Severity: "normal", Tone: "warn", Group: "stock",
		Render: func(p map[string]any) (string, string) {
			pct, _ := flt(p, "pct")
			sign := "涨"
			if pct < 0 {
				sign, pct = "跌", -pct
			}
			return fmt.Sprintf("%s 较昨收%s %.1f%%", name(p), sign, pct),
				fmt.Sprintf("%s（%s）现价 %s，较昨收%s了 %.1f%%。", name(p), str(p, "ts_code"), num(p, "observed_price"), sign, pct)
		}, Link: stockLink("holdings", "now")}
	Registry["market.brief"] = Type{TTL: 24 * time.Hour, Severity: "normal", Tone: "flat", Group: "market",
		Render: func(p map[string]any) (string, string) {
			t := str(p, "title")
			if t == "" {
				t = "市场简报"
			}
			return t, str(p, "text")
		}, Link: func(map[string]any) map[string]string { return map[string]string{"page": "market"} }}
	Registry["task.failed"] = Type{TTL: 3 * 24 * time.Hour, Severity: "normal", Tone: "bad", Group: "system",
		Render: func(p map[string]any) (string, string) {
			t := str(p, "title")
			if t == "" {
				t = str(p, "task_id")
			}
			return "定时任务出错：" + t, strings.TrimSpace(fmt.Sprintf("%v/%v 个单元失败。%s", p["dead"], p["total"], str(p, "first_error")))
		}, Link: func(p map[string]any) map[string]string {
			return map[string]string{"page": "sched", "run_id": str(p, "run_id")}
		}}
	Registry["task.completed"] = Type{TTL: 7 * 24 * time.Hour, Severity: "info", Tone: "good", Group: "system",
		ToneFn: func(p map[string]any) string {
			if str(p, "state") == "succeeded" {
				return "good"
			}
			return "warn"
		},
		Render: func(p map[string]any) (string, string) {
			t := str(p, "title")
			if t == "" {
				t = str(p, "task_id")
			}
			secs := "-"
			if d, ok := flt(p, "duration_ms"); ok {
				secs = fmt.Sprintf("%.1fs", d/1000)
			}
			line := fmt.Sprintf("本机 · %s · %s · 用时 %s", str(p, "state_text"), str(p, "trigger_text"), secs)
			if jobs, _ := flt(p, "jobs"); jobs > 0 {
				line += fmt.Sprintf(" · 单元 %v/%v", p["succeeded"], p["jobs"])
			}
			lines := []string{line}
			for _, k := range []string{"summary", "note"} {
				if v := str(p, k); v != "" {
					lines = append(lines, v)
				}
			}
			return "定时任务" + str(p, "state_text") + "：" + t, strings.Join(lines, "\n")
		}, Link: func(p map[string]any) map[string]string {
			return map[string]string{"page": "sched", "run_id": str(p, "run_id")}
		}}
}

func list(p map[string]any, k string) []string {
	var out []string
	switch v := p[k].(type) {
	case []string:
		return v
	case []any:
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
	}
	return out
}

func init() {
	Registry["scan.settled"] = Type{TTL: 3 * 24 * time.Hour, Severity: "normal", Tone: "good", Group: "system",
		Link: func(map[string]any) map[string]string { return map[string]string{"page": "scan", "tab": "done"} },
		Render: func(p map[string]any) (string, string) {
			n, _ := flt(p, "n")
			hit, _ := flt(p, "hit")
			title := fmt.Sprintf("战绩结算：今天到期 %d 条", int(n))
			if d, _ := flt(p, "decided"); d > 0 {
				title += fmt.Sprintf("，对了 %.1f%%", hit/d*100)
			}
			lines := list(p, "lines")
			return title, strings.Join(lines, "\n")
		}}
	Registry["custom.hit"] = Type{TTL: 24 * time.Hour, Severity: "normal", Tone: "blue", Group: "stock", Link: stockLink("diagnose", ""),
		Render: func(p map[string]any) (string, string) {
			return str(p, "task") + " · " + str(p, "name") + " 符合条件", strings.Join(list(p, "why"), "\n")
		}}
	Registry["custom.answer"] = Type{TTL: 24 * time.Hour, Severity: "normal", Tone: "flat", Group: "stock", Link: stockLink("diagnose", ""),
		Render: func(p map[string]any) (string, string) {
			return str(p, "task") + " · " + str(p, "name"), str(p, "text")
		}}
	day := 24 * time.Hour
	Registry["announcement.published"] = Type{TTL: 3 * day, Severity: "normal", Tone: "warn", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			topic := str(p, "topic")
			if topic == "" {
				topic = str(p, "category")
			}
			if topic == "" {
				topic = "公告"
			}
			body := str(p, "title") + "\n" + strings.TrimSpace(str(p, "source")+" "+str(p, "published_at"))
			if m := list(p, "matched"); len(m) > 0 {
				body += "\n标题提到：" + strings.Join(m, "、")
			}
			if extra, _ := flt(p, "extra"); extra > 0 {
				body += fmt.Sprintf("\n还有 %d 条：%s", int(extra), strings.Join(list(p, "others"), "；"))
			}
			return name(p) + " · " + topic, body
		}}
	Registry["limit.hit"] = Type{TTL: day, Severity: "normal", Tone: "warn", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			word := map[string]string{"limit_up": "涨停", "limit_down": "跌停", "limit_open": "炸板"}[str(p, "kind")]
			if word == "" {
				word = str(p, "kind")
			}
			return name(p) + " " + word, fmt.Sprintf("%s（%s）%s %s，收盘 %s。", name(p), str(p, "ts_code"), str(p, "trade_date"), word, num(p, "close"))
		}}
	Registry["price.extreme"] = Type{TTL: 3 * day, Severity: "info", Tone: "flat", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			days, _ := flt(p, "window_days")
			word := fmt.Sprintf("%d 日新低", int(days))
			if str(p, "kind") == "high" {
				word = fmt.Sprintf("%d 日新高", int(days))
			}
			return name(p) + " 创" + word, fmt.Sprintf("%s（%s）收盘 %s，创%s。", name(p), str(p, "ts_code"), num(p, "close"), word)
		}}
	Registry["flow.streak"] = Type{TTL: 3 * day, Severity: "normal", Tone: "flat", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			word := "流出"
			if str(p, "direction") == "in" {
				word = "流入"
			}
			days, _ := flt(p, "days")
			return fmt.Sprintf("%s 大资金连续%d日净%s", name(p), int(days), word), fmt.Sprintf("%s（%s）大资金已连续 %d 个交易日净%s。", name(p), str(p, "ts_code"), int(days), word)
		}}
	Registry["calendar.upcoming"] = Type{TTL: 7 * day, Severity: "normal", Tone: "flat", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			return name(p) + " · " + str(p, "text"), fmt.Sprintf("%s（%s）%s：%s", name(p), str(p, "ts_code"), str(p, "date"), str(p, "text"))
		}}
	Registry["outcome.settled"] = Type{TTL: 30 * day, Severity: "info", Tone: "flat", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			word := "没对"
			if h, _ := p["hit"].(bool); h {
				word = "对了"
			}
			ret, _ := flt(p, "ret")
			return name(p) + " 战绩出来了", fmt.Sprintf("%s 的判断，5 个交易日后%s（%.1f%%）。", str(p, "entry_date"), word, ret*100)
		}}
	Registry["reason.status_changed"] = Type{TTL: 7 * day, Severity: "normal", Tone: "warn", Group: "stock", Link: stockLink("holdings", "now"),
		Render: func(p map[string]any) (string, string) {
			body := str(p, "line")
			if body == "" {
				body = fmt.Sprintf("%s 的买入理由状态变成了「%s」。", name(p), str(p, "to"))
			}
			return name(p) + " 买入理由" + str(p, "to"), body
		}}
	Registry["wish.condition_met"] = Type{TTL: 7 * day, Severity: "normal", Tone: "good", Group: "stock", Link: stockLink("holdings", "buy"),
		Render: func(p map[string]any) (string, string) {
			body := strings.Join(list(p, "met"), "、")
			if body == "" {
				body = name(p) + " 满足了想买的条件。"
			}
			return name(p) + " 想买的条件到了", body
		}}
	Registry["digest.summary"] = Type{TTL: 3 * 24 * time.Hour, Severity: "normal", Tone: "flat", Group: "system",
		Render: func(p map[string]any) (string, string) {
			n := 0
			if counts, ok := p["counts"].(map[string]any); ok {
				for _, v := range counts {
					if f, ok := v.(float64); ok {
						n += int(f)
					} else if i, ok := v.(int); ok {
						n += i
					}
				}
			}
			var lines []string
			switch v := p["items"].(type) {
			case []string:
				lines = v
			case []any:
				for _, x := range v {
					lines = append(lines, fmt.Sprint(x))
				}
			}
			if n == 0 {
				n = len(lines)
			}
			label := str(p, "period_label")
			if label == "" {
				label = "摘要"
			}
			if len(lines) > 20 {
				lines = lines[:20]
			}
			return fmt.Sprintf("%s：%d 条", label, n), "· " + strings.Join(lines, "\n· ")
		}}
}

// Inbox is the store. One JSON file, capped, safe for concurrent use.
type Inbox struct {
	mu         sync.Mutex
	path       string
	now        func() time.Time
	Items      []*Item     `json:"items"`
	Seq        int         `json:"seq"`
	Config     *Settings   `json:"settings,omitempty"`
	Deliveries []*Delivery `json:"deliveries,omitempty"`
	// Senders deliver to the outside channels (WeCom, Bark, ServerChan). Tests replace them.
	Senders map[string]Sender
	// AutoDispatch makes Publish start delivering right away; the minute task covers the rest.
	AutoDispatch bool
}

const maxItems = 1000

type persisted struct {
	Items      []*Item     `json:"items"`
	Seq        int         `json:"seq"`
	Config     *Settings   `json:"settings,omitempty"`
	Deliveries []*Delivery `json:"deliveries,omitempty"`
}

func Open(path string, now func() time.Time) *Inbox {
	if now == nil {
		now = time.Now
	}
	in := &Inbox{path: path, now: now, Senders: DefaultSenders()}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var loaded persisted
			if json.Unmarshal(raw, &loaded) == nil {
				in.Items, in.Seq, in.Config, in.Deliveries = loaded.Items, loaded.Seq, loaded.Config, loaded.Deliveries
			}
		}
	}
	return in
}

func (in *Inbox) save() {
	if in.path == "" {
		return
	}
	raw, err := json.Marshal(persisted{in.Items, in.Seq, in.Config, in.Deliveries})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(in.path), 0o600|0o700)
	tmp := in.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, in.path)
	}
}

// Options of Publish.
type Options struct {
	TradeDate string
	NoDesktop bool // inbox only, never pop up
	Local     bool // the outside channels were already served elsewhere (the server): desktop only
	Severity  string
	// OccurredAt is when it really happened (ms); an old event is not pushed.
	OccurredAt int64
}

// Publish writes one item unless an item with the same dedupe key exists, then decides who to bother.
// created is false for a duplicate.
func (in *Inbox) Publish(typ, subjectKind, subjectID string, payload map[string]any, dedupe string, opt Options) (*Item, bool, error) {
	spec, ok := Registry[typ]
	if !ok {
		return nil, false, fmt.Errorf("未知的事件类型：%s", typ)
	}
	in.mu.Lock()
	item, created, due := in.publish(spec, typ, subjectKind, subjectID, payload, dedupe, opt)
	auto := in.AutoDispatch && due
	in.mu.Unlock()
	if auto {
		go in.Dispatch()
	}
	return item, created, nil
}

func (in *Inbox) publish(spec Type, typ, subjectKind, subjectID string, payload map[string]any, dedupe string, opt Options) (*Item, bool, bool) {
	if dedupe != "" {
		for _, it := range in.Items {
			if it.DedupeKey == dedupe && it.Type == typ {
				return it, false, false
			}
		}
	}
	now := in.now()
	title, body := spec.Render(payload)
	tone := spec.Tone
	if spec.ToneFn != nil {
		tone = spec.ToneFn(payload)
	}
	var link map[string]string
	if spec.Link != nil {
		link = spec.Link(payload)
	}
	severity := spec.Severity
	if opt.Severity != "" {
		severity = opt.Severity
	}
	occurred := opt.OccurredAt
	if occurred == 0 {
		occurred = now.UnixMilli()
	}
	in.Seq++
	it := &Item{ID: fmt.Sprintf("n%d", in.Seq), Type: typ, Group: spec.Group, Severity: severity, Tone: tone, Title: title, Body: body,
		SubjectKind: subjectKind, SubjectID: subjectID, Link: link, Payload: payload, DedupeKey: dedupe, TradeDate: opt.TradeDate,
		CreatedAt: now.UnixMilli()}
	collapsed := false
	if rank(severity) == 1 && spec.Group == "stock" {
		window := time.Duration(in.settings().CollapseS) * time.Second
		for _, other := range in.Items {
			if other.Type == typ && other.SubjectID == subjectID && other.CreatedAt >= now.Add(-window).UnixMilli() {
				collapsed = true
				break
			}
		}
	}
	plan := in.decide(it, spec, now, occurred, collapsed, opt)
	it.Suppressed = plan.suppressed
	due := false
	for _, d := range plan.deliver {
		if d.channel.Type == "desktop" {
			it.Desktop, it.DesktopAfter = "pending", d.after
			continue
		}
		in.Deliveries = append(in.Deliveries, &Delivery{ID: fmt.Sprintf("d%d", in.nextSeq()), ItemID: it.ID, Channel: d.channel.ID, State: "pending", After: d.after})
		due = due || d.after <= now.UnixMilli()
	}
	in.Items = append(in.Items, it)
	if len(in.Items) > maxItems {
		in.Items = in.Items[len(in.Items)-maxItems:]
	}
	in.save()
	return it, true, due
}

func (in *Inbox) nextSeq() int { in.Seq++; return in.Seq }

// List returns items newest first. group "" means all; unreadOnly narrows to unread.
func (in *Inbox) List(group string, unreadOnly bool, limit int) []Item {
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []Item
	for i := len(in.Items) - 1; i >= 0; i-- {
		it := in.Items[i]
		if (group != "" && it.Group != group) || (unreadOnly && it.ReadAt != 0) {
			continue
		}
		out = append(out, *it)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (in *Inbox) Unread() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	n := 0
	for _, it := range in.Items {
		if it.ReadAt == 0 {
			n++
		}
	}
	return n
}

// MarkRead marks the given ids (or everything when all is true) and returns how many changed.
func (in *Inbox) MarkRead(ids []string, all bool) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	n := 0
	now := in.now().UnixMilli()
	for _, it := range in.Items {
		if it.ReadAt == 0 && (all || want[it.ID]) {
			it.ReadAt = now
			n++
		}
	}
	if n > 0 {
		in.save()
	}
	return n
}

// DesktopPending returns the items the desktop shell has not shown yet, as the shell expects them.
func (in *Inbox) DesktopPending() []map[string]any {
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []map[string]any
	for _, it := range in.Items {
		if it.Desktop == "pending" && it.DesktopAfter <= in.now().UnixMilli() {
			out = append(out, map[string]any{"delivery_id": it.ID, "title": it.Title, "body": it.Body, "severity": it.Severity, "link": it.Link})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a]["delivery_id"].(string) < out[b]["delivery_id"].(string) })
	return out
}

func (in *Inbox) DesktopAck(ids []string) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	n := 0
	for _, it := range in.Items {
		if it.Desktop == "pending" && want[it.ID] {
			it.Desktop = "shown"
			n++
		}
	}
	if n > 0 {
		in.save()
	}
	return n
}

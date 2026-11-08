package inbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTP is what the senders use; tests replace it.
var HTTP = &http.Client{Timeout: 6 * time.Second}

// Delivery is one item waiting to go to one outside channel.
type Delivery struct {
	ID       string `json:"delivery_id"`
	ItemID   string `json:"notif_id"`
	Channel  string `json:"channel_id"`
	State    string `json:"state"` // pending | sent | dead
	After    int64  `json:"deliver_after"`
	Attempts int    `json:"attempts"`
	Error    string `json:"error,omitempty"`
	SentAt   int64  `json:"sent_at,omitempty"`
}

// Sender pushes one notification to one channel. Returning PermanentError means retrying cannot help.
type Sender func(Item, Channel) error

type PermanentError struct{ Msg string }

func (e PermanentError) Error() string { return e.Msg }

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

func postJSON(endpoint string, body any) (map[string]any, error) {
	raw, _ := json.Marshal(body)
	resp, err := HTTP.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if resp.StatusCode >= 400 {
		if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
			return out, PermanentError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}
		return out, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return out, nil
}

func sendWeCom(it Item, c Channel) error {
	if c.Secret == "" {
		return PermanentError{"没有配置企业微信 webhook"}
	}
	out, err := postJSON(c.Secret, map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": "**" + it.Title + "**\n" + clip(it.Body, 500)}})
	if err != nil {
		return err
	}
	if code, ok := out["errcode"].(float64); ok && code != 0 {
		return PermanentError{fmt.Sprint(out["errmsg"])}
	}
	return nil
}

func sendBark(it Item, c Channel) error {
	if c.Secret == "" {
		return PermanentError{"没有配置 Bark key"}
	}
	server := strings.TrimRight(c.Config["server"], "/")
	if server == "" {
		server = "https://api.day.app"
	}
	out, err := postJSON(server+"/"+c.Secret, map[string]string{"title": it.Title, "body": clip(it.Body, 500), "group": "pabuddy"})
	if err != nil {
		return err
	}
	if code, ok := out["code"].(float64); ok && code != 200 {
		return PermanentError{fmt.Sprint(out["message"])}
	}
	return nil
}

func sendServerChan(it Item, c Channel) error {
	if c.Secret == "" {
		return PermanentError{"没有配置 Server酱 SendKey"}
	}
	base := strings.TrimRight(c.Config["server"], "/")
	if base == "" {
		base = "https://sctapi.ftqq.com"
	}
	resp, err := HTTP.PostForm(base+"/"+c.Secret+".send", url.Values{"title": {it.Title}, "desp": {clip(it.Body, 1000)}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if code, ok := out["code"].(float64); ok && code != 0 {
		return PermanentError{fmt.Sprint(out["message"])}
	}
	return nil
}

func DefaultSenders() map[string]Sender {
	return map[string]Sender{"wecom_webhook": sendWeCom, "bark": sendBark, "serverchan": sendServerChan}
}

var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}

const maxAttempts = 3

// Dispatch sends the deliveries that are due. A failure is retried with a growing wait; a permanent
// failure or the third failure ends it. The desktop is not here: the shell comes to fetch its own.
func (in *Inbox) Dispatch() map[string]int {
	out := map[string]int{"sent": 0, "failed": 0, "dead": 0, "waiting": 0}
	in.mu.Lock()
	now := in.now().UnixMilli()
	type job struct {
		d  *Delivery
		it Item
		ch Channel
	}
	var jobs []job
	for _, d := range in.Deliveries {
		if d.State != "pending" {
			continue
		}
		if d.After > now {
			out["waiting"]++
			continue
		}
		var it *Item
		for _, x := range in.Items {
			if x.ID == d.ItemID {
				it = x
			}
		}
		var ch *Channel
		for i := range in.settings().Channels {
			if in.settings().Channels[i].ID == d.Channel {
				ch = &in.settings().Channels[i]
			}
		}
		if it == nil || ch == nil || !ch.Enabled {
			d.State, d.Error = "dead", "渠道或通知已经不在了"
			out["dead"]++
			continue
		}
		if spec, ok := Registry[it.Type]; ok && spec.TTL > 0 && now-it.CreatedAt > spec.TTL.Milliseconds() {
			d.State, d.Error = "dead", "已经过期，不再推送"
			out["dead"]++
			continue
		}
		jobs = append(jobs, job{d, *it, *ch})
	}
	senders := in.Senders
	in.mu.Unlock()
	for _, j := range jobs {
		send := senders[j.ch.Type]
		err := errors.New("没有这种渠道的发送器")
		if send != nil {
			err = send(j.it, j.ch)
		}
		in.mu.Lock()
		j.d.Attempts++
		switch {
		case err == nil:
			j.d.State, j.d.SentAt, j.d.Error = "sent", in.now().UnixMilli(), ""
			out["sent"]++
		default:
			j.d.Error = err.Error()
			var perm PermanentError
			if errors.As(err, &perm) || j.d.Attempts >= maxAttempts {
				j.d.State = "dead"
				out["dead"]++
			} else {
				j.d.After = in.now().Add(backoff[j.d.Attempts-1]).UnixMilli()
				out["failed"]++
			}
		}
		in.mu.Unlock()
	}
	in.mu.Lock()
	in.pruneDeliveries()
	in.save()
	in.mu.Unlock()
	return out
}

func (in *Inbox) pruneDeliveries() {
	cut := in.now().Add(-7 * 24 * time.Hour).UnixMilli()
	kept := in.Deliveries[:0]
	for _, d := range in.Deliveries {
		if d.State == "pending" || d.After > cut {
			kept = append(kept, d)
		}
	}
	in.Deliveries = kept
}

// ListDeliveries returns the deliveries of an item (or all), newest first.
func (in *Inbox) ListDeliveries(itemID string) []Delivery {
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []Delivery
	for i := len(in.Deliveries) - 1; i >= 0; i-- {
		if itemID == "" || in.Deliveries[i].ItemID == itemID {
			out = append(out, *in.Deliveries[i])
		}
	}
	return out
}

// Test sends a test message to one channel at once and says how it went.
func (in *Inbox) Test(id string) error {
	c, ok := in.Channel(id)
	if !ok {
		return errors.New("没有这个渠道")
	}
	if c.Type == "desktop" {
		return nil
	}
	send := in.Senders[c.Type]
	if send == nil {
		return errors.New("没有这种渠道的发送器")
	}
	return send(Item{Title: "PABuddy 测试通知", Body: "收到这条，说明这个渠道配好了。"}, c)
}

// BuildDigest folds the normal notifications that did not bother anyone on their own into one summary.
func (in *Inbox) BuildDigest(label string, since time.Time) (*Item, int) {
	in.mu.Lock()
	if !in.settings().Digest.Enabled {
		in.mu.Unlock()
		return nil, 0
	}
	var pending []*Item
	for _, it := range in.Items {
		if it.Severity == "normal" && it.DigestID == "" && it.Type != "digest.summary" && it.CreatedAt >= since.UnixMilli() {
			pending = append(pending, it)
		}
	}
	in.mu.Unlock()
	if len(pending) == 0 {
		return nil, 0
	}
	groups := map[string][]*Item{}
	var order []string
	counts := map[string]any{}
	for _, it := range pending {
		subject := it.SubjectID
		if subject == "" {
			subject = "其他"
		}
		if _, ok := groups[subject]; !ok {
			order = append(order, subject)
		}
		groups[subject] = append(groups[subject], it)
		n, _ := counts[it.Type].(int)
		counts[it.Type] = n + 1
	}
	var lines []string
	for _, subject := range order {
		items := groups[subject]
		var titles []string
		for i, it := range items {
			if i < 3 {
				titles = append(titles, it.Title)
			}
		}
		line := subject + "：" + strings.Join(titles, "；")
		if len(items) > 3 {
			line += fmt.Sprintf("…还有 %d 条", len(items)-3)
		}
		lines = append(lines, line)
	}
	day := in.now().In(zone()).Format("2006-01-02")
	made, created, err := in.Publish("digest.summary", "system", "digest", map[string]any{"period": label, "period_label": label, "items": lines, "counts": counts}, day+":"+label, Options{})
	if err != nil || !created {
		return nil, 0
	}
	in.mu.Lock()
	for _, it := range pending {
		it.DigestID = made.ID
	}
	in.save()
	in.mu.Unlock()
	return made, len(pending)
}

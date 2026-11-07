package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/sched"
)

// relayClient talks to the offline-forwarding server: it uploads the price alerts and the list of
// stocks worth preparing data for, and pulls back what the server fired while this computer was off.
// The server only ever sees stock codes and alert prices, never quantities, costs or reasons.
type relayClient struct {
	mu   sync.Mutex
	path string
	conf relayConf
	http *http.Client
	now  func() time.Time
}

type relayConf struct {
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url"`
	Token      string `json:"token"`
	DeviceID   string `json:"device_id"`
	Rev        int    `json:"rev"`
	SubRev     int    `json:"sub_rev"`
	Cursor     int    `json:"cursor"`
	SysCursor  int    `json:"sys_cursor"`
	DefsHash   string `json:"defs_hash"`
	SubsHash   string `json:"subs_hash"`
	HealthyAt  int64  `json:"healthy_at"`
	SecretHash string `json:"secret_hash"`
}

const relayHealthy = 10 * time.Minute

func newRelayClient(path string, now func() time.Time) *relayClient {
	rc := &relayClient{path: path, http: &http.Client{Timeout: 8 * time.Second}, now: now}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &rc.conf)
		}
	}
	return rc
}

func (rc *relayClient) save() {
	if rc.path == "" {
		return
	}
	raw, _ := json.Marshal(rc.conf)
	_ = os.MkdirAll(filepath.Dir(rc.path), 0o755)
	tmp := rc.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, rc.path)
	}
}

func (rc *relayClient) snapshot() relayConf {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.conf
}

func (rc *relayClient) enabled() bool {
	c := rc.snapshot()
	return c.Enabled && c.URL != "" && c.Token != ""
}

// authoritative is true while the server answered recently: it evaluates price alerts, also when this
// computer is off, so this machine does not evaluate them a second time.
func (rc *relayClient) authoritative() bool {
	c := rc.snapshot()
	return rc.enabled() && rc.now().Sub(time.UnixMilli(c.HealthyAt)) <= relayHealthy
}

func (rc *relayClient) call(method, path string, body any) (int, map[string]any, error) {
	c := rc.snapshot()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.URL, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := rc.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, nil
}

func hashOf(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

func (rc *relayClient) register(url string) error {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	rc.mu.Lock()
	rc.conf = relayConf{URL: url}
	rc.mu.Unlock()
	status, body, err := rc.call("POST", "/v1/devices", map[string]any{})
	if err != nil {
		return err
	}
	if status != http.StatusCreated {
		return fmt.Errorf("服务端注册失败（%d）", status)
	}
	token, _ := body["token"].(string)
	id, _ := body["device_id"].(string)
	rc.mu.Lock()
	rc.conf = relayConf{Enabled: true, URL: url, Token: token, DeviceID: id}
	rc.save()
	rc.mu.Unlock()
	return nil
}

func (rc *relayClient) revoke() bool {
	ok := false
	if rc.enabled() {
		status, _, err := rc.call("DELETE", "/v1/devices/me", nil)
		ok = err == nil && status == http.StatusOK
	}
	rc.mu.Lock()
	rc.conf.Enabled = false
	rc.save()
	rc.mu.Unlock()
	return ok
}

func (rc *relayClient) put(path string, rev *int, payload func(rev int) map[string]any) error {
	r := *rev + 1
	status, body, err := rc.call("PUT", path, payload(r))
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		if errBody, ok := body["error"].(map[string]any); ok {
			if det, ok := errBody["details"].(map[string]any); ok {
				if cur, ok := det["rev"].(float64); ok {
					r = int(cur) + 1
				}
			}
		}
		status, _, err = rc.call("PUT", path, payload(r))
		if err != nil {
			return err
		}
	}
	if status != http.StatusOK {
		return fmt.Errorf("同步失败（%d）", status)
	}
	*rev = r
	return nil
}

// syncAlerts uploads the alert rules (and the WeCom channel, when one is configured) if they changed.
func (s *Server) syncRelayAlerts() (map[string]any, error) {
	rc := s.relay
	if rc == nil || !rc.enabled() {
		return map[string]any{"skipped": "disabled"}, nil
	}
	var alerts []map[string]any
	for _, a := range s.personal.Alerts("") {
		confirm := "touch"
		if a.Confirm == "close" {
			confirm = "close"
		}
		label := a.Name
		if label == "" {
			label = a.Code
		}
		alerts = append(alerts, map[string]any{"alert_id": fmt.Sprintf("alert-%d", a.ID), "ts_code": a.Code, "side": a.Side, "price": a.Price,
			"confirm": confirm, "rearm": "daily", "label": label, "channels": []string{"wecom"}})
	}
	var channels []map[string]any
	var channelIDs []string
	for _, c := range s.inbox.Settings().Channels {
		if !c.Relay || !c.Enabled || c.Type == "desktop" {
			continue
		}
		full, _ := s.inbox.Channel(c.ID)
		channelIDs = append(channelIDs, c.ID)
		channels = append(channels, map[string]any{"channel_id": c.ID, "type": c.Type, "only_offline": true, "secret": full.Secret})
	}
	for _, a := range alerts {
		a["channels"] = channelIDs
	}
	defs := hashOf([]any{alerts, channels})
	c := rc.snapshot()
	if defs == c.DefsHash {
		return map[string]any{"skipped": "unchanged"}, nil
	}
	rev := c.Rev
	err := rc.put("/v1/relay/alerts", &rev, func(r int) map[string]any {
		return map[string]any{"v": 1, "rev": r, "alerts": alerts, "channels": channels}
	})
	if err != nil {
		return nil, err
	}
	rc.mu.Lock()
	rc.conf.Rev, rc.conf.DefsHash = rev, defs
	rc.save()
	rc.mu.Unlock()
	return map[string]any{"synced": len(alerts), "rev": rev}, nil
}

func (s *Server) syncRelaySubscriptions() (map[string]any, error) {
	rc := s.relay
	if rc == nil || !rc.enabled() {
		return map[string]any{"skipped": "disabled"}, nil
	}
	var codes []map[string]any
	for _, sub := range s.personal.Subjects() { // only the code and whether it is held: no quantity, cost or reason
		tier := "other"
		if sub.Tier == "hold" {
			tier = "hold"
		}
		codes = append(codes, map[string]any{"ts_code": sub.Code, "tier": tier})
	}
	digest := hashOf(codes)
	c := rc.snapshot()
	if digest == c.SubsHash {
		return map[string]any{"skipped": "unchanged"}, nil
	}
	rev := c.SubRev
	err := rc.put("/v1/subscriptions", &rev, func(r int) map[string]any { return map[string]any{"v": 1, "rev": r, "codes": codes} })
	if err != nil {
		return nil, err
	}
	rc.mu.Lock()
	rc.conf.SubRev, rc.conf.SubsHash = rev, digest
	rc.save()
	rc.mu.Unlock()
	return map[string]any{"synced": len(codes), "rev": rev}, nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// pullRelayEvents writes what the server fired into the inbox (and the desktop), then acknowledges it.
func (s *Server) pullRelayEvents() (map[string]any, error) {
	rc := s.relay
	if rc == nil || !rc.enabled() {
		return map[string]any{"skipped": "disabled"}, nil
	}
	c := rc.snapshot()
	status, body, err := rc.call("GET", fmt.Sprintf("/v1/relay/events?after=%d", c.Cursor), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("拉取提醒事件失败（%d）", status)
	}
	var seqs []int
	created := 0
	items, _ := body["items"].([]any)
	for _, raw := range items {
		item := asMap(raw)
		event := asMap(item["event"])
		payload := asMap(event["payload"])
		subject := asMap(event["subject"])
		key, _ := event["dedupe_key"].(string)
		day, _ := event["trade_date"].(string)
		_, made, err := s.inbox.Publish("price.alert.fired", fmt.Sprint(subject["kind"]), fmt.Sprint(subject["id"]), payload, key, inbox.Options{TradeDate: day, Local: true})
		if err == nil && made {
			created++
		}
		if seq, ok := item["seq"].(float64); ok {
			seqs = append(seqs, int(seq))
		}
	}
	if len(seqs) > 0 {
		max := seqs[0]
		for _, q := range seqs {
			if q > max {
				max = q
			}
		}
		_, _, _ = rc.call("POST", "/v1/relay/ack", map[string]any{"seqs": seqs})
		rc.mu.Lock()
		rc.conf.Cursor = max
		rc.mu.Unlock()
	}
	rc.mu.Lock()
	rc.conf.HealthyAt = rc.now().UnixMilli()
	rc.save()
	rc.mu.Unlock()
	return map[string]any{"events": len(seqs), "created": created}, nil
}

// pullSystemEvents brings back the server's own task receipts, so a task that ran on the server can be
// seen in the inbox too.
func (s *Server) pullSystemEvents() (map[string]any, error) {
	rc := s.relay
	if rc == nil || !rc.enabled() {
		return map[string]any{"skipped": "disabled"}, nil
	}
	c := rc.snapshot()
	status, body, err := rc.call("GET", fmt.Sprintf("/v1/system/events?after=%d", c.SysCursor), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("拉取服务端任务回执失败（%d）", status)
	}
	last, created := c.SysCursor, 0
	items, _ := body["items"].([]any)
	for _, raw := range items {
		item := asMap(raw)
		event := asMap(item["event"])
		typ, _ := event["type"].(string)
		payload := asMap(event["payload"])
		if typ != "task.completed" && typ != "task.failed" {
			continue
		}
		payload["host"] = "server"
		key, _ := event["dedupe_key"].(string)
		if _, made, err := s.inbox.Publish(typ, "system", fmt.Sprint(payload["task_id"]), payload, key, inbox.Options{NoDesktop: typ == "task.completed", Local: true}); err == nil && made {
			created++
		}
		if seq, ok := item["seq"].(float64); ok && int(seq) > last {
			last = int(seq)
		}
	}
	if last != c.SysCursor {
		rc.mu.Lock()
		rc.conf.SysCursor = last
		rc.save()
		rc.mu.Unlock()
	}
	return map[string]any{"events": len(items), "created": created}, nil
}

func (s *Server) relaySyncTask(c *sched.JobCtx) (any, error) {
	if s.relay == nil || !s.relay.enabled() {
		return map[string]any{"enabled": false}, nil
	}
	out := map[string]any{"enabled": true}
	for name, call := range map[string]func() (map[string]any, error){
		"alerts": s.syncRelayAlerts, "subscriptions": s.syncRelaySubscriptions, "events": s.pullRelayEvents, "server_tasks": s.pullSystemEvents,
	} {
		res, err := call()
		if err != nil { // the server being unreachable is not a task failure: this machine evaluates alerts itself
			out[name] = map[string]any{"error": err.Error()}
			continue
		}
		out[name] = res
	}
	return out, nil
}

// ---- HTTP

func (s *Server) handleRelayStatus(w http.ResponseWriter, _ *http.Request) {
	c := s.relay.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.relay.enabled(), "url": c.URL, "device_id": c.DeviceID, "authoritative": s.relay.authoritative()})
}

func (s *Server) handleRelayEnable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	_ = readJSON(r, &body)
	if strings.TrimSpace(body.URL) == "" {
		writeError(w, http.StatusBadRequest, errors.New("请填服务端地址"))
		return
	}
	if err := s.relay.register(body.URL); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"enabled": true, "device_id": s.relay.snapshot().DeviceID})
}

func (s *Server) handleRelayDisable(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"revoked": s.relay.revoke()})
}

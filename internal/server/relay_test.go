package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-harness/internal/inbox"
	"go-harness/internal/personal"
	"go-harness/internal/quotes"
)

type fakeRelay struct {
	mu    sync.Mutex
	puts  []map[string]any
	subs  []map[string]any
	acked []any
	rev   int
}

func (f *fakeRelay) handler() http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, code int, v any) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /v1/devices", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 201, map[string]any{"device_id": "d1", "token": "d1.secret"})
	})
	mux.HandleFunc("PUT /v1/relay/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer d1.secret" {
			reply(w, 401, map[string]any{})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.puts = append(f.puts, body)
		f.mu.Unlock()
		reply(w, 200, map[string]any{})
	})
	mux.HandleFunc("PUT /v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.subs = append(f.subs, body)
		f.mu.Unlock()
		reply(w, 200, map[string]any{})
	})
	mux.HandleFunc("GET /v1/relay/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "0" {
			reply(w, 200, map[string]any{"items": []any{}})
			return
		}
		reply(w, 200, map[string]any{"items": []any{map[string]any{"seq": 7, "event": map[string]any{
			"subject": map[string]any{"kind": "stock", "id": "600036.SH"}, "dedupe_key": "relay:alert-1:2026-11-05", "trade_date": "2026-11-05",
			"payload": map[string]any{"alert_id": 1, "ts_code": "600036.SH", "name": "招商银行", "side": "down", "trigger_price": 30.0, "observed_price": 29.5, "confirm": "touch"},
		}}}})
	})
	mux.HandleFunc("POST /v1/relay/ack", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.acked = append(f.acked, body["seqs"])
		f.mu.Unlock()
		reply(w, 200, map[string]any{})
	})
	mux.HandleFunc("GET /v1/system/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "0" {
			reply(w, 200, map[string]any{"items": []any{}})
			return
		}
		reply(w, 200, map[string]any{"items": []any{map[string]any{"seq": 3, "event": map[string]any{
			"type": "task.completed", "dedupe_key": "server:r9",
			"payload": map[string]any{"task_id": "stock.facts.warm", "title": "预热个股事实表", "state": "succeeded", "state_text": "成功", "trigger_text": "定时"},
		}}}})
	})
	return mux
}

func TestRelayUploadsOnlyAlertsAndCodesAndPullsWhatTheServerFired(t *testing.T) {
	fake := &fakeRelay{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	s, clock := newClientServer(t, zoneTime("2026-11-05", "09:00"))
	s.relay = newRelayClient("", clock.now)
	if err := s.relay.register(srv.URL); err != nil || !s.relay.enabled() {
		t.Fatalf("register: %v", err)
	}
	s.personal.AlertAdd("600036.SH", "down", 30, "touch", "招商银行")
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 1234, Date: "2026-10-01", Reason: "私人理由"})
	s.personal.WatchAdd("300750.SZ", "宁德时代")

	if out, err := s.syncRelayAlerts(); err != nil || out["synced"] != 1 {
		t.Fatalf("alerts: %v %v", out, err)
	}
	if out, _ := s.syncRelayAlerts(); out["skipped"] != "unchanged" {
		t.Fatal("re-uploaded unchanged alerts")
	}
	if _, err := s.syncRelaySubscriptions(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fake.subs)
	for _, secret := range []string{"1234", "私人理由", "10"} {
		if strings.Contains(string(raw), "\"qty\"") || strings.Contains(string(raw), secret+"\"") {
			t.Fatalf("subscription upload leaked %q: %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), "hold") || !strings.Contains(string(raw), "300750.SZ") {
		t.Fatalf("subscriptions = %s", raw)
	}

	out, err := s.pullRelayEvents()
	if err != nil || out["created"] != 1 {
		t.Fatalf("pull: %v %v", out, err)
	}
	if again, _ := s.pullRelayEvents(); again["events"] != 0 {
		t.Fatalf("cursor did not advance: %v", again)
	}
	pending := s.inbox.DesktopPending()
	if len(pending) != 1 || !strings.Contains(pending[0]["title"].(string), "招商银行") {
		t.Fatalf("pending = %v", pending)
	}
	if len(fake.acked) != 1 || !s.relay.authoritative() {
		t.Fatalf("acked = %v authoritative = %v", fake.acked, s.relay.authoritative())
	}
	if sys, _ := s.pullSystemEvents(); sys["created"] != 1 {
		t.Fatalf("system = %v", sys)
	}
	var serverReceipt bool
	for _, it := range s.inbox.List("system", false, 5) {
		serverReceipt = serverReceipt || (it.Type == "task.completed" && it.Desktop == "")
	}
	if !serverReceipt {
		t.Fatal("the server's task receipt did not reach the inbox quietly")
	}
}

func TestWhileTheServerAnswersLocalPriceAlertsStandDownAndWhenItIsGoneTheyComeBack(t *testing.T) {
	fake := &fakeRelay{}
	srv := httptest.NewServer(fake.handler())
	s, clock := newClientServer(t, zoneTime("2026-11-05", "09:00"))
	s.relay = newRelayClient("", clock.now)
	s.relay.register(srv.URL)
	s.quotes = quotes.New()
	s.quotes.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~29.00~30.00~1";`, nil }
	s.personal.AlertAdd("600036.SH", "down", 29.5, "touch", "招商银行")
	s.pullRelayEvents() // the server answered just now
	s.sch.TickSync()
	clock.advance(31 * time.Minute)
	s.pullRelayEvents() // still answering at 09:31
	s.sch.TickSync()
	for _, it := range s.inbox.List("stock", false, 20) {
		if it.Type == "price.alert.fired" && it.DedupeKey != "relay:alert-1:2026-11-05" {
			t.Fatalf("this machine evaluated an alert the server owns: %+v", it)
		}
	}
	srv.Close() // the server goes away; 11 minutes later it no longer counts as healthy
	clock.advance(11 * time.Minute)
	if s.relay.authoritative() {
		t.Fatal("a dead server is still authoritative")
	}
	s.sch.TickSync()
	var local bool
	for _, it := range s.inbox.List("stock", false, 20) {
		local = local || (it.Type == "price.alert.fired" && strings.HasPrefix(it.DedupeKey, "alert:"))
	}
	if !local {
		t.Fatal("the local fallback did not fire the alert")
	}
	if res, err := s.relaySyncTask(nil); err != nil || res.(map[string]any)["enabled"] != true {
		t.Fatalf("an unreachable server failed the sync task: %v %v", res, err)
	}
}

func TestRelayUploadsTheChannelsMarkedForRelayWithTheirSecretsAndNoOthers(t *testing.T) {
	fake := &fakeRelay{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	s, clock := newClientServer(t, zoneTime("2026-11-05", "09:00"))
	s.relay = newRelayClient("", clock.now)
	s.relay.register(srv.URL)
	s.inbox.PutChannel(inbox.Channel{ID: "wecom", Type: "wecom_webhook", Enabled: true, MinSeverity: "normal", Secret: "https://hook", Relay: true})
	s.inbox.PutChannel(inbox.Channel{ID: "bark", Type: "bark", Enabled: true, MinSeverity: "normal", Secret: "private-key", Relay: false})
	s.personal.AlertAdd("600036.SH", "down", 30, "touch", "招商银行")
	if _, err := s.syncRelayAlerts(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fake.puts[0])
	if !strings.Contains(string(raw), "https://hook") || strings.Contains(string(raw), "private-key") || !strings.Contains(string(raw), `"channels":["wecom"]`) {
		t.Fatalf("uploaded = %s", raw)
	}
}

func TestNotifySettingsRoutesHideSecretsAndRejectMistakes(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-05", "09:00"))
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	call := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, _ := call("PUT", "/notify/channels/bark", `{"type":"bark","name":"手机","enabled":true,"secret":"my-key"}`); code != 200 {
		t.Fatalf("put channel = %d", code)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/notify/settings", nil))
	if strings.Contains(rec.Body.String(), "my-key") || !strings.Contains(rec.Body.String(), `"has_secret":true`) || !strings.Contains(rec.Body.String(), "judgment.changed") {
		t.Fatalf("settings = %s", rec.Body.String())
	}
	if code, _ := call("PUT", "/notify/settings", `{"quiet_hours":{"from":"9pm","to":"08:00"}}`); code != 400 {
		t.Fatalf("bad quiet hours = %d", code)
	}
	if code, _ := call("PUT", "/notify/settings", `{"quiet_hours":{"from":"23:00","to":"07:00","urgent":true},"digest":{"Enabled":true}}`); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	if code, _ := call("DELETE", "/notify/channels/desktop", ``); code != 404 {
		t.Fatal("the desktop channel can be deleted")
	}
	if code, out := call("POST", "/notify/channels/nope/test", ``); code != 502 || out["ok"] != false {
		t.Fatalf("test unknown = %d %v", code, out)
	}
}

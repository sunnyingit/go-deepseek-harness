package inbox

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type clk struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clk) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clk) set(day, hhmm string) {
	c.mu.Lock()
	c.t, _ = time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, zone())
	c.mu.Unlock()
}
func (c *clk) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func move(code string) map[string]any {
	return map[string]any{"ts_code": code, "name": code, "pct": 8.0, "observed_price": 10.0}
}

func alert(id int) map[string]any {
	return map[string]any{"alert_id": id, "ts_code": "600036.SH", "name": "招商银行", "side": "down", "trigger_price": 30.0, "observed_price": 29.0}
}

func TestQuietHoursHoldNormalAndLetUrgentThrough(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "23:10")
	in := Open("", c.now)
	n, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m1", Options{})
	u, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(1), "a1", Options{})
	if n.Desktop != "pending" || u.Desktop != "pending" {
		t.Fatalf("n=%+v u=%+v", n, u)
	}
	if got := in.DesktopPending(); len(got) != 1 || got[0]["delivery_id"] != u.ID {
		t.Fatalf("at night only the urgent one may pop up: %v", got)
	}
	c.set("2026-11-06", "08:01")
	if got := in.DesktopPending(); len(got) != 2 {
		t.Fatalf("after the quiet stretch both should pop up: %v", got)
	}
	in.Update(SettingsPatch{Quiet: &Quiet{From: "22:00", To: "08:00", Urgent: false}})
	c.set("2026-11-06", "23:00")
	u2, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(2), "a2", Options{})
	if u2.DesktopAfter <= c.now().UnixMilli() {
		t.Fatalf("urgent was not held although the user asked for it: %+v", u2)
	}
}

func TestSameStockAndTypeCollapseAndInfoOnlyNeverPopsUp(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "10:00")
	in := Open("", c.now)
	first, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m1", Options{})
	c.add(2 * time.Minute)
	second, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m2", Options{})
	other, _, _ := in.Publish("price.move", "stock", "B", move("B"), "m3", Options{})
	if first.Suppressed != "" || second.Suppressed != "collapsed" || second.Desktop != "" || other.Desktop != "pending" {
		t.Fatalf("first=%+v second=%+v other=%+v", first, second, other)
	}
	c.add(20 * time.Minute)
	third, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m4", Options{})
	if third.Suppressed == "collapsed" {
		t.Fatal("collapse window never ended")
	}
	rec, _, _ := in.Publish("task.completed", "system", "x", map[string]any{"task_id": "x", "state": "succeeded", "state_text": "成功"}, "r", Options{})
	if rec.Suppressed != "info_only" || rec.Desktop != "" {
		t.Fatalf("receipt = %+v", rec)
	}
	if len(in.List("", false, 10)) != 5 {
		t.Fatal("a suppressed item must still be in the inbox")
	}
}

func TestPerTypeSwitchesDisabledAndExpiredEvents(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "10:00")
	in := Open("", c.now)
	off := false
	in.Update(SettingsPatch{Types: map[string]TypeSetting{"price.move": {Enabled: &off}}})
	m, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m", Options{})
	if m.Suppressed != "type_off" || m.Desktop != "" {
		t.Fatalf("m = %+v", m)
	}
	if _, err := in.Update(SettingsPatch{Types: map[string]TypeSetting{"nope": {}}}); err == nil {
		t.Fatal("unknown type accepted")
	}
	old, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(1), "old", Options{OccurredAt: c.now().Add(-3 * 24 * time.Hour).UnixMilli()})
	if old.Suppressed != "expired" {
		t.Fatalf("old = %+v", old)
	}
	tr := true
	in.Update(SettingsPatch{Enabled: &tr})
	f := false
	in.Update(SettingsPatch{Enabled: &f})
	x, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(2), "x", Options{})
	if x.Suppressed != "disabled" {
		t.Fatalf("x = %+v", x)
	}
}

func TestOutsideChannelsRetryThenGiveUpAndSecretsStayHidden(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "10:00")
	in := Open("", c.now)
	var calls int
	var fail error
	in.Senders["wecom_webhook"] = func(it Item, ch Channel) error {
		calls++
		if ch.Secret != "https://hook" {
			t.Errorf("sender got secret %q", ch.Secret)
		}
		return fail
	}
	ch, err := in.PutChannel(Channel{ID: "wecom", Type: "wecom_webhook", Name: "企业微信", Enabled: true, MinSeverity: "urgent", Secret: "https://hook", Relay: true})
	if err != nil || strings.Contains(strings.Join(mapKeys(ch.View()), ","), "secret\"") || ch.View()["has_secret"] != true {
		t.Fatalf("view = %v err = %v", ch.View(), err)
	}
	if _, ok := ch.View()["secret"]; ok {
		t.Fatal("the secret leaked into the view")
	}
	if again, _ := in.PutChannel(Channel{ID: "wecom", Type: "wecom_webhook", Enabled: true, MinSeverity: "urgent"}); again.Secret != "https://hook" {
		t.Fatal("saving without a secret erased the saved one")
	}
	in.Publish("price.move", "stock", "A", move("A"), "m", Options{}) // normal: below the channel's minimum
	it, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(1), "a", Options{})
	if ds := in.ListDeliveries(it.ID); len(ds) != 1 || len(in.ListDeliveries("")) != 1 {
		t.Fatalf("deliveries = %+v", in.ListDeliveries(""))
	}
	fail = errors.New("超时")
	if r := in.Dispatch(); r["failed"] != 1 || calls != 1 {
		t.Fatalf("first try: %v", r)
	}
	if r := in.Dispatch(); r["waiting"] != 1 || calls != 1 {
		t.Fatalf("retried too early: %v", r)
	}
	c.add(2 * time.Minute)
	in.Dispatch()
	c.add(6 * time.Minute)
	if r := in.Dispatch(); r["dead"] != 1 || calls != 3 {
		t.Fatalf("third failure should end it: %v calls=%d", r, calls)
	}
	fail = nil
	it2, _, _ := in.Publish("price.alert.fired", "stock", "B", alert(2), "a2", Options{})
	if r := in.Dispatch(); r["sent"] != 1 || in.ListDeliveries(it2.ID)[0].State != "sent" {
		t.Fatalf("send: %v", r)
	}
	fail = PermanentError{"密钥错"}
	in.Publish("price.alert.fired", "stock", "C", alert(3), "a3", Options{})
	if r := in.Dispatch(); r["dead"] != 1 {
		t.Fatalf("a permanent failure must not be retried: %v", r)
	}
	if err := in.Test("wecom"); err == nil {
		t.Fatal("test send did not report the failure")
	}
	if _, err := in.PutChannel(Channel{ID: "x", Type: "pigeon"}); err == nil {
		t.Fatal("unknown channel type accepted")
	}
	if in.DeleteChannel("desktop") || !in.DeleteChannel("wecom") {
		t.Fatal("channel deletion rules wrong")
	}
}

func mapKeys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLocalEventsOnlyGoToTheDesktopBecauseTheServerAlreadyPushedElsewhere(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "10:00")
	in := Open("", c.now)
	in.PutChannel(Channel{ID: "wecom", Type: "wecom_webhook", Enabled: true, MinSeverity: "normal", Secret: "x"})
	it, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(1), "a", Options{Local: true})
	if it.Desktop != "pending" || len(in.ListDeliveries("")) != 0 {
		t.Fatalf("it = %+v deliveries = %+v", it, in.ListDeliveries(""))
	}
}

func TestDigestFoldsQuietItemsIntoOneSummaryOnce(t *testing.T) {
	c := &clk{}
	c.set("2026-11-05", "10:00")
	in := Open("", c.now)
	if made, _ := in.BuildDigest("早间摘要", c.now().Add(-16*time.Hour)); made != nil {
		t.Fatal("digest ran while switched off")
	}
	in.Update(SettingsPatch{Digest: &struct{ Enabled bool }{true}})
	a, _, _ := in.Publish("price.move", "stock", "A", move("A"), "m1", Options{})
	in.Publish("judgment.changed", "stock", "A", map[string]any{"ts_code": "A", "name": "A", "headline": "从偏多变为不明"}, "j1", Options{})
	in.Publish("price.move", "stock", "B", move("B"), "m2", Options{})
	u, _, _ := in.Publish("price.alert.fired", "stock", "A", alert(1), "a1", Options{})
	if a.Suppressed != "digest" || a.Desktop != "" || u.Desktop != "pending" {
		t.Fatalf("a=%+v u=%+v", a, u)
	}
	made, n := in.BuildDigest("早间摘要", c.now().Add(-16*time.Hour))
	if made == nil || n != 3 || !strings.Contains(made.Title, "早间摘要：3 条") || !strings.Contains(made.Body, "A：") || made.Desktop != "pending" {
		t.Fatalf("digest = %+v n=%d", made, n)
	}
	if again, n := in.BuildDigest("早间摘要", c.now().Add(-16*time.Hour)); again != nil || n != 0 {
		t.Fatal("the same items were summarised twice")
	}
}

func TestSettingsValidationAndPersistence(t *testing.T) {
	path := t.TempDir() + "/inbox.json"
	in := Open(path, nil)
	if _, err := in.Update(SettingsPatch{Quiet: &Quiet{From: "9pm", To: "08:00"}}); err == nil {
		t.Fatal("bad time accepted")
	}
	n := 120
	if _, err := in.Update(SettingsPatch{CollapseS: &n}); err != nil {
		t.Fatal(err)
	}
	in.PutChannel(Channel{ID: "bark", Type: "bark", Name: "手机", Enabled: true, MinSeverity: "normal", Secret: "key"})
	again := Open(path, nil)
	s := again.Settings()
	if s.CollapseS != 120 || len(s.Channels) != 2 || s.Quiet.From != "22:00" {
		t.Fatalf("settings = %+v", s)
	}
	if c, _ := again.Channel("bark"); c.Secret != "key" {
		t.Fatal("secret was not persisted")
	}
}

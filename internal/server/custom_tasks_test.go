package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type customAPI struct {
	t   *testing.T
	mux *http.ServeMux
}

func (a customAPI) call(method, path, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func newCustomAPI(t *testing.T, s *Server) customAPI {
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	return customAPI{t, mux}
}

func inboxTypes(s *Server, typ string) []map[string]any {
	var out []map[string]any
	for _, it := range s.inbox.List("", false, 50) {
		if it.Type == typ {
			out = append(out, map[string]any{"title": it.Title, "body": it.Body, "subject": it.SubjectID})
		}
	}
	return out
}

func TestCustomTasksAreCheckedSavedAndScheduled(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00")) // a Monday
	a := newCustomAPI(t, s)
	if code, out := a.call("POST", "/api/scan/custom", `{"title":"","kind":"analyze","scope":"hold","at":"14:30"}`); code != 400 || !strings.Contains(out["error"].(string), "名字") {
		t.Fatalf("empty title = %d %v", code, out)
	}
	if code, out := a.call("POST", "/api/scan/custom", `{"title":"x","kind":"scan","scope":"all","at":"14:30","rules":[]}`); code != 400 || !strings.Contains(out["error"].(string), "条件") {
		t.Fatalf("no rules = %d %v", code, out)
	}
	code, out := a.call("POST", "/api/scan/custom", `{"title":"收盘后扫一遍","kind":"scan","scope":"codes","codes":["600036.SH"],"at":"15:40","days":"trading","match":"all","rules":[{"kind":"below_ma","value":5}]}`)
	if code != 201 {
		t.Fatalf("create = %d %v", code, out)
	}
	task := out["task"].(map[string]any)
	id := task["id"].(string)
	if task["kind_text"] != "条件扫描" || task["schedule_text"] != "每个交易日 15:40" || task["rules_text"] != "收盘跌破 5 日均线" || task["when_text"] != "周一 11-09 15:40 · 今天" {
		t.Fatalf("view = %v", task)
	}
	sp, ok := s.sch.Spec("custom." + id)
	if !ok || sp.Trigger.At != "15:40" || sp.Queue != "data" {
		t.Fatalf("spec = %+v", sp)
	}
	// switching it off pauses it; editing re-registers it; deleting removes it
	a.call("POST", "/api/scan/custom/"+id+"/toggle", `{"enabled":false}`)
	if !s.sch.Store.Paused("custom." + id) {
		t.Fatal("not paused")
	}
	if code, _ := a.call("PUT", "/api/scan/custom/"+id, `{"title":"改了","kind":"ask","scope":"hold","at":"09:45","question":"有什么新消息？"}`); code != 200 {
		t.Fatalf("update = %d", code)
	}
	if sp, _ := s.sch.Spec("custom." + id); sp.Title != "改了" || sp.Trigger.At != "09:45" || sp.Queue != "llm" || !s.sch.Store.Paused("custom."+id) {
		t.Fatalf("spec after edit = %+v", sp)
	}
	if code, _ := a.call("DELETE", "/api/scan/custom/"+id, ""); code != 200 {
		t.Fatal("delete failed")
	}
	if _, ok := s.sch.Spec("custom." + id); ok {
		t.Fatal("spec still registered")
	}
	if code, _ := a.call("DELETE", "/api/scan/custom/"+id, ""); code != 404 {
		t.Fatal("a second delete was not a 404")
	}
}

func TestCustomScanTellsOnlyWhatMatchesAndOnlyOncePerDay(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 12, 10, -0.2), "000001.SZ": series("2026-10-26", 12, 10, 0.2)}}
	ds.start(t)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "16:00"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	s.personal.WatchAdd("000001.SZ", "平安银行")
	a := newCustomAPI(t, s)
	_, out := a.call("POST", "/api/scan/custom", `{"title":"跌破五日线","kind":"scan","scope":"watch","at":"15:40","rules":[{"kind":"below_ma","value":5}]}`)
	id := out["task"].(map[string]any)["id"].(string)
	if code, out := a.call("POST", "/api/scan/custom/"+id+"/run", ""); code != 202 || out["queued"].(float64) != 2 {
		t.Fatalf("run = %d %v", code, out)
	}
	s.sch.TickSync()
	hits := inboxTypes(s, "custom.hit")
	if len(hits) != 1 || hits[0]["subject"] != "600036.SH" || !strings.Contains(hits[0]["title"].(string), "跌破五日线 · 招商银行") || !strings.Contains(hits[0]["body"].(string), "跌破 5 日均线") {
		t.Fatalf("hits = %v", hits)
	}
	// the same condition on the same day is not announced twice
	a.call("POST", "/api/scan/custom/"+id+"/run", "")
	s.sch.TickSync()
	if n := len(inboxTypes(s, "custom.hit")); n != 1 {
		t.Fatalf("announced %d times", n)
	}
}

func TestCustomAskPutsTheAnswerInTheInbox(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	var asked []string
	s.askFn = func(_ context.Context, code, q string) (string, error) {
		asked = append(asked, code+"|"+q)
		return "今天没有新公告，资金小幅流入。", nil
	}
	a := newCustomAPI(t, s)
	_, out := a.call("POST", "/api/scan/custom", `{"title":"午前看一眼","kind":"ask","scope":"watch","at":"11:00","question":"今天有没有新公告和资金异动？"}`)
	id := out["task"].(map[string]any)["id"].(string)
	a.call("POST", "/api/scan/custom/"+id+"/run", "")
	s.sch.TickSync()
	got := inboxTypes(s, "custom.answer")
	if len(asked) != 1 || asked[0] != "600036.SH|今天有没有新公告和资金异动？" || len(got) != 1 || got[0]["body"] != "今天没有新公告，资金小幅流入。" || !strings.Contains(got[0]["title"].(string), "午前看一眼 · 招商银行") {
		t.Fatalf("asked %v, inbox %v", asked, got)
	}
}

func TestCustomAnalyzeMakesAFreshConclusionAndWritesItDown(t *testing.T) {
	ds := &dataService{closes: map[string][][2]any{"600036.SH": series("2026-10-26", 9, 10, 0.1)}}
	ds.start(t)
	s, clk := newClientServer(t, zoneTime("2026-11-09", "14:00"))
	sp := &speaker{}
	s.stepFn = sp.fn
	s.personal.WatchAdd("600036.SH", "招商银行")
	seedDebate(s, "600036.SH")
	a := newCustomAPI(t, s)
	_, out := a.call("POST", "/api/scan/custom", `{"title":"午后再分析","kind":"analyze","scope":"watch","at":"14:30"}`)
	id := out["task"].(map[string]any)["id"].(string)
	for i := 0; i < 2; i++ { // the second run must make a new conclusion, not reuse today's
		if code, _ := a.call("POST", "/api/scan/custom/"+id+"/run", ""); code != 202 {
			t.Fatalf("run %d refused", i)
		}
		s.sch.TickSync()
		clk.advance(10 * time.Minute)
	}
	if n := len(s.scan.All()); n != 2 {
		t.Fatalf("predictions = %d", n)
	}
	for _, p := range s.scan.All() {
		if p.Source != "scan" {
			t.Fatalf("source = %s", p.Source)
		}
	}
}

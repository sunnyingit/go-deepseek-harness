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
	s.askFn = func(_ context.Context, code, analyst, q string) (string, error) {
		asked = append(asked, code+"|"+analyst+"|"+q)
		return "今天没有新公告，资金小幅流入。", nil
	}
	a := newCustomAPI(t, s)
	_, out := a.call("POST", "/api/scan/custom", `{"title":"午前看一眼","kind":"ask","scope":"watch","at":"11:00","question":"今天有没有新公告和资金异动？"}`)
	id := out["task"].(map[string]any)["id"].(string)
	a.call("POST", "/api/scan/custom/"+id+"/run", "")
	s.sch.TickSync()
	got := inboxTypes(s, "custom.answer")
	if len(asked) != 1 || asked[0] != "600036.SH|chief|今天有没有新公告和资金异动？" || len(got) != 1 || got[0]["body"] != "今天没有新公告，资金小幅流入。" || !strings.Contains(got[0]["title"].(string), "午前看一眼 · 招商银行") {
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

func TestCustomTasksCanRepeatEveryFewMinutes(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:10")) // a Monday, inside the morning session
	a := newCustomAPI(t, s)
	code, out := a.call("POST", "/api/scan/custom", `{"title":"盘中异动","kind":"scan","scope":"all","every":30,"days":"trading","match":"any","rules":[{"kind":"change_up","value":5},{"kind":"change_down","value":5}]}`)
	if code != 201 {
		t.Fatalf("create = %d %v", code, out)
	}
	task := out["task"].(map[string]any)
	if task["schedule_text"] != "交易时段每 30 分钟" || task["when_text"] != "下一次 周一 11-09 10:30 · 今天" {
		t.Fatalf("view = %v", task)
	}
	sp, _ := s.sch.Spec("custom." + task["id"].(string))
	if sp.Trigger.Kind != "interval" || sp.Trigger.EveryMin != 30 || !sp.Frequent || sp.MaxAttempts != 1 {
		t.Fatalf("spec = %+v", sp)
	}
	// the whole day, and the model kinds get a daily budget
	_, out = a.call("POST", "/api/scan/custom", `{"title":"每两小时问一次","kind":"ask","scope":"hold","every":120,"days":"any","question":"有什么新消息"}`)
	ask := out["task"].(map[string]any)
	sp, _ = s.sch.Spec("custom." + ask["id"].(string))
	if ask["schedule_text"] != "每 2 小时（全天）" || sp.Trigger.Kind != "every" || sp.BudgetCalls == 0 {
		t.Fatalf("ask = %v spec = %+v", ask, sp)
	}
	// too often for the model kinds is refused with the reason
	if code, out := a.call("POST", "/api/scan/custom", `{"title":"太勤","kind":"ask","scope":"hold","every":10,"days":"any","question":"x"}`); code != 400 || !strings.Contains(out["error"].(string), "最短每 30 分钟") {
		t.Fatalf("too often = %d %v", code, out)
	}
	// after the close, the next run of a session task is the next session
	s2, _ := newClientServer(t, zoneTime("2026-11-09", "16:00"))
	_, out = newCustomAPI(t, s2).call("POST", "/api/scan/custom", `{"title":"盘中异动","kind":"scan","scope":"all","every":15,"days":"trading","rules":[{"kind":"change_up","value":5}]}`)
	if w := out["task"].(map[string]any)["when_text"]; w != "下一次 周二 11-10 09:30 · 明天" {
		t.Fatalf("after close: %v", w)
	}
}

func TestCustomAskCanAskSeveralAnalystsAndAnalysisCanBeLimitedToSomeOfThem(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.personal.WatchAdd("600036.SH", "招商银行")
	var asked []string
	s.askFn = func(_ context.Context, code, analyst, q string) (string, error) {
		asked = append(asked, analyst)
		return analyst + "的回答", nil
	}
	sp := &speaker{}
	s.stepFn = sp.fn
	a := newCustomAPI(t, s)
	_, out := a.call("POST", "/api/scan/custom", `{"title":"问两位","kind":"ask","scope":"watch","at":"11:00","question":"怎么看","analysts":["news","volume_flow"]}`)
	task := out["task"].(map[string]any)
	if task["analysts_text"] != "问：新闻、资金流" {
		t.Fatalf("view = %v", task)
	}
	a.call("POST", "/api/scan/custom/"+task["id"].(string)+"/run", "")
	s.sch.TickSync()
	answers := inboxTypes(s, "custom.answer")
	if strings.Join(asked, ",") != "news,volume_flow" || len(answers) != 1 || !strings.Contains(answers[0]["body"].(string), "【新闻】news的回答") || !strings.Contains(answers[0]["body"].(string), "【资金流】volume_flow的回答") {
		t.Fatalf("asked %v answers %v", asked, answers)
	}
	// an analysis of two sides only: each one speaks, nobody weighs them, nothing is written down as a prediction
	_, out = a.call("POST", "/api/scan/custom", `{"title":"只看资金和板块","kind":"analyze","scope":"watch","at":"14:30","analysts":["volume_flow","sector"]}`)
	id := out["task"].(map[string]any)["id"].(string)
	a.call("POST", "/api/scan/custom/"+id+"/run", "")
	s.sch.TickSync()
	got := inboxTypes(s, "custom.analysis")
	if len(got) != 1 || !strings.Contains(got[0]["body"].(string), "资金流：偏多 — volume_flow 的结论") || !strings.Contains(got[0]["body"].(string), "板块：") || strings.Contains(got[0]["body"].(string), "新闻") {
		t.Fatalf("analysis = %v", got)
	}
	if sp.calls["chief"] != 0 || sp.calls["news"] != 0 || len(s.scan.All()) != 0 {
		t.Fatalf("the others spoke or a prediction was made: %v, %d", sp.calls, len(s.scan.All()))
	}
	if code, out := a.call("POST", "/api/scan/custom", `{"title":"x","kind":"analyze","scope":"hold","at":"10:00","analysts":["chief"]}`); code != 400 || !strings.Contains(out["error"].(string), "没有这位分析师") {
		t.Fatalf("chief for analyze = %d %v", code, out)
	}
}

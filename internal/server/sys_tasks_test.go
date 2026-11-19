package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sysAPI struct {
	t   *testing.T
	mux *http.ServeMux
}

func (a sysAPI) call(method, path, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func newSysAPI(t *testing.T, s *Server) sysAPI {
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	return sysAPI{t, mux}
}

func TestSysTasksEditSoftDeleteRestoreAndJobs(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-11", "10:00"))
	a := newSysAPI(t, s)

	code, out := a.call("GET", "/api/scan/sys/tasks?category=scan&include_deleted=1", "")
	if code != 200 {
		t.Fatalf("list = %d %v", code, out)
	}
	tasks, _ := out["tasks"].([]any)
	if len(tasks) == 0 {
		t.Fatal("expected scan system tasks")
	}
	var nightly map[string]any
	for _, raw := range tasks {
		item := raw.(map[string]any)
		if item["task_id"] == "stock.nightly" {
			nightly = item
			break
		}
	}
	if nightly == nil {
		t.Fatal("missing stock.nightly")
	}
	if nightly["editable"] != true || nightly["category"] != "scan" {
		t.Fatalf("nightly meta = %v", nightly)
	}

	code, out = a.call("PUT", "/api/scan/sys/tasks/stock.nightly", `{"paused":true,"trigger":{"kind":"daily","at":"21:10","window_min":20,"days":"trading"}}`)
	if code != 200 {
		t.Fatalf("put = %d %v", code, out)
	}
	if out["paused"] != true {
		t.Fatalf("paused = %v", out["paused"])
	}
	tr := out["trigger"].(map[string]any)
	if tr["at"] != "21:10" || tr["window_min"] != float64(20) {
		t.Fatalf("trigger = %v", tr)
	}
	sp, _ := s.sch.Spec("stock.nightly")
	eff := s.sch.EffectiveTrigger(sp)
	if eff.At != "21:10" || eff.WindowMin != 20 {
		t.Fatalf("effective = %+v", eff)
	}

	code, out = a.call("DELETE", "/api/scan/sys/tasks/stock.nightly", "")
	if code != 200 || out["deleted"] != true {
		t.Fatalf("delete = %d %v", code, out)
	}
	if !s.sch.Store.Deleted("stock.nightly") || !s.sch.Store.Paused("stock.nightly") {
		t.Fatal("soft delete should mark deleted+paused")
	}
	if code, out = a.call("POST", "/api/scan/sys/tasks/stock.nightly/run", ""); code != 409 {
		t.Fatalf("run deleted = %d %v", code, out)
	}

	code, out = a.call("GET", "/api/scan/sys/tasks?category=scan&include_deleted=1", "")
	if code != 200 {
		t.Fatalf("list deleted = %d", code)
	}
	deleted, _ := out["deleted"].([]any)
	if len(deleted) == 0 {
		t.Fatal("expected deleted list")
	}

	code, out = a.call("POST", "/api/scan/sys/tasks/stock.nightly/restore", "")
	if code != 200 || out["deleted"] != false {
		t.Fatalf("restore = %d %v", code, out)
	}
	if s.sch.Store.Deleted("stock.nightly") {
		t.Fatal("still deleted after restore")
	}

	code, out = a.call("POST", "/api/scan/sys/tasks/stock.nightly/run", "")
	if code != 202 || out["queued"] != true {
		t.Fatalf("run = %d %v", code, out)
	}
	runID, _ := out["run_id"].(string)
	if runID == "" {
		t.Fatal("missing run_id")
	}
	s.sch.Wait()

	code, out = a.call("GET", "/api/scan/sys/tasks/stock.nightly/runs?limit=5", "")
	if code != 200 {
		t.Fatalf("runs = %d %v", code, out)
	}
	runs, _ := out["runs"].([]any)
	if len(runs) == 0 {
		t.Fatal("expected runs")
	}

	code, out = a.call("GET", "/api/scan/sys/runs/"+runID+"/jobs", "")
	if code != 200 {
		t.Fatalf("jobs = %d %v", code, out)
	}
	if _, ok := out["jobs"]; !ok {
		t.Fatalf("jobs missing: %v", out)
	}

	if code, _ = a.call("PUT", "/api/scan/sys/tasks/no.such", `{"paused":true}`); code != 404 {
		t.Fatalf("unknown id = %d", code)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go-harness/internal/personal"
)

// qualityService plays the data service's quality endpoints.
func qualityService(t *testing.T, peersStatus *atomic.Value) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v1/shared/quality-peers/"):
			if st, _ := peersStatus.Load().(string); st == "computing" {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "computing", "items": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "industry": "银行", "checked": 30, "items": []any{
				map[string]any{"ts_code": "600036.SH", "name": "好公司甲", "why": "ROE 18.0% · 营收同比 +12%"},
				map[string]any{"ts_code": "000001.SZ", "name": "好公司乙", "why": "ROE 15.0%"},
			}})
		case p == "/api/v1/shared/quality/000001.SZ":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "level": "red", "flags": []any{map[string]any{"level": "red", "text": "被特别处理（ST/*ST 或退市整理）：有退市风险"}}, "metrics": map[string]any{"roe": 0.01}})
		case strings.HasPrefix(p, "/api/v1/shared/quality/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "level": "clean", "flags": []any{}, "metrics": map[string]any{"roe": 0.12}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("STOCK_API_BASE", api.URL)
}

func TestTheQualityScreenReadsWhatTheUserHoldsAndWantsAndTellsOnceWhenItIsBad(t *testing.T) {
	var peers atomic.Value
	peers.Store("ok")
	qualityService(t, &peers)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "20:10"))
	s.personal.TradeAdd(personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-10-01"})
	if _, err := s.personal.WishAdd(personal.Wish{Code: "600519.SH", Name: "贵州茅台", Reason: "x", Conditions: []personal.Item{{"kind": "price_below", "value": 1000.0}}}); err != nil {
		t.Fatal(err)
	}
	a := newCustomAPI(t, s)
	code, out := a.call("GET", "/api/v1/holdings/quality", "")
	if code != 200 || out["red"] != float64(1) || out["pending"] != false {
		t.Fatalf("view = %d %v", code, out)
	}
	rows := out["rows"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["kind"] != "hold" || rows[0].(map[string]any)["level"] != "red" || rows[1].(map[string]any)["kind"] != "wish" {
		t.Fatalf("rows = %v", rows)
	}
	picks := out["picks"].([]any)
	if len(picks) != 1 || picks[0].(map[string]any)["for_code"] != "600519.SH" || len(picks[0].(map[string]any)["items"].([]any)) != 2 {
		t.Fatalf("picks = %v", picks)
	}

	// the daily look says the red line once, and the better companies once for the same list
	s.runTask(t, "quality.watch")
	s.runTask(t, "quality.watch")
	red, picked := inboxTypes(s, "quality.redline"), inboxTypes(s, "quality.picks")
	if len(red) != 1 || !strings.Contains(red[0]["title"].(string), "平安银行（你持有）踩到质地红线：被特别处理") {
		t.Fatalf("red notices = %v", red)
	}
	if len(picked) != 1 || !strings.Contains(picked[0]["body"].(string), "好公司甲 · ROE 18.0%") || !strings.Contains(picked[0]["body"].(string), "不是买入建议") {
		t.Fatalf("picks notices = %v", picked)
	}
}

func TestTheQualityViewSaysWhenTheSameIndustryScreenIsStillRunning(t *testing.T) {
	var peers atomic.Value
	peers.Store("computing")
	qualityService(t, &peers)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	if _, err := s.personal.WishAdd(personal.Wish{Code: "600519.SH", Name: "贵州茅台", Reason: "x", Conditions: []personal.Item{{"kind": "price_below", "value": 1000.0}}}); err != nil {
		t.Fatal(err)
	}
	_, out := newCustomAPI(t, s).call("GET", "/api/v1/holdings/quality", "")
	if out["pending"] != true || out["picks"].([]any)[0].(map[string]any)["status"] != "computing" {
		t.Fatalf("view = %v", out)
	}
}

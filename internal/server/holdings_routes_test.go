package server

import (
	"encoding/json"
	"go-harness/internal/personal"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeShared plays the data service's shared endpoints.
func fakeShared(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v1/shared/sector/"):
			_, _ = w.Write([]byte(`{"name":"银行","verdict":"三层同向","flow":8.2,"heat":1.5,"source":"ETF","tracked":true}`))
		case p == "/api/v1/shared/market":
			_, _ = w.Write([]byte(`{"market":{"level":"中性","tilt":"偏弱","action":"x","text":"中性偏弱 · 不加仓","stance":"不加仓"}}`))
		case p == "/api/v1/shared/market-since":
			_, _ = w.Write([]byte(`{"since":"2026-10-26"}`))
		case p == "/api/v1/shared/tracked-sectors":
			_, _ = w.Write([]byte(`{"items":[{"name":"银行","verdict":"三层同向","flow":8.2,"heat":1.5,"tracked":true}]}`))
		case strings.HasPrefix(p, "/api/v1/shared/flow/"):
			_, _ = w.Write([]byte(`{"in":0,"out":0}`))
		case strings.HasPrefix(p, "/api/v1/shared/closes/"), p == "/api/v1/shared/index-closes", p == "/api/v1/shared/calendar":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case p == "/api/v1/stocks/search":
			_, _ = w.Write([]byte(`{"items":[{"ts_code":"600036.SH","name":"招商银行"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("STOCK_API_BASE", api.URL)
}

func TestHoldingsRoutesRecordValueAndRefuseImpossibleSales(t *testing.T) {
	fakeShared(t)
	s, _ := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	s.quotes.Get = func(string) (string, error) { return `v_sh600036="1~招商银行~600036~11.00~10.50~1";`, nil }
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	call := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, out := call("POST", "/api/v1/holdings/trades", `{"ts_code":"600036.SH","side":"sell","price":11,"qty":100,"date":"2026-11-01"}`); code != 400 || !strings.Contains(out["error"].(string), "卖出") {
		t.Fatalf("sell = %d %v", code, out)
	}
	code, out := call("POST", "/api/v1/holdings/trades", `{"ts_code":"600036.SH","side":"buy","price":10,"qty":100,"date":"2026-11-01","reason":"高股息","invalid":[{"kind":"close_below","value":9}]}`)
	if code != 201 || out["label"] != "建仓" {
		t.Fatalf("buy = %d %v", code, out)
	}
	code, out = call("GET", "/api/v1/holdings/summary", "")
	row := out["rows"].([]any)[0].(map[string]any)
	if code != 200 || out["status"] != "ok" || row["name"] != "招商银行" || row["pnl"].(float64) != 100 || out["header"].(map[string]any)["mv"].(float64) != 1100 {
		t.Fatalf("summary = %d %v", code, out)
	}
	if code, out := call("GET", "/api/v1/holdings/stock/600036.SH", ""); code != 200 || out["needs_price"] != false {
		t.Fatalf("detail = %d %v", code, out)
	}
	if code, _ := call("GET", "/api/v1/holdings/stock/000001.SZ", ""); code != 404 {
		t.Fatalf("unknown stock = %d", code)
	}
	if code, out := call("GET", "/api/v1/holdings/tab/chk", ""); code != 200 || out["empty"] != false {
		t.Fatalf("check = %d %v", code, out)
	}
	if code, _ := call("GET", "/api/v1/holdings/tab/zzz", ""); code != 400 {
		t.Fatal("bad tab accepted")
	}
	if code, out := call("POST", "/api/v1/holdings/wishes", `{"ts_code":"600036.SH","reason":"x"}`); code != 400 {
		t.Fatalf("wish on a held stock = %d %v", code, out)
	}
}

func TestPersonalImportLoadsOnceAndOnlyIntoAnEmptyStore(t *testing.T) {
	s, _ := newClientServer(t, zoneTime("2026-11-05", "20:00"))
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	body := `{"trades":[{"id":7,"ts_code":"600036.SH","name":"招商银行","side":"buy","price":10,"qty":100,"date":"2026-11-01","invalid":[{"kind":"close_below","value":9}],"context":{"imported":true},"created_at":1}],
	"reasons":[{"ts_code":"600036.SH","text":"高股息","invalid":[],"written_at":"2026-11-01","updated_at":1}],"watch":[{"ts_code":"300750.SZ","name":"宁德时代","added_at":1,"sort_order":1}],"alerts":[{"id":9,"ts_code":"600036.SH","side":"down","price":9,"confirm":"close","base_name":"理由不成立","created_at":1}]}`
	post := func() int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/personal/import", strings.NewReader(body)))
		return rec.Code
	}
	if post() != 200 || len(s.personal.Trades()) != 1 || len(s.personal.WatchList()) != 1 || len(s.personal.Alerts("600036.SH")) != 1 {
		t.Fatal("import did not load the data")
	}
	if r, ok := s.personal.ReasonOf("600036.SH"); !ok || r.Text != "高股息" {
		t.Fatalf("reason = %+v", r)
	}
	if post() != 409 || len(s.personal.Trades()) != 1 {
		t.Fatal("a second import went through")
	}
	next, err := s.personal.TradeAdd(personalTrade())
	if err != nil || next.ID <= 9 {
		t.Fatalf("new ids collide with imported ones: %+v %v", next, err)
	}
}

func personalTrade() personal.Trade {
	return personal.Trade{Code: "000001.SZ", Name: "平安银行", Side: "buy", Price: 10, Qty: 100, Date: "2026-11-02"}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetService puts the process-wide address setting back as it was found.
func resetService(t *testing.T) {
	t.Helper()
	serviceMu.Lock()
	oldSaved, oldEnv, oldOk, oldFile := serviceSaved, serviceEnv, serviceEnvOk, serviceFile
	serviceMu.Unlock()
	oldEnvVar, hadEnvVar := os.LookupEnv("STOCK_API_BASE")
	t.Cleanup(func() {
		serviceMu.Lock()
		serviceSaved, serviceEnv, serviceEnvOk, serviceFile = oldSaved, oldEnv, oldOk, oldFile
		serviceMu.Unlock()
		if hadEnvVar {
			_ = os.Setenv("STOCK_API_BASE", oldEnvVar)
		} else {
			_ = os.Unsetenv("STOCK_API_BASE")
		}
		quotesDownMu.Lock()
		quotesDownUntil = time.Time{}
		quotesDownMu.Unlock()
	})
	serviceMu.Lock()
	serviceSaved, serviceEnvOk = "", false
	serviceMu.Unlock()
}

func TestTheServiceAddressIsCheckedSavedAndWinsOverTheEnvironment(t *testing.T) {
	resetService(t)
	t.Setenv("STOCK_API_BASE", "http://from-env:1")
	file := filepath.Join(t.TempDir(), "service.json")
	loadServiceSetting(file)
	if stockAPIBase() != "http://from-env:1" || serviceSource() != "env" {
		t.Fatalf("before: %s %s", stockAPIBase(), serviceSource())
	}
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	mux := http.NewServeMux()
	s.registerClientRoutes(mux)
	call := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	for _, bad := range []string{`{"base_url":"ftp://x"}`, `{"base_url":"http://x/api/v1"}`, `{"base_url":"http://"}`} {
		if code, out := call("PUT", "/api/v1/settings/service", bad); code != 400 {
			t.Fatalf("%s was accepted: %v", bad, out)
		}
	}
	code, out := call("PUT", "/api/v1/settings/service", `{"base_url":" 10.0.0.5:8090/ "}`)
	if code != 200 || out["effective"] != "http://10.0.0.5:8090" || out["source"] != "saved" || stockAPIBase() != "http://10.0.0.5:8090" {
		t.Fatalf("saved = %d %v (%s)", code, out, stockAPIBase())
	}
	if raw, _ := os.ReadFile(file); !strings.Contains(string(raw), "10.0.0.5:8090") {
		t.Fatalf("not written to disk: %s", raw)
	}
	// the next start reads it back
	resetService(t)
	t.Setenv("STOCK_API_BASE", "http://from-env:1")
	loadServiceSetting(file)
	if stockAPIBase() != "http://10.0.0.5:8090" {
		t.Fatalf("after restart: %s", stockAPIBase())
	}
	// clearing it goes back to the environment
	if code, out := call("PUT", "/api/v1/settings/service", `{"base_url":""}`); code != 200 || out["source"] != "env" || stockAPIBase() != "http://from-env:1" {
		t.Fatalf("cleared = %d %v (%s)", code, out, stockAPIBase())
	}
}

func TestEveryPageIsServedWithTheCurrentServiceAddress(t *testing.T) {
	resetService(t)
	t.Setenv("STOCK_API_BASE", "http://remote.example:8090")
	rec := httptest.NewRecorder()
	serveStaticFile(rec, "static/scan.html")
	if !strings.Contains(rec.Body.String(), `<meta name="stock-api-base" content="http://remote.example:8090">`) {
		t.Fatalf("the page still points at the old address")
	}
}

func TestPricesComeFromTheServiceAndTheSourceCoversWhatItLacks(t *testing.T) {
	resetService(t)
	var asked []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/shared/quotes" {
			http.NotFound(w, r)
			return
		}
		asked = append(asked, r.URL.Query().Get("codes"))
		_, _ = w.Write([]byte(`{"status":"ok","items":{"600036.SH":{"price":39.5,"prev":39.2,"change":0.3,"change_pct":0.77}}}`))
	}))
	t.Cleanup(api.Close)
	t.Setenv("STOCK_API_BASE", api.URL)
	s, _ := newClientServer(t, zoneTime("2026-11-09", "10:00"))
	s.quotes.Via = s.backendQuotes
	s.quotes.Get = func(url string) (string, error) {
		return `v_sz000001="1~平安银行~000001~11.00~10.50~1";`, nil
	}
	got, err := s.quotes.Fetch([]string{"600036.SH", "000001.SZ"})
	if err != nil || got["600036.SH"].Price != 39.5 || got["000001.SZ"].Price != 11 || len(asked) != 1 || asked[0] != "600036.SH,000001.SZ" {
		t.Fatalf("got %v err %v asked %v", got, err, asked)
	}
	// a service that is down is left alone for a while instead of costing a timeout every time
	api.Close()
	s.quotes.TTL = 0
	_, _ = s.quotes.Fetch([]string{"600036.SH"})
	start := time.Now()
	_, _ = s.quotes.Fetch([]string{"600036.SH"})
	if time.Since(start) > time.Second {
		t.Fatalf("a downed service was asked again (%v)", time.Since(start))
	}
}

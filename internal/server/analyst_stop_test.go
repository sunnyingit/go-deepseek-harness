package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-harness/core/agentloop"
	"go-harness/core/session"
)

func newTrackedIdleAgent(t *testing.T) *agentloop.Agent {
	t.Helper()
	sess := session.New(session.SessionId("stop-test"), session.Header{})
	return agentloop.NewAgent(sess, wrapSpeakLLM{text: `{"text":"x","status":"neutral","status_text":"不明","fact_ids":[]}`}, fakeTools{}, "sys", agentloop.Hooks{})
}

func TestCancelAnalystsCallsCancel(t *testing.T) {
	s := &Server{}
	ag := newTrackedIdleAgent(t)
	untrack := s.trackAnalyst("600000.SH", ag)
	if n := s.cancelAnalysts("600000.SH", "user stop"); n != 1 {
		t.Fatalf("cancelled = %d, want 1", n)
	}
	if n := s.cancelAnalysts("600000.SH", "again"); n != 1 {
		t.Fatalf("still tracked until untrack: cancelled = %d", n)
	}
	untrack()
	if n := s.cancelAnalysts("600000.SH", "gone"); n != 0 {
		t.Fatalf("after untrack cancelled = %d, want 0", n)
	}
}

func TestHandleAnalystStopIdempotent(t *testing.T) {
	s := &Server{}
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/stocks/600000.SH/agents/stop", bytes.NewBufferString(`{"reason":"user stop"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "cancelling" {
		t.Fatalf("status = %v", body["status"])
	}
	if n, _ := body["cancelled"].(float64); n != 0 {
		t.Fatalf("cancelled = %v, want 0", body["cancelled"])
	}

	ag := newTrackedIdleAgent(t)
	untrack := s.trackAnalyst("600000.SH", ag)
	defer untrack()

	req2 := httptest.NewRequest(http.MethodPost, "/api/stocks/600000.SH/agents/stop", bytes.NewBufferString(`{}`))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	var body2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if n, _ := body2["cancelled"].(float64); n != 1 {
		t.Fatalf("cancelled = %v, want 1", body2["cancelled"])
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/stocks/NOTACODe/agents/stop", nil)
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad code status = %d", badRec.Code)
	}
}

func TestAnalystTrackKey(t *testing.T) {
	if got := analystTrackKey("600000.SH", agentConfig{ID: "news"}); got != "600000.SH" {
		t.Fatalf("got %q", got)
	}
	if got := analystTrackKey("", agentConfig{ID: "news"}); got != "news:topic" {
		t.Fatalf("got %q", got)
	}
}

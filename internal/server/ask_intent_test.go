package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-harness/core/agentloop"
)

func TestParseAskIntentSectorThemeDoesNotReuse(t *testing.T) {
	raw := `{"intent":"sector_theme","reuse_last":true,"stock_query":"603986.SH","theme":"AI硬件","ask":"分析AI硬件板块","note":"板块","analysts":["sector"]}`
	got := parseAskIntent(raw, "603986.SH")
	if got.Intent != "sector_theme" || got.ReuseLast {
		t.Fatalf("got %+v", got)
	}
	if got.Theme != "AI硬件" || len(got.Analysts) != 1 || got.Analysts[0] != "sector" {
		t.Fatalf("theme/analysts = %+v", got)
	}
}

func TestParseAskIntentFollowupNeedsLast(t *testing.T) {
	raw := `{"intent":"stock_followup","reuse_last":true,"ask":"关于兆易创新所属产业：这个产业走下坡路吗","note":"沿用","analysts":["sector"]}`
	got := parseAskIntent(raw, "603986.SH")
	if got.Intent != "stock_followup" || !got.ReuseLast {
		t.Fatalf("with last: %+v", got)
	}
	empty := parseAskIntent(raw, "")
	if empty.Intent != "general" || empty.ReuseLast {
		t.Fatalf("without last: %+v", empty)
	}
}

func TestParseAskIntentUnknownFallsToGeneral(t *testing.T) {
	got := parseAskIntent(`{"intent":"weird"}`, "603986.SH")
	if got.Intent != "general" || got.ReuseLast {
		t.Fatalf("%+v", got)
	}
}

func TestResolveAskIntentStockNewFromJSON(t *testing.T) {
	s, _ := newTestServer(t)
	s.intentLLM = oneShotLLM{reply: `{"intent":"stock_new","reuse_last":false,"stock_query":"兆易创新","theme":"","ask":"分析兆易创新","note":"新点名","analysts":["fundamental"]}`}
	got := s.resolveAskIntent(context.Background(), "兆易创新怎么样", "", "")
	if got.Intent != "stock_new" || got.StockQuery != "兆易创新" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveAskIntentEmptyFallsToGeneral(t *testing.T) {
	s, _ := newTestServer(t)
	s.intentLLM = oneShotLLM{reply: ""}
	got := s.resolveAskIntent(context.Background(), "随便聊聊", "", "")
	if got.Intent != "general" || got.ReuseLast {
		t.Fatalf("got %+v", got)
	}
}

type slowLLM struct{ delay time.Duration }

func (s slowLLM) Stream(ctx context.Context, _ agentloop.GenerateRequest) (<-chan agentloop.StreamChunk, error) {
	ch := make(chan agentloop.StreamChunk)
	go func() {
		defer close(ch)
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			ch <- agentloop.StreamChunk{Delta: agentloop.ContentBlock{Kind: agentloop.BlockText, Text: `{"intent":"stock_new","stock_query":"late"}`}}
		}
	}()
	return ch, nil
}

func TestResolveAskIntentTimeoutFallsToGeneral(t *testing.T) {
	s, _ := newTestServer(t)
	s.intentLLM = slowLLM{delay: 3 * time.Second}
	start := time.Now()
	got := s.resolveAskIntent(context.Background(), "兆易创新怎么样", "", "")
	elapsed := time.Since(start)
	if got.Intent != "general" {
		t.Fatalf("got %+v, want general on timeout", got)
	}
	if elapsed > askIntentTimeout+800*time.Millisecond {
		t.Fatalf("elapsed %v, want around askIntentTimeout=%v", elapsed, askIntentTimeout)
	}
}

func TestHandleAskIntentHTTP(t *testing.T) {
	s, _ := newTestServer(t)
	s.intentLLM = oneShotLLM{reply: `{"intent":"market_screen","reuse_last":false,"stock_query":"","theme":"","ask":"连涨3天有哪些","note":"筛选","analysts":[]}`}
	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	res, err := http.Post(srv.URL+"/api/ask/intent", "application/json",
		strings.NewReader(`{"question":"连涨3天以上有哪些"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var got askIntent
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Intent != "market_screen" {
		t.Fatalf("got %+v", got)
	}
}

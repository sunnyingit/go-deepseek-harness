package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"go-harness/core/session"
)

func TestWriteStreamEventEmitsStockCardAndKeepsToolText(t *testing.T) {
	cards := newUICardStash()
	cards.put("c9", map[string]any{"kind": "stock", "code": "603986.SH", "name": "兆易创新"})
	rec := httptest.NewRecorder()
	text := "ts_code=603986.SH name=兆易创新"

	writeStreamEvent(rec, session.Event{
		Type: "tool/result",
		Data: map[string]any{"message": session.Message{
			Role: session.RoleUser,
			Content: []session.ContentBlock{{
				Kind: session.BlockToolResult, CallID: "c9", Text: text,
			}},
		}},
	}, map[string]displayPending{}, cards)

	body := rec.Body.String()
	if !strings.Contains(body, "event: stock-card\n") {
		t.Fatalf("want stock-card, got %q", body)
	}
	if !strings.Contains(body, `"kind":"stock"`) || !strings.Contains(body, "603986.SH") {
		t.Fatalf("card payload missing, got %q", body)
	}
	if !strings.Contains(body, "event: tool-result\n") || !strings.Contains(body, text) {
		t.Fatalf("model-visible text must still stream, got %q", body)
	}
	if _, ok := cards.take("c9"); ok {
		t.Fatal("stash should be consumed")
	}
}

func TestExcerptRunes(t *testing.T) {
	if got := excerptRunes("一二三", 5); got != "一二三" {
		t.Fatalf("short = %q", got)
	}
	if got := excerptRunes("一二三四五六", 4); got != "一二三四…" {
		t.Fatalf("cut = %q", got)
	}
}

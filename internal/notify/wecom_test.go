package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmitSendsOncePerDay(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = append(got, string(raw))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()
	key := "chief:test-once"
	if status := Emit(srv.URL, key, "601689.SH 主分析师 偏空", "资金在撤。"); status != "sent" {
		t.Fatalf("status = %s", status)
	}
	if status := Emit(srv.URL, key, "again", "again"); status != "duplicate" {
		t.Fatalf("second = %s", status)
	}
	if len(got) != 1 || !strings.Contains(got[0], "资金在撤") {
		t.Fatalf("body = %v", got)
	}
}

func TestEmitSkipsWithoutWebhook(t *testing.T) {
	if status := Emit("", "chief:empty", "标题", "正文"); status != "unconfigured" {
		t.Fatalf("status = %s", status)
	}
}

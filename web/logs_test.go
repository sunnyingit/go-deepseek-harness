package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailFileTruncatesAtNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.log")
	body := strings.Repeat("a", 40) + "\nkeep-me\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	text, size, truncated, _, err := tailFile(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", size, len(body))
	}
	if !truncated {
		t.Fatal("expected truncated")
	}
	if !strings.Contains(text, "keep-me") {
		t.Fatalf("text = %q, want keep-me", text)
	}
	if strings.Contains(text, "aaa") {
		t.Fatalf("text still has dropped prefix: %q", text)
	}
}

func TestGetLogsMissingFile(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	t.Setenv("WEB_DEMO_LOG_FILE", filepath.Join(dir, "missing.log"))
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var view logView
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Available {
		t.Fatalf("available = true: %+v", view)
	}
	if view.Path == "" || view.Hint == "" {
		t.Fatalf("want path and hint, got %+v", view)
	}
}

func TestGetLogsReturnsTail(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "web.log")
	if err := os.WriteFile(path, []byte("startup: listening on :8081\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB_DEMO_LOG_FILE", path)
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var view logView
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.Available || !strings.Contains(view.Text, "listening") {
		t.Fatalf("view = %+v", view)
	}
	if view.Path != path {
		t.Fatalf("path = %q, want %q", view.Path, path)
	}
}

func TestRuntimeLogPathPrefersSiblingWebLog(t *testing.T) {
	s, _ := newTestServer(t)
	t.Setenv("WEB_DEMO_LOG_FILE", "")
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(root, "web.log")
	if err := os.WriteFile(sibling, []byte("from-electron\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.cfg.DataDir = dataDir
	if got := s.runtimeLogPath(); got != sibling {
		t.Fatalf("runtimeLogPath = %q, want %q", got, sibling)
	}
}

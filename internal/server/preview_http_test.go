package server

import (
	"bytes"
	"go-harness/internal/preview/previewtest"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleGetFileServesPptxPreview(t *testing.T) {
	s, sessionID := newTestServer(t)
	workspace := s.workspaceRoot(sessionID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	previewtest.WriteMinimalPPTX(t, filepath.Join(workspace, "deck.pptx"))

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	t.Run("inline html preview", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/deck.pptx")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "Quarter Review") {
			t.Fatalf("preview body missing slide text: %s", body)
		}
	})

	t.Run("download stays original zip", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/deck.pptx?download=1")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if got := res.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") {
			t.Fatalf("Content-Disposition = %q", got)
		}
		body, _ := io.ReadAll(res.Body)
		if !bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("download should be the pptx zip, got %q", body[:min(20, len(body))])
		}
	})
}

func TestHandleGetFileServesXlsxPreview(t *testing.T) {
	s, sessionID := newTestServer(t)
	workspace := s.workspaceRoot(sessionID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	previewtest.WriteMinimalXLSX(t, filepath.Join(workspace, "book.xlsx"))

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	t.Run("inline html preview", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/book.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "咖啡") {
			t.Fatalf("preview body missing cell text: %s", body)
		}
	})

	t.Run("download stays original zip", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/book.xlsx?download=1")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if got := res.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") {
			t.Fatalf("Content-Disposition = %q", got)
		}
		body, _ := io.ReadAll(res.Body)
		if !bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("download should be the xlsx zip, got %q", body[:min(20, len(body))])
		}
	})
}

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIPages(t *testing.T) {
	s, _ := newTestServer(t)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/", "今天帮你做些什么"},
		{"/dialog.html", "PABuddy AI"},
		{"/dailog.html", "PABuddy AI"},
		{"/space.html", "个人空间"},
		{"/settings.html", "运行日志"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			res, err := http.Get(srv.URL + tc.path)
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
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("body does not contain %q", tc.want)
			}
			if strings.Contains(string(body), "cdnjs.cloudflare.com") || strings.Contains(string(body), "fonts.googleapis.com") {
				t.Fatal("page still points at a CDN for CSS/fonts")
			}
		})
	}
}

func TestVendoredStaticAssets(t *testing.T) {
	s, _ := newTestServer(t)
	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	t.Run("inter css", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/static/vendor/fonts/inter.css")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Fatalf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(res.Body)
		if !strings.Contains(string(body), "font-family: Inter") {
			t.Fatalf("unexpected css: %s", body)
		}
	})

	t.Run("font awesome css", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/static/vendor/fontawesome/css/all.min.css")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if strings.Contains(string(body), "cdnjs") {
			t.Fatal("font-awesome css still references a CDN")
		}
	})

	t.Run("woff2", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/static/vendor/fontawesome/webfonts/fa-solid-900.woff2")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct != "font/woff2" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("gallery png", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/static/vendor/gallery/starry.png")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("html not served via /static", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/static/index.html")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", res.StatusCode)
		}
	})
}

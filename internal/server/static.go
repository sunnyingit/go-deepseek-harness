package server

import (
	"errors"
	"go-harness/web"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticFS is the UI embedded by package web (static/*.html plus vendored
// assets); every page handler below serves a file out of it.
var staticFS = web.FS

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	serveStaticFile(w, "static/index.html")
}

// handleDialog serves the conversation page (static/dailog.html — original
// filename) at both GET /dialog.html and GET /dailog.html. Index.html's
// send button navigates here with session/text in the query string.
func (s *Server) handleDialog(w http.ResponseWriter, r *http.Request) {
	serveStaticFile(w, "static/dailog.html")
}

func (s *Server) handleSpace(w http.ResponseWriter, r *http.Request) {
	serveStaticFile(w, "static/space.html")
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	serveStaticFile(w, "static/settings.html")
}

// handleStaticAsset serves vendored CSS/fonts/images under /static/vendor/... —
// the only files besides the three HTML pages that the UI is allowed to
// fetch. Paths are confined to that prefix so this cannot be used to read
// the HTML sources (or anything else) out of the embed FS.
func (s *Server) handleStaticAsset(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean("/" + r.PathValue("path"))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." || !strings.HasPrefix(rel, "vendor/") {
		http.NotFound(w, r)
		return
	}
	name := path.Join("static", rel)
	switch path.Ext(rel) {
	case ".css", ".woff2", ".png", ".jpg", ".jpeg", ".webp":
	default:
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch path.Ext(rel) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

func serveStaticFile(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Homepage JS keeps a draft sessionId in memory. bfcache restoring that
	// page after a trip through dialog.html would reuse the previous
	// conversation and append the next "new task" onto it.
	w.Header().Set("Cache-Control", "no-store")
	data, err := staticFS.ReadFile(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

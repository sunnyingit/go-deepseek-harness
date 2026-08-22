// Workspace file listing/download/preview — the read-side counterpart to
// handleUploadFile in handlers.go. A session's workspace fills up with
// files the model itself created (write/edit/bash/run_python), not just
// ones a client uploaded, and until now there was no way for a client to
// see or fetch any of them back out.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	fileSourceUpload    = "upload"
	fileSourceGenerated = "generated"
	uploadsManifestName = ".harness-uploads"
)

// fileEntryDTO is one file the workspace-files listing reports — metadata
// only, never content (content is fetched separately via handleGetFile, so
// a listing stays cheap regardless of how large individual files are).
type fileEntryDTO struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ModTime   string `json:"modTime"`
	Source    string `json:"source"`
	SessionID string `json:"sessionId,omitempty"`
}

// handleListFiles lists every regular file anywhere under a session's
// workspace, recursively — the model's write/edit/bash/run_python tools can
// all create subdirectories, unlike the flat-only POST .../files upload —
// so a client can build a "files generated in this session" view without
// the model having to announce each one in chat.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.sessionWorkspaceOK(id) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	entries, err := listWorkspaceFiles(s.workspaceRoot(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleListAllFiles is the personal-space listing: every regular file in
// every session workspace on disk (live agents plus leftover folders),
// tagged with sessionId so the UI can preview/download/delete without a
// second round-trip to discover which session owns the path.
func (s *Server) handleListAllFiles(w http.ResponseWriter, r *http.Request) {
	out := []fileEntryDTO{}
	for _, id := range s.allSessionIDs() {
		entries, err := listWorkspaceFiles(s.workspaceRoot(id))
		if err != nil {
			continue
		}
		for _, e := range entries {
			e.SessionID = id
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	writeJSON(w, http.StatusOK, out)
}

// handleDeleteFile removes one file from a session workspace. Hidden
// harness metadata (the uploads manifest) is not a client-visible file and
// cannot be deleted through this path.
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.sessionWorkspaceOK(id) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}
	rel := r.PathValue("path")
	if isHarnessMeta(filepath.Base(rel)) {
		writeError(w, http.StatusBadRequest, errors.New("cannot delete workspace metadata"))
		return
	}
	root := s.workspaceRoot(id)
	full, err := resolveWorkspaceFile(root, rel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errors.New("no such file in this session's workspace"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, errors.New("path is a directory, not a file"))
		return
	}
	if err := os.Remove(full); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = unrecordUploadedPath(root, rel)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent)
}

// emitFileEvents diffs the workspace's current file list against before —
// the snapshot handleStreamSession took right before starting this turn —
// and writes one SSE "file" event per new or changed entry, so a client
// gets an automatic download/preview card for anything the turn's
// write/edit/bash/run_python tool calls produced, with no dependence on the
// model remembering to announce it itself. Errors listing the workspace are
// swallowed: a turn that otherwise completed successfully shouldn't fail
// its whole response over a file-listing hiccup.
func emitFileEvents(w http.ResponseWriter, workspaceRoot string, before []fileEntryDTO) {
	after, err := listWorkspaceFiles(workspaceRoot)
	if err != nil {
		return
	}
	for _, f := range diffNewOrChangedFiles(before, after) {
		writeSSE(w, "file", f)
	}
}

// diffNewOrChangedFiles returns every entry in after that is new or has
// changed (by size or modTime) relative to before.
func diffNewOrChangedFiles(before, after []fileEntryDTO) []fileEntryDTO {
	prev := make(map[string]fileEntryDTO, len(before))
	for _, f := range before {
		prev[f.Path] = f
	}
	var changed []fileEntryDTO
	for _, f := range after {
		old, existed := prev[f.Path]
		if !existed || old.Size != f.Size || old.ModTime != f.ModTime {
			changed = append(changed, f)
		}
	}
	return changed
}

func listWorkspaceFiles(root string) ([]fileEntryDTO, error) {
	uploads := loadUploadSet(root)
	out := []fileEntryDTO{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipWorkspaceDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if skipWorkspaceRel(relSlash) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // an entry that vanished between WalkDir and Info is skipped, not fatal
		}
		source := fileSourceGenerated
		if _, ok := uploads[relSlash]; ok {
			source = fileSourceUpload
		}
		out = append(out, fileEntryDTO{
			Path:    relSlash,
			Name:    d.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
			Source:  source,
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // no workspace directory yet — nothing generated, not an error
		}
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out, nil
}

// handleGetFile serves one file out of a session's workspace: inline by
// default (so a browser can render an image/PDF/text file directly, or the
// UI's own preview panel can fetch its content), or as an attachment via
// ?download=1.
//
// resolveWorkspaceFile is the confinement check this handler needs but
// fssandbox.Provider doesn't itself provide for reads (see its package doc:
// "every mode permits reading" — it trusts the caller). That's fine for the
// model's own read tool, whose path argument is mediated by the agent loop;
// it is not fine here, where the path arrives directly off the URL of an
// unauthenticated HTTP request.
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.sessionWorkspaceOK(id) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such session %q", id))
		return
	}

	full, err := resolveWorkspaceFile(s.workspaceRoot(id), r.PathValue("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errors.New("no such file in this session's workspace"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, errors.New("path is a directory, not a file"))
		return
	}

	filename := filepath.Base(full)
	download := r.URL.Query().Get("download") == "1"
	if !download {
		if htmlBody, ok := pptxPreviewHTML(full, filename); ok {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.html"`, filename))
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, filename+".html", info.ModTime(), bytes.NewReader(htmlBody))
			return
		}
		if htmlBody, ok := xlsxPreviewHTML(full, filename); ok {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.html"`, filename))
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, filename+".html", info.ModTime(), bytes.NewReader(htmlBody))
			return
		}
	}

	f, err := os.Open(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer f.Close()

	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filename))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if ctype := contentTypeFor(filename); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func contentTypeFor(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	default:
		return ""
	}
}

// resolveWorkspaceFile joins rel onto root and rejects the result unless it
// stays inside root — the one check standing between an arbitrary HTTP
// client and reading any file the server process can see. r.URL.Path (which
// rel is drawn from) is already percent-decoded by net/http, so a "../"
// segment arrives literal here, not disguised — filepath.Join + Clean
// collapses it before the prefix check runs.
func resolveWorkspaceFile(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("file path is required")
	}
	cleanRoot := filepath.Clean(root)
	full := filepath.Join(cleanRoot, filepath.FromSlash(rel))
	if full != cleanRoot && !strings.HasPrefix(full, cleanRoot+string(filepath.Separator)) {
		return "", errors.New("path escapes the session workspace")
	}
	return full, nil
}

func isHarnessMeta(name string) bool {
	return strings.HasPrefix(name, ".harness-")
}

// sessionWorkspaceOK is true when the UI can list/preview files for id:
// either the agent is still in memory, or the on-disk workspace survived
// (reopening a chat after restart used to 404 the file API and look empty).
func (s *Server) sessionWorkspaceOK(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := s.agents.Get(id); ok {
		return true
	}
	info, err := os.Stat(s.workspaceRoot(id))
	return err == nil && info.IsDir()
}

func (s *Server) allSessionIDs() []string {
	seen := map[string]struct{}{}
	var ids []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, id := range s.agents.List() {
		add(id)
	}
	ents, err := os.ReadDir(filepath.Join(s.cfg.DataDir, "sessions"))
	if err != nil {
		return ids
	}
	for _, e := range ents {
		if e.IsDir() {
			add(e.Name())
		}
	}
	return ids
}

// skipWorkspaceDir hides install/build trees from the session file list
// and from the chat's automatic HTML preview cards. An artifacts project
// often lands node_modules next to bundle.html; listing those files would
// drown the one page the UI should iframe. dist/ is kept — that's where
// the built page usually lives.
func skipWorkspaceDir(name string) bool {
	switch name {
	case "node_modules", ".git", ".parcel-cache", ".vite", "coverage", "__pycache__", ".venv", "venv":
		return true
	default:
		return false
	}
}

func skipWorkspaceFile(name string) bool {
	if isHarnessMeta(name) || strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"pnpm-workspace.yaml", "components.json",
		"postcss.config.js", "postcss.config.cjs", "postcss.config.mjs",
		"vite.config.ts", "vite.config.js",
		"tsconfig.json", "tsconfig.app.json", "tsconfig.node.json":
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".map") || strings.HasSuffix(lower, ".tsbuildinfo")
}

func skipWorkspaceRel(rel string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	if skipWorkspaceFile(base) {
		return true
	}
	lower := strings.ToLower(rel)
	if strings.Contains(lower, "/assets/") {
		switch strings.ToLower(filepath.Ext(base)) {
		case ".js", ".css", ".map":
			return true
		}
	}
	return false
}

func loadUploadSet(root string) map[string]struct{} {
	out := map[string]struct{}{}
	data, err := os.ReadFile(filepath.Join(root, uploadsManifestName))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out[filepath.ToSlash(line)] = struct{}{}
	}
	return out
}

func recordUploadedPath(root, rel string) error {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if _, ok := loadUploadSet(root)[rel]; ok {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(root, uploadsManifestName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, rel)
	return err
}

func unrecordUploadedPath(root, rel string) error {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	set := loadUploadSet(root)
	if _, ok := set[rel]; !ok {
		return nil
	}
	delete(set, rel)
	lines := make([]string, 0, len(set))
	for p := range set {
		lines = append(lines, p)
	}
	sort.Strings(lines)
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(filepath.Join(root, uploadsManifestName), []byte(body), 0o644)
}

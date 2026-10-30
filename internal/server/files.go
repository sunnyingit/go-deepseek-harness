// Workspace file listing/download/preview — the read-side counterpart to
// handleUploadFile in handlers.go. A session's workspace fills up with
// files the model itself created (write/edit/bash/run_python), not just
// ones a client uploaded, and until now there was no way for a client to
// see or fetch any of them back out.
package server

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

	"go-harness/internal/preview"
)

const (
	fileSourceUpload      = "upload"
	fileSourceGenerated   = "generated"
	fileSourceLocal       = "local"
	uploadsManifestName   = ".harness-uploads"
	generatedManifestName = ".harness-generated"
	sessionOwnedFilesName = "workspace-files"
	localListCap          = 2000
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
	Kind      string `json:"kind,omitempty"`
	Location  string `json:"location,omitempty"`
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
	entries, err := s.listFilesForSession(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// listFilesForSession is the per-session file listing the dialog page
// consumes. A shared workspace_dir holds every task's artifacts in one
// folder — listing that whole folder as "this session's files" mixed
// other conversations into the current chat. When the workspace is shared,
// only files this session uploaded or generated are returned. Per-session
// sandboxes still list the sandbox directory as before.
func (s *Server) listFilesForSession(id string) ([]fileEntryDTO, error) {
	root := s.workspaceRoot(id)
	if shared := strings.TrimSpace(s.cfg.WorkspaceDir); shared != "" && sameDir(root, shared) {
		return s.listSessionOwnedFiles(id)
	}
	return listWorkspaceFiles(root)
}

// handleListAllFiles is the personal-space listing: every regular file in
// every session workspace on disk (live agents plus leftover folders),
// tagged with sessionId so the UI can preview/download/delete without a
// second round-trip to discover which session owns the path.
func (s *Server) handleListAllFiles(w http.ResponseWriter, r *http.Request) {
	out := []fileEntryDTO{}
	shared := strings.TrimSpace(s.cfg.WorkspaceDir)
	if shared != "" {
		entries, err := listKnowledgeDirFiles(shared)
		if err == nil {
			out = append(out, entries...)
		}
	}
	for _, id := range s.allSessionIDs() {
		root := s.sessionSandboxDir(id)
		if shared != "" && sameDir(root, shared) {
			continue
		}
		entries, err := listWorkspaceFiles(root)
		if err != nil {
			continue
		}
		loc := s.sessionListLocation(id)
		for _, e := range entries {
			e.SessionID = id
			e.Kind = fileKindOf(e.Name)
			if e.Location == "" {
				e.Location = loc
			}
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
	_ = unrecordGeneratedPath(root, rel)
	_ = s.unrecordSessionFile(id, rel)
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
func emitFileEvents(w http.ResponseWriter, workspaceRoot string, before []fileEntryDTO) []fileEntryDTO {
	after, err := listWorkspaceFiles(workspaceRoot)
	if err != nil {
		return nil
	}
	changed := diffNewOrChangedFiles(before, after)
	paths := make([]string, 0, len(changed))
	for _, f := range changed {
		paths = append(paths, f.Path)
	}
	_ = recordGeneratedPaths(workspaceRoot, paths)
	for _, f := range changed {
		if f.Source == "" || f.Source == fileSourceLocal {
			f.Source = fileSourceGenerated
		}
		writeSSE(w, "file", f)
	}
	return changed
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
			if _, uploaded := uploads[relSlash]; !uploaded {
				return nil
			}
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
			Kind:    fileKindOf(d.Name()),
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
	return filterSidecarFiles(out), nil
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
	serveDiskFile(w, r, full, "no such file in this session's workspace")
}

// handleGetWorkspaceFile serves one file from the configured working
// directory (server.workspace_dir). Same confinement as session files:
// the path must stay inside that root.
func (s *Server) handleGetWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(s.cfg.WorkspaceDir)
	if root == "" {
		writeError(w, http.StatusNotFound, errors.New("workspace_dir is not configured"))
		return
	}
	full, err := resolveWorkspaceFile(root, r.PathValue("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	serveDiskFile(w, r, full, "no such file in the working directory")
}

func serveDiskFile(w http.ResponseWriter, r *http.Request, full, missingMsg string) {
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errors.New(missingMsg))
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
		if htmlBody, ok := preview.PPTXHTML(full, filename); ok {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.html"`, filename))
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, filename+".html", info.ModTime(), bytes.NewReader(htmlBody))
			return
		}
		if htmlBody, ok := preview.XLSXHTML(full, filename); ok {
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
	info, err := os.Stat(s.sessionDataDir(id))
	return err == nil && info.IsDir()
}

func (s *Server) sessionListLocation(id string) string {
	if ag, ok := s.agents.Get(id); ok {
		if sess := ag.Session(); sess != nil {
			if t := sessionTitle(sess); t != "" {
				return t
			}
		}
	}
	if id == "" {
		return "任务"
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
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
const (
	fileKindDoc     = "doc"
	fileKindCode    = "code"
	fileKindSheet   = "sheet"
	fileKindPPT     = "ppt"
	fileKindWeb     = "web"
	fileKindImage   = "image"
	fileKindArchive = "archive"
	fileKindFile    = "file"
)

func fileKindOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
		return fileKindWeb
	case ".md", ".txt", ".doc", ".docx", ".pdf", ".rtf", ".log":
		return fileKindDoc
	case ".xlsx", ".xls", ".xlsm", ".csv":
		return fileKindSheet
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return fileKindImage
	case ".ppt", ".pptx", ".pptm":
		return fileKindPPT
	case ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".go", ".rs", ".java",
		".c", ".h", ".cpp", ".json", ".yaml", ".yml", ".xml", ".toml", ".sh",
		".bash", ".zsh", ".css", ".scss", ".sass", ".less":
		return fileKindCode
	case ".zip", ".tar", ".gz", ".tgz", ".rar", ".7z":
		return fileKindArchive
	default:
		return fileKindFile
	}
}

func skipLocalDir(name string) bool {
	if skipWorkspaceDir(name) {
		return true
	}
	switch name {
	case ".idea", ".vscode", ".next", ".nuxt", ".turbo", "target",
		".tox", ".mypy_cache", ".pytest_cache", ".hg", ".svn",
		".DS_Store":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func skipLocalFile(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "thumbs.db", "desktop.ini":
		return true
	}
	return false
}

func localLocation(rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return "工作目录"
	}
	if i := strings.Index(rel, "/"); i > 0 {
		return rel[:i]
	}
	return "工作目录"
}

func listLocalDirFiles(root string) ([]fileEntryDTO, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	out := []fileEntryDTO{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipLocalDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if skipLocalFile(d.Name()) {
			return nil
		}
		if len(out) >= localListCap {
			return fs.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		out = append(out, fileEntryDTO{
			Path:     relSlash,
			Name:     d.Name(),
			Size:     fi.Size(),
			ModTime:  fi.ModTime().UTC().Format(time.RFC3339),
			Source:   fileSourceLocal,
			Kind:     fileKindOf(d.Name()),
			Location: localLocation(relSlash),
		})
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return nil, err
	}
	return out, nil
}

func skipWorkspaceDir(name string) bool {
	switch name {
	case "node_modules", ".git", ".parcel-cache", ".vite", "coverage",
		"__pycache__", ".venv", "venv", ".pptx-icons":
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

func skipScaffoldName(name string) bool {
	switch strings.ToLower(name) {
	case "favicon.svg", "favicon.ico", "vite.svg":
		return true
	default:
		return false
	}
}

func skipSourceExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ts", ".tsx", ".mts", ".cts", ".jsx", ".vue",
		".css", ".scss", ".sass", ".less":
		return true
	default:
		return false
	}
}

func skipWorkspaceRel(rel string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	if skipWorkspaceFile(base) || skipScaffoldName(base) || skipSourceExt(base) {
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

// filterSidecarFiles drops generated extras that only exist to build a
// deliverable. A pptx run writes icon PNGs/SVGs and generate.js next to the
// deck; an HTML page does the same with assets. Uploads and the deliverable
// itself stay visible.
func filterSidecarFiles(entries []fileEntryDTO) []fileEntryDTO {
	var hasPPTX, hasHTML bool
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Path)) {
		case ".pptx", ".pptm", ".ppt":
			hasPPTX = true
		case ".html", ".htm":
			hasHTML = true
		}
	}
	if !hasPPTX && !hasHTML {
		return entries
	}
	out := make([]fileEntryDTO, 0, len(entries))
	for _, e := range entries {
		if e.Source == fileSourceUpload || e.Source == fileSourceLocal || !hideGeneratedSidecar(e.Path, hasPPTX, hasHTML) {
			out = append(out, e)
		}
	}
	return out
}

func hideGeneratedSidecar(rel string, hasPPTX, hasHTML bool) bool {
	if !hasPPTX && !hasHTML {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg",
		".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && filepath.Clean(aa) == filepath.Clean(bb)
}

func listKnowledgeDirFiles(root string) ([]fileEntryDTO, error) {
	entries, err := listLocalDirFiles(root)
	if err != nil {
		return nil, err
	}
	uploads := loadUploadSet(root)
	generated := loadGeneratedSet(root)
	for i, e := range entries {
		switch {
		case containsPath(uploads, e.Path):
			entries[i].Source = fileSourceUpload
		case containsPath(generated, e.Path):
			entries[i].Source = fileSourceGenerated
		default:
			entries[i].Source = fileSourceLocal
		}
		if entries[i].Kind == "" {
			entries[i].Kind = fileKindOf(e.Name)
		}
	}
	return filterSidecarFiles(entries), nil
}

func containsPath(set map[string]struct{}, path string) bool {
	_, ok := set[filepath.ToSlash(path)]
	return ok
}

func (s *Server) listSessionOwnedFiles(id string) ([]fileEntryDTO, error) {
	root := s.workspaceRoot(id)
	owned := loadPathSet(s.sessionDataDir(id), sessionOwnedFilesName)
	if len(owned) == 0 {
		return []fileEntryDTO{}, nil
	}
	all, err := listKnowledgeDirFiles(root)
	if err != nil {
		return nil, err
	}
	out := make([]fileEntryDTO, 0, len(owned))
	for _, e := range all {
		if containsPath(owned, e.Path) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Server) recordSessionFiles(sessionID string, paths []string) error {
	if strings.TrimSpace(sessionID) == "" || len(paths) == 0 {
		return nil
	}
	dir := s.sessionDataDir(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	set := loadPathSet(dir, sessionOwnedFilesName)
	changed := false
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" || isHarnessMeta(filepath.Base(p)) {
			continue
		}
		if _, ok := set[p]; ok {
			continue
		}
		set[p] = struct{}{}
		changed = true
	}
	if !changed {
		return nil
	}
	return writePathSet(dir, sessionOwnedFilesName, set)
}

func (s *Server) unrecordSessionFile(sessionID, rel string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return removePathFromSet(s.sessionDataDir(sessionID), sessionOwnedFilesName, rel)
}

func loadGeneratedSet(root string) map[string]struct{} {
	return loadPathSet(root, generatedManifestName)
}

func recordGeneratedPaths(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	uploads := loadUploadSet(root)
	set := loadGeneratedSet(root)
	changed := false
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" || isHarnessMeta(filepath.Base(p)) {
			continue
		}
		if _, ok := uploads[p]; ok {
			continue
		}
		if _, ok := set[p]; ok {
			continue
		}
		set[p] = struct{}{}
		changed = true
	}
	if !changed {
		return nil
	}
	return writePathSet(root, generatedManifestName, set)
}

func unrecordGeneratedPath(root, rel string) error {
	return removePathFromSet(root, generatedManifestName, rel)
}

func loadPathSet(root, name string) map[string]struct{} {
	out := map[string]struct{}{}
	data, err := os.ReadFile(filepath.Join(root, name))
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

func writePathSet(root, name string, set map[string]struct{}) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	lines := make([]string, 0, len(set))
	for p := range set {
		lines = append(lines, p)
	}
	sort.Strings(lines)
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(filepath.Join(root, name), []byte(body), 0o644)
}

func removePathFromSet(root, name, rel string) error {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	set := loadPathSet(root, name)
	if _, ok := set[rel]; !ok {
		return nil
	}
	delete(set, rel)
	return writePathSet(root, name, set)
}

func loadUploadSet(root string) map[string]struct{} {
	return loadPathSet(root, uploadsManifestName)
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
	return removePathFromSet(root, uploadsManifestName, rel)
}

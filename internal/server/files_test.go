package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreagent "go-harness/core/agent"
	"go-harness/core/agentloop"
	"go-harness/internal/appconfig"
)

// stubAgent is a minimal coreagent.Agent — just enough to register into the
// live-agent registry the file handlers key off (s.agents.Get(id)) without
// spinning up a real model/session, mirroring core/agent's own test helper.
type stubAgent struct{}

func (stubAgent) Status() string                     { return "idle" }
func (stubAgent) Session() agentloop.Session         { return nil }
func (stubAgent) Followup(agentloop.Message)         {}
func (stubAgent) Steer(agentloop.Message)            {}
func (stubAgent) Inject(agentloop.Message)           {}
func (stubAgent) Cancel(agentloop.CancelCause, bool) {}
func (stubAgent) WhenIdle()                          {}

var _ coreagent.Agent = stubAgent{}

// newTestServer builds a *Server with just enough wired up (agents registry
// + dataDir) to exercise the routes under test — handleListFiles/
// handleGetFile only ever touch s.agents and s.workspaceRoot, never the
// LLM/skills/sandbox fields a full New(ctx, cfg) would need (and which
// need a live DEEPSEEK_API_KEY + provisioned Python env to construct).
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	s := &Server{agents: coreagent.NewRegistry(), cfg: appconfig.Config{DataDir: dataDir}, answers: newAnswerRegistry()}
	const sessionID = "sess-1"
	if _, err := s.agents.Register(sessionID, stubAgent{}, ""); err != nil {
		t.Fatalf("registering stub agent: %v", err)
	}
	return s, sessionID
}

func TestResolveWorkspaceFile(t *testing.T) {
	root := t.TempDir()

	// filepath.Join doesn't special-case a leading "/" in its later
	// arguments — Join(root, "/etc/passwd") lands under root as
	// root/etc/passwd, it does not escape to the real /etc/passwd. Asserted
	// explicitly here since that's the property the whole confinement check
	// leans on.
	t.Run("leading slash stays confined", func(t *testing.T) {
		got, err := resolveWorkspaceFile(root, "/etc/passwd")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(filepath.Clean(root), "etc", "passwd")
		if got != want {
			t.Fatalf("resolveWorkspaceFile(%q) = %q, want %q", "/etc/passwd", got, want)
		}
	})

	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"plain file", "report.csv", false},
		{"nested file", "sub/dir/report.csv", false},
		{"traversal", "../outside.txt", true},
		{"traversal after nesting", "sub/../../outside.txt", true},
		{"empty", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWorkspaceFile(root, tc.rel)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveWorkspaceFile(%q) = %q, want error", tc.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveWorkspaceFile(%q) unexpected error: %v", tc.rel, err)
			}
			cleanRoot := filepath.Clean(root)
			if got != filepath.Join(cleanRoot, tc.rel) {
				t.Fatalf("resolveWorkspaceFile(%q) = %q, want %q", tc.rel, got, filepath.Join(cleanRoot, tc.rel))
			}
		})
	}
}

func TestListWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "nested.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatalf("listWorkspaceFiles: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["top.txt"]; !ok || e.Size != 5 {
		t.Errorf("top.txt entry wrong: %+v (ok=%v)", e, ok)
	}
	if e, ok := byPath["sub/nested.csv"]; !ok || e.Name != "nested.csv" {
		t.Errorf("sub/nested.csv entry wrong: %+v (ok=%v)", e, ok)
	}
	if e := byPath["top.txt"]; e.Source != fileSourceGenerated {
		t.Errorf("top.txt source = %q, want generated", e.Source)
	}
}

func TestListWorkspaceFilesSkipsInstallTrees(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<h1>built</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if _, ok := byPath["page.html"]; !ok {
		t.Fatalf("got %+v, want page.html", entries)
	}
	if _, ok := byPath["dist/index.html"]; !ok {
		t.Fatalf("got %+v, want dist/index.html (built artifacts must stay visible)", entries)
	}
	if _, ok := byPath["package.json"]; ok {
		t.Fatalf("package.json should be hidden from the file list: %+v", entries)
	}
	for _, e := range entries {
		if strings.Contains(e.Path, "node_modules") {
			t.Fatalf("node_modules leaked into listing: %+v", e)
		}
	}
}

func TestDiffNewOrChangedFiles(t *testing.T) {
	before := []fileEntryDTO{
		{Path: "a.txt", Name: "a.txt", Size: 10, ModTime: "2026-01-01T00:00:00Z"},
		{Path: "b.txt", Name: "b.txt", Size: 20, ModTime: "2026-01-01T00:00:00Z"},
	}
	after := []fileEntryDTO{
		{Path: "a.txt", Name: "a.txt", Size: 10, ModTime: "2026-01-01T00:00:00Z"}, // unchanged
		{Path: "b.txt", Name: "b.txt", Size: 25, ModTime: "2026-01-01T00:05:00Z"}, // modified
		{Path: "c.txt", Name: "c.txt", Size: 5, ModTime: "2026-01-01T00:05:00Z"},  // new
	}

	got := diffNewOrChangedFiles(before, after)
	if len(got) != 2 {
		t.Fatalf("got %d changed entries, want 2: %+v", len(got), got)
	}
	byPath := map[string]fileEntryDTO{}
	for _, f := range got {
		byPath[f.Path] = f
	}
	if _, ok := byPath["a.txt"]; ok {
		t.Errorf("a.txt is unchanged, should not be reported")
	}
	if f, ok := byPath["b.txt"]; !ok || f.Size != 25 {
		t.Errorf("b.txt modification not reported correctly: %+v (ok=%v)", f, ok)
	}
	if _, ok := byPath["c.txt"]; !ok {
		t.Errorf("c.txt new file not reported")
	}
}

// TestEmitFileEvents exercises the function handleStreamSession calls right
// before "done" — given a before-turn snapshot and the workspace's current
// state, it must write one SSE "file" event per new/changed file and none
// for a turn that touched nothing.
func TestEmitFileEvents(t *testing.T) {
	root := t.TempDir()
	before, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("no changes emits nothing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		emitFileEvents(rec, root, before)
		if rec.Body.Len() != 0 {
			t.Fatalf("expected no SSE output, got: %q", rec.Body.String())
		}
	})

	if err := os.WriteFile(filepath.Join(root, "out.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("new file emits one file event", func(t *testing.T) {
		rec := httptest.NewRecorder()
		emitFileEvents(rec, root, before)
		body := rec.Body.String()
		if !strings.Contains(body, "event: file") {
			t.Fatalf("expected an \"event: file\" line, got: %q", body)
		}
		if !strings.Contains(body, `"name":"out.txt"`) {
			t.Fatalf("expected out.txt in the event payload, got: %q", body)
		}
	})
}

func TestListWorkspaceFilesMissingRoot(t *testing.T) {
	entries, err := listWorkspaceFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected no error for a missing workspace, got: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty listing, got %+v", entries)
	}
}

// TestFileRoutesEndToEnd drives the real mux (s.Routes()) over HTTP, the
// same path a browser hits — list, inline preview, forced download, path
// -traversal rejection, and the unknown-session 404.
func TestFileRoutesEndToEnd(t *testing.T) {
	s, sessionID := newTestServer(t)
	workspace := s.workspaceRoot(sessionID)
	if err := os.MkdirAll(filepath.Join(workspace, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reports", "q2.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	t.Run("list", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		var entries []fileEntryDTO
		if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Path != "reports/q2.csv" {
			t.Fatalf("unexpected listing: %+v", entries)
		}
	})

	t.Run("inline preview", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/reports/q2.csv")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		if got := res.Header.Get("Content-Disposition"); got != `inline; filename="q2.csv"` {
			t.Errorf("Content-Disposition = %q", got)
		}
		body, _ := io.ReadAll(res.Body)
		if string(body) != "a,b\n1,2\n" {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("forced download", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/reports/q2.csv?download=1")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="q2.csv"` {
			t.Errorf("Content-Disposition = %q", got)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/../../../../../../etc/passwd")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		// net/http's own client/URL normalization already collapses ".."
		// before the request even leaves — either way this must never come
		// back as a 200 with content from outside the workspace.
		if res.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("traversal attempt returned 200: %q", body)
		}
	})

	t.Run("html served as text/html for iframe preview", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(workspace, "page.html"), []byte("<!doctype html><h1>ok</h1>"), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files/page.html")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		ctype := res.Header.Get("Content-Type")
		if !strings.HasPrefix(ctype, "text/html") {
			t.Fatalf("Content-Type = %q, want text/html so the dialog iframe can render it", ctype)
		}
		if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q", got)
		}
	})

	t.Run("unknown session 404s", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/does-not-exist/files")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})
}

func TestListWorkspaceFilesHidesScaffoldSources(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("page.html", "<h1>ok</h1>")
	mustWrite("src/App.tsx", "export default function App() {}")
	mustWrite("src/index.css", "body{}")
	mustWrite("favicon.svg", "<svg></svg>")
	mustWrite("chart.svg", "<svg id='chart'></svg>")
	mustWrite("hero.png", "png")
	mustWrite("generate.js", "require('pptxgenjs')")
	mustWrite("theme.css", "/* uploaded */")
	if err := os.WriteFile(filepath.Join(root, uploadsManifestName), []byte("theme.css\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if _, ok := byPath["page.html"]; !ok {
		t.Fatalf("page.html should stay visible: %+v", entries)
	}
	if _, ok := byPath["theme.css"]; !ok || byPath["theme.css"].Source != fileSourceUpload {
		t.Fatalf("uploaded css should stay visible: %+v", entries)
	}
	for _, hidden := range []string{"src/App.tsx", "src/index.css", "favicon.svg", "chart.svg", "hero.png", "generate.js"} {
		if _, ok := byPath[hidden]; ok {
			t.Fatalf("%s should be hidden from task outputs: %+v", hidden, entries)
		}
	}
}

func TestListWorkspaceFilesHidesPPTXSidecars(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("新能源汽车.pptx", "PK")
	mustWrite("generate.js", "require('pptxgenjs')")
	mustWrite("wifi.png", "png")
	mustWrite("bolt.png", "png")
	mustWrite("car.png", "png")
	mustWrite("leaf.svg", "<svg></svg>")
	mustWrite("新能源汽车.pdf", "%PDF")
	mustWrite(".pptx-icons/leaf.png", "png")
	if err := os.WriteFile(filepath.Join(root, uploadsManifestName), []byte("新能源汽车.pdf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if _, ok := byPath["新能源汽车.pptx"]; !ok {
		t.Fatalf("pptx should stay visible: %+v", entries)
	}
	if e, ok := byPath["新能源汽车.pdf"]; !ok || e.Source != fileSourceUpload {
		t.Fatalf("uploaded pdf should stay visible: %+v", entries)
	}
	for _, hidden := range []string{"generate.js", "wifi.png", "bolt.png", "car.png", "leaf.svg", ".pptx-icons/leaf.png"} {
		if _, ok := byPath[hidden]; ok {
			t.Fatalf("%s is a pptx sidecar and should be hidden: %+v", hidden, entries)
		}
	}
	if len(entries) != 2 {
		t.Fatalf("listing = %+v, want only pptx + uploaded pdf", entries)
	}
}

func TestListWorkspaceFilesKeepsStandaloneImages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "poster.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "poster.png" {
		t.Fatalf("standalone image should stay visible when there is no pptx: %+v", entries)
	}
}

func TestListWorkspaceFilesSkipsHarnessMeta(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, uploadsManifestName), []byte("visible.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Path != "visible.txt" {
		t.Fatalf("listing = %+v, want only visible.txt", entries)
	}
	if entries[0].Source != fileSourceUpload {
		t.Fatalf("source = %q, want upload (path is in the manifest)", entries[0].Source)
	}
}

func TestUploadDeleteAndListAllFiles(t *testing.T) {
	s, sessionID := newTestServer(t)
	if _, err := s.agents.Register("sess-2", stubAgent{}, ""); err != nil {
		t.Fatalf("registering second stub: %v", err)
	}
	workspace2 := s.workspaceRoot("sess-2")
	if err := os.MkdirAll(workspace2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace2, "report.csv"), []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions/"+sessionID+"/files", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("upload status = %d, body = %s", res.StatusCode, body)
	}

	t.Run("session listing tags the upload", func(t *testing.T) {
		list, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer list.Body.Close()
		var entries []fileEntryDTO
		if err := json.NewDecoder(list.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Path != "notes.txt" || entries[0].Source != fileSourceUpload {
			t.Fatalf("listing = %+v", entries)
		}
		if entries[0].SessionID != "" {
			t.Fatalf("per-session listing should omit sessionId, got %q", entries[0].SessionID)
		}
	})

	t.Run("GET /files aggregates every session", func(t *testing.T) {
		list, err := http.Get(srv.URL + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer list.Body.Close()
		if list.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", list.StatusCode)
		}
		var entries []fileEntryDTO
		if err := json.NewDecoder(list.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		byPath := map[string]fileEntryDTO{}
		for _, e := range entries {
			byPath[e.SessionID+"/"+e.Path] = e
		}
		up, ok := byPath[sessionID+"/notes.txt"]
		if !ok || up.Source != fileSourceUpload {
			t.Fatalf("uploaded file missing or mis-tagged: %+v", entries)
		}
		gen, ok := byPath["sess-2/report.csv"]
		if !ok || gen.Source != fileSourceGenerated {
			t.Fatalf("generated file missing or mis-tagged: %+v", entries)
		}
	})

	t.Run("delete removes the file", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/sessions/"+sessionID+"/files/notes.txt", nil)
		if err != nil {
			t.Fatal(err)
		}
		del, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer del.Body.Close()
		if del.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(del.Body)
			t.Fatalf("delete status = %d, body = %s", del.StatusCode, body)
		}
		list, err := http.Get(srv.URL + "/sessions/" + sessionID + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer list.Body.Close()
		var entries []fileEntryDTO
		if err := json.NewDecoder(list.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected empty listing after delete, got %+v", entries)
		}
	})

	t.Run("delete missing file 404s", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/sessions/"+sessionID+"/files/notes.txt", nil)
		if err != nil {
			t.Fatal(err)
		}
		del, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer del.Body.Close()
		if del.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", del.StatusCode)
		}
	})

	t.Run("cannot delete harness metadata", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/sessions/"+sessionID+"/files/"+uploadsManifestName, nil)
		if err != nil {
			t.Fatal(err)
		}
		del, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer del.Body.Close()
		if del.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", del.StatusCode)
		}
	})
}

func TestListFilesWithoutLiveAgent(t *testing.T) {
	s, _ := newTestServer(t)
	const orphan = "orphan-sess"
	root := s.workspaceRoot(orphan)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "budget.xlsx"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	t.Run("per-session listing", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + orphan + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		var entries []fileEntryDTO
		if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Path != "budget.xlsx" {
			t.Fatalf("listing = %+v, want budget.xlsx", entries)
		}
	})

	t.Run("GET /files includes orphan workspace", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var entries []fileEntryDTO
		if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entries {
			if e.SessionID == orphan && e.Path == "budget.xlsx" {
				found = true
			}
		}
		if !found {
			t.Fatalf("orphan file missing from /files: %+v", entries)
		}
	})

	t.Run("inline preview still works", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/sessions/" + orphan + "/files/budget.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})
}

func TestListLocalDirFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "plan.docx"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := listLocalDirFiles(root)
	if err != nil {
		t.Fatalf("listLocalDirFiles: %v", err)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["readme.md"]; !ok || e.Source != fileSourceLocal || e.Kind != fileKindDoc || e.Location != "工作目录" {
		t.Fatalf("readme.md = %+v (ok=%v)", e, ok)
	}
	if e, ok := byPath["docs/plan.docx"]; !ok || e.Location != "docs" || e.Kind != fileKindDoc {
		t.Fatalf("docs/plan.docx = %+v (ok=%v)", e, ok)
	}
	if e, ok := byPath["app.js"]; !ok || e.Kind != fileKindCode {
		t.Fatalf("app.js = %+v (ok=%v)", e, ok)
	}
	for _, p := range []string{"node_modules/pkg/index.js", ".git/HEAD", ".env"} {
		if _, ok := byPath[p]; ok {
			t.Fatalf("%s should be hidden from the local listing: %+v", p, entries)
		}
	}
}

func TestListLocalDirFilesMissingRoot(t *testing.T) {
	entries, err := listLocalDirFiles(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing root should be empty, not an error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %+v", entries)
	}
}

func TestListAllFilesMergesWorkspaceDir(t *testing.T) {
	s, sessionID := newTestServer(t)
	if err := os.MkdirAll(s.workspaceRoot(sessionID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.workspaceRoot(sessionID), "gen.txt"), []byte("ai"), 0o644); err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "notes.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.cfg.WorkspaceDir = local

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/files")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var entries []fileEntryDTO
	if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	var sawGen, sawLocal bool
	for _, e := range entries {
		if e.SessionID == sessionID && e.Path == "gen.txt" {
			sawGen = true
			if e.Source != fileSourceGenerated || e.Kind != fileKindDoc {
				t.Fatalf("session file = %+v", e)
			}
		}
		if e.Source == fileSourceLocal && e.Path == "notes.md" {
			sawLocal = true
			if e.SessionID != "" || e.Location != "工作目录" {
				t.Fatalf("local file = %+v", e)
			}
		}
	}
	if !sawGen || !sawLocal {
		t.Fatalf("merge missing gen=%v local=%v entries=%+v", sawGen, sawLocal, entries)
	}
}

func TestWorkspaceFileRoutes(t *testing.T) {
	s, _ := newTestServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.cfg.WorkspaceDir = root
	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	t.Run("inline preview", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/workspace-files/hello.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if string(body) != "world" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("download", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/workspace-files/hello.txt?download=1")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="hello.txt"` {
			t.Errorf("Content-Disposition = %q", got)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/workspace-files/../../../../../../etc/passwd")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("traversal attempt returned 200: %q", body)
		}
	})

	t.Run("unconfigured 404s", func(t *testing.T) {
		s.cfg.WorkspaceDir = ""
		res, err := http.Get(srv.URL + "/workspace-files/hello.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})
}

func TestWorkspaceRootUsesConfiguredDir(t *testing.T) {
	s, sessionID := newTestServer(t)
	dir := t.TempDir()
	s.cfg.WorkspaceDir = dir
	if got := s.workspaceRoot(sessionID); got != filepath.Clean(dir) {
		t.Fatalf("workspaceRoot = %q, want %q", got, dir)
	}
	if s.sessionSandboxDir(sessionID) == s.workspaceRoot(sessionID) {
		t.Fatal("legacy session sandbox should stay under data_dir")
	}
}

func TestSessionWorkspaceOKDoesNotTreatSharedRootAsAnyID(t *testing.T) {
	s, sessionID := newTestServer(t)
	s.cfg.WorkspaceDir = t.TempDir()
	if !s.sessionWorkspaceOK(sessionID) {
		t.Fatal("live session should still be ok")
	}
	if s.sessionWorkspaceOK("not-a-session") {
		t.Fatal("unknown id must not become readable just because workspace_dir exists")
	}
}

func TestGeneratedFilesLandInWorkspaceDir(t *testing.T) {
	s, sessionID := newTestServer(t)
	dir := t.TempDir()
	s.cfg.WorkspaceDir = dir
	root := s.workspaceRoot(sessionID)
	if err := os.WriteFile(filepath.Join(root, "deck.pptx"), []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordGeneratedPaths(root, []string{"deck.pptx"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deck.pptx")); err != nil {
		t.Fatalf("generated file should sit in workspace_dir: %v", err)
	}

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/files")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var entries []fileEntryDTO
	if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		if e.Path == "deck.pptx" {
			found++
			if e.Source != fileSourceGenerated {
				t.Fatalf("source = %q, want generated", e.Source)
			}
			if e.SessionID != "" {
				t.Fatalf("shared workspace file should not be duplicated per session, got session %q", e.SessionID)
			}
		}
	}
	if found != 1 {
		t.Fatalf("deck.pptx listed %d times, want 1: %+v", found, entries)
	}
}

func TestListKnowledgeDirFilesClassifiesSources(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mine.md"), []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "up.txt"), []byte("up"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ai.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordUploadedPath(root, "up.txt"); err != nil {
		t.Fatal(err)
	}
	if err := recordGeneratedPaths(root, []string{"ai.html"}); err != nil {
		t.Fatal(err)
	}
	entries, err := listKnowledgeDirFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]fileEntryDTO{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e := byPath["mine.md"]; e.Source != fileSourceLocal {
		t.Fatalf("mine.md source = %q", e.Source)
	}
	if e := byPath["up.txt"]; e.Source != fileSourceUpload {
		t.Fatalf("up.txt source = %q", e.Source)
	}
	if e := byPath["ai.html"]; e.Source != fileSourceGenerated {
		t.Fatalf("ai.html source = %q", e.Source)
	}
}

func TestEmitFileEventsRecordsGeneratedManifest(t *testing.T) {
	root := t.TempDir()
	before, err := listWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	emitFileEvents(rec, root, before)
	if !strings.Contains(rec.Body.String(), `"name":"out.md"`) {
		t.Fatalf("missing file event: %s", rec.Body.String())
	}
	if _, ok := loadGeneratedSet(root)["out.md"]; !ok {
		t.Fatal("out.md should be recorded as generated")
	}
}

func TestSharedWorkspaceSessionFilesStayIsolated(t *testing.T) {
	s, sessionA := newTestServer(t)
	dir := t.TempDir()
	s.cfg.WorkspaceDir = dir
	const sessionB = "sess-2"
	if _, err := s.agents.Register(sessionB, stubAgent{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.html"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.html"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.recordSessionFiles(sessionA, []string{"a.html"}); err != nil {
		t.Fatal(err)
	}
	if err := s.recordSessionFiles(sessionB, []string{"b.html"}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)

	list := func(id string) []fileEntryDTO {
		t.Helper()
		res, err := http.Get(srv.URL + "/sessions/" + id + "/files")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		var entries []fileEntryDTO
		if err := json.NewDecoder(res.Body).Decode(&entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}

	gotA := list(sessionA)
	if len(gotA) != 1 || gotA[0].Path != "a.html" {
		t.Fatalf("session A files = %+v, want only a.html", gotA)
	}
	gotB := list(sessionB)
	if len(gotB) != 1 || gotB[0].Path != "b.html" {
		t.Fatalf("session B files = %+v, want only b.html", gotB)
	}
}

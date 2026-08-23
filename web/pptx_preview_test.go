package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMinimalPPTX(t *testing.T, dest string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("ppt/presentation.xml", `<?xml version="1.0"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst>
    <p:sldId id="256" r:id="rId2"/>
    <p:sldId id="257" r:id="rId3"/>
  </p:sldIdLst>
</p:presentation>`)
	add("ppt/_rels/presentation.xml.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>
</Relationships>`)
	add("ppt/slides/slide1.xml", `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>Quarter Review</a:t></a:r></a:p>
    <a:p><a:r><a:t>Revenue up 12%</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`)
	add("ppt/slides/slide2.xml", `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody>
    <a:p><a:r><a:t>Next Steps</a:t></a:r></a:p>
  </p:txBody></p:sp></p:spTree></p:cSld>
</p:sld>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDesignedPPTX(t *testing.T, dest string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("ppt/presentation.xml", `<?xml version="1.0"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldSz cx="12192000" cy="6858000"/>
  <p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst>
</p:presentation>`)
	add("ppt/_rels/presentation.xml.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
</Relationships>`)
	add("ppt/slides/slide1.xml", `<?xml version="1.0"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>
    <p:bg><p:bgPr><a:solidFill><a:srgbClr val="10241A"/></a:solidFill></p:bgPr></p:bg>
    <p:spTree>
      <p:sp>
        <p:spPr>
          <a:xfrm><a:off x="0" y="0"/><a:ext cx="5486400" cy="6858000"/></a:xfrm>
          <a:solidFill><a:srgbClr val="D4A017"/></a:solidFill>
        </p:spPr>
      </p:sp>
      <p:sp>
        <p:spPr>
          <a:xfrm><a:off x="640080" y="1828800"/><a:ext cx="4114800" cy="1371600"/></a:xfrm>
        </p:spPr>
        <p:txBody>
          <a:p><a:r><a:rPr sz="3600" b="1"><a:solidFill><a:srgbClr val="10241A"/></a:solidFill></a:rPr><a:t>SEED ROUND</a:t></a:r></a:p>
        </p:txBody>
      </p:sp>
    </p:spTree>
  </p:cSld>
</p:sld>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPptxPreviewHTML(t *testing.T) {
	dir := t.TempDir()
	pptxPath := filepath.Join(dir, "review.pptx")
	writeMinimalPPTX(t, pptxPath)

	body, ok := pptxPreviewHTML(pptxPath, "review.pptx")
	if !ok {
		t.Fatal("expected preview html for .pptx")
	}
	html := string(body)
	for _, want := range []string{"Quarter Review", "Revenue up 12%", "Next Steps", "第 1", "第 2", "2 页"} {
		if !strings.Contains(html, want) {
			t.Errorf("preview missing %q\n%s", want, html)
		}
	}

	old, ok := pptxPreviewHTML(filepath.Join(dir, "missing.ppt"), "deck.ppt")
	if !ok {
		t.Fatal("old .ppt should still get a fallback page")
	}
	if !strings.Contains(string(old), "旧版") {
		t.Errorf("fallback page = %s", old)
	}

	if _, ok := pptxPreviewHTML(pptxPath, "notes.txt"); ok {
		t.Fatal("non-ppt extensions must not claim a preview")
	}
}

func TestPptxPreviewKeepsSlideLayout(t *testing.T) {
	dir := t.TempDir()
	pptxPath := filepath.Join(dir, "designed.pptx")
	writeDesignedPPTX(t, pptxPath)
	body, ok := pptxPreviewHTML(pptxPath, "designed.pptx")
	if !ok {
		t.Fatal("expected preview html")
	}
	html := string(body)
	for _, want := range []string{`class="stage"`, "10241A", "D4A017", "SEED ROUND", "calc(36.00 * 100cqw / 960)"} {
		if !strings.Contains(html, want) {
			t.Errorf("visual preview missing %q\n%s", want, html)
		}
	}
	if strings.Contains(html, "<ul>") {
		t.Errorf("designed slide should not fall back to a bullet dump")
	}
}

func TestHandleGetFileServesPptxPreview(t *testing.T) {
	s, sessionID := newTestServer(t)
	workspace := s.workspaceRoot(sessionID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMinimalPPTX(t, filepath.Join(workspace, "deck.pptx"))

	srv := httptest.NewServer(s.routes())
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

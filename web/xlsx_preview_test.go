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

func writeMinimalXLSX(t *testing.T, dest string) {
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
	add("xl/workbook.xml", `<?xml version="1.0"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="销售" sheetId="1" r:id="rId1"/>
    <sheet name="备注" sheetId="2" r:id="rId2"/>
  </sheets>
</workbook>`)
	add("xl/_rels/workbook.xml.rels", `<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`)
	add("xl/sharedStrings.xml", `<?xml version="1.0"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">
  <si><t>产品</t></si>
  <si><t>咖啡</t></si>
</sst>`)
	add("xl/worksheets/sheet1.xml", `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
      <c r="B1"><v>100</v></c>
    </row>
    <row r="2">
      <c r="A2" t="s"><v>1</v></c>
      <c r="B2"><v>42</v></c>
    </row>
  </sheetData>
</worksheet>`)
	add("xl/worksheets/sheet2.xml", `<?xml version="1.0"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="inlineStr"><is><t>内部备注</t></is></c>
    </row>
  </sheetData>
</worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestXlsxPreviewHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	writeMinimalXLSX(t, path)

	body, ok := xlsxPreviewHTML(path, "book.xlsx")
	if !ok {
		t.Fatal("expected preview html for .xlsx")
	}
	html := string(body)
	for _, want := range []string{"销售", "备注", "产品", "咖啡", "100", "42", "内部备注"} {
		if !strings.Contains(html, want) {
			t.Errorf("preview missing %q\n%s", want, html)
		}
	}

	old, ok := xlsxPreviewHTML(filepath.Join(dir, "old.xls"), "grid.xls")
	if !ok {
		t.Fatal("old .xls should get a fallback page")
	}
	if !strings.Contains(string(old), "旧版") {
		t.Errorf("fallback page = %s", old)
	}

	if _, ok := xlsxPreviewHTML(path, "notes.txt"); ok {
		t.Fatal("non-excel extensions must not claim a preview")
	}
}

func TestHandleGetFileServesXlsxPreview(t *testing.T) {
	s, sessionID := newTestServer(t)
	workspace := s.workspaceRoot(sessionID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMinimalXLSX(t, filepath.Join(workspace, "book.xlsx"))

	srv := httptest.NewServer(s.routes())
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

func TestSplitCellRef(t *testing.T) {
	c, r := splitCellRef("A1")
	if c != 1 || r != 1 {
		t.Fatalf("A1 = %d,%d", c, r)
	}
	c, r = splitCellRef("AA12")
	if c != 27 || r != 12 {
		t.Fatalf("AA12 = %d,%d", c, r)
	}
}

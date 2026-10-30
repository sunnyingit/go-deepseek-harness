package preview

import (
	"go-harness/internal/preview/previewtest"
	"path/filepath"
	"strings"
	"testing"
)

func TestXlsxPreviewHTML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.xlsx")
	previewtest.WriteMinimalXLSX(t, path)

	body, ok := XLSXHTML(path, "book.xlsx")
	if !ok {
		t.Fatal("expected preview html for .xlsx")
	}
	html := string(body)
	for _, want := range []string{"销售", "备注", "产品", "咖啡", "100", "42", "内部备注"} {
		if !strings.Contains(html, want) {
			t.Errorf("preview missing %q\n%s", want, html)
		}
	}

	old, ok := XLSXHTML(filepath.Join(dir, "old.xls"), "grid.xls")
	if !ok {
		t.Fatal("old .xls should get a fallback page")
	}
	if !strings.Contains(string(old), "旧版") {
		t.Errorf("fallback page = %s", old)
	}

	if _, ok := XLSXHTML(path, "notes.txt"); ok {
		t.Fatal("non-excel extensions must not claim a preview")
	}
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

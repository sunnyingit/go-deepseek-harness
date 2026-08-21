package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"path"
	"strconv"
	"strings"
)

const (
	maxXlsxPreviewBytes  = 80 << 20
	maxXlsxPreviewRows   = 200
	maxXlsxPreviewCols   = 40
	maxXlsxPreviewSheets = 16
)

// xlsxPreviewHTML turns an OOXML spreadsheet into a self-contained HTML
// workbook the UI can iframe. Old binary .xls gets an explanation page.
func xlsxPreviewHTML(fullPath, filename string) (body []byte, ok bool) {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".xls":
		return []byte(xlsxFallbackPage(filename, "这是旧版 .xls 二进制格式，无法在线拆表。请下载后用 Excel 打开。")), true
	case ".xlsx", ".xlsm":
		htmlBody, err := renderXlsxFile(fullPath, filename)
		if err != nil {
			return []byte(xlsxFallbackPage(filename, "无法解析这份表格："+err.Error())), true
		}
		return htmlBody, true
	default:
		return nil, false
	}
}

func renderXlsxFile(fullPath, filename string) ([]byte, error) {
	zr, err := zip.OpenReader(fullPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var total int64
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		total += int64(f.UncompressedSize64)
		if total > maxXlsxPreviewBytes {
			return nil, fmt.Errorf("file too large to preview")
		}
		files[strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")] = f
	}

	shared, _ := parseSharedStrings(files["xl/sharedStrings.xml"])
	sheets := xlsxSheetList(files)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no worksheets found")
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(filename))
	b.WriteString(`</title><style>`)
	b.WriteString(xlsxPreviewCSS)
	b.WriteString(`</style></head><body>`)
	b.WriteString(`<header class="book-head"><h1>`)
	b.WriteString(html.EscapeString(filename))
	b.WriteString(`</h1><span>`)
	b.WriteString(strconv.Itoa(len(sheets)))
	b.WriteString(` 个工作表 · 每表最多预览 `)
	b.WriteString(strconv.Itoa(maxXlsxPreviewRows))
	b.WriteString(` 行</span></header><nav class="tabs">`)

	for i, sh := range sheets {
		b.WriteString(`<button type="button" data-i="`)
		b.WriteString(strconv.Itoa(i))
		if i == 0 {
			b.WriteString(`" class="on">`)
		} else {
			b.WriteString(`">`)
		}
		b.WriteString(html.EscapeString(sh.Name))
		b.WriteString(`</button>`)
	}
	b.WriteString(`</nav>`)

	for i, sh := range sheets {
		grid, err := parseSheetGrid(files[sh.path], shared)
		if err != nil {
			grid = nil
		}
		b.WriteString(`<section class="sheet`)
		if i == 0 {
			b.WriteString(` on`)
		}
		b.WriteString(`" data-i="`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`">`)
		if len(grid) == 0 {
			b.WriteString(`<p class="empty">（此表没有可显示的单元格）</p>`)
		} else {
			writeXlsxTable(&b, grid)
		}
		b.WriteString(`</section>`)
	}

	b.WriteString(`<script>
document.querySelectorAll(".tabs button").forEach(function(btn){
  btn.addEventListener("click", function(){
    var i = btn.getAttribute("data-i");
    document.querySelectorAll(".tabs button").forEach(function(b){ b.classList.toggle("on", b===btn); });
    document.querySelectorAll(".sheet").forEach(function(s){ s.classList.toggle("on", s.getAttribute("data-i")===i); });
  });
});
</script></body></html>`)
	return []byte(b.String()), nil
}

type xlsxSheetRef struct {
	Name string
	path string
}

func xlsxSheetList(files map[string]*zip.File) []xlsxSheetRef {
	rels := pptxRels(files, "xl/_rels/workbook.xml.rels")
	data, err := readZipFile(files["xl/workbook.xml"])
	if err != nil {
		return nil
	}
	var out []xlsxSheetRef
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		var name, rid string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "name":
				name = a.Value
			case "id":
				rid = a.Value
			}
		}
		if name == "" {
			name = "Sheet"
		}
		target, ok := rels[rid]
		if !ok {
			continue
		}
		out = append(out, xlsxSheetRef{Name: name, path: resolveZipPath("xl", target)})
		if len(out) >= maxXlsxPreviewSheets {
			break
		}
	}
	return out
}

func parseSharedStrings(f *zip.File) ([]string, error) {
	data, err := readZipFile(f)
	if err != nil {
		return nil, err
	}
	var out []string
	var cur strings.Builder
	inSI, inT := false, false
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				inT = inSI
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "si":
				out = append(out, cur.String())
				inSI = false
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
	}
	return out, nil
}

func parseSheetGrid(f *zip.File, shared []string) ([][]string, error) {
	data, err := readZipFile(f)
	if err != nil {
		return nil, err
	}
	type cell struct {
		row, col int
		val      string
	}
	var cells []cell
	maxR, maxC := 0, 0
	var row, col int
	var tAttr, v strings.Builder
	inC, inV, inT, inIS := false, false, false, false
	cellType := ""

	flushCell := func() {
		if !inC || row <= 0 || col <= 0 {
			return
		}
		if row > maxXlsxPreviewRows || col > maxXlsxPreviewCols {
			return
		}
		raw := strings.TrimSpace(v.String())
		val := decodeXlsxCell(cellType, raw, tAttr.String(), shared)
		cells = append(cells, cell{row, col, val})
		if row > maxR {
			maxR = row
		}
		if col > maxC {
			maxC = col
		}
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				inC, inV, inT, inIS = true, false, false, false
				cellType, row, col = "", 0, 0
				v.Reset()
				tAttr.Reset()
				var ref string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						ref = a.Value
					case "t":
						cellType = a.Value
					}
				}
				col, row = splitCellRef(ref)
			case "v":
				if inC {
					inV = true
					v.Reset()
				}
			case "is":
				if inC {
					inIS = true
					tAttr.Reset()
				}
			case "t":
				if inIS {
					inT = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "is":
				inIS = false
			case "c":
				flushCell()
				inC = false
			}
		case xml.CharData:
			if inV {
				v.Write(t)
			} else if inT {
				tAttr.Write(t)
			}
		}
	}
	flushCell()

	if maxR == 0 || maxC == 0 {
		return nil, nil
	}
	grid := make([][]string, maxR)
	for i := range grid {
		grid[i] = make([]string, maxC)
	}
	for _, c := range cells {
		if c.row >= 1 && c.row <= maxR && c.col >= 1 && c.col <= maxC {
			grid[c.row-1][c.col-1] = c.val
		}
	}
	return grid, nil
}

func decodeXlsxCell(t, v, inline string, shared []string) string {
	switch t {
	case "s":
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && i >= 0 && i < len(shared) {
			return shared[i]
		}
		return v
	case "inlineStr", "str":
		if inline != "" {
			return inline
		}
		return v
	case "b":
		if v == "1" {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		return v
	default:
		return v
	}
}

func splitCellRef(ref string) (col, row int) {
	i := 0
	for i < len(ref) {
		c := ref[i]
		if c >= 'A' && c <= 'Z' {
			col = col*26 + int(c-'A'+1)
			i++
			continue
		}
		if c >= 'a' && c <= 'z' {
			col = col*26 + int(c-'a'+1)
			i++
			continue
		}
		break
	}
	row, _ = strconv.Atoi(ref[i:])
	return col, row
}

func writeXlsxTable(b *strings.Builder, grid [][]string) {
	cols := 0
	for _, r := range grid {
		if len(r) > cols {
			cols = len(r)
		}
	}
	b.WriteString(`<div class="grid-wrap"><table>`)
	b.WriteString(`<thead><tr><th class="corner"></th>`)
	for c := 1; c <= cols; c++ {
		b.WriteString(`<th>`)
		b.WriteString(xlsxColName(c))
		b.WriteString(`</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for i, row := range grid {
		b.WriteString(`<tr><th>`)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(`</th>`)
		for c := 0; c < cols; c++ {
			val := ""
			if c < len(row) {
				val = row[c]
			}
			b.WriteString(`<td>`)
			b.WriteString(html.EscapeString(val))
			b.WriteString(`</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table></div>`)
}

func xlsxColName(n int) string {
	var s []byte
	for n > 0 {
		n--
		s = append([]byte{byte('A' + n%26)}, s...)
		n /= 26
	}
	return string(s)
}

func xlsxFallbackPage(filename, msg string) string {
	return `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>` +
		html.EscapeString(filename) + `</title><style>` + xlsxPreviewCSS +
		`</style></head><body><header class="book-head"><h1>` +
		html.EscapeString(filename) + `</h1></header><p class="empty">` +
		html.EscapeString(msg) + `</p></body></html>`
}

const xlsxPreviewCSS = `
html,body{margin:0;padding:0;background:#f4f1ec;font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#1c1917}
.book-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;padding:14px 16px 8px}
.book-head h1{font-size:14px;font-weight:650;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.book-head span{font-size:12px;color:#8a8278;flex-shrink:0}
.tabs{display:flex;gap:6px;flex-wrap:wrap;padding:0 16px 10px;border-bottom:1px solid #e6e0d8}
.tabs button{border:1px solid #ddd6cc;background:#fff;color:#3f3a36;border-radius:8px 8px 0 0;padding:6px 12px;font-size:12px;cursor:pointer}
.tabs button.on{background:#217346;color:#fff;border-color:#217346}
.sheet{display:none;padding:0 0 16px}
.sheet.on{display:block}
.grid-wrap{overflow:auto;max-height:calc(100vh - 88px);background:#fff}
table{border-collapse:collapse;font-size:12px;min-width:100%}
th,td{border:1px solid #d9d9d9;padding:4px 8px;white-space:nowrap;max-width:280px;overflow:hidden;text-overflow:ellipsis}
thead th,.corner,tbody th{background:#f3f3f3;color:#5f5f5f;font-weight:600;text-align:center;position:sticky;top:0;z-index:1}
tbody th{position:sticky;left:0;z-index:2;background:#f3f3f3}
thead th.corner{z-index:3;left:0}
td{color:#1c1917}
.empty{color:#8a8278;font-size:14px;padding:24px 16px}
`

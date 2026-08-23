package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"mime"
	"path"
	"sort"
	"strconv"
	"strings"
)

const maxPptxPreviewBytes = 80 << 20 // 80 MiB zip; skip giant decks
const maxPptxImageBytes = 2 << 20    // 2 MiB per embedded picture

// pptxPreviewHTML turns an OOXML .pptx/.pptm into a self-contained HTML
// slide deck the UI can iframe. Old binary .ppt gets a short explanation
// page. ok is false for any other extension so the caller can serve the
// original bytes.
func pptxPreviewHTML(fullPath, filename string) (body []byte, ok bool) {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".ppt":
		return []byte(pptxFallbackPage(filename, "这是旧版 .ppt 二进制格式，无法在线拆页。请下载后用 PowerPoint 打开。")), true
	case ".pptx", ".pptm":
		htmlBody, err := renderPptxFile(fullPath, filename)
		if err != nil {
			return []byte(pptxFallbackPage(filename, "无法解析这份演示文稿："+err.Error())), true
		}
		return htmlBody, true
	default:
		return nil, false
	}
}

func renderPptxFile(fullPath, filename string) ([]byte, error) {
	zr, err := zip.OpenReader(fullPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var total int64
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		total += int64(f.UncompressedSize64)
		if total > maxPptxPreviewBytes {
			return nil, fmt.Errorf("file too large to preview")
		}
		files[strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")] = f
	}

	order := pptxSlideOrder(files)
	if len(order) == 0 {
		return nil, fmt.Errorf("no slides found")
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">`)
	b.WriteString(`<title>`)
	b.WriteString(html.EscapeString(filename))
	b.WriteString(`</title><style>`)
	b.WriteString(pptxPreviewCSS)
	b.WriteString(`</style></head><body>`)
	b.WriteString(`<header class="deck-head"><h1>`)
	b.WriteString(html.EscapeString(filename))
	b.WriteString(`</h1><span>`)
	b.WriteString(strconv.Itoa(len(order)))
	b.WriteString(` 页 · 按版式还原，图表请下载后用 PowerPoint 打开</span></header>`)

	theme := parseThemeColors(files)
	slideW, slideH := pptxSlideSize(files)

	for i, slidePath := range order {
		slide, err := readZipFile(files[slidePath])
		if err != nil {
			continue
		}
		rels := pptxRels(files, relsPathFor(slidePath))
		view := parseSlideView(slide, slideW, slideH, theme)
		b.WriteString(`<article class="frame"><div class="slide-num">第 `)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(` / `)
		b.WriteString(strconv.Itoa(len(order)))
		b.WriteString(` 页</div>`)
		if view.HasLayout {
			writeVisualSlide(&b, view, rels, files, slidePath)
		} else {
			paras, imageIDs := extractSlideParts(slide)
			b.WriteString(`<div class="slide">`)
			if len(paras) > 0 {
				b.WriteString(`<h2>`)
				b.WriteString(html.EscapeString(paras[0]))
				b.WriteString(`</h2>`)
				if len(paras) > 1 {
					b.WriteString(`<ul>`)
					for _, p := range paras[1:] {
						b.WriteString(`<li>`)
						b.WriteString(html.EscapeString(p))
						b.WriteString(`</li>`)
					}
					b.WriteString(`</ul>`)
				}
			}
			for _, id := range imageIDs {
				target, ok := rels[id]
				if !ok {
					continue
				}
				imgPath := resolveZipPath(path.Dir(slidePath), target)
				dataURI := pptxImageDataURI(files[imgPath])
				if dataURI == "" {
					continue
				}
				b.WriteString(`<img src="`)
				b.WriteString(dataURI)
				b.WriteString(`" alt="">`)
			}
			if len(paras) == 0 && len(imageIDs) == 0 {
				b.WriteString(`<p class="empty">（此页无可提取的文字或图片）</p>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</article>`)
	}
	b.WriteString(`</body></html>`)
	return []byte(b.String()), nil
}

func pptxSlideOrder(files map[string]*zip.File) []string {
	rels := pptxRels(files, "ppt/_rels/presentation.xml.rels")
	pres, err := readZipFile(files["ppt/presentation.xml"])
	if err == nil {
		var ids []string
		dec := xml.NewDecoder(bytes.NewReader(pres))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok || se.Name.Local != "sldId" {
				continue
			}
			rid := ""
			for _, a := range se.Attr {
				if a.Name.Local != "id" {
					continue
				}
				if a.Name.Space != "" || strings.HasPrefix(a.Value, "rId") {
					rid = a.Value
					break
				}
			}
			if rid != "" {
				ids = append(ids, rid)
			}
		}
		var order []string
		seen := map[string]bool{}
		for _, id := range ids {
			target, ok := rels[id]
			if !ok {
				continue
			}
			p := resolveZipPath("ppt", target)
			if files[p] == nil || seen[p] {
				continue
			}
			seen[p] = true
			order = append(order, p)
		}
		if len(order) > 0 {
			return order
		}
	}
	var slides []string
	for name := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "/_rels/") {
			slides = append(slides, name)
		}
	}
	sort.Slice(slides, func(i, j int) bool { return naturalSlideLess(slides[i], slides[j]) })
	return slides
}

func naturalSlideLess(a, b string) bool {
	na, nb := slideNumber(a), slideNumber(b)
	if na != nb {
		return na < nb
	}
	return a < b
}

func slideNumber(name string) int {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	n := strings.TrimPrefix(base, "slide")
	v, _ := strconv.Atoi(n)
	return v
}

func extractSlideParts(data []byte) (paras []string, imageIDs []string) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var para strings.Builder
	inT := false
	flush := func() {
		t := strings.TrimSpace(para.String())
		para.Reset()
		if t != "" {
			paras = append(paras, t)
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT = true
			case "p":
				flush()
			case "br":
				if inT {
					para.WriteByte(' ')
				}
			case "blip":
				for _, a := range t.Attr {
					if a.Name.Local == "embed" && a.Value != "" {
						imageIDs = append(imageIDs, a.Value)
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			}
		case xml.CharData:
			if inT {
				para.Write(t)
			}
		}
	}
	flush()
	return paras, imageIDs
}

func pptxRels(files map[string]*zip.File, relsFile string) map[string]string {
	out := map[string]string{}
	data, err := readZipFile(files[relsFile])
	if err != nil {
		return out
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var id, target, typ string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			case "Type":
				typ = a.Value
			}
		}
		if id == "" || target == "" {
			continue
		}
		if strings.Contains(typ, "notesSlide") || strings.Contains(typ, "comment") {
			continue
		}
		out[id] = target
	}
	return out
}

func relsPathFor(part string) string {
	dir, base := path.Split(part)
	return path.Join(dir, "_rels", base+".rels")
}

func resolveZipPath(fromDir, target string) string {
	target = strings.ReplaceAll(target, "\\", "/")
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Clean(path.Join(fromDir, target))
}

func pptxImageDataURI(f *zip.File) string {
	if f == nil || int64(f.UncompressedSize64) > maxPptxImageBytes {
		return ""
	}
	data, err := readZipFile(f)
	if err != nil || len(data) == 0 {
		return ""
	}
	ctype := mime.TypeByExtension(strings.ToLower(path.Ext(f.Name)))
	if ctype == "" {
		switch strings.ToLower(path.Ext(f.Name)) {
		case ".png":
			ctype = "image/png"
		case ".jpg", ".jpeg":
			ctype = "image/jpeg"
		case ".gif":
			ctype = "image/gif"
		case ".webp":
			ctype = "image/webp"
		case ".svg":
			ctype = "image/svg+xml"
		default:
			ctype = "application/octet-stream"
		}
	}
	return "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func readZipFile(f *zip.File) ([]byte, error) {
	if f == nil {
		return nil, io.EOF
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxPptxPreviewBytes))
}

func pptxFallbackPage(filename, msg string) string {
	return `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>` +
		html.EscapeString(filename) + `</title><style>` + pptxPreviewCSS +
		`</style></head><body><article class="frame"><div class="slide"><h2>` +
		html.EscapeString(filename) + `</h2><p class="empty">` +
		html.EscapeString(msg) + `</p></div></article></body></html>`
}

const pptxPreviewCSS = `
html,body{margin:0;padding:0;background:#eef1f6;font-family:"PingFang SC","Microsoft YaHei",Inter,-apple-system,sans-serif;color:#0b1a33}
.deck-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;padding:16px 20px 8px;max-width:960px;margin:0 auto}
.deck-head h1{font-size:14px;font-weight:650;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.deck-head span{font-size:12px;color:#8a95a8;flex-shrink:0}
.frame{max-width:960px;margin:12px auto 24px;padding:0 16px}
.slide-num{font-size:11px;color:#8a95a8;font-weight:600;margin-bottom:8px;letter-spacing:.3px}
.stage{container-type:inline-size;position:relative;width:100%;aspect-ratio:16/9;overflow:hidden;border-radius:10px;box-shadow:0 12px 32px rgba(15,23,42,.12);background:#fff}
.el{position:absolute;overflow:hidden;box-sizing:border-box}
.el img{width:100%;height:100%;object-fit:contain;display:block}
.el .tx{line-height:1.25;padding:0 2px}
.el .tx span{white-space:pre-wrap}
.chart-ph{width:100%;height:100%;display:flex;align-items:center;justify-content:center;background:repeating-linear-gradient(-45deg,rgba(255,255,255,.08),rgba(255,255,255,.08) 8px,rgba(0,0,0,.08) 8px,rgba(0,0,0,.08) 16px);color:rgba(255,255,255,.85);font-size:13px;font-weight:600;letter-spacing:.4px}
.slide{background:#fff;border:1px solid #e8edf4;border-radius:18px;box-shadow:0 10px 28px rgba(15,23,42,.06);padding:28px 32px 32px;min-height:220px}
.slide h2{font-size:22px;line-height:1.35;margin:0 0 14px;color:#0b1a33}
.slide ul{margin:0;padding-left:20px}
.slide li{font-size:15px;line-height:1.65;color:#3c4a5e;margin:4px 0}
.slide img{display:block;max-width:100%;max-height:360px;object-fit:contain;margin:16px auto 0;border-radius:10px;background:#f8f9fc}
.slide .empty{color:#8a95a8;font-size:14px;margin-top:24px}
`

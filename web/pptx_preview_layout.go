package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path"
	"strconv"
	"strings"
)

type previewShape struct {
	X, Y, W, H float64 // percent of slide
	Fill       string
	Image      string
	Chart      bool
	Paras      []previewPara
}

type previewPara struct {
	Align string
	Runs  []previewRun
}

type previewRun struct {
	Text  string
	Size  float64
	Color string
	Bold  bool
}

type previewSlide struct {
	Bg        string
	Shapes    []previewShape
	HasLayout bool
	Texts     []string
	ImageIDs  []string
}

type xmlNode struct {
	Name string
	Attr map[string]string
	Text string
	Kids []*xmlNode
}

func parseXMLRoot(data []byte) *xmlNode {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: t.Name.Local, Attr: map[string]string{}}
			for _, a := range t.Attr {
				n.Attr[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				root = n
			} else {
				p := stack[len(stack)-1]
				p.Kids = append(p.Kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	return root
}

func (n *xmlNode) child(name string) *xmlNode {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (n *xmlNode) find(name string) *xmlNode {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for _, k := range n.Kids {
		if f := k.find(name); f != nil {
			return f
		}
	}
	return nil
}

func (n *xmlNode) attr(key string) string {
	if n == nil || n.Attr == nil {
		return ""
	}
	return n.Attr[key]
}

func pptxSlideSize(files map[string]*zip.File) (w, h int64) {
	w, h = 12192000, 6858000 // 13.333" × 7.5"
	data, err := readZipFile(files["ppt/presentation.xml"])
	if err != nil {
		return w, h
	}
	root := parseXMLRoot(data)
	sz := root.find("sldSz")
	if sz == nil {
		return w, h
	}
	if v, err := strconv.ParseInt(sz.attr("cx"), 10, 64); err == nil && v > 0 {
		w = v
	}
	if v, err := strconv.ParseInt(sz.attr("cy"), 10, 64); err == nil && v > 0 {
		h = v
	}
	return w, h
}

func parseThemeColors(files map[string]*zip.File) map[string]string {
	out := map[string]string{
		"dk1": "000000", "tx1": "000000", "dk2": "1F1F1F", "tx2": "1F1F1F",
		"lt1": "FFFFFF", "bg1": "FFFFFF", "lt2": "F2F2F2", "bg2": "F2F2F2",
		"accent1": "4472C4", "accent2": "ED7D31", "accent3": "A5A5A5",
		"accent4": "FFC000", "accent5": "5B9BD5", "accent6": "70AD47",
	}
	themePath := ""
	for name := range files {
		if strings.HasPrefix(name, "ppt/theme/theme") && strings.HasSuffix(name, ".xml") {
			themePath = name
			break
		}
	}
	if themePath == "" {
		return out
	}
	data, err := readZipFile(files[themePath])
	if err != nil {
		return out
	}
	scheme := parseXMLRoot(data).find("clrScheme")
	if scheme == nil {
		return out
	}
	for _, kid := range scheme.Kids {
		if hex := colorFromNode(kid, out); hex != "" {
			out[kid.Name] = hex
			switch kid.Name {
			case "dk1":
				out["tx1"] = hex
			case "lt1":
				out["bg1"] = hex
			case "dk2":
				out["tx2"] = hex
			case "lt2":
				out["bg2"] = hex
			}
		}
	}
	return out
}

func colorFromNode(n *xmlNode, theme map[string]string) string {
	if n == nil {
		return ""
	}
	if s := n.find("srgbClr"); s != nil {
		return strings.ToUpper(strings.TrimPrefix(s.attr("val"), "#"))
	}
	if s := n.find("sysClr"); s != nil {
		if v := s.attr("lastClr"); v != "" {
			return strings.ToUpper(v)
		}
	}
	if s := n.find("schemeClr"); s != nil {
		if v := theme[s.attr("val")]; v != "" {
			return v
		}
	}
	return ""
}

func fillOf(n *xmlNode, theme map[string]string) string {
	if n == nil {
		return ""
	}
	if sf := n.child("solidFill"); sf != nil {
		return colorFromNode(sf, theme)
	}
	if sf := n.find("solidFill"); sf != nil && n.child("gradFill") == nil {
		// avoid grabbing text run fills when we meant shape fill — caller should pass spPr
		return colorFromNode(sf, theme)
	}
	return ""
}

func parseXfrm(n *xmlNode) (x, y, cx, cy int64) {
	if n == nil {
		return
	}
	xf := n.child("xfrm")
	if xf == nil {
		xf = n.find("xfrm")
	}
	if xf == nil {
		return
	}
	if off := xf.child("off"); off != nil {
		x, _ = strconv.ParseInt(off.attr("x"), 10, 64)
		y, _ = strconv.ParseInt(off.attr("y"), 10, 64)
	}
	if ext := xf.child("ext"); ext != nil {
		cx, _ = strconv.ParseInt(ext.attr("cx"), 10, 64)
		cy, _ = strconv.ParseInt(ext.attr("cy"), 10, 64)
	}
	return
}

func pct(v, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(v) * 100 / float64(total)
}

func parseSlideView(slide []byte, slideW, slideH int64, theme map[string]string) previewSlide {
	view := previewSlide{Bg: "FFFFFF"}
	root := parseXMLRoot(slide)
	cSld := root.find("cSld")
	if cSld == nil {
		return view
	}
	if bg := cSld.child("bg"); bg != nil {
		if hex := colorFromNode(bg, theme); hex != "" {
			view.Bg = hex
		}
	}
	tree := cSld.child("spTree")
	if tree == nil {
		tree = cSld.find("spTree")
	}
	if tree == nil {
		return view
	}
	for _, kid := range tree.Kids {
		switch kid.Name {
		case "sp", "cxnSp":
			sh := shapeFromSp(kid, slideW, slideH, theme)
			if sh.W > 0.15 && sh.H > 0.15 {
				view.Shapes = append(view.Shapes, sh)
				view.HasLayout = true
			}
			for _, p := range sh.Paras {
				for _, r := range p.Runs {
					if t := strings.TrimSpace(r.Text); t != "" {
						view.Texts = append(view.Texts, t)
					}
				}
			}
		case "pic":
			sh := shapeFromPic(kid, slideW, slideH, theme)
			if sh.W > 0.15 && sh.H > 0.15 {
				view.Shapes = append(view.Shapes, sh)
				view.HasLayout = true
			}
			if sh.Image != "" {
				view.ImageIDs = append(view.ImageIDs, sh.Image)
			}
		case "graphicFrame":
			sh := shapeFromFrame(kid, slideW, slideH, theme)
			if sh.W > 0.15 && sh.H > 0.15 {
				view.Shapes = append(view.Shapes, sh)
				view.HasLayout = true
			}
		}
	}
	return view
}

func shapeFromSp(n *xmlNode, slideW, slideH int64, theme map[string]string) previewShape {
	spPr := n.child("spPr")
	x, y, cx, cy := parseXfrm(spPr)
	if cx == 0 && cy == 0 {
		x, y, cx, cy = parseXfrm(n)
	}
	sh := previewShape{
		X: pct(x, slideW), Y: pct(y, slideH), W: pct(cx, slideW), H: pct(cy, slideH),
		Fill: fillOf(spPr, theme),
	}
	sh.Paras = parasFromTxBody(n.child("txBody"), theme)
	return sh
}

func shapeFromPic(n *xmlNode, slideW, slideH int64, theme map[string]string) previewShape {
	spPr := n.child("spPr")
	x, y, cx, cy := parseXfrm(spPr)
	if cx == 0 {
		x, y, cx, cy = parseXfrm(n)
	}
	sh := previewShape{
		X: pct(x, slideW), Y: pct(y, slideH), W: pct(cx, slideW), H: pct(cy, slideH),
	}
	if blip := n.find("blip"); blip != nil {
		sh.Image = blip.attr("embed")
	}
	return sh
}

func shapeFromFrame(n *xmlNode, slideW, slideH int64, theme map[string]string) previewShape {
	x, y, cx, cy := parseXfrm(n)
	sh := previewShape{
		X: pct(x, slideW), Y: pct(y, slideH), W: pct(cx, slideW), H: pct(cy, slideH),
		Chart: n.find("graphicData") != nil,
		Fill:  "00000010",
	}
	return sh
}

func parasFromTxBody(tx *xmlNode, theme map[string]string) []previewPara {
	if tx == nil {
		return nil
	}
	var out []previewPara
	for _, p := range tx.Kids {
		if p.Name != "p" {
			continue
		}
		para := previewPara{Align: "l"}
		if pPr := p.child("pPr"); pPr != nil {
			if a := pPr.attr("algn"); a != "" {
				para.Align = a
			}
		}
		for _, r := range p.Kids {
			if r.Name != "r" && r.Name != "fld" {
				continue
			}
			t := r.child("t")
			if t == nil {
				continue
			}
			run := previewRun{Text: t.Text, Size: 18, Color: "1A1A1A"}
			if rPr := r.child("rPr"); rPr != nil {
				if sz, err := strconv.ParseFloat(rPr.attr("sz"), 64); err == nil && sz > 0 {
					run.Size = sz / 100
				}
				if rPr.attr("b") == "1" || strings.EqualFold(rPr.attr("b"), "true") {
					run.Bold = true
				}
				if hex := colorFromNode(rPr, theme); hex != "" {
					run.Color = hex
				}
			}
			if strings.TrimSpace(run.Text) == "" && run.Text == "" {
				continue
			}
			para.Runs = append(para.Runs, run)
		}
		if len(para.Runs) > 0 {
			out = append(out, para)
		}
	}
	return out
}

func writeVisualSlide(b *strings.Builder, view previewSlide, rels map[string]string, files map[string]*zip.File, slidePath string) {
	bg := view.Bg
	if bg == "" {
		bg = "FFFFFF"
	}
	b.WriteString(`<div class="stage" style="background:#`)
	b.WriteString(html.EscapeString(bg))
	b.WriteString(`">`)
	for _, sh := range view.Shapes {
		b.WriteString(`<div class="el" style="left:`)
		fmt.Fprintf(b, `%.3f%%;top:%.3f%%;width:%.3f%%;height:%.3f%%`, sh.X, sh.Y, sh.W, sh.H)
		if sh.Fill != "" && !sh.Chart {
			b.WriteString(`;background:#`)
			b.WriteString(html.EscapeString(sh.Fill))
		}
		b.WriteString(`">`)
		if sh.Chart {
			b.WriteString(`<div class="chart-ph">PPT 原生图表</div>`)
		}
		if sh.Image != "" {
			target, ok := rels[sh.Image]
			if ok {
				imgPath := resolveZipPath(path.Dir(slidePath), target)
				if uri := pptxImageDataURI(files[imgPath]); uri != "" {
					b.WriteString(`<img src="`)
					b.WriteString(uri)
					b.WriteString(`" alt="">`)
				}
			}
		}
		for _, para := range sh.Paras {
			align := "left"
			switch para.Align {
			case "ctr", "center":
				align = "center"
			case "r", "right":
				align = "right"
			}
			b.WriteString(`<div class="tx" style="text-align:`)
			b.WriteString(align)
			b.WriteString(`">`)
			for _, run := range para.Runs {
				b.WriteString(`<span style="`)
				if run.Size > 0 {
					fmt.Fprintf(b, `font-size:calc(%.2f * 100cqw / 960);`, run.Size)
				}
				if run.Color != "" {
					b.WriteString(`color:#`)
					b.WriteString(html.EscapeString(run.Color))
					b.WriteString(`;`)
				}
				if run.Bold {
					b.WriteString(`font-weight:700;`)
				}
				b.WriteString(`">`)
				b.WriteString(html.EscapeString(run.Text))
				b.WriteString(`</span>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
}

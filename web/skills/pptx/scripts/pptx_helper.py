"""Legacy python-pptx helper. New decks must use scripts/deck.js (pptxgenjs)."""

from __future__ import annotations

import os
import platform
from dataclasses import dataclass
from typing import Sequence, Tuple, Union

from pptx import Presentation
from pptx.chart.data import CategoryChartData
from pptx.dml.color import RGBColor
from pptx.enum.chart import XL_CHART_TYPE, XL_LEGEND_POSITION
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import MSO_ANCHOR, PP_ALIGN
from pptx.oxml.ns import qn
from pptx.oxml import parse_xml
from pptx.util import Inches, Pt

WIDE_W = Inches(13.333)
WIDE_H = Inches(7.5)


def _rgb(h: str) -> RGBColor:
    h = h.lstrip("#")
    return RGBColor(int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))


@dataclass(frozen=True)
class Theme:
    name: str
    bg: RGBColor
    surface: RGBColor
    ink: RGBColor
    muted: RGBColor
    accent: RGBColor
    on_accent: RGBColor


THEMES = {
    "slate": Theme("slate", _rgb("F4F6F8"), _rgb("FFFFFF"), _rgb("1B2430"), _rgb("5C6B7A"), _rgb("2F6FED"), _rgb("FFFFFF")),
    "night": Theme("night", _rgb("12151A"), _rgb("1C2129"), _rgb("F3F1EC"), _rgb("9AA3B0"), _rgb("E8C547"), _rgb("12151A")),
    "field": Theme("field", _rgb("F3F6F1"), _rgb("FFFFFF"), _rgb("1E2A1C"), _rgb("5B6B57"), _rgb("3D6B4F"), _rgb("FFFFFF")),
    "brick": Theme("brick", _rgb("FBF6F2"), _rgb("FFFFFF"), _rgb("2A1810"), _rgb("7A5A4A"), _rgb("9C3B2A"), _rgb("FFFFFF")),
    "sea": Theme("sea", _rgb("F2F6F8"), _rgb("FFFFFF"), _rgb("0E2A36"), _rgb("4E6A75"), _rgb("1F6F8B"), _rgb("FFFFFF")),
}


def pick_theme(subject: str = "") -> str:
    s = (subject or "").lower()
    if any(k in s for k in ("夜", "发布会", "keynote", "dark", "科技发布")):
        return "night"
    if any(k in s for k in ("农", "绿", "可持续", "环境", "健康", "食品")):
        return "field"
    if any(k in s for k in ("文化", "历史", "品牌", "故事", "杂志")):
        return "brick"
    if any(k in s for k in ("海", "水", "金融", "航运", "研究")):
        return "sea"
    return "slate"


def _font_name() -> str:
    sysname = platform.system()
    if sysname == "Darwin":
        return "PingFang SC"
    if sysname == "Windows":
        return "Microsoft YaHei"
    return "Noto Sans CJK SC"


def _set_run(run, *, size: int, bold: bool = False, color: RGBColor, align=None):
    name = _font_name()
    run.font.size = Pt(size)
    run.font.bold = bold
    run.font.italic = False
    run.font.color.rgb = color
    run.font.name = name
    rPr = run._r.get_or_add_rPr()
    for tag in ("a:latin", "a:ea", "a:cs"):
        el = rPr.find(qn(tag))
        if el is None:
            rPr.append(parse_xml(f'<{tag} xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" typeface="{name}"/>'))
        else:
            el.set("typeface", name)


def _textbox(slide, left, top, width, height, text, *, size, bold=False, color=None, align=PP_ALIGN.LEFT, anchor=MSO_ANCHOR.TOP):
    box = slide.shapes.add_textbox(left, top, width, height)
    tf = box.text_frame
    tf.word_wrap = True
    try:
        tf._txBody.bodyPr.set("anchor", {MSO_ANCHOR.TOP: "t", MSO_ANCHOR.MIDDLE: "ctr", MSO_ANCHOR.BOTTOM: "b"}[anchor])
    except Exception:
        pass
    p = tf.paragraphs[0]
    p.alignment = align
    p.text = text or ""
    if p.runs:
        _set_run(p.runs[0], size=size, bold=bold, color=color)
    return box


def _rect(slide, left, top, width, height, fill: RGBColor):
    sh = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, left, top, width, height)
    sh.fill.solid()
    sh.fill.fore_color.rgb = fill
    sh.line.fill.background()
    return sh


def _send_back(slide, shape):
    sp = shape._element
    sp.getparent().remove(sp)
    slide.shapes._spTree.insert(2, sp)


class Deck:
    def __init__(self, theme: Theme):
        self.theme = theme
        self.prs = Presentation()
        self.prs.slide_width = WIDE_W
        self.prs.slide_height = WIDE_H

    def _blank(self):
        slide = self.prs.slides.add_slide(self.prs.slide_layouts[6])
        bg = _rect(slide, 0, 0, WIDE_W, WIDE_H, self.theme.bg)
        _send_back(slide, bg)
        return slide

    def _rail(self, slide):
        _rect(slide, 0, 0, Inches(0.14), WIDE_H, self.theme.accent)

    def _kicker(self, slide, text, left, top, width):
        if not text:
            return
        _textbox(slide, left, top, width, Inches(0.32), text.upper(), size=11, bold=True, color=self.theme.accent)

    def cover(self, title: str, subtitle: str = "", kicker: str = ""):
        slide = self._blank()
        _rect(slide, 0, 0, Inches(6.4), WIDE_H, self.theme.accent)
        if kicker:
            _textbox(slide, Inches(0.7), Inches(2.0), Inches(5.2), Inches(0.4), kicker.upper(), size=13, bold=True, color=self.theme.on_accent)
        _textbox(slide, Inches(0.7), Inches(2.5), Inches(5.2), Inches(2.6), title, size=36, bold=True, color=self.theme.on_accent, anchor=MSO_ANCHOR.TOP)
        if subtitle:
            _textbox(slide, Inches(7.0), Inches(5.6), Inches(5.6), Inches(1.2), subtitle, size=16, color=self.theme.muted)
        return slide

    def section(self, label: str, title: str):
        slide = self._blank()
        _rect(slide, 0, 0, Inches(0.14), WIDE_H, self.theme.accent)
        _textbox(slide, Inches(0.9), Inches(2.4), Inches(11.4), Inches(0.4), label.upper(), size=13, bold=True, color=self.theme.accent)
        _textbox(slide, Inches(0.9), Inches(2.9), Inches(11.4), Inches(2.2), title, size=34, bold=True, color=self.theme.ink)
        return slide

    def bullets(self, title: str, items: Sequence[str], kicker: str = ""):
        slide = self._blank()
        self._rail(slide)
        self._kicker(slide, kicker, Inches(0.7), Inches(0.42), Inches(11.8))
        _textbox(slide, Inches(0.7), Inches(0.78), Inches(11.8), Inches(0.7), title, size=28, bold=True, color=self.theme.ink)
        y = Inches(1.7)
        for i, item in enumerate(items[:5]):
            _rect(slide, Inches(0.7), y + Inches(0.12), Inches(0.12), Inches(0.12), self.theme.accent)
            _textbox(slide, Inches(1.05), y, Inches(11.3), Inches(0.7), item, size=18, color=self.theme.ink)
            y += Inches(0.82)
        return slide

    def stats(self, title: str, pairs: Sequence[Tuple[str, str]], kicker: str = ""):
        slide = self._blank()
        self._rail(slide)
        self._kicker(slide, kicker, Inches(0.7), Inches(0.42), Inches(11.8))
        _textbox(slide, Inches(0.7), Inches(0.78), Inches(11.8), Inches(0.7), title, size=28, bold=True, color=self.theme.ink)
        n = max(1, min(4, len(pairs)))
        gap = Inches(0.28)
        left = Inches(0.7)
        usable = WIDE_W - Inches(1.4) - gap * (n - 1)
        w = int(usable / n)
        for i, (value, label) in enumerate(pairs[:n]):
            x = left + i * (w + gap)
            _rect(slide, x, Inches(2.1), w, Inches(3.6), self.theme.surface)
            _rect(slide, x, Inches(2.1), w, Inches(0.08), self.theme.accent)
            _textbox(slide, x + Inches(0.28), Inches(2.5), w - Inches(0.5), Inches(1.6), value, size=36, bold=True, color=self.theme.accent)
            _textbox(slide, x + Inches(0.28), Inches(4.2), w - Inches(0.5), Inches(1.1), label, size=14, color=self.theme.muted)
        return slide

    def split(self, title: str, left: Tuple[str, Sequence[str]], right: Tuple[str, Sequence[str]], kicker: str = ""):
        slide = self._blank()
        self._rail(slide)
        self._kicker(slide, kicker, Inches(0.7), Inches(0.42), Inches(11.8))
        _textbox(slide, Inches(0.7), Inches(0.78), Inches(11.8), Inches(0.7), title, size=28, bold=True, color=self.theme.ink)
        cols = [(Inches(0.7), left), (Inches(7.05), right)]
        for x, (heading, items) in cols:
            _rect(slide, x, Inches(1.75), Inches(5.55), Inches(4.9), self.theme.surface)
            _textbox(slide, x + Inches(0.32), Inches(1.95), Inches(4.9), Inches(0.5), heading, size=16, bold=True, color=self.theme.accent)
            y = Inches(2.6)
            for item in list(items)[:5]:
                _textbox(slide, x + Inches(0.32), y, Inches(4.9), Inches(0.7), "·  " + item, size=15, color=self.theme.ink)
                y += Inches(0.7)
        return slide

    def quote(self, text: str, attribution: str = ""):
        slide = self._blank()
        self._rail(slide)
        _textbox(slide, Inches(1.1), Inches(2.1), Inches(11.1), Inches(2.6), "“" + text + "”", size=26, bold=True, color=self.theme.ink)
        if attribution:
            _textbox(slide, Inches(1.1), Inches(5.0), Inches(11.1), Inches(0.5), attribution, size=14, color=self.theme.muted)
        return slide

    def table(self, title: str, headers: Sequence[str], rows: Sequence[Sequence[str]], kicker: str = ""):
        slide = self._blank()
        self._rail(slide)
        self._kicker(slide, kicker, Inches(0.7), Inches(0.42), Inches(11.8))
        _textbox(slide, Inches(0.7), Inches(0.78), Inches(11.8), Inches(0.7), title, size=28, bold=True, color=self.theme.ink)
        rcount = len(rows) + 1
        ccount = len(headers)
        tbl = slide.shapes.add_table(rcount, ccount, Inches(0.7), Inches(1.7), Inches(11.9), Inches(5.0)).table
        col_w = int(Inches(11.9) / max(1, ccount))
        for c in range(ccount):
            tbl.columns[c].width = col_w
        for c, h in enumerate(headers):
            cell = tbl.cell(0, c)
            cell.text = str(h)
            cell.fill.solid()
            cell.fill.fore_color.rgb = self.theme.accent
            p = cell.text_frame.paragraphs[0]
            p.alignment = PP_ALIGN.LEFT
            if p.runs:
                _set_run(p.runs[0], size=12, bold=True, color=self.theme.on_accent)
            cell.vertical_anchor = MSO_ANCHOR.MIDDLE
        for r, row in enumerate(rows, start=1):
            for c, val in enumerate(row):
                cell = tbl.cell(r, c)
                cell.text = str(val)
                cell.fill.solid()
                cell.fill.fore_color.rgb = self.theme.surface if r % 2 else self.theme.bg
                p = cell.text_frame.paragraphs[0]
                if p.runs:
                    _set_run(p.runs[0], size=12, color=self.theme.ink)
                cell.vertical_anchor = MSO_ANCHOR.MIDDLE
        return slide

    def chart(self, title: str, categories: Sequence[str], series: dict, kind: str = "column", kicker: str = ""):
        slide = self._blank()
        self._rail(slide)
        self._kicker(slide, kicker, Inches(0.7), Inches(0.42), Inches(11.8))
        _textbox(slide, Inches(0.7), Inches(0.78), Inches(11.8), Inches(0.7), title, size=28, bold=True, color=self.theme.ink)
        data = CategoryChartData()
        data.categories = list(categories)
        for name, values in series.items():
            data.add_series(str(name), list(values))
        kinds = {
            "column": XL_CHART_TYPE.COLUMN_CLUSTERED,
            "bar": XL_CHART_TYPE.BAR_CLUSTERED,
            "line": XL_CHART_TYPE.LINE,
            "pie": XL_CHART_TYPE.PIE,
        }
        chart = slide.shapes.add_chart(
            kinds.get(kind, XL_CHART_TYPE.COLUMN_CLUSTERED),
            Inches(0.7), Inches(1.7), Inches(11.9), Inches(5.1),
            data,
        ).chart
        chart.has_legend = len(series) > 1
        if chart.has_legend:
            chart.legend.position = XL_LEGEND_POSITION.BOTTOM
            chart.legend.include_in_layout = False
        palette = [self.theme.accent, self.theme.ink, self.theme.muted, _rgb("7B9E89")]
        try:
            for i, ser in enumerate(chart.series):
                ser.format.fill.solid()
                ser.format.fill.fore_color.rgb = palette[i % len(palette)]
        except Exception:
            pass
        return slide

    def close(self, title: str, subtitle: str = ""):
        slide = self._blank()
        _rect(slide, 0, 0, WIDE_W, WIDE_H, self.theme.accent)
        _textbox(slide, Inches(1.0), Inches(2.6), Inches(11.3), Inches(1.6), title, size=34, bold=True, color=self.theme.on_accent)
        if subtitle:
            _textbox(slide, Inches(1.0), Inches(4.4), Inches(11.3), Inches(1.0), subtitle, size=16, color=self.theme.on_accent)
        return slide

    def save(self, path: str) -> str:
        if not path.endswith(".pptx"):
            path += ".pptx"
        self.prs.save(path)
        return os.path.abspath(path)


def new_deck(theme: Union[str, Theme, None] = None, subject: str = "") -> Deck:
    if isinstance(theme, Theme):
        t = theme
    else:
        key = theme or pick_theme(subject)
        t = THEMES.get(key, THEMES["slate"])
    return Deck(t)

'use strict';

const path = require('path');
const os = require('os');
const PptxGenJS = require('pptxgenjs');
const { renderIcon } = require('./icons');

const W = 13.333;
const H = 7.5;

const PALETTES = {
  'moss-amber': {
    bg: '10241A', paper: 'F4F1EA', surface: 'FFFFFF',
    ink: 'F3EEE3', inkDark: '1C1917',
    muted: 'B7C4B0', mutedDark: '6B7368',
    accent: 'C9A227', onAccent: '10241A', name: 'moss-amber',
  },
  pitch: {
    bg: '111827', paper: 'F6F4EF', surface: 'FFFFFF',
    ink: 'F8FAFC', inkDark: '111827',
    muted: '94A3B8', mutedDark: '64748B',
    accent: 'C9A227', onAccent: '111827', name: 'pitch',
  },
  'night-copper': {
    bg: '14110F', paper: 'F7F1EA', surface: 'FFFFFF',
    ink: 'F2EBE3', inkDark: '1A1410',
    muted: 'A89888', mutedDark: '7A6B5C',
    accent: 'C46B3A', onAccent: '14110F', name: 'night-copper',
  },
  'ink-paper': {
    bg: '1A1714', paper: 'F6F1E8', surface: 'FFFFFF',
    ink: 'F6F1E8', inkDark: '1A1714',
    muted: 'C4B8A8', mutedDark: '6F675E',
    accent: '8C2F1B', onAccent: 'F6F1E8', name: 'ink-paper',
  },
};

function pickPalette(subject) {
  const s = String(subject || '');
  if (/融资|路演|pitch|seed|投资|估值/i.test(s)) return 'pitch';
  if (/新能源|光伏|风电|能源|碳|绿色|气候|机器人|产业研究/i.test(s)) return 'moss-amber';
  if (/文化|历史|品牌|故事|杂志/i.test(s)) return 'ink-paper';
  if (/夜|发布会|keynote/i.test(s)) return 'night-copper';
  return 'moss-amber';
}

function bodyFont() {
  return os.platform() === 'win32' ? 'Microsoft YaHei' : 'PingFang SC';
}

function titleFont() {
  if (os.platform() === 'darwin') return 'Songti SC';
  if (os.platform() === 'win32') return 'SimSun';
  return 'Noto Serif CJK SC';
}

function hex(v) {
  return String(v || '').replace(/^#/, '').toUpperCase();
}

class Deck {
  constructor(opts = {}) {
    const pal = opts.palette;
    if (pal && typeof pal === 'object') {
      this.pal = { ...PALETTES['moss-amber'], ...pal };
      for (const k of Object.keys(this.pal)) {
        if (typeof this.pal[k] === 'string' && /^#?[0-9A-Fa-f]{6}$/.test(this.pal[k])) {
          this.pal[k] = hex(this.pal[k]);
        }
      }
    } else {
      this.pal = PALETTES[pal] || PALETTES[pickPalette(opts.subject)];
    }
    this.font = bodyFont();
    this.titleFace = titleFont();
    this.iconDir = path.resolve(opts.iconDir || path.join(process.cwd(), '.pptx-icons'));
    this.icons = {};
    this.footerLabel = opts.footer || opts.title || '';
    this.totalPages = opts.pages || 0;
    this._n = 0;
    this.pptx = new PptxGenJS();
    this.pptx.defineLayout({ name: 'WIDE', width: W, height: H });
    this.pptx.layout = 'WIDE';
    this.pptx.title = opts.title || '';
    this.pptx.author = 'PABuddy';
    this.pptx.theme = { headFontFace: this.titleFace, bodyFontFace: this.font };
  }

  async icon(alias, iconName, color) {
    const out = path.join(this.iconDir, `${alias}.png`);
    await renderIcon(iconName, { color: color || `#${this.pal.accent}`, size: 512, out });
    this.icons[alias] = out;
    return out;
  }

  _paint(s, color) {
    s.addShape(this.pptx.ShapeType.rect, {
      x: 0, y: 0, w: W, h: H,
      fill: { color }, line: { color },
    });
    return s;
  }

  _dark() {
    return this._paint(this.pptx.addSlide(), this.pal.bg);
  }

  _light() {
    return this._paint(this.pptx.addSlide(), this.pal.paper);
  }

  _text(s, text, o) {
    s.addText(text, {
      fontFace: o.fontFace || this.font,
      margin: 0,
      valign: o.valign || 'top',
      color: o.color,
      ...o,
    });
  }

  _folio(s, dark) {
    this._n += 1;
    const c = dark ? this.pal.muted : this.pal.mutedDark;
    if (this.footerLabel) {
      this._text(s, this.footerLabel, {
        x: 0.7, y: 7.08, w: 8.2, h: 0.26, fontSize: 10, color: c,
      });
    }
    const num = this.totalPages ? `Page ${this._n} / ${this.totalPages}` : `Page ${this._n}`;
    s.addShape(this.pptx.ShapeType.roundRect, {
      x: 11.15, y: 6.95, w: 1.55, h: 0.32,
      fill: { color: dark ? '0C1C14' : 'E8E4DC' },
      line: { color: dark ? '0C1C14' : 'E8E4DC' },
      rectRadius: 0.06,
    });
    this._text(s, num, {
      x: 11.15, y: 6.95, w: 1.55, h: 0.32,
      fontSize: 9, color: c, align: 'center', valign: 'middle',
    });
  }

  cover({ kicker, title, subtitle, icon, metrics, source } = {}) {
    const s = this._dark();
    s.addShape(this.pptx.ShapeType.rect, {
      x: 0, y: 0, w: 0.22, h: H,
      fill: { color: this.pal.accent }, line: { color: this.pal.accent },
    });
    if (kicker) {
      this._text(s, String(kicker), {
        x: 0.7, y: 1.15, w: 9, h: 0.38,
        fontSize: 13, bold: true, color: this.pal.accent, charSpacing: 1.2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 1.7, w: 9.2, h: 1.6,
      fontFace: this.titleFace, fontSize: 40, bold: true, color: this.pal.ink,
    });
    if (subtitle) {
      this._text(s, subtitle, {
        x: 0.7, y: 3.45, w: 9.2, h: 0.55,
        fontSize: 16, color: this.pal.ink,
      });
    }
    const iconPath = icon && this.icons[icon];
    if (iconPath) {
      s.addImage({ path: iconPath, x: 10.35, y: 2.15, w: 2.1, h: 2.1 });
    }
    const list = (metrics || []).slice(0, 3);
    list.forEach((m, i) => {
      const x = 0.7 + i * 4.1;
      this._text(s, String(m.value || ''), {
        x, y: 5.15, w: 3.8, h: 0.7,
        fontSize: 26, bold: true, color: this.pal.accent,
      });
      this._text(s, String(m.label || ''), {
        x, y: 5.85, w: 3.8, h: 0.55,
        fontSize: 12, color: this.pal.ink,
      });
    });
    if (source) {
      this._text(s, source, {
        x: 0.7, y: 6.55, w: 10, h: 0.28, fontSize: 10, color: this.pal.muted,
      });
    }
    this._folio(s, true);
    return s;
  }

  toc({ items } = {}) {
    const s = this._light();
    this._text(s, 'CONTENTS', {
      x: 0.7, y: 0.38, w: 6, h: 0.28,
      fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2.5,
    });
    this._text(s, '目录', {
      x: 0.7, y: 0.68, w: 6, h: 0.7,
      fontFace: this.titleFace, fontSize: 32, bold: true, color: this.pal.inkDark,
    });
    const rows = (items || []).slice(0, 6);
    const top = 1.55;
    const rowH = 0.85;
    rows.forEach((it, i) => {
      const y = top + i * rowH;
      s.addShape(this.pptx.ShapeType.rect, {
        x: 0.7, y: y + rowH - 0.04, w: 11.9, h: 0.01,
        fill: { color: 'E4DFD6' }, line: { color: 'E4DFD6' },
      });
      this._text(s, String(i + 1).padStart(2, '0'), {
        x: 0.7, y, w: 1.15, h: 0.7,
        fontFace: this.titleFace, fontSize: 28, color: 'D5D0C6', valign: 'middle',
      });
      s.addShape(this.pptx.ShapeType.ellipse, {
        x: 2.05, y: y + 0.14, w: 0.48, h: 0.48,
        fill: { color: this.pal.bg }, line: { color: this.pal.bg },
      });
      const iconPath = it.icon && this.icons[it.icon];
      if (iconPath) {
        s.addImage({ path: iconPath, x: 2.14, y: y + 0.23, w: 0.3, h: 0.3 });
      }
      this._text(s, String(it.title || ''), {
        x: 2.75, y: y + 0.06, w: 9.5, h: 0.34,
        fontSize: 16, bold: true, color: this.pal.inkDark,
      });
      this._text(s, String(it.desc || ''), {
        x: 2.75, y: y + 0.4, w: 9.5, h: 0.3,
        fontSize: 12, color: this.pal.mutedDark,
      });
    });
    this._folio(s, false);
    return s;
  }

  section({ label, title } = {}) {
    const s = this._dark();
    this._text(s, String(label || '01'), {
      x: 0.7, y: 2.05, w: 12, h: 1.0,
      fontFace: this.titleFace, fontSize: 56, bold: true, color: this.pal.accent,
    });
    this._text(s, title || '', {
      x: 0.7, y: 3.2, w: 12, h: 1.6,
      fontFace: this.titleFace, fontSize: 32, bold: true, color: this.pal.ink,
    });
    this._folio(s, true);
    return s;
  }

  stats({ title, kicker, items } = {}) {
    const s = this._light();
    if (kicker) {
      this._text(s, String(kicker).toUpperCase(), {
        x: 0.7, y: 0.38, w: 12, h: 0.28, fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 0.7, w: 12, h: 0.55,
      fontFace: this.titleFace, fontSize: 26, bold: true, color: this.pal.inkDark,
    });
    const list = (items || []).slice(0, 4);
    const n = Math.max(1, list.length);
    const gap = 0.28;
    const usable = W - 1.4 - gap * (n - 1);
    const w = usable / n;
    list.forEach((it, i) => {
      const x = 0.7 + i * (w + gap);
      s.addShape(this.pptx.ShapeType.roundRect, {
        x, y: 1.55, w, h: 4.85,
        fill: { color: this.pal.surface }, line: { color: 'EDE8E0' }, rectRadius: 0.08,
      });
      s.addShape(this.pptx.ShapeType.rect, {
        x, y: 1.55, w, h: 0.08,
        fill: { color: this.pal.accent }, line: { color: this.pal.accent },
      });
      const iconPath = it.icon && this.icons[it.icon];
      if (iconPath) {
        s.addShape(this.pptx.ShapeType.ellipse, {
          x: x + 0.28, y: 1.9, w: 0.46, h: 0.46,
          fill: { color: this.pal.bg }, line: { color: this.pal.bg },
        });
        s.addImage({ path: iconPath, x: x + 0.37, y: 1.99, w: 0.28, h: 0.28 });
      }
      this._text(s, String(it.value || ''), {
        x: x + 0.28, y: 2.6, w: w - 0.5, h: 1.4,
        fontSize: 28, bold: true, color: this.pal.accent,
      });
      this._text(s, String(it.label || ''), {
        x: x + 0.28, y: 4.2, w: w - 0.5, h: 1.6,
        fontSize: 13, color: this.pal.mutedDark,
      });
    });
    this._folio(s, false);
    return s;
  }

  bullets({ title, kicker, items } = {}) {
    const s = this._light();
    if (kicker) {
      this._text(s, String(kicker).toUpperCase(), {
        x: 0.7, y: 0.38, w: 12, h: 0.28, fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 0.7, w: 12, h: 0.55,
      fontFace: this.titleFace, fontSize: 26, bold: true, color: this.pal.inkDark,
    });
    (items || []).slice(0, 5).forEach((item, i) => {
      const y = 1.5 + i * 0.95;
      s.addShape(this.pptx.ShapeType.ellipse, {
        x: 0.74, y: y + 0.2, w: 0.14, h: 0.14,
        fill: { color: this.pal.accent }, line: { color: this.pal.accent },
      });
      this._text(s, String(item), {
        x: 1.1, y, w: 11.4, h: 0.85, fontSize: 17, color: this.pal.inkDark, valign: 'middle',
      });
    });
    this._folio(s, false);
    return s;
  }

  split({ title, kicker, left, right } = {}) {
    const s = this._light();
    if (kicker) {
      this._text(s, String(kicker).toUpperCase(), {
        x: 0.7, y: 0.38, w: 12, h: 0.28, fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 0.7, w: 12, h: 0.55,
      fontFace: this.titleFace, fontSize: 26, bold: true, color: this.pal.inkDark,
    });
    [{ x: 0.7, block: left }, { x: 7.05, block: right }].forEach(({ x, block }) => {
      const b = block || { heading: '', items: [] };
      s.addShape(this.pptx.ShapeType.roundRect, {
        x, y: 1.5, w: 5.55, h: 5.15,
        fill: { color: this.pal.surface }, line: { color: 'EDE8E0' }, rectRadius: 0.08,
      });
      this._text(s, String(b.heading || ''), {
        x: x + 0.32, y: 1.7, w: 4.9, h: 0.45, fontSize: 16, bold: true, color: this.pal.accent,
      });
      (b.items || []).slice(0, 5).forEach((item, i) => {
        this._text(s, '·  ' + item, {
          x: x + 0.32, y: 2.3 + i * 0.72, w: 4.9, h: 0.65, fontSize: 15, color: this.pal.inkDark,
        });
      });
    });
    this._folio(s, false);
    return s;
  }

  chart({ title, kicker, categories, series, kind } = {}) {
    const s = this._light();
    if (kicker) {
      this._text(s, String(kicker).toUpperCase(), {
        x: 0.7, y: 0.32, w: 12, h: 0.26, fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 0.6, w: 12, h: 0.48,
      fontFace: this.titleFace, fontSize: 24, bold: true, color: this.pal.inkDark,
    });
    const data = Object.entries(series || {}).map(([name, values]) => ({
      name, labels: categories || [], values,
    }));
    const typeMap = {
      column: this.pptx.charts.BAR,
      bar: this.pptx.charts.BAR,
      line: this.pptx.charts.LINE,
      pie: this.pptx.charts.PIE,
      doughnut: this.pptx.charts.DOUGHNUT,
    };
    s.addChart(typeMap[kind] || this.pptx.charts.BAR, data, {
      x: 0.6, y: 1.25, w: 12.1, h: 5.45,
      barGrouping: 'clustered',
      barGapWidthPct: 40,
      showValue: kind !== 'pie' && kind !== 'doughnut',
      showLegend: data.length > 1 || kind === 'pie' || kind === 'doughnut',
      legendPos: 'b',
      chartColors: [this.pal.accent, this.pal.bg, this.pal.mutedDark, '3D6B4F'],
      chartArea: { fill: { color: this.pal.paper } },
      plotArea: { fill: { color: this.pal.paper } },
      border: { pt: 0 },
      valAxisHidden: kind === 'pie' || kind === 'doughnut',
      catAxisLabelColor: this.pal.mutedDark,
      valAxisLabelColor: this.pal.mutedDark,
      legendColor: this.pal.inkDark,
      fontFace: this.font,
    });
    this._folio(s, false);
    return s;
  }

  table({ title, kicker, headers, rows } = {}) {
    const s = this._light();
    if (kicker) {
      this._text(s, String(kicker).toUpperCase(), {
        x: 0.7, y: 0.32, w: 12, h: 0.26, fontSize: 11, bold: true, color: this.pal.accent, charSpacing: 2,
      });
    }
    this._text(s, title || '', {
      x: 0.7, y: 0.6, w: 12, h: 0.48,
      fontFace: this.titleFace, fontSize: 24, bold: true, color: this.pal.inkDark,
    });
    const head = (headers || []).map((h) => ({
      text: String(h),
      options: { fill: { color: this.pal.bg }, color: this.pal.ink, bold: true, align: 'left', valign: 'middle' },
    }));
    const body = (rows || []).map((row, ri) => row.map((cell) => ({
      text: String(cell),
      options: {
        fill: { color: ri % 2 ? this.pal.surface : this.pal.paper },
        color: this.pal.inkDark, align: 'left', valign: 'middle',
      },
    })));
    s.addTable([head, ...body], {
      x: 0.7, y: 1.25, w: 11.9, h: 5.4,
      colW: headers && headers.length ? Array(headers.length).fill(11.9 / headers.length) : undefined,
      border: [{ pt: 0 }, { pt: 0 }, { pt: 0 }, { pt: 0 }],
      fontFace: this.font, fontSize: 13, align: 'left', valign: 'middle',
    });
    this._folio(s, false);
    return s;
  }

  quote({ text, attribution } = {}) {
    const s = this._light();
    s.addShape(this.pptx.ShapeType.rect, {
      x: 0.7, y: 2.35, w: 0.12, h: 2.4,
      fill: { color: this.pal.accent }, line: { color: this.pal.accent },
    });
    this._text(s, '“' + (text || '') + '”', {
      x: 1.15, y: 2.25, w: 11.2, h: 2.2,
      fontFace: this.titleFace, fontSize: 24, bold: true, color: this.pal.inkDark,
    });
    if (attribution) {
      this._text(s, attribution, {
        x: 1.15, y: 4.65, w: 11.2, h: 0.4, fontSize: 14, color: this.pal.mutedDark,
      });
    }
    this._folio(s, false);
    return s;
  }

  close({ title, subtitle } = {}) {
    const s = this._dark();
    s.addShape(this.pptx.ShapeType.rect, {
      x: 0, y: 0, w: 0.22, h: H,
      fill: { color: this.pal.accent }, line: { color: this.pal.accent },
    });
    this._text(s, title || '', {
      x: 0.85, y: 2.5, w: 11.4, h: 1.6,
      fontFace: this.titleFace, fontSize: 34, bold: true, color: this.pal.ink,
    });
    if (subtitle) {
      this._text(s, subtitle, {
        x: 0.85, y: 4.3, w: 11.4, h: 1.0, fontSize: 16, color: this.pal.muted,
      });
    }
    this._folio(s, true);
    return s;
  }

  async save(fileName) {
    const name = String(fileName || 'deck.pptx');
    const out = name.endsWith('.pptx') ? name : `${name}.pptx`;
    await this.pptx.writeFile({ fileName: out });
    return path.resolve(out);
  }
}

function newDeck(opts) {
  return new Deck(opts);
}

module.exports = { newDeck, Deck, PALETTES, pickPalette };

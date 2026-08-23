'use strict';

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

function unzipList(file) {
  const py = `
import zipfile, sys
z = zipfile.ZipFile(sys.argv[1])
for n in z.namelist():
    print(n)
`;
  const r = spawnSync('python3', ['-c', py, file], { encoding: 'utf8' });
  if (r.status !== 0) {
    throw new Error(r.stderr || 'unzip failed');
  }
  return r.stdout.split(/\r?\n/).filter(Boolean);
}

function main() {
  const file = process.argv[2];
  if (!file) {
    console.error('usage: node validate.js <deck.pptx>');
    process.exit(2);
  }
  const abs = path.resolve(file);
  if (!fs.existsSync(abs)) {
    console.error('missing file:', abs);
    process.exit(1);
  }
  const names = unzipList(abs);
  const slides = names.filter((n) => /^ppt\/slides\/slide\d+\.xml$/.test(n));
  const charts = names.filter((n) => n.startsWith('ppt/charts/'));
  const media = names.filter((n) => n.startsWith('ppt/media/'));
  const issues = [];
  if (slides.length === 0) issues.push('no slides in pptx zip');
  if (slides.length > 20) issues.push(`too many slides (${slides.length}); keep a pitch deck ≤ 16`);
  if (!names.includes('[Content_Types].xml')) issues.push('not a valid OOXML package');

  const pyRead = `
import zipfile, sys, re
z = zipfile.ZipFile(sys.argv[1])
calibri = 0
white_only = 0
fills = set()
for n in z.namelist():
    if not n.endswith('.xml'):
        continue
    data = z.read(n).decode('utf-8', 'ignore')
    if 'Calibri' in data and n.startswith('ppt/slides/'):
        calibri += 1
    for m in re.finditer(r'srgbClr val="([0-9A-Fa-f]{6})"', data):
        fills.add(m.group(1).upper())
print('CALIBRI', calibri)
print('FILLS', ','.join(sorted(fills)))
`;
  const info = spawnSync('python3', ['-c', pyRead, abs], { encoding: 'utf8' });
  const calibri = /CALIBRI (\d+)/.exec(info.stdout || '');
  const fills = /FILLS (.*)/.exec(info.stdout || '');
  if (calibri && Number(calibri[1]) > 0) {
    issues.push('slide XML still mentions Calibri — Office default typeface leaked in');
  }
  const fillSet = new Set((fills && fills[1] ? fills[1].split(',') : []).filter(Boolean));
  const nonBW = [...fillSet].filter((c) => c !== 'FFFFFF' && c !== '000000');
  if (nonBW.length === 0) {
    issues.push('no non-black/white fills — deck looks like a text dump, not a designed slide');
  }

  console.log(`slides=${slides.length} charts=${charts.length} media=${media.length} colors=${nonBW.join(',') || '(none)'}`);
  if (issues.length) {
    console.log('FAIL');
    issues.forEach((i) => console.log('- ' + i));
    process.exit(1);
  }
  console.log('PASS');
}

main();

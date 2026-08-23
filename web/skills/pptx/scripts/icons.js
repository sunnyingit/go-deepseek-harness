'use strict';

const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const fs = require('fs');
const path = require('path');
const sharp = require('sharp');

const PREFIX_TO_PACK = {
  Fa6: 'fa6', Fa: 'fa', Md: 'md', Hi2: 'hi2', Hi: 'hi',
  Lu: 'lu', Bi: 'bi', Io5: 'io5', Io: 'io', Ri: 'ri',
  Tb: 'tb', Gi: 'gi', Wi: 'wi', Bs: 'bs', Pi: 'pi',
  Go: 'go', Fi: 'fi', Ai: 'ai', Cg: 'cg', Sl: 'sl',
  Lia: 'lia', Rx: 'rx', Vsc: 'vsc', Si: 'si',
};

function packFor(name) {
  const keys = Object.keys(PREFIX_TO_PACK).sort((a, b) => b.length - a.length);
  for (const p of keys) {
    if (name.startsWith(p) && name.length > p.length) return PREFIX_TO_PACK[p];
  }
  return 'fa';
}

function loadIcon(name) {
  const pack = packFor(name);
  let mod;
  try {
    mod = require(`react-icons/${pack}`);
  } catch (err) {
    throw new Error(`react-icons/${pack} is not available (icon ${name}): ${err.message}`);
  }
  const Icon = mod[name];
  if (!Icon) {
    throw new Error(`icon ${name} not found in react-icons/${pack}`);
  }
  return Icon;
}

async function renderIcon(name, { color = '#ffffff', size = 512, out } = {}) {
  if (!out) throw new Error('renderIcon: out path is required');
  const Icon = loadIcon(name);
  let svg = renderToStaticMarkup(React.createElement(Icon, { size, color }));
  if (!svg.includes('xmlns')) {
    svg = svg.replace('<svg', '<svg xmlns="http://www.w3.org/2000/svg"');
  }
  await fs.promises.mkdir(path.dirname(out), { recursive: true });
  await sharp(Buffer.from(svg)).png().toFile(out);
  return out;
}

module.exports = { renderIcon, loadIcon };

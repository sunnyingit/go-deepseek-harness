// 运行：node host.test.js
const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { linkToUrl, stockBase, readPrefs, writePrefs } = require('./host');

const base = 'http://127.0.0.1:8090';
assert.strictEqual(linkToUrl({ page: 'market' }, base), `${base}/market.html`);
assert.strictEqual(linkToUrl({ page: 'market', tab: 'team' }, base), `${base}/market.html#team`);
assert.strictEqual(linkToUrl({ page: 'holdings', tab: 'now', code: '600036.SH' }, base), `${base}/holdings.html?code=600036.SH#now`);
assert.strictEqual(linkToUrl({ page: 'diagnose', code: '600036.SH' }, base), `${base}/?stock=600036.SH`);
assert.strictEqual(linkToUrl({}, base), `${base}/`);
assert.strictEqual(linkToUrl(null, base), `${base}/`);
assert.strictEqual(stockBase({}), base);
assert.strictEqual(stockBase({ STOCK_API_URL: 'http://x:1/' }), 'http://x:1');

const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'host-')), 'sub', 'host.json');
assert.deepStrictEqual(readPrefs(file), {});
writePrefs(file, { autostart: false });
assert.deepStrictEqual(readPrefs(file), { autostart: false });
console.log('host.test.js ok');

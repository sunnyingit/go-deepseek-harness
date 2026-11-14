// 客户端宿主：托盘常驻、登录自启、休眠恢复补跑、桌面通知。（单实例锁在 main.js 最前面，要赶在启动后端之前。）
// 定时任务在本机的 Go 服务里跑（调度、分析师、收件箱）；Python 股票服务只提供数据，不跑任务。这里只负责「让它有人看着」：
//   - 电脑从休眠恢复时唤醒调度器（POST /sched/tick?reason=resume），错过的任务按补跑策略处理；
//   - 把 Go 服务排好的桌面通知取出来弹给用户，点击后打开对应页面；
//   - 托盘显示未读数，关窗口不退出（右键托盘才退出）。
// 纯函数放在最上面导出，方便单测（host.test.js）。

const fs = require('fs');
const http = require('http');
const path = require('path');

const POLL_MS = 10 * 1000;
const WAKE_RETRY_MS = [0, 3000, 15000, 60000];

function stockBase(env = process.env) {
  return (env.STOCK_API_URL || 'http://127.0.0.1:8090').replace(/\/$/, '');
}

// 通知里的 link 是 {page, tab, code}；转成要打开的页面地址
function linkToUrl(link, base) {
  const l = link || {};
  const q = new URLSearchParams();
  if (l.code) q.set('code', l.code);
  const suffix = (qs, hash) => (qs ? `?${qs}` : '') + (hash ? `#${hash}` : '');
  switch (l.page) {
    case 'market': return `${base}/market.html${suffix('', l.tab || '')}`;
    case 'holdings': return `${base}/holdings.html${suffix(q.toString(), l.tab || '')}`;
    case 'diagnose': return `${base}/${suffix(l.code ? `stock=${encodeURIComponent(l.code)}` : '', '')}`;
    default: return `${base}/`;
  }
}

function readPrefs(file) {
  try { return JSON.parse(fs.readFileSync(file, 'utf8')); } catch { return {}; }
}

function writePrefs(file, prefs) {
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify(prefs, null, 2));
  } catch { /* 偏好写不进去就算了，不影响使用 */ }
}

function request(method, url, body, timeoutMs = 4000) {
  return new Promise((resolve) => {
    const data = body ? JSON.stringify(body) : null;
    const u = new URL(url);
    const req = http.request({
      method, hostname: u.hostname, port: u.port, path: u.pathname + u.search,
      headers: data ? { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) } : {},
    }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => {
        try { resolve({ ok: res.statusCode < 400, json: JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}') }); }
        catch { resolve({ ok: false, json: {} }); }
      });
    });
    req.on('error', () => resolve({ ok: false, json: {} }));
    req.setTimeout(timeoutMs, () => { req.destroy(); resolve({ ok: false, json: {} }); });
    if (data) req.write(data);
    req.end();
  });
}

function init({ app, electron, pageBase, showMainWindow, loadPage, iconPath, userData }) {
  const { Tray, Menu, Notification, powerMonitor, nativeImage } = electron;
  const pages = pageBase;            // 本机 Go 服务：页面、调度和通知接口
  const base = pageBase;
  const prefsFile = path.join(userData, 'host.json');
  const state = { tray: null, quitting: false, unread: 0, timer: null, shown: new Set() };
  const prefs = { autostart: true, ...readPrefs(prefsFile) };

  function applyAutostart() {
    try { app.setLoginItemSettings({ openAtLogin: !!prefs.autostart, openAsHidden: true }); } catch { /* linux 不支持 */ }
  }

  async function wake(reason) {
    for (const wait of WAKE_RETRY_MS) {
      if (wait) await new Promise((r) => setTimeout(r, wait));
      const res = await request('POST', `${base}/sched/tick?reason=${encodeURIComponent(reason)}`, {});
      if (res.ok) return true;
    }
    return false;
  }

  function refreshTray() {
    if (!state.tray) return;
    state.tray.setToolTip(state.unread ? `牛宝 · ${state.unread} 条未读` : '牛宝');
    if (process.platform === 'darwin') {
      state.tray.setTitle(state.unread ? ` ${state.unread}` : '');
      app.dock && app.dock.setBadge(state.unread ? String(state.unread) : '');
    } else if (typeof app.setBadgeCount === 'function') {
      app.setBadgeCount(state.unread);
    }
  }

  function open(url) {
    showMainWindow();
    loadPage(url || `${pages}/market.html`);
  }

  function buildMenu() {
    return Menu.buildFromTemplate([
      { label: '打开牛宝', click: () => open() },
      { label: state.unread ? `未读通知 ${state.unread} 条` : '没有未读通知', enabled: false },
      { type: 'separator' },
      { label: '市场', click: () => open(`${pages}/market.html`) },
      { label: '持仓', click: () => open(`${pages}/holdings.html`) },
      { type: 'separator' },
      { label: '立即检查定时任务', click: () => wake('manual') },
      {
        label: '开机自动启动', type: 'checkbox', checked: !!prefs.autostart,
        click: (item) => { prefs.autostart = item.checked; writePrefs(prefsFile, prefs); applyAutostart(); },
      },
      { type: 'separator' },
      { label: '退出', click: () => { state.quitting = true; app.quit(); } },
    ]);
  }

  function createTray() {
    let image = nativeImage.createFromPath(iconPath);
    if (!image.isEmpty()) image = image.resize({ width: 18, height: 18 });
    state.tray = new Tray(image);
    const rebuild = () => state.tray.setContextMenu(buildMenu());
    state.tray.on('click', () => open());
    rebuild();
    state.rebuild = rebuild;
    refreshTray();
  }

  function notify(item) {
    if (state.shown.has(item.delivery_id) || !Notification.isSupported()) return;
    state.shown.add(item.delivery_id);
    const n = new Notification({
      title: item.title, body: item.body || '', silent: item.severity !== 'urgent',
      urgency: item.severity === 'urgent' ? 'critical' : 'normal',
    });
    n.on('click', () => open(linkToUrl(item.link, pages)));
    n.show();
  }

  async function poll() {
    const res = await request('GET', `${base}/notify/desktop/pending`);
    if (!res.ok) return;
    const items = res.json.items || [];
    items.forEach(notify);
    if (items.length) await request('POST', `${base}/notify/desktop/ack`, { delivery_ids: items.map((i) => i.delivery_id) });
    const unread = Number(res.json.unread || 0);
    if (unread !== state.unread) {
      state.unread = unread;
      refreshTray();
      if (state.rebuild) state.rebuild();
    }
  }

  applyAutostart();
  createTray();
  powerMonitor.on('resume', () => wake('resume'));
  powerMonitor.on('unlock-screen', () => wake('unlock'));
  state.timer = setInterval(() => { poll().catch(() => {}); }, POLL_MS);
  poll().catch(() => {});
  wake('start');

  app.on('before-quit', () => { state.quitting = true; clearInterval(state.timer); });

  return {
    // 关窗口时调用：托盘在就藏起来，不退出
    shouldHideOnClose: () => !state.quitting && !!state.tray,
    quitting: () => state.quitting,
    prefs,
  };
}

module.exports = { init, linkToUrl, stockBase, readPrefs, writePrefs };

const { app, BrowserWindow, BrowserView, Menu, shell, dialog } = require('electron');
const { spawn, execSync } = require('child_process');
const http = require('http');
const fs = require('fs');
const path = require('path');
const electron = require('electron');
const host = require('./host');

const PORT = process.env.PORT || '8081';
const BASE = (process.env.HARNESS_URL || `http://127.0.0.1:${PORT}`).replace(/\/$/, '');

const PAGES = {
  market: `${BASE}/market.html`,
  index: `${BASE}/`,
  dialog: `${BASE}/dialog.html`,
  space: `${BASE}/space.html`,
  settings: `${BASE}/settings.html`,
  logs: `${BASE}/settings.html#logs`,
};

let win = null;
let splash = null;
let backend = null;
let spawnedBackend = false;
let hostApi = null;
// 常驻页面：切走不销毁，问问/研判的流式输出可继续，回来接着看。
const pageViews = Object.create(null);
let activePageKey = '';

// 单实例：必须赶在启动后端之前。第二个实例会杀掉占着端口的第一个实例的后端，所以直接退出。
const gotLock = app.requestSingleInstanceLock();
if (!gotLock) {
  app.quit();
} else {
  app.on('second-instance', () => showMainWindow());
}
let startHidden = false;
let stockBackend = null;
let spawnedStockBackend = false;

function runtimeRoot() {
  if (app.isPackaged) return path.join(process.resourcesPath, 'runtime');
  return path.join(__dirname, 'runtime');
}

function stockAgentDir() {
  const bundled = path.join(runtimeRoot(), 'stock-agent');
  if (fs.existsSync(path.join(bundled, 'app', 'main.py'))) return bundled;
  if (process.env.STOCK_AGENT_DIR) return process.env.STOCK_AGENT_DIR;
  return path.resolve(__dirname, '..', '..', 'stock_agent_assitant');
}

function stockVenvRoot() {
  return path.join(runtimeRoot(), 'stock-venv');
}

function stockSitePackages() {
  return path.join(stockVenvRoot(), 'site-packages');
}

function stockSiteReady() {
  const site = stockSitePackages();
  return fs.existsSync(site) && fs.existsSync(path.join(stockVenvRoot(), '.relocatable'));
}

function stockPythonBin() {
  // Relocatable layout: always run the bundled interpreter with PYTHONPATH=site-packages.
  if (stockSiteReady() && fs.existsSync(pythonBin())) return pythonBin();
  if (fs.existsSync(pythonBin())) return pythonBin();
  return process.env.STOCK_AGENT_PYTHON || 'python3';
}

function stockDataDir() {
  return path.join(app.getPath('userData'), 'stock-data');
}

function stockLogPath() {
  return path.join(app.getPath('userData'), 'stock-backend.log');
}

/** Cross-builds ship wheels only; fill site-packages once on first launch. */
function ensureStockVenv() {
  if (stockSiteReady()) return true;
  const agent = path.join(runtimeRoot(), 'stock-agent');
  const req = path.join(agent, 'requirements.txt');
  const basePy = pythonBin();
  if (!fs.existsSync(path.join(agent, 'app', 'main.py')) || !fs.existsSync(req) || !fs.existsSync(basePy)) {
    return false;
  }
  const venv = stockVenvRoot();
  const site = stockSitePackages();
  fs.mkdirSync(path.dirname(stockLogPath()), { recursive: true });
  const log = fs.createWriteStream(stockLogPath(), { flags: 'a' });
  try {
    log.write(`[stock-venv] creating relocatable site-packages with ${basePy}\n`);
    fs.mkdirSync(site, { recursive: true });
    const wheels = path.join(runtimeRoot(), 'stock-wheels');
    const find = fs.existsSync(wheels) ? ` --find-links "${wheels}"` : '';
    execSync(`"${basePy}" -m pip install --upgrade pip`, { stdio: ['ignore', 'pipe', 'pipe'] });
    execSync(`"${basePy}" -m pip install --target "${site}"${find} -r "${req}"`, {
      stdio: ['ignore', 'pipe', 'pipe'],
      timeout: 30 * 60 * 1000,
    });
    fs.writeFileSync(path.join(venv, '.relocatable'), 'relocatable=1\n');
    if (process.platform !== 'win32') {
      const bin = path.join(venv, 'bin');
      fs.mkdirSync(bin, { recursive: true });
      const link = path.join(bin, 'python3');
      try {
        fs.rmSync(link, { force: true });
        fs.symlinkSync(path.join('..', '..', 'python', 'bin', 'python3'), link);
      } catch { /* ignore */ }
    }
    log.write('[stock-venv] ready\n');
    return stockSiteReady();
  } catch (err) {
    log.write(`[stock-venv] failed: ${err && err.message ? err.message : err}\n`);
    return false;
  } finally {
    try { log.end(); } catch { /* ignore */ }
  }
}

function webBinary() {
  const name = process.platform === 'win32' ? 'pabuddy-web.exe' : 'pabuddy-web';
  return path.join(runtimeRoot(), 'bin', name);
}

function pythonBin() {
  if (process.platform === 'win32') return path.join(runtimeRoot(), 'python', 'python.exe');
  return path.join(runtimeRoot(), 'python', 'bin', 'python3');
}

function pythonBinDir() {
  if (process.platform === 'win32') return path.join(runtimeRoot(), 'python');
  return path.join(runtimeRoot(), 'python', 'bin');
}

function nodeBinDir() {
  if (process.platform === 'win32') return path.join(runtimeRoot(), 'node');
  return path.join(runtimeRoot(), 'node', 'bin');
}

function gitBinDirs() {
  const git = path.join(runtimeRoot(), 'git');
  if (process.platform !== 'win32') return [];
  return [
    path.join(git, 'usr', 'bin'),
    path.join(git, 'cmd'),
    path.join(git, 'mingw64', 'bin'),
  ];
}

function npmPrefix() {
  return path.join(app.getPath('userData'), 'npm-global');
}

function npmBinDir() {
  if (process.platform === 'win32') return npmPrefix();
  return path.join(npmPrefix(), 'bin');
}

function bundledNodeModules() {
  return path.join(runtimeRoot(), 'node-packages', 'node_modules');
}

function userNodeModules() {
  if (process.platform === 'win32') return path.join(npmPrefix(), 'node_modules');
  return path.join(npmPrefix(), 'lib', 'node_modules');
}

function userConfigPath() {
  return path.join(app.getPath('userData'), 'config.yaml');
}

function webLogPath() {
  return path.join(app.getPath('userData'), 'web.log');
}

function parseYamlApiKey(text) {
  const m = String(text || '').match(/api_key:\s*["']?([^"'#\n]+)/);
  if (!m) return '';
  const v = m[1].trim().replace(/^["']|["']$/g, '');
  if (!v || v === '""' || v === "''") return '';
  return v;
}

function readApiKeyFromFile(p) {
  try {
    if (!p || !fs.existsSync(p)) return '';
    return parseYamlApiKey(fs.readFileSync(p, 'utf8'));
  } catch {
    return '';
  }
}

function readConfiguredApiKey() {
  if (process.env.DEEPSEEK_API_KEY) return process.env.DEEPSEEK_API_KEY.trim();
  // Packaged installs only read the user-data config — never the bundle copy
  // (prepare-runtime ships an empty api_key; older zips may still have a key).
  const candidates = app.isPackaged
    ? [userConfigPath()]
    : [
      userConfigPath(),
      path.resolve(__dirname, '..', 'web', 'config.yaml'),
      path.join(runtimeRoot(), 'config.yaml'),
    ];
  for (const p of candidates) {
    const key = readApiKeyFromFile(p);
    if (key) return key;
  }
  return '';
}

function parseEnvFileValue(text, key) {
  const re = new RegExp(`^\\s*${key}\\s*=\\s*(.*)$`, 'm');
  const m = String(text || '').match(re);
  if (!m) return '';
  return m[1].trim().replace(/^["']|["']$/g, '');
}

function readEnvFileKey(filePath, key) {
  try {
    if (!filePath || !fs.existsSync(filePath)) return '';
    return parseEnvFileValue(fs.readFileSync(filePath, 'utf8'), key);
  } catch {
    return '';
  }
}

/** Tushare token stays out of the .app; prefer env / userData / sibling repo .env. */
function readConfiguredTushareToken() {
  if (process.env.TUSHARE_TOKEN) return process.env.TUSHARE_TOKEN.trim();
  const candidates = [
    path.join(stockDataDir(), '.env'),
    path.join(app.getPath('userData'), '.env'),
  ];
  if (!app.isPackaged) {
    candidates.push(path.join(stockAgentDir(), '.env'));
    candidates.push(path.resolve(__dirname, '..', '..', 'stock_agent_assitant', '.env'));
  }
  for (const p of candidates) {
    const token = readEnvFileKey(p, 'TUSHARE_TOKEN');
    if (token) return token;
  }
  return '';
}

function probeHealth() {
  return new Promise((resolve) => {
    const req = http.get(`${BASE}/healthz`, (res) => {
      resolve(res.statusCode === 200);
      res.resume();
    });
    req.on('error', () => resolve(false));
    req.setTimeout(1500, () => {
      req.destroy();
      resolve(false);
    });
  });
}

async function waitForHealth(ms) {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    if (await probeHealth()) return true;
    if (spawnedBackend && !backend) return false;
    await new Promise((r) => setTimeout(r, 400));
  }
  return false;
}

function backendEnv(apiKey) {
  const sep = process.platform === 'win32' ? ';' : ':';
  fs.mkdirSync(npmBinDir(), { recursive: true });
  const extras = [pythonBinDir(), nodeBinDir(), npmBinDir(), ...gitBinDirs()]
    .filter((d) => fs.existsSync(d));
  const pathKey = process.platform === 'win32' ? 'Path' : 'PATH';
  const env = { ...process.env };
  env[pathKey] = [...extras, process.env[pathKey] || process.env.PATH || ''].join(sep);
  env.PATH = env[pathKey];
  env.PORT = String(new URL(BASE).port || PORT);
  if (apiKey) env.DEEPSEEK_API_KEY = apiKey;
  // Prefer the writable user config so keys never need to live inside the .app.
  const userCfg = userConfigPath();
  const bundledConfig = path.join(runtimeRoot(), 'config.yaml');
  if (fs.existsSync(userCfg)) env.WEB_DEMO_CONFIG = userCfg;
  else if (fs.existsSync(bundledConfig)) env.WEB_DEMO_CONFIG = bundledConfig;
  env.WEB_DEMO_DATA_DIR = path.join(app.getPath('userData'), 'data');
  env.WEB_DEMO_LOG_FILE = webLogPath();
  env.WEB_DEMO_PYTHON_CACHE_DIR = path.join(app.getPath('userData'), 'python-venv');
  const py = pythonBin();
  if (fs.existsSync(py)) env.HARNESS_PYTHON_BIN = py;
  const skills = path.join(runtimeRoot(), 'skills');
  if (fs.existsSync(skills)) env.HARNESS_SKILLS_DIR = skills;
  const agents = path.join(runtimeRoot(), 'agents');
  if (fs.existsSync(agents)) env.HARNESS_AGENTS_DIR = agents;
  const wheels = path.join(runtimeRoot(), 'wheels');
  if (fs.existsSync(wheels)) env.PIP_FIND_LINKS = wheels;
  env.npm_config_prefix = npmPrefix();
  env.NPM_CONFIG_PREFIX = npmPrefix();
  env.npm_config_cache = path.join(app.getPath('userData'), 'npm-cache');
  env.COREPACK_HOME = path.join(app.getPath('userData'), 'corepack');
  const nodePath = [bundledNodeModules(), userNodeModules()].filter((d) => fs.existsSync(d));
  if (nodePath.length) {
    const existing = (env.NODE_PATH || '').split(sep).filter(Boolean);
    env.NODE_PATH = [...nodePath, ...existing].join(sep);
  }
  return env;
}

function freeListenPort(port) {
  const p = String(port);
  try {
    let pids = [];
    if (process.platform === 'win32') {
      const out = execSync('netstat -ano', { encoding: 'utf8' });
      for (const line of out.split(/\r?\n/)) {
        if (!line.includes(`:${p}`) || !/LISTENING/i.test(line)) continue;
        const pid = line.trim().split(/\s+/).pop();
        if (pid && /^\d+$/.test(pid) && pid !== '0') pids.push(pid);
      }
    } else {
      const out = execSync(`lsof -nP -iTCP:${p} -sTCP:LISTEN -t`, {
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
      }).trim();
      pids = out ? out.split(/\n/).filter(Boolean) : [];
    }
    for (const pid of [...new Set(pids)]) {
      try {
        if (process.platform === 'win32') execSync(`taskkill /F /PID ${pid}`, { stdio: 'ignore' });
        else process.kill(Number(pid), 'SIGTERM');
      } catch { /* already gone */ }
    }
    if (pids.length) console.log(`freed port ${p} (killed ${pids.join(', ')})`);
  } catch {
    // nothing listening
  }
}

function startBackend(apiKey) {
  const bundled = webBinary();
  const env = backendEnv(apiKey);
  const logPath = webLogPath();
  fs.mkdirSync(path.dirname(logPath), { recursive: true });
  const log = fs.createWriteStream(logPath, { flags: 'a' });

  // Packaged: always the binary that was just built into the app.
  // Unpackaged: always `go run` so edits to web/static/*.html show up without make mac.
  // (runtime/bin/pabuddy-web is a snapshot and will look "not latest" if we spawn it in dev.)
  if (app.isPackaged) {
    if (!fs.existsSync(bundled)) return false;
    const args = env.WEB_DEMO_CONFIG ? ['-config', env.WEB_DEMO_CONFIG] : [];
    backend = spawn(bundled, args, {
      cwd: app.getPath('userData'),
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
  } else {
    const webDir = path.resolve(__dirname, '..', 'web');
    backend = spawn('go', ['run', '../cmd/web'], {
      cwd: webDir,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
  }
  backend.on('error', (err) => {
    log.write(String(err) + '\n');
    console.error('web backend spawn error:', err);
  });
  spawnedBackend = true;
  backend.stdout.on('data', (d) => { process.stdout.write(d); log.write(d); });
  backend.stderr.on('data', (d) => { process.stderr.write(d); log.write(d); });
  backend.on('exit', (code, signal) => {
    log.end();
    if (spawnedBackend) {
      console.error(`web backend exited (code=${code}, signal=${signal})`);
    }
    spawnedBackend = false;
    backend = null;
  });
  return true;
}

function showSplash() {
  if (splash && !splash.isDestroyed()) return;
  splash = new BrowserWindow({
    width: 400,
    height: 240,
    resizable: false,
    frame: false,
    title: '牛宝',
    backgroundColor: '#ffe8e4',
    icon: path.join(__dirname, 'icon.png'),
    webPreferences: { sandbox: true, contextIsolation: true, nodeIntegration: false },
  });
  splash.loadFile(path.join(__dirname, 'splash.html'));
}

function closeSplash() {
  if (splash && !splash.isDestroyed()) splash.close();
  splash = null;
}

function showMainWindow() {
  if (!app.isReady()) return;
  if (!win || win.isDestroyed()) { createWindow(); return; }
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
}

function pageKeyFromUrl(url) {
  let u;
  try { u = new URL(String(url || ''), BASE); } catch { return 'other'; }
  const p = (u.pathname || '/').replace(/\/+$/, '') || '/';
  if (p.endsWith('market.html') || p.endsWith('/market')) return 'market';
  if (p.endsWith('dialog.html') || p.endsWith('dailog.html')) return 'dialog';
  if (p.endsWith('space.html')) return 'space';
  if (p.endsWith('settings.html')) return 'settings';
  if (p.endsWith('holdings.html')) return 'holdings';
  if (p.endsWith('scan.html')) return 'scan';
  if (p.endsWith('tasks.html')) return 'tasks';
  if (p === '/' || p.endsWith('index.html')) return 'index';
  return p;
}

function isAppPageUrl(url) {
  try {
    const u = new URL(String(url || ''), BASE);
    if (u.origin !== new URL(BASE).origin) return false;
    return pageKeyFromUrl(u.href) !== 'other' || /\.html$/i.test(u.pathname);
  } catch {
    return false;
  }
}

function urlIntent(url) {
  try {
    const u = new URL(String(url || ''), BASE);
    return {
      text: u.searchParams.get('text') || '',
      session: u.searchParams.get('session') || '',
      stock: u.searchParams.get('stock') || u.searchParams.get('code') || '',
      hash: u.hash || '',
    };
  } catch {
    return { text: '', session: '', stock: '', hash: '' };
  }
}

function shouldReloadPage(key, url) {
  const view = pageViews[key];
  if (!view || !view.webContents || view.webContents.isDestroyed()) return true;
  const next = urlIntent(url);
  if (next.text) return true;
  let curUrl = '';
  try { curUrl = view.webContents.getURL(); } catch { return true; }
  const cur = urlIntent(curUrl);
  if (next.session && next.session !== cur.session) return true;
  if (next.stock && next.stock !== cur.stock) return true;
  if (next.hash && next.hash !== cur.hash) return true;
  return false;
}

function insertChromeCSS(wc) {
  if (!wc || wc.isDestroyed()) return;
  wc.insertCSS(`
    html { --electron-title-pad: 72px; }
    .navbar { padding-left: var(--electron-title-pad) !important; -webkit-app-region: drag; }
    .navbar .brand, .navbar .nav-right, .navbar .back-btn,
    .navbar .nav-tabs, .navbar button { -webkit-app-region: no-drag; }
    body > div > header {
      padding-left: var(--electron-title-pad) !important;
      box-sizing: border-box !important;
      -webkit-app-region: drag;
    }
    body > div > header a,
    body > div > header button,
    body > div > header input,
    body > div > header textarea,
    body > div > header select { -webkit-app-region: no-drag; }
  `).catch(() => {});
}

function layoutActivePage() {
  if (!win || win.isDestroyed() || !activePageKey) return;
  const view = pageViews[activePageKey];
  if (!view) return;
  const b = win.getContentBounds();
  view.setBounds({ x: 0, y: 0, width: b.width, height: b.height });
  view.setAutoResize({ width: true, height: true });
}

function attachPageView(view) {
  const wc = view.webContents;
  wc.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });
  wc.on('will-navigate', (event, url) => {
    if (!isAppPageUrl(url)) return;
    event.preventDefault();
    const key = pageKeyFromUrl(url);
    // 同页无新意图：留在常驻实例，避免把进行中的流拆掉。
    if (key === activePageKey && !shouldReloadPage(key, url)) return;
    loadPage(url);
  });
  wc.on('did-finish-load', () => insertChromeCSS(wc));
}

function makePageView() {
  const view = new BrowserView({
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      // 切走页面时勿节流定时器/动画，否则问问/研判 SSE 渲染会卡住。
      backgroundThrottling: false,
    },
  });
  attachPageView(view);
  return view;
}

function loadPage(url) {
  if (!win || win.isDestroyed()) return;
  const target = String(url || PAGES.market);
  const key = pageKeyFromUrl(target);
  let view = pageViews[key];
  const first = !view || !view.webContents || view.webContents.isDestroyed();
  if (first) {
    view = makePageView();
    pageViews[key] = view;
  }
  if (activePageKey && activePageKey !== key && pageViews[activePageKey]) {
    try { win.removeBrowserView(pageViews[activePageKey]); } catch { /* ignore */ }
  }
  win.setBrowserView(view);
  activePageKey = key;
  layoutActivePage();
  if (first || shouldReloadPage(key, target)) {
    view.webContents.loadURL(target);
  } else {
    insertChromeCSS(view.webContents);
  }
  if (!win.isVisible() && !startHidden) win.show();
}

function buildMenu() {
  const pageItems = [
    { label: '市场', accelerator: 'CmdOrCtrl+1', click: () => loadPage(PAGES.market) },
    { label: '工作台', accelerator: 'CmdOrCtrl+2', click: () => loadPage(PAGES.index) },
    { label: '对话', accelerator: 'CmdOrCtrl+3', click: () => loadPage(PAGES.dialog) },
    { label: '个人工作区', accelerator: 'CmdOrCtrl+4', click: () => loadPage(PAGES.space) },
    { label: '设置', accelerator: 'CmdOrCtrl+,', click: () => loadPage(PAGES.settings) },
    { label: '运行日志', accelerator: 'CmdOrCtrl+Shift+L', click: () => loadPage(PAGES.logs) },
  ];
  const revealLogsLabel = process.platform === 'darwin' ? '在访达中显示日志' : '在文件夹中显示日志';
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    {
      label: app.name,
      submenu: [
        { role: 'about' },
        { type: 'separator' },
        { role: 'quit' },
      ],
    },
    { label: '页面', submenu: pageItems },
    { role: 'editMenu' },
    { role: 'viewMenu' },
    { role: 'windowMenu' },
    {
      role: 'help',
      submenu: [
        { label: '运行日志', click: () => loadPage(PAGES.logs) },
        {
          label: revealLogsLabel,
          click: () => {
            const p = webLogPath();
            fs.mkdirSync(path.dirname(p), { recursive: true });
            if (!fs.existsSync(p)) fs.writeFileSync(p, '');
            shell.showItemInFolder(p);
          },
        },
      ],
    },
  ]));
}

function createWindow() {
  const isMac = process.platform === 'darwin';
  win = new BrowserWindow({
    width: 1400,
    height: 940,
    minWidth: 960,
    minHeight: 640,
    backgroundColor: '#ffffff',
    title: '牛宝',
    icon: path.join(__dirname, 'icon.png'),
    show: false,
    titleBarStyle: isMac ? 'hiddenInset' : 'hidden',
    // Keep traffic lights clear of the 48px header (left rail starts after this).
    trafficLightPosition: { x: 14, y: 16 },
    ...(isMac ? {} : {
      titleBarOverlay: { color: '#fafafa', symbolColor: '#1c2a44', height: 48 },
    }),
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  win.once('ready-to-show', () => { if (!startHidden) win.show(); });
  win.on('resize', () => layoutActivePage());
  win.on('closed', () => {
    Object.keys(pageViews).forEach((k) => {
      try {
        if (pageViews[k] && pageViews[k].webContents && !pageViews[k].webContents.isDestroyed()) {
          pageViews[k].webContents.destroy();
        }
      } catch { /* ignore */ }
      delete pageViews[k];
    });
    activePageKey = '';
    win = null;
  });
  // 托盘常驻：关窗口只是藏起来，定时任务和提醒继续跑；从托盘菜单退出才真退出
  win.on('close', (event) => {
    if (hostApi && hostApi.shouldHideOnClose()) {
      event.preventDefault();
      win.hide();
    }
  });

  loadPage(PAGES.market);
}

// 股票数据服务（Python，8090）：安装包自带 stock-agent + stock-venv；开发时也可旁路仓库。
function stockApiHealthy() {
  return new Promise((resolve) => {
    const req = http.get(`${host.stockBase()}/healthz`, (res) => { resolve(res.statusCode === 200); res.resume(); });
    req.on('error', () => resolve(false));
    req.setTimeout(1500, () => { req.destroy(); resolve(false); });
  });
}

async function ensureStockBackend() {
  if (await stockApiHealthy()) return;
  const dir = stockAgentDir();
  if (!fs.existsSync(path.join(dir, 'app', 'main.py'))) {
    console.warn('stock agent not found at', dir);
    return;
  }
  if (!stockSiteReady()) {
    ensureStockVenv();
  }
  const py = stockPythonBin();
  const dataDir = stockDataDir();
  fs.mkdirSync(dataDir, { recursive: true });
  const logPath = stockLogPath();
  fs.mkdirSync(path.dirname(logPath), { recursive: true });
  const log = fs.createWriteStream(logPath, { flags: 'a' });
  const port = String(new URL(host.stockBase()).port || '8090');
  const env = {
    ...process.env,
    PORT: port,
    STOCK_DATA_DIR: dataDir,
    PYTHONUNBUFFERED: '1',
  };
  const tushare = readConfiguredTushareToken();
  if (tushare) env.TUSHARE_TOKEN = tushare;
  else log.write('[stock] warn: TUSHARE_TOKEN missing; ETF 份额/国家队等会取不到\n');
  if (stockSiteReady()) {
    const site = stockSitePackages();
    env.PYTHONPATH = [site, process.env.PYTHONPATH || ''].filter(Boolean).join(path.delimiter);
  }
  log.write(`[stock] spawn ${py} -m app.main cwd=${dir} PORT=${port} STOCK_DATA_DIR=${dataDir} tushare=${tushare ? 'yes' : 'no'}\n`);
  stockBackend = spawn(py, ['-m', 'app.main'], {
    cwd: dir,
    env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  spawnedStockBackend = true;
  stockBackend.stdout.on('data', (d) => log.write(d));
  stockBackend.stderr.on('data', (d) => log.write(d));
  stockBackend.on('error', (err) => log.write(String(err) + '\n'));
  stockBackend.on('exit', (code, signal) => {
    log.write(`[stock] exited code=${code} signal=${signal}\n`);
    spawnedStockBackend = false;
    stockBackend = null;
  });
  // Wait briefly so the first market page load is less likely to race an empty port.
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (await stockApiHealthy()) return;
    if (!spawnedStockBackend) return;
    await new Promise((r) => setTimeout(r, 400));
  }
}

async function boot() {
  buildMenu();
  showSplash();
  const apiKey = readConfiguredApiKey();
  try {
    const port = String(new URL(BASE).port || PORT);
    freeListenPort(port);
    await new Promise((r) => setTimeout(r, 300));
    if (!startBackend(apiKey)) {
      closeSplash();
      dialog.showErrorBox(
        '牛宝',
        '安装包里缺少 web 运行时。请用 make mac / make win 重新打包。',
      );
      app.quit();
      return;
    }
    const ok = await waitForHealth(10 * 60 * 1000);
    if (!ok) {
      closeSplash();
      dialog.showErrorBox(
        '牛宝',
        '无法启动内置服务。请查看日志：\n' + webLogPath(),
      );
      app.quit();
      return;
    }
  } catch (err) {
    closeSplash();
    dialog.showErrorBox('牛宝', '启动失败：' + (err && err.message ? err.message : err));
    app.quit();
    return;
  }
  closeSplash();
  // 开机自启时只带托盘起来，不抢焦点弹窗口
  startHidden = process.argv.includes('--hidden') || (app.getLoginItemSettings && app.getLoginItemSettings().wasOpenedAsHidden);
  await ensureStockBackend();
  createWindow();
  hostApi = host.init({
    app, electron, pageBase: BASE, showMainWindow, loadPage,
    iconPath: path.join(__dirname, 'icon.png'), userData: app.getPath('userData'),
  });
}

if (gotLock) app.whenReady().then(boot);

app.on('activate', () => {
  if (BrowserWindow.getAllWindows().length === 0 && app.isReady()) createWindow();
});

app.on('window-all-closed', () => {
  // 托盘还在就不退出：关掉最后一个窗口后，定时任务和价格提醒继续跑
  if (process.platform !== 'darwin' && !(hostApi && hostApi.shouldHideOnClose())) app.quit();
});

app.on('before-quit', () => {
  if (spawnedStockBackend && stockBackend) {
    spawnedStockBackend = false;
    try { stockBackend.kill('SIGTERM'); } catch { /* ignore */ }
    stockBackend = null;
  }
  if (spawnedBackend && backend) {
    spawnedBackend = false;
    backend.kill('SIGTERM');
    backend = null;
  }
});

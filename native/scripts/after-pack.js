const fs = require('fs');
const path = require('path');

function walk(dir, visit) {
  if (!fs.existsSync(dir)) return;
  for (const name of fs.readdirSync(dir)) {
    const p = path.join(dir, name);
    let st;
    try {
      st = fs.lstatSync(p);
    } catch {
      continue;
    }
    if (st.isSymbolicLink()) continue;
    if (st.isDirectory()) walk(p, visit);
    else visit(p);
  }
}

exports.default = async function afterPack(context) {
  const product = context.packager.appInfo.productFilename;
  const runtime = context.electronPlatformName === 'darwin'
    ? path.join(context.appOutDir, `${product}.app`, 'Contents', 'Resources', 'runtime')
    : path.join(context.appOutDir, 'resources', 'runtime');

  if (!fs.existsSync(runtime)) {
    throw new Error(`bundled runtime missing at ${runtime}; run scripts/prepare-runtime.sh before packaging`);
  }
  const webName = context.electronPlatformName === 'win32' ? 'pabuddy-web.exe' : 'pabuddy-web';
  const web = path.join(runtime, 'bin', webName);
  if (!fs.existsSync(web)) {
    throw new Error(`web binary missing at ${web}`);
  }
  const nodePkgs = path.join(runtime, 'node-packages', 'node_modules');
  for (const pkg of ['pptxgenjs', 'react', 'react-dom', 'react-icons', 'sharp']) {
    if (!fs.existsSync(path.join(nodePkgs, pkg))) {
      throw new Error(`bundled node package missing: ${pkg} under ${nodePkgs}`);
    }
  }

  const executables = new Set([
    webName, 'python3', 'python', 'pip', 'pip3', 'node', 'npm', 'npx',
    'python.exe', 'python3.exe', 'pip.exe', 'node.exe', 'npm.cmd', 'npx.cmd',
    'bash.exe', 'bash', 'git.exe',
  ]);
  walk(runtime, (p) => {
    const base = path.basename(p);
    if (executables.has(base) || p.includes(`${path.sep}bin${path.sep}`)) {
      try { fs.chmodSync(p, 0o755); } catch { /* ignore */ }
    }
  });
};

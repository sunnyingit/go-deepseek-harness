const $ = (id) => document.getElementById(id);
let step = 1;

function paintSteps() {
  document.querySelectorAll('.stp').forEach((el) => {
    const n = Number(el.dataset.step);
    el.classList.toggle('on', n === step);
    el.classList.toggle('done', n < step);
    el.querySelector('.si').textContent = n < step ? '✓' : String(n);
  });
  $('step-welcome').hidden = step !== 1;
  $('step-setup').hidden = step !== 2;
  $('step-done').hidden = step !== 3;
  $('ban-ok').style.display = 'none';
  $('ban-err').style.display = 'none';
  $('ban-err').textContent = '';
}

function goStep(n) {
  step = n;
  paintSteps();
  if (n === 2) setTimeout(() => $('key').focus(), 0);
}

function finish(skipped) {
  if (!window.pabuddy || typeof window.pabuddy.finishSetup !== 'function') {
    $('ban-err').style.display = 'flex';
    $('ban-err').textContent = '应用预加载失败，请重新安装或从终端查看日志。';
    return;
  }
  window.pabuddy.finishSetup({ skipped: !!skipped }).catch((e) => {
    $('ban-err').style.display = 'flex';
    $('ban-err').textContent = String(e && e.message ? e.message : e);
  });
}

$('to-setup').addEventListener('click', () => goStep(2));
$('back-welcome').addEventListener('click', () => goStep(1));
$('skip-welcome').addEventListener('click', () => finish(true));
$('skip-setup').addEventListener('click', () => finish(true));
$('enter').addEventListener('click', () => finish(false));

$('open-deepseek').addEventListener('click', (e) => {
  e.preventDefault();
  if (window.pabuddy && typeof window.pabuddy.openExternal === 'function') {
    window.pabuddy.openExternal('https://platform.deepseek.com/');
  }
});

const key = $('key');
const eye = $('eye');
eye.addEventListener('click', () => {
  const show = key.type === 'password';
  key.type = show ? 'text' : 'password';
  eye.textContent = show ? '隐藏' : '显示';
});

const go = $('go');
const goT = $('go-t');
const spin = $('spin');
go.addEventListener('click', async () => {
  if (go.classList.contains('loading')) return;
  const v = (key.value || '').trim();
  const model = ($('model').value || '').trim();
  $('ban-ok').style.display = 'none';
  $('ban-err').style.display = 'none';
  if (!v || v.length < 8) {
    $('ban-err').style.display = 'flex';
    $('ban-err').textContent = '请先粘贴有效的 API Key（一般以 sk- 开头）。';
    return;
  }
  if (!window.pabuddy || typeof window.pabuddy.saveApiKey !== 'function') {
    $('ban-err').style.display = 'flex';
    $('ban-err').textContent = '应用预加载失败，请重新安装或从终端查看日志。';
    return;
  }
  go.classList.add('loading');
  go.disabled = true;
  goT.textContent = '正在保存…';
  spin.style.display = 'inline-block';
  try {
    const res = await window.pabuddy.saveApiKey(v, { model });
    if (!res || !res.ok) throw new Error((res && res.error) || '保存失败');
    $('ban-ok').style.display = 'flex';
    setTimeout(() => goStep(3), 280);
  } catch (e) {
    $('ban-err').style.display = 'flex';
    $('ban-err').textContent = String(e && e.message ? e.message : e);
  } finally {
    spin.style.display = 'none';
    go.classList.remove('loading');
    go.disabled = false;
    goT.textContent = '保存并继续';
  }
});

key.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') go.click();
});

paintSteps();

const input = document.getElementById('key');
const btn = document.getElementById('save');
const err = document.getElementById('err');

const go = async () => {
  const v = (input.value || '').trim();
  if (!v) {
    err.textContent = '请先填写 API Key。';
    return;
  }
  if (!window.pabuddy || typeof window.pabuddy.saveApiKey !== 'function') {
    err.textContent = '应用预加载失败，请重新安装或从终端查看日志。';
    return;
  }
  btn.disabled = true;
  err.textContent = '正在保存…';
  try {
    const res = await window.pabuddy.saveApiKey(v);
    if (!res || !res.ok) {
      btn.disabled = false;
      err.textContent = (res && res.error) || '保存失败。';
    }
  } catch (e) {
    btn.disabled = false;
    err.textContent = String(e && e.message ? e.message : e);
  }
};

btn.addEventListener('click', go);
input.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') go();
});

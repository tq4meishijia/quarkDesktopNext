/**
 * toast.js —— 轻提示。
 *
 * 挂在 #toast-root（独立于应用树），因此页面整体重渲染不会把提示一起清掉。
 * 同一条消息在短时间内重复触发会被合并，避免上传失败时刷屏。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';

const ICON_BY_LEVEL = { success: 'check', error: 'alert', warn: 'alert', info: 'info' };
const DEFAULT_MS = 2800;

const recent = new Map();

function root() {
  let el = document.getElementById('toast-root');
  if (!el) {
    el = h('div', { id: 'toast-root', class: 'toast-root' });
    document.body.appendChild(el);
  }
  return el;
}

/**
 * 显示一条提示。
 * @param {string} text 文案
 * @param {'success'|'error'|'warn'|'info'} level 级别（决定左侧色条与图标）
 * @param {number} ms 停留时长
 */
export function toast(text, level = 'info', ms = DEFAULT_MS) {
  const message = String(text || '').trim();
  if (!message) return;

  // 同一文案 800ms 内只出一次
  const last = recent.get(message) || 0;
  const now = Date.now();
  if (now - last < 800) return;
  recent.set(message, now);

  const node = h(
    'div',
    { class: 'toast', dataset: { level } },
    icon(ICON_BY_LEVEL[level] || 'info', 16),
    h('span', { class: 'grow', text: message })
  );

  const close = () => {
    if (!node.parentNode) return;
    node.classList.add('toast--leave');
    setTimeout(() => node.remove(), 180);
  };

  node.addEventListener('click', close);
  root().appendChild(node);
  setTimeout(close, ms);
}

export const notify = {
  info: (t, ms) => toast(t, 'info', ms),
  success: (t, ms) => toast(t, 'success', ms),
  error: (t, ms) => toast(t, 'error', ms),
  warn: (t, ms) => toast(t, 'warn', ms),
};

/** 把任意异常转成一条错误提示，统一错误文案入口。 */
export function toastError(err, fallback = '操作失败') {
  const msg = err && err.message ? err.message : typeof err === 'string' ? err : fallback;
  toast(msg, 'error', 3600);
}

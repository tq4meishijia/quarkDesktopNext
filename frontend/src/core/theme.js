/**
 * theme.js —— 亮/暗主题应用。
 *
 * 主题值三态：light / dark / system。system 会解析成实际取值后写到
 * <html data-theme>，并监听系统偏好变化实时跟随。
 */

const QUERY = '(prefers-color-scheme: dark)';

/** 把 theme 偏好解析成实际取值。 */
export function resolveTheme(pref) {
  if (pref === 'system') {
    if (typeof window.matchMedia === 'function') {
      return window.matchMedia(QUERY).matches ? 'dark' : 'light';
    }
    return 'light';
  }
  return pref === 'light' ? 'light' : 'dark';
}

/** 应用主题到文档根。 */
export function applyTheme(pref) {
  document.documentElement.dataset.theme = resolveTheme(pref);
  return document.documentElement.dataset.theme;
}

/**
 * 监听系统主题变化；回调参数为系统当前是 dark 还是 light。
 * 返回取消监听函数。
 */
export function watchSystemTheme(cb) {
  if (typeof window.matchMedia !== 'function') return () => {};
  const mq = window.matchMedia(QUERY);
  const handler = (e) => cb(e.matches ? 'dark' : 'light');
  // Safari < 14 只有 addListener
  if (typeof mq.addEventListener === 'function') {
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }
  mq.addListener(handler);
  return () => mq.removeListener(handler);
}

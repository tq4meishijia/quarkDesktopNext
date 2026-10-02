/**
 * router.js —— 基于 location.hash 的前端路由。
 *
 * 用 hash 而非 History API：Wails 的 AssetServer 在 wails:// 协议下没有
 * 可用的 pushState 基址，hash 路由在桌面壳与静态服务器预览两种场景下行为一致。
 */

/**
 * @param {object} opts
 * @param {Record<string, {title: string, render: (ctx:object)=>Node}>} opts.routes
 * @param {string} opts.fallback 默认路由名
 * @param {(name:string, prev:string)=>void} opts.onChange
 */
export function createRouter({ routes, fallback = 'files', onChange }) {
  let current = fallback;

  const parse = () => {
    const raw = (location.hash || '').replace(/^#\/?/, '').split('?')[0];
    return Object.prototype.hasOwnProperty.call(routes, raw) ? raw : fallback;
  };

  const emit = (next) => {
    const prev = current;
    current = next;
    if (onChange) onChange(next, prev);
  };

  const onHashChange = () => emit(parse());

  return {
    get current() {
      return current;
    },
    /** 取当前路由的元信息。 */
    meta(name = current) {
      return routes[name] || routes[fallback];
    },
    /** 启动路由监听。 */
    start() {
      window.addEventListener('hashchange', onHashChange);
      emit(parse());
    },
    /** 跳转。 */
    go(name) {
      if (!Object.prototype.hasOwnProperty.call(routes, name)) return;
      if (current === name) {
        emit(name); // 同名也触发一次，便于「再点一次刷新当前页」
        return;
      }
      location.hash = '#/' + name;
    },
    destroy() {
      window.removeEventListener('hashchange', onHashChange);
    },
  };
}

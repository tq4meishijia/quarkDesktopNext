/**
 * session.js —— 会话失效时的本地痕迹清理。
 *
 * 为什么需要单独一个模块：退出登录有多个入口（侧边栏退出、设置页清凭证、
 * 会话过期、接口鉴权失败），每处都手写一遍清理逻辑 inevitably 会漏。
 * 收敛到这里后，新增入口只需调用一次 clearSession。
 *
 * 清理范围：
 *   1. localStorage / sessionStorage：本客户端写过的全部键；
 *   2. Cookie：登录令牌类（本客户端写入的、以及夸克凭证）；
 *   3. 内存态：交给 store 层面的 reset（见 app.js）。
 *
 * 关于「服务端会话注销」：夸克是第三方 Cookie 模式，SDK 未提供单点登出接口，
 * 因此本模块只能做到「清除本机痕迹 + 断开内存会话」。真正的服务端注销
 * 在夸克侧不存在可调用的公开端点，这一点在 README 与设置页均有说明。
 */

/** 本客户端会写入的存储键。任何新增的持久化键都必须登记到这里。 */
const OWNED_KEYS = [
  'kuake-desktop.mock.settings', // 预览模式的设置
  'kuake-desktop.view',          // 文件页视图模式
];

/**
 * Cookie 名前缀/精确名：登录令牌类一律清掉。
 * ctoken 是夸克下发的 CSRF 会话令牌，登录态失效后必须一并清除。
 */
const OWNED_COOKIES = ['ctoken', 'b-user-id', '__pus', '__puus', '__kps', '__kp', '__ktd', 'auth'];

/**
 * 清空 localStorage。
 * 优先只删本客户端自己的键；只有在无法枚举时才整体清空——
 * WebView 的 localStorage 与网页同域，误删用户其它站点的数据不合适。
 */
export function clearLocalStorage() {
  try {
    if (typeof localStorage === 'undefined') return;
    for (const k of OWNED_KEYS) localStorage.removeItem(k);
    // 兜底：清掉本客户端命名空间下的任何残留（历史版本可能写过别的键）
    const doomed = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith('kuake-desktop.')) doomed.push(k);
    }
    doomed.forEach((k) => localStorage.removeItem(k));
  } catch (err) {
    console.warn('[session] 清理 localStorage 失败', err);
  }
}

/** 清空 sessionStorage。 */
export function clearSessionStorage() {
  try {
    if (typeof sessionStorage === 'undefined') return;
    for (const k of OWNED_KEYS) sessionStorage.removeItem(k);
    sessionStorage.clear();
  } catch (err) {
    console.warn('[session] 清理 sessionStorage 失败', err);
  }
}

/**
 * 删除登录令牌 Cookie。
 *
 * Cookie 的 path/domain 由写入方决定，逐个尝试常见组合；
 * 写不掉的（HttpOnly 等）由浏览器自行处理，这里不做也无法做更多。
 */
export function clearAuthCookies() {
  if (typeof document === 'undefined') return;
  const secure = location.protocol === 'https:' ? '; Secure' : '';
  for (const name of OWNED_COOKIES) {
    // 逐个尝试：不同 path/domain 下可能存在多份同名 Cookie
    const variants = [
      `${name}=; Max-Age=0; Path=/${secure}`,
      `${name}=; Max-Age=0; Path=/__proxy/${secure}`,
      `${name}=; Max-Age=0; Path=/; Domain=quark.cn${secure}`,
      `${name}=; Max-Age=0; Path=/; Domain=.quark.cn${secure}`,
    ];
    for (const v of variants) {
      try {
        document.cookie = v;
      } catch (err) {
        // 单个写失败不影响其余尝试
      }
    }
  }
}

/**
 * 执行完整清理。所有退出入口（主动退出 / 会话过期 / 鉴权失败 / 清凭证）
 * 都必须调用它，保证行为一致。
 */
export function clearSession() {
  clearLocalStorage();
  clearSessionStorage();
  clearAuthCookies();
}

// 供测试与调试查看当前残留
export function listOwnedKeys() {
  const out = { local: [], session: [], cookie: '' };
  try {
    if (typeof localStorage !== 'undefined') {
      for (let i = 0; i < localStorage.length; i++) {
        const k = localStorage.key(i);
        if (k) out.local.push(k);
      }
    }
    if (typeof sessionStorage !== 'undefined') {
      for (let i = 0; i < sessionStorage.length; i++) {
        const k = sessionStorage.key(i);
        if (k) out.session.push(k);
      }
    }
    if (typeof document !== 'undefined') out.cookie = document.cookie || '';
  } catch (err) {
    console.warn('[session] 枚举失败', err);
  }
  return out;
}

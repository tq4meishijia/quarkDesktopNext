/**
 * wails.js —— 真实后端适配器。
 *
 * 只做两件事：
 *   1. 把 wails 生成的绑定（window.go.app.App.*）包装成 bridge 契约里的分组方法；
 *   2. 把 Go 侧 reject 出来的字符串错误统一成 Error 对象。
 * 这里不写任何业务逻辑，业务判断一律留给页面与 Go 侧。
 */

/** Go 侧 reject 的多为字符串，统一成 Error，便于调用方读 message。 */
function normalize(err) {
  if (err instanceof Error) return err;
  if (typeof err === 'string') return new Error(err);
  if (err && typeof err.message === 'string') return new Error(err.message);
  return new Error('调用后端失败');
}

export async function createWailsBridge() {
  // 未注入 window.go 说明不是跑在 Wails 壳里（例如用静态服务器预览），直接抛出让上层回落 mock。
  const injected = !!(window.go && window.go.app && window.go.app.App);
  if (!injected) {
    throw new Error('未检测到 Wails 绑定');
  }

  let api;
  let rt;
  try {
    api = await import('../../wailsjs/go/app/App.js');
    rt = await import('../../wailsjs/runtime/runtime.js');
  } catch (err) {
    throw new Error('Wails 绑定模块加载失败：' + (err && err.message ? err.message : err));
  }

  /** 调用一个绑定方法并归一化错误。 */
  const call = (name, ...args) => {
    const fn = api[name];
    if (typeof fn !== 'function') {
      return Promise.reject(new Error('后端缺少方法：' + name));
    }
    return Promise.resolve()
      .then(() => fn(...args))
      .catch((e) => {
        throw normalize(e);
      });
  };

  /** 无参数的整数型操作统一成 boolean 返回。 */
  const ok = (name, ...args) => call(name, ...args).then(() => true);

  return {
    mode: 'wails',

    auth: {
      status: () => call('AuthStatus'),
      login: (cookie) => call('Login', cookie),
      loginEnv: () => call('LoginFromEnv'),
      logout: () => ok('Logout'),
      profile: () => call('GetProfile'),
      // 交互式登录：起本地代理并让用户照常登录，成功后自动完成登录
      interactiveStart: () => call('StartInteractiveLogin'),
      interactiveStatus: () => call('InteractiveLoginStatus'),
      interactiveCancel: () => ok('CancelInteractiveLogin'),
    },

    files: {
      list: (path) => call('ListDir', path),
      search: (kw, root, recursive) => call('Search', kw, root, recursive),
      mkdir: (parent, name) => call('CreateFolder', parent, name),
      rename: (path, newName) => ok('Rename', path, newName),
      remove: (paths) => call('Delete', paths),
      move: (src, destDir) => ok('Move', src, destDir),
      copy: (src, destDir) => ok('Copy', src, destDir),
      resolveFid: (path) => call('ResolveFid', path),
    },

    transfer: {
      pickFiles: () => call('PickUploadFiles'),
      pickDir: () => call('PickDownloadDir'),
      upload: (paths, dir) => call('EnqueueUploads', paths, dir),
      download: (items) => call('EnqueueDownloads', items),
      list: () => call('ListTasks'),
      pause: (id) => ok('PauseTask', id),
      resume: (id) => ok('ResumeTask', id),
      cancel: (id) => ok('CancelTask', id),
      retry: (id) => ok('RetryTask', id),
      pauseAll: () => call('PauseAllTasks'),
      resumeAll: () => call('ResumeAllTasks'),
      clearCompleted: () => call('ClearCompletedTasks'),
    },

    share: {
      parse: (text) => call('ParseShare', text),
      save: (req) => call('SaveShare', req),
      create: (path, days, needPwd) => call('CreateShareLink', path, days, needPwd),
      myShares: (page, size) => call('ListMyShares', page, size),
      remove: (ids) => ok('DeleteShare', ids),
    },

    settings: {
      get: () => call('GetSettings'),
      save: (s) => call('SaveSettings', s),
      dir: () => call('ConfigDir'),
      ensureDir: (d) => call('EnsureDownloadDir', d),
    },

    /**
     * 订阅后端事件。返回取消订阅函数。
     * Wails 的 EventsOn 同名事件可多次注册，这里按回调逐个解绑。
     */
    on(event, cb) {
      if (rt && typeof rt.EventsOn === 'function') {
        return rt.EventsOn(event, cb) || (() => rt.EventsOff && rt.EventsOff(event));
      }
      return () => {};
    },
  };
}

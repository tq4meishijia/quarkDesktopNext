/**
 * app.js —— 应用外壳：登录态、路由、导航与全局事件。
 *
 * 外壳只负责三件事：
 *   1. 决定显示登录页还是主界面；
 *   2. 按 hash 路由挂载页面，并在切换时销毁上一页（释放事件订阅）；
 *   3. 把后端的全局事件（登录态变化、传输进度、提示）转到 store / toast。
 * 具体业务一律在页面内部完成。
 */
import { h, mount } from './core/dom.js';
import { icon } from './core/icons.js';
import { createStore } from './core/store.js';
import { createRouter } from './core/router.js';
import { applyTheme, watchSystemTheme } from './core/theme.js';
import { clearSession } from './core/session.js';
import { bridge, initBridge, onEvent, EVENTS, bridgeMode } from './bridge/index.js';
import { sidebar } from './components/sidebar.js';
import { toast, toastError, notify } from './components/toast.js';
import { confirmDialog } from './components/modal.js';

import { filesPage, meta as filesMeta } from './pages/files.js';
import { transferPage, meta as transferMeta } from './pages/transfer.js';
import { sharePage, meta as shareMeta } from './pages/share.js';
import { settingsPage, meta as settingsMeta } from './pages/settings.js';
import { loginPage } from './pages/login.js';

const FALLBACK_SETTINGS = { downloadDir: '', concurrency: 3, theme: 'system', uploadPolicy: 'skip', startMinimized: false };
const ACTIVE_STATUS = new Set(['pending', 'running', 'paused']);

/**
 * 启动应用。
 * @param {HTMLElement} root 挂载点
 */
export async function startApp(root) {
  const { mode, reason } = await initBridge();
  const api = () => bridge();

  // 设置与登录态：任何一项失败都不能阻止界面启动，用回落值兜住。
  let settings = FALLBACK_SETTINGS;
  try {
    settings = (await api().settings.get()) || FALLBACK_SETTINGS;
  } catch (err) {
    console.warn('[app] 读取设置失败，使用默认值', err);
  }

  let auth = { loggedIn: false };
  try {
    auth = (await api().auth.status()) || { loggedIn: false };
  } catch (err) {
    console.warn('[app] 读取登录态失败', err);
  }

  applyTheme(settings.theme || 'system');
  if ((settings.theme || 'system') === 'system') {
    watchSystemTheme(() => applyTheme('system'));
  }

  const store = createStore({
    auth,
    settings,
    mode,
    tasks: new Map(),
    route: 'files',
  });

  if (mode === 'mock') {
    console.info('[app] 预览模式：' + (reason || '未检测到 Wails 绑定') + '，界面使用 mock 数据');
  }

  // ---------- 全局事件 ----------

  onEvent(EVENTS.authChanged, (next) => {
    // 任何导致未登录的变化都要清掉本地痕迹：主动退出、会话过期、鉴权失败、
    // 清除本机凭证都走这条路径（后端 invalidateSession 统一收口）。
    if (!next || !next.loggedIn) {
      clearSession();
    }
    store.set({ auth: next || { loggedIn: false } });
    render();
  });

  onEvent(EVENTS.toast, (payload) => {
    if (!payload) return;
    toast(payload.text || '', payload.level || 'info');
  });

  const upsertTask = (task) => {
    if (!task || !task.id) return;
    const map = new Map(store.get().tasks);
    map.set(task.id, task);
    store.set({ tasks: map });
    renderSidebarOnly();
  };
  onEvent(EVENTS.transferUpdate, upsertTask);
  onEvent(EVENTS.transferList, (list) => {
    if (!Array.isArray(list)) return;
    const map = new Map();
    for (const t of list) if (t && t.id) map.set(t.id, t);
    store.set({ tasks: map });
    renderSidebarOnly();
  });

  // ---------- 路由 ----------

  const shell = h('div', { class: 'app-shell' });
  let currentView = null;
  let sidebarNode = null;

  const ctx = {
    go(name) {
      router.go(name);
    },
    applyTheme(theme) {
      store.set({ settings: { ...store.get().settings, theme } });
      applyTheme(theme);
    },
    /**
     * sessionInvalid 由页面在发现会话失效时调用（令牌过期、接口鉴权失败、
     * 登录态校验不通过）。内部会通知后端执行统一清理，并清掉本地存储。
     */
    sessionInvalid(reason) {
      return reportSessionInvalid(reason || 'expired');
    },
    store,
  };

  const routes = {
    files: { ...filesMeta, render: () => filesPage(ctx) },
    transfer: { ...transferMeta, render: () => transferPage(ctx) },
    share: { ...shareMeta, render: () => sharePage(ctx) },
    settings: { ...settingsMeta, render: () => settingsPage(ctx) },
  };

  const router = createRouter({
    routes,
    fallback: 'files',
    onChange(name) {
      store.set({ route: name });
      if (!store.get().auth.loggedIn) return;
      mountPage(name);
    },
  });

  function mountPage(name) {
    if (currentView && typeof currentView.destroy === 'function') {
      try {
        currentView.destroy();
      } catch (err) {
        console.warn('[app] 页面销毁失败', err);
      }
    }
    const route = routes[name] || routes.files;
    currentView = route.render();
    const content = shell.querySelector('.app-content');
    if (content) mount(content, currentView.node);
    renderTopbar(route.title || '');
  }

  // ---------- 外壳渲染 ----------

  function activeTaskCount() {
    let n = 0;
    for (const t of store.get().tasks.values()) {
      if (ACTIVE_STATUS.has(t.status)) n++;
    }
    return n;
  }

  async function doLogout() {
    const ok = await confirmDialog({
      title: '退出登录',
      message: '退出后会清除本机保存的凭证与登录状态，需要重新登录才能继续使用。',
      confirmText: '退出',
      danger: true,
    });
    if (!ok) return;
    try {
      await api().auth.logout();
      // 后端已删 session.json 并广播 auth:changed（那里会调 clearSession），
      // 这里再清一次是为了覆盖「事件未送达」的情况，保证不留痕迹。
      resetSessionState();
      notify.info('已退出登录');
      render();
    } catch (err) {
      toastError(err, '退出失败');
    }
  }

  /**
   * resetSessionState 清空内存态与本地存储，是「会话失效」的本地收口。
   * 任何入口（退出 / 过期 / 鉴权失败）都调用它，保证行为一致。
   */
  function resetSessionState() {
    clearSession();
    // 内存态一并重置：用户信息、任务列表、当前路由都要归位，
    // 否则残留的昵称/头像/容量会短暂出现在界面上。
    store.set({
      auth: { loggedIn: false, reason: 'logout' },
      tasks: new Map(),
      route: 'files',
    });
    if (currentView && typeof currentView.destroy === 'function') {
      currentView.destroy();
    }
    currentView = null;
  }

  /**
   * reportSessionInvalid 由页面在发现会话失效时调用（令牌过期、接口鉴权失败）。
   * 会通知后端执行统一清理，并同步清掉本地存储。
   */
  async function reportSessionInvalid(reason) {
    try {
      await api().auth.sessionInvalid(reason);
    } catch (err) {
      // 后端不可达时仍要清本地，不能因为这一步失败就留下痕迹
      console.warn('[app] 通知后端会话失效失败，仍执行本地清理', err);
    }
    resetSessionState();
    render();
  }

  function buildSidebar() {
    return sidebar({
      active: store.get().route,
      onNavigate: (name) => router.go(name),
      auth: store.get().auth,
      activeTaskCount: activeTaskCount(),
      onLogout: doLogout,
    });
  }

  /** 只换侧边栏，避免任务事件把整个页面重渲染（会打断输入）。 */
  function renderSidebarOnly() {
    if (!sidebarNode || !sidebarNode.parentNode) return;
    const next = buildSidebar();
    sidebarNode.replaceWith(next);
    sidebarNode = next;
  }

  function renderTopbar(title) {
    const bar = shell.querySelector('.topbar');
    if (!bar) return;
    mount(
      bar,
      h('div', { class: 'topbar__title', text: title }),
      h('span', { class: 'grow' }),
      store.get().mode === 'mock' ? h('span', { class: 'badge badge--warn', text: '预览模式' }) : null
    );
  }

  async function buildShell() {
    const s = store.get();
    sidebarNode = buildSidebar();
    mount(
      shell,
      sidebarNode,
      h(
        'div',
        { class: 'app-main' },
        h('header', { class: 'topbar' }),
        h('div', { class: 'app-content' })
      )
    );

    // 进入主界面时先拉一次任务列表，让侧边栏角标一开始就是准的
    try {
      const list = await api().transfer.list();
      const map = new Map();
      for (const t of list || []) if (t && t.id) map.set(t.id, t);
      store.set({ tasks: map });
      sidebarNode.replaceWith(buildSidebar());
      sidebarNode = shell.querySelector('.sidebar');
    } catch (err) {
      /* 任务列表拉取失败不影响主流程 */
    }

    router.start();
  }

  function renderLogin() {
    const view = loginPage({
      onSuccess(next) {
        store.set({ auth: next || { loggedIn: true } });
        notify.success('登录成功');
        render();
      },
    });
    mount(root, view.node);
  }

  function render() {
    const s = store.get();
    if (!s.auth || !s.auth.loggedIn) {
      renderLogin();
      return;
    }
    mount(root, shell);
    buildShell();
  }

  render();
  return store;
}

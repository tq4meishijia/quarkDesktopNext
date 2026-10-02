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
      message: '退出后需要重新粘贴 Cookie 才能继续使用。',
      confirmText: '退出',
      danger: true,
    });
    if (!ok) return;
    try {
      await api().auth.logout();
      store.set({ auth: { loggedIn: false } });
      notify.info('已退出登录');
      render();
    } catch (err) {
      toastError(err, '退出失败');
    }
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

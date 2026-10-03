/**
 * mock.js —— 预览用假数据适配器。
 *
 * 当页面不是跑在 Wails 壳里（例如用任意静态服务器打开 frontend/）时，
 * bridge/index.js 会回落到本模块，让全部页面依旧可点击、可演示。
 * 它实现与 wails.js 完全相同的方法契约，因此页面代码不需要区分两种模式。
 */

const LS_KEY = 'kuake-desktop.mock.settings';
const MB = 1024 * 1024;
const DAY = 86400;

const nowSec = () => Math.floor(Date.now() / 1000);

function item(parent, o) {
  const p = (parent === '/' ? '' : parent) + '/' + o.name;
  return {
    fid: o.fid,
    name: o.name,
    path: p,
    size: o.size || 0,
    isDir: !!o.isDir,
    mtime: o.mtime,
    ctime: o.ctime || o.mtime,
    ext: o.isDir ? '' : (o.name.includes('.') ? o.name.split('.').pop().toLowerCase() : ''),
  };
}

function seedDB() {
  const T = nowSec();
  return {
    '/': [
      item('/', { fid: 'd-doc', name: '文档', isDir: true, mtime: T - 2 * DAY }),
      item('/', { fid: 'd-img', name: '图片', isDir: true, mtime: T - 5 * DAY }),
      item('/', { fid: 'd-vid', name: '视频', isDir: true, mtime: T - 9 * DAY }),
      item('/', { fid: 'd-arc', name: '项目归档', isDir: true, mtime: T - 30 * DAY }),
      item('/', { fid: 'f-001', name: '入门指南.pdf', size: 2411724, mtime: T - DAY }),
      item('/', { fid: 'f-002', name: '2026 年度规划.docx', size: 188416, mtime: T - 2 * DAY }),
      item('/', { fid: 'f-003', name: '财务报表.xlsx', size: 45056, mtime: T - 4 * DAY }),
      item('/', { fid: 'f-004', name: 'README.md', size: 4096, mtime: T - 6 * DAY }),
    ],
    '/文档': [
      item('/文档', { fid: 'd-doc-2', name: '合同', isDir: true, mtime: T - 12 * DAY }),
      item('/文档', { fid: 'f-101', name: '需求说明书 v2.pdf', size: 3563520, mtime: T - 3 * DAY }),
      item('/文档', { fid: 'f-102', name: '接口约定.md', size: 27648, mtime: T - 7 * DAY }),
      item('/文档', { fid: 'f-103', name: '会议纪要.docx', size: 61440, mtime: T - 11 * DAY }),
    ],
    '/文档/合同': [
      item('/文档/合同', { fid: 'f-201', name: '服务协议-已签署.pdf', size: 1048576, mtime: T - 40 * DAY }),
      item('/文档/合同', { fid: 'f-202', name: '保密协议.pdf', size: 524288, mtime: T - 41 * DAY }),
    ],
    '/图片': [
      item('/图片', { fid: 'f-301', name: '首页设计稿.png', size: 8388608, mtime: T - 5 * DAY }),
      item('/图片', { fid: 'f-302', name: '产品白底图.jpg', size: 4194304, mtime: T - 8 * DAY }),
      item('/图片', { fid: 'f-303', name: '团队合影.heic', size: 12582912, mtime: T - 20 * DAY }),
      item('/图片', { fid: 'f-304', name: '图标汇总.svg', size: 98304, mtime: T - 22 * DAY }),
    ],
    '/视频': [
      item('/视频', { fid: 'f-401', name: '产品宣传片.mp4', size: 734003200, mtime: T - 9 * DAY }),
      item('/视频', { fid: 'f-402', name: '发布会回放.mkv', size: 2147483648, mtime: T - 15 * DAY }),
      item('/视频', { fid: 'f-403', name: '操作演示.mov', size: 157286400, mtime: T - 18 * DAY }),
    ],
    '/项目归档': [
      item('/项目归档', { fid: 'd-arc-2', name: '2025', isDir: true, mtime: T - 200 * DAY }),
      item('/项目归档', { fid: 'f-501', name: '全量代码备份.zip', size: 536870912, mtime: T - 30 * DAY }),
      item('/项目归档', { fid: 'f-502', name: '数据库快照.sql', size: 104857600, mtime: T - 31 * DAY }),
    ],
    '/项目归档/2025': [
      item('/项目归档/2025', { fid: 'f-601', name: '年度总结.pptx', size: 20971520, mtime: T - 210 * DAY }),
      item('/项目归档/2025', { fid: 'f-602', name: '旧版设计稿.sketch', size: 67108864, mtime: T - 240 * DAY }),
    ],
  };
}

function seedTasks() {
  const T = nowSec();
  return [
    {
      id: 'dl-mock-1', kind: 'download', name: '产品宣传片.mp4',
      localPath: 'C:/Users/demo/Downloads/QuarkDrive', dest: 'C:/Users/demo/Downloads/QuarkDrive/产品宣传片.mp4',
      remotePath: '/视频/产品宣传片.mp4',
      size: 734003200, done: 268435456, status: 'running', speed: 9437184,
      error: '', engine: '内建下载器（多线程分片）', createdAt: T - 120, finishedAt: 0,
    },
    {
      id: 'up-mock-1', kind: 'upload', name: '首页设计稿 v3.png',
      localPath: 'C:/Users/demo/Desktop/首页设计稿 v3.png', dest: '', remotePath: '/图片/首页设计稿 v3.png',
      size: 12582912, done: 4718592, status: 'running', speed: 2097152,
      error: '', engine: '内建上传', createdAt: T - 60, finishedAt: 0,
    },
    {
      id: 'dl-mock-2', kind: 'download', name: '会议纪要.docx',
      localPath: 'C:/Users/demo/Downloads/QuarkDrive', dest: 'C:/Users/demo/Downloads/QuarkDrive/会议纪要.docx',
      remotePath: '/文档/会议纪要.docx',
      size: 61440, done: 61440, status: 'completed', speed: 0,
      error: '', engine: 'aria2', createdAt: T - 300, finishedAt: T - 292,
    },
    {
      id: 'dl-mock-3', kind: 'download', name: '全量代码备份.zip',
      localPath: 'C:/Users/demo/Downloads/QuarkDrive', dest: 'C:/Users/demo/Downloads/QuarkDrive/全量代码备份.zip',
      remotePath: '/项目归档/全量代码备份.zip',
      size: 536870912, done: 0, status: 'handedoff', speed: 0,
      error: '', engine: '迅雷', createdAt: T - 200, finishedAt: T - 199,
    },
    {
      id: 'up-mock-2', kind: 'upload', name: '全量代码备份.zip',
      localPath: 'C:/Users/demo/项目归档/全量代码备份.zip', dest: '', remotePath: '/项目归档/全量代码备份.zip',
      size: 536870912, done: 125829120, status: 'failed', speed: 0,
      error: '连接被重置（模拟故障）', engine: '内建上传', createdAt: T - 420, finishedAt: T - 400,
    },
  ];
}

const DEFAULT_SETTINGS = {
  downloadDir: '',
  concurrency: 3,
  theme: 'dark',
  uploadPolicy: 'skip',
  startMinimized: false,
  downloader: 'builtin',
  downloaderExec: '',
  downloaderArgs: [],
  segments: 4,
  sameName: 'rename',
};

// 预览模式下的下载器清单，与真实后端 registry 保持同样的字段。
const MOCK_DOWNLOADERS = [
  { id: 'builtin', label: '内建下载器（多线程分片）', note: '纯 Go 实现，支持分片并发与断点续传。', kind: 'process', path: 'builtin', ready: true, defaultArgs: [] },
  { id: 'aria2c', label: 'aria2', note: '需要本机已安装 aria2。', kind: 'process', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'wget', label: 'Wget', note: '类 Unix 环境常见。', kind: 'process', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'curl', label: 'cURL', note: '几乎所有系统自带。', kind: 'process', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'idm', label: 'Internet Download Manager', note: '唤起 IDM 接管下载。', kind: 'launch', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'thunder', label: '迅雷', note: '唤起迅雷接管下载。', kind: 'launch', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'motrix', label: 'Motrix', note: '唤起 Motrix 接管下载。', kind: 'launch', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'fdm', label: 'Free Download Manager', note: '唤起 FDM 接管下载。', kind: 'launch', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'jdownloader', label: 'JDownloader', note: '唤起 JDownloader 接管下载。', kind: 'launch', path: '', ready: false, defaultArgs: ['{url}'] },
  { id: 'browser', label: '系统浏览器', note: '复制直链并用默认浏览器打开。', kind: 'url', path: '', ready: true, defaultArgs: ['{url}'] },
];

function loadSettings() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) return { ...DEFAULT_SETTINGS, ...JSON.parse(raw) };
  } catch (err) {
    /* localStorage 不可用（如 file:// 下）时静默回落到默认值 */
  }
  return { ...DEFAULT_SETTINGS };
}

/** 生成一个 0-1 之间的抖动系数，让模拟进度看起来不匀速。 */
const jitter = () => 0.65 + Math.random() * 0.7;

export function createMockBridge() {
  const listeners = new Map();
  const db = seedDB();
  const tasks = seedTasks();
  const fidIndex = new Map();
  let settings = loadSettings();
  let auth = { loggedIn: false, source: '', masked: '', message: '未登录', profile: null };
  let seq = 200;
  // 交互式登录的模拟状态与定时器
  let interactive = { active: false, phase: 'idle', hint: '', url: '', collected: 0, remaining: 0 };
  let interactiveTimer = null;
  let interactiveTimer2 = null;

  const reindex = () => {
    fidIndex.clear();
    for (const list of Object.values(db)) {
      for (const it of list) fidIndex.set(it.fid, it);
    }
  };
  reindex();

  const emit = (name, payload) => {
    const arr = listeners.get(name);
    if (!arr) return;
    for (const fn of Array.from(arr)) {
      try {
        fn(payload);
      } catch (err) {
        console.error('[mock] listener error', err);
      }
    }
  };

  const dto = (t) => ({
    ...t,
    progress: t.size > 0 ? Math.round((t.done / t.size) * 1000) / 10 : 0,
  });
  const emitTask = (t) => emit('transfer:update', dto(t));

  // ---- 模拟调度：每 500ms 推进一次进行中的任务 ----
  const tick = () => {
    let changed = false;
    for (const t of tasks) {
      if (t.status !== 'running') continue;
      const step = Math.max(1, Math.round(t.speed * 0.5 * jitter()));
      t.done = Math.min(t.size, t.done + step);
      t.speed = Math.max(524288, Math.round(t.speed * jitter()));
      if (t.done >= t.size) {
        t.status = 'completed';
        t.speed = 0;
        t.finishedAt = nowSec();
      }
      emitTask(t);
      changed = true;
    }
    // 队列里排队等待的任务在有空位时启动（受并发数限制）
    if (!changed) return;
    const running = tasks.filter((t) => t.status === 'running').length;
    if (running < settings.concurrency) {
      const pending = tasks.find((t) => t.status === 'pending');
      if (pending) {
        pending.status = 'running';
        pending.speed = 3 * MB;
        emitTask(pending);
      }
    }
  };
  const timer = setInterval(tick, 500);

  const findTask = (id) => tasks.find((t) => t.id === id);
  const delay = (ms) => new Promise((r) => setTimeout(r, ms));

  const api = {
    mode: 'mock',

    auth: {
      status: async () => auth,
      login: async (cookie) => {
        const c = String(cookie || '').trim();
        if (c.length < 4) throw new Error('Cookie 太短，请粘贴完整的凭证串（预览模式下至少 4 个字符）');
        auth = {
          loggedIn: true,
          source: 'manual',
          masked: c.slice(0, 6) + '***' + c.slice(-4),
          message: '已登录',
          profile: {
            nickname: '预览用户',
            avatar: '',
            memberType: 'vip',
            usedBytes: 412316860416,
            totalBytes: 1099511627776,
          },
        };
        emit('auth:changed', auth);
        return auth;
      },
      loginEnv: async () => {
        throw new Error('预览模式没有环境变量，请直接粘贴 Cookie');
      },
      logout: async () => {
        auth = { loggedIn: false, source: '', masked: '', message: '未登录', profile: null };
        emit('auth:changed', auth);
        return true;
      },
      profile: async () => auth.profile || {},
      // 预览模式下没有真实网盘，用一段可预期的等待过程演示交互：
      // 启动 -> 等待中 -> 自动登录成功，以便完整走通界面流程。
      interactiveStart: async () => {
        interactive = { active: true, phase: 'waiting', hint: '预览模式：正在模拟浏览器登录…', url: 'https://pan.quark.cn', collected: 0, remaining: 300 };
        if (interactiveTimer) clearTimeout(interactiveTimer);
        interactiveTimer = setTimeout(() => {
          interactiveTimer = null;
          interactive = { active: false, phase: 'capturing', hint: '已捕获凭证，正在校验…', url: '', collected: 6, remaining: 0 };
        }, 1200);
        if (interactiveTimer2) clearTimeout(interactiveTimer2);
        interactiveTimer2 = setTimeout(() => {
          interactiveTimer2 = null;
          interactive = { active: false, phase: 'success', hint: '预览模式：已模拟登录成功', url: '', collected: 6, remaining: 0 };
          auth = {
            loggedIn: true,
            source: 'interactive',
            masked: 'mock***ookie',
            message: '已登录',
            profile: {
              nickname: '预览用户',
              avatar: '',
              memberType: 'vip',
              usedBytes: 412316860416,
              totalBytes: 1099511627776,
            },
          };
          emit('auth:changed', auth);
        }, 2200);
        return interactive;
      },
      interactiveStatus: async () => interactive,
      interactiveCancel: async () => {
        if (interactiveTimer) {
          clearTimeout(interactiveTimer);
          interactiveTimer = null;
        }
        interactive = { active: false, phase: 'cancelled', hint: '已取消登录', url: '' };
        return true;
      },
      interactiveReopen: async () => true,
    },

    files: {
      list: async (path) => {
        await delay(160);
        const key = normalizePath(path);
        const items = db[key] || db[keyByFid(path)] || [];
        return { path: key, items: items.slice(), reachedCap: false };
      },
      search: async (kw, root, recursive) => {
        await delay(220);
        const needle = String(kw || '').toLowerCase();
        if (!needle) return { path: root, items: [], reachedCap: false };
        const start = normalizePath(root);
        const hits = [];
        if (recursive) {
          for (const [dir, list] of Object.entries(db)) {
            if (dir !== start && !dir.startsWith(start + '/') && start !== '/') continue;
            for (const it of list) {
              if (it.name.toLowerCase().includes(needle)) hits.push(it);
              if (hits.length >= 300) break;
            }
          }
        } else {
          for (const it of db[start] || []) {
            if (it.name.toLowerCase().includes(needle)) hits.push(it);
          }
        }
        return { path: start, items: hits, reachedCap: hits.length >= 300 };
      },
      mkdir: async (parent, name) => {
        await delay(180);
        const key = normalizePath(parent);
        if (!db[key]) throw new Error('父目录不存在：' + key);
        const created = item(key, {
          fid: 'new-' + ++seq,
          name: String(name || '').trim(),
          isDir: true,
          mtime: nowSec(),
        });
        db[key].unshift(created);
        if (!db[created.path]) db[created.path] = [];
        reindex();
        return created;
      },
      rename: async (path, newName) => {
        await delay(160);
        const target = fidIndex.get(path) || findByPath(path);
        if (!target) throw new Error('文件不存在：' + path);
        target.name = String(newName || '').trim();
        return true;
      },
      remove: async (paths) => {
        await delay(200);
        let n = 0;
        for (const p of paths) {
          const target = findByPath(p);
          if (!target) continue;
          const parent = parentOf(p);
          if (db[parent]) db[parent] = db[parent].filter((x) => x.fid !== target.fid);
          if (target.isDir) delete db[p];
          n++;
        }
        reindex();
        return n;
      },
      move: async (src, destDir) => {
        await delay(200);
        const target = findByPath(src);
        if (!target) throw new Error('文件不存在：' + src);
        if (parentOf(src) === normalizePath(destDir)) throw new Error('源与目标目录相同');
        return true;
      },
      copy: async (src, destDir) => {
        await delay(200);
        const target = findByPath(src);
        if (!target) throw new Error('文件不存在：' + src);
        return true;
      },
      resolveFid: async (path) => {
        const target = findByPath(path);
        return target ? target.fid : '0';
      },
    },

    transfer: {
      pickFiles: async () => {
        await delay(120);
        return ['C:/Users/demo/Desktop/演示文件.txt', 'C:/Users/demo/Desktop/截图 2026-10-02.png'];
      },
      pickDir: async () => {
        await delay(120);
        return settings.downloadDir || 'C:/Users/demo/Downloads/QuarkDrive';
      },
      upload: async (paths, dir) => {
        const created = [];
        for (const p of paths) {
          const name = String(p).split(/[\\/]/).pop() || '未命名文件';
          const t = {
            id: 'up-mock-' + ++seq,
            kind: 'upload',
            name,
            localPath: p,
            remotePath: (normalizePath(dir) === '/' ? '' : normalizePath(dir)) + '/' + name,
            size: Math.round((6 + Math.random() * 60) * MB),
            done: 0,
            status: 'pending',
            speed: 0,
            error: '',
            createdAt: nowSec(),
            finishedAt: 0,
          };
          tasks.push(t);
          created.push(dto(t));
        }
        return created;
      },
      download: async (items, dir, keepTree) => {
        const created = [];
        const base = String(dir || settings.downloadDir || '');
        for (const it of items) {
          const t = {
            id: 'dl-mock-' + ++seq,
            kind: 'download',
            name: it.name || it.fid,
            localPath: base,
            dest: base + '/' + (it.name || it.fid),
            remotePath: it.remotePath || '/',
            size: it.size || Math.round((2 + Math.random() * 40) * MB),
            done: 0,
            status: 'pending',
            speed: 0,
            error: '',
            engine: '内建下载器（多线程分片）',
            createdAt: nowSec(),
            finishedAt: 0,
          };
          tasks.push(t);
          created.push(dto(t));
        }
        return created;
      },
      list: async () => tasks.map(dto),
      pause: async (id) => {
        const t = findTask(id);
        if (!t) throw new Error('任务不存在');
        if (t.status === 'running' || t.status === 'pending') {
          t.status = 'paused';
          t.speed = 0;
          emitTask(t);
        }
        return true;
      },
      resume: async (id) => {
        const t = findTask(id);
        if (!t) throw new Error('任务不存在');
        if (t.status === 'paused') {
          t.status = 'running';
          t.speed = 4 * MB;
          emitTask(t);
        }
        return true;
      },
      cancel: async (id) => {
        const t = findTask(id);
        if (!t) throw new Error('任务不存在');
        if (t.status !== 'completed') {
          t.status = 'cancelled';
          t.speed = 0;
          t.finishedAt = nowSec();
          emitTask(t);
        }
        return true;
      },
      retry: async (id) => {
        const t = findTask(id);
        if (!t) throw new Error('任务不存在');
        if (t.status !== 'failed' && t.status !== 'cancelled') throw new Error('只有失败或已取消的任务可以重试');
        t.status = 'running';
        t.done = 0;
        t.error = '';
        t.speed = 3 * MB;
        t.finishedAt = 0;
        emitTask(t);
        return true;
      },
      pauseAll: async () => {
        let n = 0;
        for (const t of tasks) {
          if (t.status === 'running' || t.status === 'pending') {
            t.status = 'paused';
            t.speed = 0;
            emitTask(t);
            n++;
          }
        }
        return n;
      },
      resumeAll: async () => {
        let n = 0;
        for (const t of tasks) {
          if (t.status === 'paused') {
            t.status = 'running';
            t.speed = 4 * MB;
            emitTask(t);
            n++;
          }
        }
        return n;
      },
      clearCompleted: async () => {
        const before = tasks.length;
        for (let i = tasks.length - 1; i >= 0; i--) {
          const s = tasks[i].status;
          if (s === 'completed' || s === 'failed' || s === 'cancelled' || s === 'handedoff') tasks.splice(i, 1);
        }
        return before - tasks.length;
      },
      openDest: async () => true,
    },

    share: {
      parse: async (text) => {
        await delay(500);
        const raw = String(text || '');
        const m = raw.match(/\/s\/([A-Za-z0-9]+)/);
        if (!m) throw new Error('未能识别分享链接，请粘贴形如 https://pan.quark.cn/s/xxxx 的链接');
        const code = (raw.match(/提取码[:：]\s*([A-Za-z0-9]{4})/) || [])[1] || '';
        return {
          pwdId: m[1],
          passcode: code,
          stoken: 'mock-stoken-' + m[1],
          title: '分享内容（预览数据）',
          items: [
            { fid: 's-1', name: '公开课合集', size: 0, isDir: true, token: 'tok-1' },
            { fid: 's-2', name: '第 01 讲.mp4', size: 524288000, isDir: false, token: 'tok-2' },
            { fid: 's-3', name: '第 02 讲.mp4', size: 498073600, isDir: false, token: 'tok-3' },
            { fid: 's-4', name: '课件.pdf', size: 3145728, isDir: false, token: 'tok-4' },
            { fid: 's-5', name: '补充资料.zip', size: 146800640, isDir: false, token: 'tok-5' },
          ],
        };
      },
      save: async (req) => {
        await delay(900);
        if (!req || !req.pwdId) throw new Error('分享信息不完整，请重新解析');
        return { ok: true, message: '转存任务已提交（预览模式未真正写入网盘）', taskId: 'mock-task-1' };
      },
      create: async (path, days) => {
        await delay(500);
        if (!path) throw new Error('请先选择要分享的文件');
        return {
          url: 'https://pan.quark.cn/s/mock0' + (++seq % 9) + 'demo',
          passcode: '7f3a',
          pwdId: 'mock0' + (seq % 9) + 'demo',
          expiresAt: days === 0 ? 0 : nowSec() + days * DAY,
        };
      },
      myShares: async () => {
        await delay(300);
        return [
          {
            shareId: 'sh-1', title: '公开课合集', url: 'https://pan.quark.cn/s/mock1demo',
            passcode: '7f3a', viewCnt: 128, saveCnt: 34, createdAt: nowSec() - 3 * DAY, expired: false,
          },
          {
            shareId: 'sh-2', title: '产品宣传片.mp4', url: 'https://pan.quark.cn/s/mock2demo',
            passcode: '', viewCnt: 12, saveCnt: 3, createdAt: nowSec() - 40 * DAY, expired: true,
          },
        ];
      },
      remove: async (ids) => {
        await delay(300);
        if (!ids || !ids.length) throw new Error('请先选择要取消的分享');
        return true;
      },
    },

    settings: {
      get: async () => ({ ...settings }),
      save: async (s) => {
        settings = { ...settings, ...s };
        try {
          localStorage.setItem(LS_KEY, JSON.stringify(settings));
        } catch (err) {
          /* 忽略：预览模式下写不进去也不影响界面回显 */
        }
        return { ...settings };
      },
      dir: async () => '(预览模式) 配置保存在浏览器 localStorage',
      ensureDir: async (d) => d || 'C:/Users/demo/Downloads/QuarkDrive',
      downloaders: async () => MOCK_DOWNLOADERS.map((d) => ({ ...d })),
      pickExec: async () => 'C:/Program Files/aria2/aria2c.exe',
      openDir: async () => true,
    },

    on(event, cb) {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event).add(cb);
      return () => {
        const set = listeners.get(event);
        if (set) set.delete(cb);
      };
    },

    /** 预览模式专属：释放内部定时器（真实模式无需调用）。 */
    destroy() {
      clearInterval(timer);
    },
  };

  // mock 内部工具
  function findByPath(p) {
    const key = normalizePath(p);
    for (const list of Object.values(db)) {
      for (const it of list) {
        if (it.path === key) return it;
      }
    }
    return null;
  }

  function keyByFid(p) {
    const target = fidIndex.get(p);
    return target ? target.path : '';
  }

  return api;
}

function normalizePath(p) {
  const s = String(p || '/').trim();
  if (!s || s === '/') return '/';
  if (!s.startsWith('/')) return '/' + s;
  return s.replace(/\/+$/, '') || '/';
}

function parentOf(p) {
  const s = normalizePath(p);
  if (s === '/') return '/';
  const i = s.lastIndexOf('/');
  return i <= 0 ? '/' : s.slice(0, i);
}

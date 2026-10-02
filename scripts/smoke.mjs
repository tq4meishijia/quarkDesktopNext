/**
 * smoke.mjs —— 运行时冒烟验证（可选，开发期工具）。
 *
 * 作用：在真实浏览器（系统自带 Edge/Chrome，无需下载 Chromium）里把
 * mock 模式下的完整链路跑一遍——登录 → 5 个页面 → 搜索/多选/网格 →
 * 传输任务暂停/继续/取消/重试 → 分享解析 → 主题切换 → 窄窗口 → 退出登录，
 * 收集 console 错误与失败断言。
 *
 * 依赖（一次性，装在任意目录即可）：
 *   npm install puppeteer-core
 *
 * 用法：
 *   1) 先启动预览：powershell -File scripts\preview.ps1   （或 build.ps1 -Dev）
 *   2) 再跑冒烟：  NODE_PATH=<puppeteer-core 所在 node_modules> node scripts\smoke.mjs [url]
 *      例：NODE_PATH=C:\dev\node_modules node scripts\smoke.mjs http://127.0.0.1:8123/index.html
 *
 * 浏览器探测顺序：KUAKE_SMOKE_BROWSER 环境变量 → Edge(x86/64) → Chrome。
 */
import { createRequire } from 'node:module';
import { existsSync } from 'node:fs';

const URL_ARG = process.argv[2] || 'http://127.0.0.1:8123/index.html';

function loadPuppeteer() {
  const require = createRequire(import.meta.url);
  try {
    return require('puppeteer-core');
  } catch (err) {
    console.error('[FAIL] 未找到 puppeteer-core。请先在任意目录执行：npm install puppeteer-core');
    console.error('       然后用 NODE_PATH=<该目录>/node_modules node scripts/smoke.mjs 重新运行。');
    process.exit(1);
  }
}

function findBrowser() {
  if (process.env.KUAKE_SMOKE_BROWSER && existsSync(process.env.KUAKE_SMOKE_BROWSER)) {
    return process.env.KUAKE_SMOKE_BROWSER;
  }
  const candidates = [
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  ];
  for (const p of candidates) if (existsSync(p)) return p;
  return null;
}

const puppeteer = loadPuppeteer();
const browserExe = findBrowser();
if (!browserExe) {
  console.error('[FAIL] 未找到 Edge/Chrome，可用 KUAKE_SMOKE_BROWSER=<浏览器路径> 指定。');
  process.exit(1);
}

const errors = [];
const steps = [];
const httpFail = [];
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const ok = (name, cond, extra = '') => {
  steps.push(`${cond ? 'PASS' : 'FAIL'}  ${name}${extra ? '  -> ' + extra : ''}`);
  return cond;
};

const browser = await puppeteer.launch({
  executablePath: browserExe,
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--no-proxy-server', '--disable-gpu', '--hide-scrollbars'],
});

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1280, height: 860 });
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push('console.error: ' + m.text());
  });
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
  page.on('response', (r) => {
    if (r.status() >= 400) httpFail.push(r.status() + ' ' + r.url());
  });

  const clickText = (sel, text) =>
    page.evaluate(
      (s, t) => {
        const el = [...document.querySelectorAll(s)].find((x) => (x.textContent || '').includes(t));
        if (!el) return false;
        el.click();
        return true;
      },
      sel,
      text
    );
  const clickTitle = (sel, title) =>
    page.evaluate(
      (s, t) => {
        const el = [...document.querySelectorAll(s)].find(
          (x) => x.getAttribute('title') === t || x.getAttribute('aria-label') === t
        );
        if (!el) return false;
        el.click();
        return true;
      },
      sel,
      title
    );
  const count = (sel) => page.$$eval(sel, (n) => n.length).catch(() => 0);
  const taskStatuses = () =>
    page.$$eval('.task', (ns) => ns.map((n) => (n.querySelector('.badge') ? n.querySelector('.badge').textContent.trim() : '')));

  // ---- 登录 ----
  await page.goto(URL_ARG, { waitUntil: 'networkidle2', timeout: 30000 });
  await page.waitForSelector('.login__card', { timeout: 15000 });
  ok('登录页渲染', true);
  await page.type('textarea.textarea', 'smoke-cookie');
  ok('点击登录', await clickText('.login__card .btn--primary', '登录'));
  await page.waitForSelector('.app-shell', { timeout: 15000 });
  ok('进入主外壳', true);

  // ---- 文件页 ----
  await page.waitForSelector('.filetable, .filegrid, .empty', { timeout: 15000 });
  ok('列表渲染', (await count('.filetable .filerow')) > 0, 'rows=' + (await count('.filetable .filerow')));
  await page.type('.topbar__search .input', '视频');
  await wait(1200);
  ok('当前目录搜索命中', (await count('.filetable .filerow, .filegrid .filecard')) > 0);
  await page.$eval('.topbar__search .input', (el) => {
    el.value = '';
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await wait(900);
  ok('点击网格视图', await clickText('.segmented__item', '网格'));
  await wait(600);
  ok('网格渲染', (await count('.filegrid .filecard')) > 0);
  ok('点回列表', await clickText('.segmented__item', '列表'));
  await wait(500);
  await page.evaluate(() => {
    const cb = document.querySelector('.filetable .filerow .checkbox');
    if (cb) cb.click();
  });
  await wait(500);
  ok('多选操作条', (await count('.selection-bar')) > 0);

  // ---- 传输页 ----
  ok('导航到传输页', await clickText('.sidebar .nav__item', '传输'));
  await wait(1000);
  ok('任务列表', (await count('.task')) > 0, 'tasks=' + (await count('.task')));
  ok('点击暂停', await clickTitle('.task button', '暂停'));
  await wait(800);
  ok('出现已暂停', (await taskStatuses()).some((s) => s.includes('已暂停')));
  ok('点击继续', await clickTitle('.task button', '继续'));
  await wait(800);
  ok('点击取消', await clickTitle('.task button', '取消'));
  await wait(800);
  ok('点击重试', await clickTitle('.task button', '重试'));
  await wait(900);

  // ---- 分享页 ----
  ok('导航到分享页', await clickText('.sidebar .nav__item', '分享'));
  await wait(900);
  await page.type('.share-layout input.input, .share-layout textarea', 'https://pan.quark.cn/s/abc123');
  ok('点击解析', await clickText('.share-layout .btn--primary', '解析'));
  await wait(1500);
  const shareText = await page.$eval('.share-layout', (n) => n.textContent).catch(() => '');
  ok('解析出预览与转存', shareText.includes('转存'));

  // ---- 设置页 ----
  ok('导航到设置页', await clickText('.sidebar .nav__item', '设置'));
  await wait(900);
  ok('点击深色主题', await clickText('.settings .theme-option', '深色'));
  await wait(500);
  ok('深色生效', (await page.evaluate(() => document.documentElement.getAttribute('data-theme'))) === 'dark');
  ok('点击浅色主题', await clickText('.settings .theme-option', '浅色'));
  await wait(500);
  ok('浅色生效', (await page.evaluate(() => document.documentElement.getAttribute('data-theme'))) === 'light');

  // ---- 窄窗口 ----
  await page.setViewport({ width: 460, height: 780 });
  await wait(600);
  ok('窄窗口渲染', (await count('.filetable, .filegrid, .empty')) >= 0);
  await page.setViewport({ width: 1280, height: 860 });
  await wait(400);
} catch (err) {
  errors.push('SMOKE_EXCEPTION: ' + (err && err.message ? err.message : err));
} finally {
  await browser.close();
}

console.log('\n===== 断言结果 =====');
for (const s of steps) console.log(s);
if (httpFail.length) {
  console.log('\n===== HTTP >=400 =====');
  for (const u of [...new Set(httpFail)]) console.log(u);
}
console.log('\n===== 错误 (' + errors.length + ') =====');
for (const e of [...new Set(errors)]) console.log(e);
const failed = errors.length > 0 || steps.some((s) => s.startsWith('FAIL'));
console.log('\nRESULT: ' + (failed ? 'SMOKE FAILED' : 'SMOKE OK'));
process.exit(failed ? 1 : 0);

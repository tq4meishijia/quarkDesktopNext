/**
 * main.js —— 入口。
 *
 * 只做引导与兜底：加载应用外壳，并在启动失败时把错误画到页面上，
 * 而不是让用户面对一个永远空白的窗口。
 */
import { h, mount } from './core/dom.js';
import { startApp } from './app.js';

const root = document.getElementById('app') || document.body;

function fatal(err) {
  console.error('[kuake-desktop] 启动失败', err);
  const message = err && err.message ? err.message : String(err);
  mount(
    root,
    h(
      'div',
      {
        class: 'login',
      },
      h(
        'div',
        { class: 'login__card' },
        h('div', { class: 'login__title', text: '启动失败' }),
        h('div', { class: 'login__desc', text: message }),
        h('div', {
          class: 'field__hint',
          text: '请确认前端资源完整（frontend/ 下含 index.html 与 src/），或重新执行 wails build。',
        })
      )
    )
  );
}

startApp(root).catch(fatal);

/**
 * login.js —— 登录授权页。
 *
 * 夸克网盘没有面向第三方应用的 OAuth 授权入口，凭证沿用 CLI 的做法：
 * 用户在浏览器登录网盘后从开发者工具里复制 Cookie，粘贴到这里。
 * 因此本页的重点是「把怎么拿到 Cookie 讲清楚」，而不是做一个假授权按钮。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bridge, bridgeMode } from '../bridge/index.js';
import { toastError } from '../components/toast.js';

const STEPS = [
  '用浏览器打开 pan.quark.cn 并完成登录',
  '按 F12 打开开发者工具，切到 Network（网络）面板',
  '刷新页面，任选一条 drive-pc.quark.cn 的请求',
  '在请求头里复制 Cookie 整段内容，粘贴到下方输入框',
];

/**
 * @param {object} opts
 * @param {(auth:object)=>void} opts.onSuccess 登录成功回调
 * @returns {{node: HTMLElement}}
 */
export function loginPage({ onSuccess }) {
  const isMock = bridgeMode() === 'mock';

  const input = h('textarea', {
    class: 'textarea input--mono',
    placeholder: '在此粘贴 Cookie，例如 __pus=xxxx; __puus=yyyy;',
    spellcheck: 'false',
  });

  const error = h('div', { class: 'field__error hidden' });

  // 交互式登录的进度条；成功后调用后端会广播 auth:changed，由外壳切换界面。
  const statusBox = h('div', { class: 'login__interactive hidden' });
  let pollTimer = 0;

  const renderStatus = (st) => {
    if (!st) return;
    const waiting = st.phase === 'waiting';
    statusBox.classList.remove('hidden');
    statusBox.innerHTML = '';
    statusBox.appendChild(
      h(
        'div',
        { class: 'login__interactive-row' },
        waiting ? h('span', { class: 'spinner spinner--sm' }) : h('span', { class: 'dot dot--' + (st.phase === 'success' ? 'ok' : 'warn') }),
        h('span', { class: 'login__interactive-text', text: st.hint || '' })
      )
    );
    if (st.url) {
      statusBox.appendChild(
        h('div', { class: 'login__interactive-url', text: st.url })
      );
    }
    if (waiting) {
      statusBox.appendChild(
        h(
          'button',
          { class: 'btn btn--sm', type: 'button', onClick: () => doCancel() },
          h('span', { text: '取消' })
        )
      );
    }
  };

  const submitBtn = h(
    'button',
    { class: 'btn btn--primary btn--block', type: 'button' },
    icon('check', 15),
    h('span', { text: '登录' })
  );

  const envBtn = h(
    'button',
    { class: 'btn btn--block', type: 'button' },
    icon('harddrive', 15),
    h('span', { text: '从环境变量读取' })
  );

  const interactiveBtn = h(
    'button',
    { class: 'btn btn--primary btn--block', type: 'button' },
    icon('monitor', 15),
    h('span', { text: '在浏览器中登录' })
  );

  const setBusy = (busy, text) => {
    submitBtn.disabled = busy;
    envBtn.disabled = busy;
    const label = submitBtn.querySelector('span');
    if (label) label.textContent = text || '登录';
  };

  const showError = (msg) => {
    error.textContent = msg;
    error.classList.remove('hidden');
  };
  const clearError = () => {
    error.textContent = '';
    error.classList.add('hidden');
  };

  const doLogin = async (raw, source) => {
    clearError();
    const value = String(raw || '').trim();
    if (!value && source === 'manual') {
      showError('请先粘贴 Cookie');
      input.focus();
      return;
    }
    setBusy(true, '正在登录…');
    try {
      const auth = source === 'env' ? await bridge().auth.loginEnv() : await bridge().auth.login(value);
      if (onSuccess) onSuccess(auth);
    } catch (err) {
      showError(err && err.message ? err.message : '登录失败');
      toastError(err, '登录失败');
    } finally {
      setBusy(false);
    }
  };

  const beginInteractive = async () => {
    clearError();
    interactiveBtn.disabled = true;
    try {
      const st = await bridge().auth.interactiveStart();
      renderStatus(st);
      if (st.active) {
        pollTimer = setInterval(async () => {
          const cur = await bridge().auth.interactiveStatus();
          renderStatus(cur);
          if (!cur.active) clearInterval(pollTimer);
        }, 900);
      } else {
        interactiveBtn.disabled = false;
      }
    } catch (err) {
      interactiveBtn.disabled = false;
      showError(err && err.message ? err.message : '无法启动登录');
      toastError(err, '无法启动登录');
    }
  };

  const doCancel = async () => {
    clearInterval(pollTimer);
    try {
      await bridge().auth.interactiveCancel();
    } finally {
      interactiveBtn.disabled = false;
    }
    renderStatus({ active: false, phase: 'cancelled', hint: '已取消登录，可重新点击上方按钮。', url: '' });
  };

  submitBtn.addEventListener('click', () => doLogin(input.value, 'manual'));
  envBtn.addEventListener('click', () => doLogin('', 'env'));
  interactiveBtn.addEventListener('click', () => beginInteractive());
  input.addEventListener('keydown', (e) => {
    // Ctrl/Cmd + Enter 提交，避免与换行冲突
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) doLogin(input.value, 'manual');
  });

  const card = h(
    'div',
    { class: 'login__card' },
    h(
      'div',
      { class: 'login__brand' },
      h('div', { class: 'brand__mark', text: 'QK' }),
      h(
        'div',
        { class: 'col' },
        h('div', { class: 'login__title', text: '夸克网盘桌面版' }),
        h('div', { class: 'brand__sub', text: isMock ? '预览模式（mock 数据）' : '已连接本地服务' })
      )
    ),

    h('div', {
      class: 'login__desc',
      text: '本客户端是非官方第三方工具，不提供账号密码登录，也不上传你的凭证到任何服务器：Cookie 只保存在本机配置目录，权限 0600。',
    }),

    h(
      'div',
      { class: 'field' },
      h('label', { class: 'field__label', text: '会话 Cookie' }),
      input,
      error,
      h('div', {
        class: 'field__hint',
        text: isMock
          ? '当前是预览模式，任意填写 4 个字符以上即可进入界面查看效果。'
          : '凭证会写入本机配置文件；也可设置 KUAKE_COOKIE 后用下方按钮直接读取。',
      })
    ),

    h('div', { class: 'col', style: { gap: 'var(--sp-2)' } }, interactiveBtn),
    statusBox,

    h('div', { class: 'login__divider' }, h('span', { text: '或使用 Cookie 手动登录' })),
    h('div', { class: 'col', style: { gap: 'var(--sp-2)' } }, submitBtn, envBtn),

    h(
      'div',
      { class: 'login__steps' },
      h('div', { style: { fontWeight: '600', marginBottom: '2px' }, text: '如何获取 Cookie' }),
      ...STEPS.map((s, i) => h('div', { text: i + 1 + '. ' + s }))
    ),

    h('div', {
      class: 'login__footer',
      text: '使用本工具即表示你已了解相关风险：账号凭证可能失效，接口变更可能导致不可用。',
    })
  );

  return {
    node: h('div', { class: 'login' }, card),
    /** 离开登录页时停掉轮询，避免定时器泄漏。 */
    destroy() {
      clearInterval(pollTimer);
    },
  };
}

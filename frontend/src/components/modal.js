/**
 * modal.js —— 模态框（确认框与输入框）。
 *
 * 只提供两种高频形态：confirm 与 prompt。返回值是 Promise，
 * 调用处用 await 写起来接近原生 confirm/prompt，但样式与主题统一。
 */
import { h } from '../core/dom.js';

function modalRoot() {
  let el = document.getElementById('modal-root');
  if (!el) {
    el = h('div', { id: 'modal-root' });
    document.body.appendChild(el);
  }
  return el;
}

/**
 * 打开一个自定义模态框。
 * @param {{title:string, body:Node, footer?:Node, width?:number}} opts
 * @returns {{close: (result?:any)=>void, node: HTMLElement}}
 */
export function openModal({ title, body, footer, width }) {
  let done = null;

  const mask = h('div', { class: 'modal-mask' });
  const box = h(
    'div',
    { class: 'modal', style: width ? { width: 'min(' + width + 'px, 100%)' } : {} },
    h('div', { class: 'modal__header' }, h('div', { class: 'modal__title', text: title })),
    h('div', { class: 'modal__body' }, body),
    footer ? h('div', { class: 'modal__footer' }, footer) : null
  );
  mask.appendChild(box);

  const close = () => {
    document.removeEventListener('keydown', onKey);
    mask.remove();
    if (done) done();
  };

  function onKey(e) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
    }
  }

  // 点遮罩关闭，点内容区不关闭
  mask.addEventListener('mousedown', (e) => {
    if (e.target === mask) close();
  });
  document.addEventListener('keydown', onKey);

  modalRoot().appendChild(mask);

  return {
    node: box,
    close,
    /** 注册关闭回调，便于 confirm/prompt 在关闭时 resolve。 */
    onClose(fn) {
      done = fn;
    },
  };
}

/**
 * 确认框。
 * @returns {Promise<boolean>} 用户点「确定」为 true，关闭/取消为 false
 */
export function confirmDialog({
  title = '请确认',
  message = '',
  confirmText = '确定',
  cancelText = '取消',
  danger = false,
  hint = '',
} = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      m.close();
      resolve(value);
    };

    const body = h(
      'div',
      { class: 'col', style: { gap: 'var(--sp-2)' } },
      h('div', { class: 'text-2', text: message }),
      hint ? h('div', { class: 'field__hint', text: hint }) : null
    );

    const footer = h(
      'div',
      { class: 'row' },
      h('button', { class: 'btn', onClick: () => finish(false), text: cancelText }),
      h('button', {
        class: 'btn ' + (danger ? 'btn--danger' : 'btn--primary'),
        onClick: () => finish(true),
        text: confirmText,
      })
    );

    const m = openModal({ title, body, footer });
    m.onClose(() => {
      if (!settled) {
        settled = true;
        resolve(false);
      }
    });
  });
}

/**
 * 输入框。
 * @returns {Promise<string|null>} 确定时返回输入值（已 trim），取消返回 null
 */
export function promptDialog({
  title = '请输入',
  label = '',
  value = '',
  placeholder = '',
  confirmText = '确定',
  cancelText = '取消',
  mono = false,
  hint = '',
} = {}) {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (val) => {
      if (settled) return;
      settled = true;
      m.close();
      resolve(val);
    };

    const input = h('input', {
      class: 'input' + (mono ? ' input--mono' : ''),
      value: value || '',
      placeholder: placeholder || '',
    });

    const submit = () => finish(String(input.value || '').trim() || null);

    const body = h(
      'div',
      { class: 'col', style: { gap: 'var(--sp-2)' } },
      label ? h('label', { class: 'field__label', text: label }) : null,
      input,
      hint ? h('div', { class: 'field__hint', text: hint }) : null
    );

    const footer = h(
      'div',
      { class: 'row' },
      h('button', { class: 'btn', onClick: () => finish(null), text: cancelText }),
      h('button', { class: 'btn btn--primary', onClick: submit, text: confirmText })
    );

    const m = openModal({ title, body, footer });
    m.onClose(() => {
      if (!settled) {
        settled = true;
        resolve(null);
      }
    });

    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') submit();
    });
    setTimeout(() => input.focus(), 30);
  });
}

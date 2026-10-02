/**
 * empty.js —— 空数据与加载/错误态的统一呈现。
 *
 * 三种状态共用一个组件，保证「空 / 加载 / 出错」在视觉上永远有明确反馈，
 * 不会出现一片空白让人以为界面卡死。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';

/**
 * @param {object} opts
 * @param {string} opts.icon 图标名
 * @param {string} opts.title 主文案
 * @param {string} [opts.desc] 说明文案
 * @param {Node} [opts.action] 主操作按钮
 */
export function emptyState({ icon: iconName = 'inbox', title, desc, action } = {}) {
  return h(
    'div',
    { class: 'empty' },
    h('div', { class: 'empty__icon' }, icon(iconName, 26)),
    h('div', { class: 'empty__title', text: title || '暂无数据' }),
    desc ? h('div', { class: 'empty__desc', text: desc }) : null,
    action ? h('div', { class: 'empty__action' }, action) : null
  );
}

/** 加载中：中间一个旋转指示器 + 文案。 */
export function loadingState(text = '正在加载…') {
  return h(
    'div',
    { class: 'empty' },
    h('div', { class: 'spinner spinner--lg' }),
    h('div', { class: 'empty__desc', text })
  );
}

/**
 * 加载失败：给出错误原因与一个重试按钮，避免用户只能刷新整个应用。
 */
export function errorState({ message = '加载失败', onRetry } = {}) {
  return emptyState({
    icon: 'alert',
    title: '加载失败',
    desc: message,
    action: onRetry
      ? h(
          'button',
          { class: 'btn', onClick: onRetry },
          icon('refresh', 15),
          h('span', { text: '重试' })
        )
      : null,
  });
}

/** 列表加载骨架，用于文件列表首屏。 */
export function skeletonRows(count = 6) {
  const rows = [];
  for (let i = 0; i < count; i++) {
    rows.push(
      h(
        'div',
        { class: 'filerow' },
        h('div', { class: 'skeleton', style: { width: '16px', height: '16px', borderRadius: '4px' } }),
        h('div', {
          class: 'skeleton',
          style: { width: (40 + ((i * 13) % 45)) + '%', height: '12px' },
        }),
        h('div', { class: 'skeleton', style: { width: '60px', height: '12px' } }),
        h('div', { class: 'skeleton', style: { width: '90px', height: '12px' } }),
        h('div', {})
      )
    );
  }
  return rows;
}

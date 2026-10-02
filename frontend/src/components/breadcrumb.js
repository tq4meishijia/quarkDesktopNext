/**
 * breadcrumb.js —— 网盘路径面包屑。
 *
 * 把 "/文档/合同" 拆成可点击的层级，点击任意一级即可跳回该目录。
 * 路径过长时最后一级完整展示，中间级截断，保证不撑破工具栏。
 */
import { h } from '../core/dom.js';

/**
 * @param {object} opts
 * @param {string} opts.path 当前路径，如 "/文档/合同"
 * @param {(path:string)=>void} opts.onNavigate 点击某级时回调
 */
export function breadcrumb({ path = '/', onNavigate } = {}) {
  const parts = String(path || '/')
    .split('/')
    .filter(Boolean);

  const nodes = [
    crumb('根目录', '/', parts.length === 0, () => onNavigate && onNavigate('/')),
  ];

  let acc = '';
  parts.forEach((name, idx) => {
    acc += '/' + name;
    const target = acc;
    const isLast = idx === parts.length - 1;
    nodes.push(h('span', { class: 'crumbs__sep', text: '/' }));
    nodes.push(crumb(name, target, isLast, () => onNavigate && onNavigate(target)));
  });

  function crumb(label, target, isLast, onClick) {
    return h('button', {
      class: 'crumbs__item',
      type: 'button',
      title: target,
      'aria-current': isLast ? 'true' : 'false',
      onClick: isLast ? null : onClick,
      text: label,
    });
  }

  return h('nav', { class: 'crumbs', 'aria-label': '路径导航' }, nodes);
}

/** 取路径的父目录，供「返回上一级」按钮使用。 */
export function parentPath(path) {
  const parts = String(path || '/').split('/').filter(Boolean);
  if (parts.length <= 1) return '/';
  return '/' + parts.slice(0, -1).join('/');
}

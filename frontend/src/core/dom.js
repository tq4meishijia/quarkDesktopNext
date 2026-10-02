/**
 * dom.js —— 极简 DOM 构建工具（hyperscript）。
 *
 * 之所以自己写而不用框架：本项目要求零构建步骤，前端要以原生 ES Module 直接跑在
 * Wails 的 AssetServer 上。这里只提供两个能力：
 *   - h()：声明式创建元素（含事件、dataset、ref）
 *   - mount()/clear()：整块替换子树
 * 组件层统一用它返回真实 DOM 节点，不做虚拟 DOM diff。
 */

/**
 * 创建元素。
 * @param {string} tag 标签名
 * @param {object|null} props 属性；支持 class/className、text、html、style 对象、
 *        dataset 对象、on* 事件、ref 回调，其余作为普通 attribute（true 写空串）。
 * @param {...any} children 子节点，支持节点、字符串、数字、数组与 null/false。
 */
export function h(tag, props = null, ...children) {
  const node = document.createElement(tag);
  applyProps(node, props);
  append(node, children);
  return node;
}

/** 创建文档片段，用于一次性挂载多个子节点。 */
export function frag(...children) {
  const f = document.createDocumentFragment();
  append(f, children);
  return f;
}

function applyProps(node, props) {
  if (!props) return;
  for (const [key, value] of Object.entries(props)) {
    if (value === null || value === undefined || value === false) continue;

    if (key === 'class' || key === 'className') {
      node.className = Array.isArray(value) ? value.filter(Boolean).join(' ') : String(value);
    } else if (key === 'text') {
      node.textContent = String(value);
    } else if (key === 'html') {
      node.innerHTML = value;
    } else if (key === 'style' && typeof value === 'object') {
      Object.assign(node.style, value);
    } else if (key === 'dataset' && typeof value === 'object') {
      Object.assign(node.dataset, value);
    } else if (key === 'ref' && typeof value === 'function') {
      value(node);
    } else if (key.startsWith('on') && typeof value === 'function') {
      node.addEventListener(key.slice(2).toLowerCase(), value);
    } else {
      node.setAttribute(key, value === true ? '' : String(value));
    }
  }
}

function append(parent, children) {
  for (const child of children) {
    if (child === null || child === undefined || child === false || child === true) continue;
    if (Array.isArray(child)) {
      append(parent, child);
    } else if (child instanceof Node) {
      parent.appendChild(child);
    } else {
      parent.appendChild(document.createTextNode(String(child)));
    }
  }
}

/** 清空节点的所有子节点。 */
export function clear(node) {
  while (node && node.firstChild) node.removeChild(node.firstChild);
  return node;
}

/** 清空后重新挂载，页面切换时用它做整块替换。 */
export function mount(parent, ...children) {
  clear(parent);
  append(parent, children);
  return parent;
}

/** 查询单个元素。 */
export function qs(selector, root = document) {
  return root.querySelector(selector);
}

/** 查询多个元素，返回数组。 */
export function qsa(selector, root = document) {
  return Array.from(root.querySelectorAll(selector));
}

/**
 * fileview.js —— 文件浏览的两种视图（列表 / 网格）。
 *
 * 视图本身不持有数据：条目、选中集合、排序都由页面传入，
 * 组件只负责渲染与把交互（点击区间选择、双击打开、排序）回调出去，
 * 这样切换视图时不用重建选中状态。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bytes, datetime, kindOf } from '../core/format.js';

const KIND_ICON = {
  dir: 'folder',
  image: 'image',
  video: 'video',
  audio: 'audio',
  archive: 'archive',
  doc: 'doc',
  code: 'code',
  file: 'file',
};

/**
 * 文件图标：目录与可识别类型用图形，其余用扩展名文字，
 * 这样任意后缀都不会落成一个毫无信息量的通用图标。
 */
export function fileIcon(item, size = 22) {
  const kind = kindOf(item.name, item.isDir);
  if (kind === 'dir') return icon('folder', size);
  if (KIND_ICON[kind] && kind !== 'file') return icon(KIND_ICON[kind], size);
  const ext = (item.ext || item.name.split('.').pop() || '').slice(0, 4).toUpperCase();
  return h('span', { class: 'fileext', text: ext || '···' });
}

/** 复用的受控复选框。 */
function checkbox(checked, onChange, title) {
  return h('div', {
    class: 'checkbox',
    role: 'checkbox',
    tabindex: '0',
    title: title || '选择',
    'aria-checked': checked ? 'true' : 'false',
    dataset: checked ? { checked: 'true' } : {},
    onClick: (e) => {
      e.stopPropagation();
      onChange();
    },
    onKeydown: (e) => {
      if (e.key === ' ' || e.key === 'Enter') {
        e.preventDefault();
        e.stopPropagation();
        onChange();
      }
    },
  });
}

/**
 * @param {object} opts
 * @param {'list'|'grid'} opts.view
 * @param {Array} opts.items
 * @param {Set<string>} opts.selected 选中的 fid 集合
 * @param {(item:object, index:number, ev:MouseEvent)=>void} opts.onSelect 单击行/卡片
 * @param {(item:object, index:number)=>void} opts.onActivate 双击或点「打开」
 * @param {(fid:string)=>void} opts.onToggle 勾选框
 * @param {()=>void} opts.onToggleAll 表头全选
 * @param {{key:string,dir:'asc'|'desc'}} opts.sort
 * @param {(key:string)=>void} opts.onSort
 * @param {Array<{icon:string,title:string,show?:(item)=>boolean,onClick:(item)=>void}>} opts.actions
 */
export function fileView(opts) {
  return opts.view === 'grid' ? fileGrid(opts) : fileTable(opts);
}

const COLUMNS = [
  { key: 'name', label: '名称', align: 'left' },
  { key: 'size', label: '大小', align: 'left' },
  { key: 'mtime', label: '修改时间', align: 'left' },
];

function sortMark(sort, key) {
  if (sort.key !== key) return '';
  return sort.dir === 'asc' ? ' ↑' : ' ↓';
}

function fileTable({
  items,
  selected,
  onSelect,
  onActivate,
  onToggle,
  onToggleAll,
  sort,
  onSort,
  actions = [],
}) {
  const allSelected = items.length > 0 && items.every((it) => selected.has(it.fid));
  const someSelected = items.some((it) => selected.has(it.fid));

  const head = h(
    'div',
    { class: 'filetable__head' },
    checkbox(allSelected, onToggleAll, '全选 / 取消全选'),
    ...COLUMNS.map((col) =>
      h('span', { onClick: () => onSort && onSort(col.key), text: col.label + sortMark(sort, col.key) })
    ),
    h('span', { text: '' })
  );

  const rows = items.map((item, index) => {
    const isSel = selected.has(item.fid);
    const visibleActions = actions.filter((a) => !a.show || a.show(item));

    return h(
      'div',
      {
        class: 'filerow',
        dataset: { selected: isSel ? 'true' : 'false', fid: item.fid },
        onClick: (e) => onSelect && onSelect(item, index, e),
        onDblclick: () => onActivate && onActivate(item, index),
      },
      checkbox(isSel, () => onToggle && onToggle(item.fid), '选择 ' + item.name),
      h(
        'div',
        { class: 'filerow__name' },
        h(
          'span',
          { class: 'filerow__icon' + (item.isDir ? ' filerow__icon--dir' : '') },
          item.isDir ? icon('folder', 15) : fileIcon(item, 14)
        ),
        h('span', { class: 'filerow__text', title: item.name, text: item.name })
      ),
      h('div', { class: 'filerow__meta', text: item.isDir ? '—' : bytes(item.size) }),
      h('div', { class: 'filerow__meta filerow__meta--time', text: datetime(item.mtime) }),
      h(
        'div',
        { class: 'filerow__actions' },
        ...visibleActions.map((a) =>
          h('button', {
            class: 'btn btn--ghost btn--icon btn--sm',
            type: 'button',
            title: a.title,
            'aria-label': a.title,
            onClick: (e) => {
              e.stopPropagation();
              a.onClick(item);
            },
          }, icon(a.icon, 15))
        )
      )
    );
  });

  return h('div', { class: 'filetable' }, head, h('div', { class: 'filetable__body' }, rows));
}

function fileGrid({ items, selected, onSelect, onActivate, onToggle, actions = [] }) {
  const cards = items.map((item, index) => {
    const isSel = selected.has(item.fid);
    return h(
      'div',
      {
        class: 'filecard',
        dataset: { selected: isSel ? 'true' : 'false', fid: item.fid },
        onClick: (e) => onSelect && onSelect(item, index, e),
        onDblclick: () => onActivate && onActivate(item, index),
      },
      h('div', { class: 'filecard__check' }, checkbox(isSel, () => onToggle && onToggle(item.fid))),
      h(
        'div',
        { class: 'filecard__icon' + (item.isDir ? ' filecard__icon--dir' : '') },
        item.isDir ? icon('folder', 24) : fileIcon(item, 24)
      ),
      h('div', { class: 'filecard__name', title: item.name, text: item.name }),
      h('div', { class: 'filecard__meta', text: item.isDir ? '文件夹' : bytes(item.size) })
    );
  });

  return h('div', { class: 'filegrid' }, cards);
}

/**
 * 计算一次点击后的选中集合。
 * 支持三种交互：普通点击（单选）、Ctrl/Cmd 点击（增减）、Shift 点击（区间）。
 *
 * @returns {{next: Set<string>, anchor: number}}
 */
export function applySelection({ items, selected, index, item, event, anchor }) {
  const next = new Set(selected);
  const fid = item.fid;
  const shift = !!(event && event.shiftKey);
  const multi = !!(event && (event.metaKey || event.ctrlKey));

  // Shift：把锚点到当前项之间的条目全部选中（用于批量勾选）
  if (shift) {
    const from = typeof anchor === 'number' ? anchor : index;
    const lo = Math.min(from, index);
    const hi = Math.max(from, index);
    for (let i = lo; i <= hi; i++) {
      if (items[i]) next.add(items[i].fid);
    }
    return { next, anchor: from };
  }

  // Ctrl / Cmd：切换单项，不影响其他已选项
  if (multi) {
    if (next.has(fid)) next.delete(fid);
    else next.add(fid);
    return { next, anchor: index };
  }

  // 普通点击：单选；再点一次同一项则取消
  if (next.size === 1 && next.has(fid)) {
    next.delete(fid);
    return { next, anchor: index };
  }
  next.clear();
  next.add(fid);
  return { next, anchor: index };
}

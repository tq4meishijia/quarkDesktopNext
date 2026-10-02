/**
 * files.js —— 文件浏览页。
 *
 * 能力：面包屑导航、列表 / 网格双视图、搜索（当前目录或含子目录）、
 * 多选（点击 / Ctrl 增减 / Shift 区间）与批量操作、排序、新建文件夹。
 *
 * 页面自己持有视图状态（路径、选中、排序、视图模式），
 * 数据一律经 bridge() 获取，不直接碰任何网络代码。
 */
import { h, mount } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bridge } from '../bridge/index.js';
import { notify, toastError } from '../components/toast.js';
import { confirmDialog, promptDialog, openModal } from '../components/modal.js';
import { emptyState, errorState, skeletonRows } from '../components/empty.js';
import { breadcrumb, parentPath } from '../components/breadcrumb.js';
import { fileView, applySelection } from '../components/fileview.js';

const VIEW_KEY = 'kuake-desktop.view';

export const meta = { title: '文件' };

export function filesPage(ctx) {
  const api = () => bridge();

  const state = {
    path: '/',
    items: [],
    loading: false,
    error: '',
    view: readView(),
    sort: { key: 'name', dir: 'asc' },
    selected: new Set(),
    anchor: null,
    keyword: '',
    searching: false,
    recursive: false,
    reachedCap: false,
  };

  const page = h('div', { class: 'page' });

  // 输入即搜索的防抖定时器（回车仍然立即触发，清空则回到目录）
  let searchTimer = 0;

  // ---------- 数据 ----------

  async function load(target = state.path) {
    state.path = target || '/';
    state.loading = true;
    state.error = '';
    state.selected = new Set();
    state.anchor = null;
    state.keyword = '';
    state.searching = false;
    render();
    try {
      const res = await api().files.list(state.path);
      state.items = (res && res.items) || [];
    } catch (err) {
      state.items = [];
      state.error = err && err.message ? err.message : '目录加载失败';
    } finally {
      state.loading = false;
      render();
    }
  }

  async function runSearch(keyword, recursive) {
    const kw = String(keyword || '').trim();
    if (!kw) {
      await load(state.path);
      return;
    }
    state.loading = true;
    state.error = '';
    state.searching = true;
    state.keyword = kw;
    state.recursive = recursive;
    state.selected = new Set();
    render();
    try {
      const res = await api().files.search(kw, state.path, recursive);
      state.items = (res && res.items) || [];
      state.reachedCap = !!(res && res.reachedCap);
    } catch (err) {
      state.items = [];
      state.error = err && err.message ? err.message : '搜索失败';
    } finally {
      state.loading = false;
      render();
    }
  }

  // ---------- 视图数据加工 ----------

  function visibleItems() {
    const { key, dir } = state.sort;
    const factor = dir === 'asc' ? 1 : -1;
    return state.items.slice().sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1; // 目录永远置顶
      if (key === 'size') return (a.size - b.size) * factor;
      if (key === 'mtime') return (a.mtime - b.mtime) * factor;
      return String(a.name).toLowerCase().localeCompare(String(b.name).toLowerCase()) * factor;
    });
  }

  function selectedItems() {
    return state.items.filter((it) => state.selected.has(it.fid));
  }

  // ---------- 交互 ----------

  function onSelect(item, index, ev) {
    const res = applySelection({
      items: visibleItems(),
      selected: state.selected,
      index,
      item,
      event: ev,
      anchor: state.anchor,
    });
    state.selected = res.next;
    state.anchor = res.anchor;
    render();
  }

  function onToggle(fid) {
    if (state.selected.has(fid)) state.selected.delete(fid);
    else state.selected.add(fid);
    render();
  }

  function onToggleAll() {
    const items = visibleItems();
    const allSelected = items.length > 0 && items.every((it) => state.selected.has(it.fid));
    state.selected = allSelected ? new Set() : new Set(items.map((it) => it.fid));
    render();
  }

  async function onActivate(item) {
    if (item.isDir) {
      await load(item.path);
    } else {
      await downloadItems([item]);
    }
  }

  function onSort(key) {
    if (state.sort.key === key) {
      state.sort = { key, dir: state.sort.dir === 'asc' ? 'desc' : 'asc' };
    } else {
      state.sort = { key, dir: key === 'name' ? 'asc' : 'desc' };
    }
    render();
  }

  function setView(view) {
    state.view = view;
    try {
      localStorage.setItem(VIEW_KEY, view);
    } catch (err) {
      /* 忽略存储失败 */
    }
    render();
  }

  async function uploadHere() {
    try {
      const paths = await api().transfer.pickFiles();
      if (!paths || !paths.length) return;
      await api().transfer.upload(paths, state.path);
      notify.success('已加入上传队列：' + paths.length + ' 个文件');
      ctx.go('transfer');
    } catch (err) {
      toastError(err, '上传失败');
    }
  }

  async function mkdir() {
    const name = await promptDialog({
      title: '新建文件夹',
      label: '文件夹名称',
      placeholder: '例如：2026 归档',
    });
    if (!name) return;
    try {
      await api().files.mkdir(state.path, name);
      notify.success('已创建：' + name);
      await load(state.path);
    } catch (err) {
      toastError(err, '创建失败');
    }
  }

  async function downloadItems(items) {
    if (!items.length) return;
    try {
      await api().transfer.download(
        items.map((it) => ({ fid: it.fid, name: it.name, size: it.size, remotePath: it.path }))
      );
      notify.success('已加入下载队列：' + items.length + ' 个文件');
      ctx.go('transfer');
    } catch (err) {
      toastError(err, '下载失败');
    }
  }

  async function renameItem(item) {
    const name = await promptDialog({
      title: '重命名',
      label: '新名称',
      value: item.name,
      hint: item.isDir ? '文件夹名称不能包含 /' : '保留原扩展名可避免类型变化',
    });
    if (!name || name === item.name) return;
    try {
      await api().files.rename(item.path, name);
      notify.success('已重命名');
      await load(state.path);
    } catch (err) {
      toastError(err, '重命名失败');
    }
  }

  async function removeItems(items) {
    const ok = await confirmDialog({
      title: '删除',
      message: '确定删除选中的 ' + items.length + ' 项吗？',
      hint: '删除后无法在本客户端内恢复。',
      confirmText: '删除',
      danger: true,
    });
    if (!ok) return;
    try {
      const n = await api().files.remove(items.map((it) => it.path));
      notify.success('已删除 ' + n + ' 项');
      state.selected = new Set();
      await load(state.path);
    } catch (err) {
      toastError(err, '删除失败');
    }
  }

  async function relocateItems(items, kind) {
    const dest = await promptDialog({
      title: kind === 'move' ? '移动到' : '复制到',
      label: '目标目录（以 / 开头，根目录填 /）',
      value: '/',
      mono: true,
    });
    if (!dest) return;
    try {
      for (const it of items) {
        if (kind === 'move') await api().files.move(it.path, dest);
        else await api().files.copy(it.path, dest);
      }
      notify.success(kind === 'move' ? '已移动' : '已复制');
      await load(state.path);
    } catch (err) {
      toastError(err, kind === 'move' ? '移动失败' : '复制失败');
    }
  }

  async function shareItem(item) {
    try {
      const link = await api().share.create(item.path, 7, false);
      showShareResult(link, item.name);
    } catch (err) {
      toastError(err, '创建分享失败');
    }
  }

  function showShareResult(link, name) {
    const box = h('div', { class: 'linkbox grow', text: link.url || '' });
    const copyBtn = h(
      'button',
      {
        class: 'btn',
        type: 'button',
        onClick: async () => {
          try {
            await navigator.clipboard.writeText(link.url || '');
            notify.success('链接已复制');
          } catch (err) {
            notify.warn('复制失败，请手动选中链接');
          }
        },
      },
      icon('copy', 15)
    );

    openModal({
      title: '分享已创建',
      width: 520,
      body: h(
        'div',
        { class: 'col', style: { gap: 'var(--sp-3)' } },
        h('div', { class: 'text-2', text: name }),
        h('div', { class: 'row' }, box, copyBtn),
        link.passcode
          ? h('div', { class: 'field__hint', text: '提取码：' + link.passcode })
          : h('div', { class: 'field__hint', text: '未设置提取码，任何人拿到链接即可访问。' })
      ),
      footer: h('button', {
        class: 'btn btn--primary',
        type: 'button',
        text: '完成',
        onClick: () => {
          const mask = document.querySelector('.modal-mask');
          if (mask) mask.remove();
        },
      }),
    });
  }

  const rowActions = [
    {
      icon: 'download',
      title: '下载',
      show: (item) => !item.isDir,
      onClick: (item) => downloadItems([item]),
    },
    { icon: 'link', title: '创建分享', onClick: (item) => shareItem(item) },
    { icon: 'pencil', title: '重命名', onClick: (item) => renameItem(item) },
    { icon: 'trash', title: '删除', onClick: (item) => removeItems([item]) },
  ];

  // ---------- 渲染 ----------

  function renderHead() {
    const searchInput = h('input', {
      class: 'input',
      placeholder: state.recursive ? '搜索当前目录及子目录…' : '搜索当前目录…',
      value: state.keyword,
      onInput: (e) => {
        const v = e.target.value;
        state.keyword = v;
        clearTimeout(searchTimer);
        searchTimer = setTimeout(() => runSearch(v, state.recursive), 350);
      },
      onKeydown: (e) => {
        if (e.key === 'Enter') {
          clearTimeout(searchTimer);
          runSearch(e.target.value, state.recursive);
        }
        if (e.key === 'Escape') {
          clearTimeout(searchTimer);
          e.target.value = '';
          load(state.path);
        }
      },
    });

    const recursiveToggle = h('button', {
      class: 'btn btn--sm',
      type: 'button',
      title: '是否下钻子目录',
      text: state.recursive ? '含子目录' : '仅当前',
      onClick: () => {
        state.recursive = !state.recursive;
        render();
      },
    });

    return h(
      'div',
      { class: 'page__head' },
      breadcrumb({ path: state.path, onNavigate: (p) => load(p) }),
      h('span', { class: 'grow' }),
      h(
        'div',
        { class: 'topbar__search', style: { maxWidth: '260px' } },
        h('span', { class: 'topbar__search-icon' }, icon('search', 15)),
        searchInput
      ),
      recursiveToggle,
      h(
        'button',
        { class: 'btn btn--icon', type: 'button', title: '刷新', onClick: () => load(state.path) },
        icon('refresh', 16)
      )
    );
  }

  function renderToolbar() {
    const sel = selectedItems();
    const hasDir = sel.some((it) => it.isDir);

    if (sel.length > 0) {
      return h(
        'div',
        { class: 'selection-bar' },
        h('span', { class: 'selection-bar__count', text: '已选 ' + sel.length + ' 项' }),
        h('span', { class: 'grow' }),
        h(
          'button',
          {
            class: 'btn btn--sm',
            type: 'button',
            disabled: hasDir,
            title: hasDir ? '文件夹需要打包后才能下载，暂不支持' : '下载所选文件',
            onClick: () => downloadItems(sel.filter((it) => !it.isDir)),
          },
          icon('download', 15),
          h('span', { text: '下载' })
        ),
        h(
          'button',
          {
            class: 'btn btn--sm',
            type: 'button',
            disabled: sel.length !== 1,
            onClick: () => renameItem(sel[0]),
          },
          icon('pencil', 15),
          h('span', { text: '重命名' })
        ),
        h(
          'button',
          { class: 'btn btn--sm', type: 'button', onClick: () => relocateItems(sel, 'move') },
          icon('move', 15),
          h('span', { text: '移动' })
        ),
        h(
          'button',
          { class: 'btn btn--sm', type: 'button', onClick: () => relocateItems(sel, 'copy') },
          icon('copy', 15),
          h('span', { text: '复制' })
        ),
        h(
          'button',
          {
            class: 'btn btn--sm btn--danger',
            type: 'button',
            onClick: () => removeItems(sel),
          },
          icon('trash', 15),
          h('span', { text: '删除' })
        ),
        h('button', {
          class: 'btn btn--sm btn--ghost',
          type: 'button',
          text: '取消选择',
          onClick: () => {
            state.selected = new Set();
            render();
          },
        })
      );
    }

    return h(
      'div',
      { class: 'toolbar' },
      h(
        'button',
        { class: 'btn btn--primary', type: 'button', onClick: uploadHere },
        icon('upload', 16),
        h('span', { text: '上传' })
      ),
      h(
        'button',
        { class: 'btn', type: 'button', onClick: mkdir },
        icon('folderPlus', 16),
        h('span', { text: '新建文件夹' })
      ),
      h(
        'button',
        {
          class: 'btn',
          type: 'button',
          disabled: state.path === '/',
          title: '返回上一级',
          onClick: () => load(parentPath(state.path)),
        },
        icon('chevronRight', 16),
        h('span', { text: '上一级' })
      ),
      h('span', { class: 'toolbar__spacer' }),
      h(
        'div',
        { class: 'segmented', role: 'tablist', 'aria-label': '视图切换' },
        h(
          'button',
          {
            class: 'segmented__item',
            type: 'button',
            role: 'tab',
            'aria-selected': state.view === 'list' ? 'true' : 'false',
            onClick: () => setView('list'),
          },
          icon('list', 14),
          h('span', { text: '列表' })
        ),
        h(
          'button',
          {
            class: 'segmented__item',
            type: 'button',
            role: 'tab',
            'aria-selected': state.view === 'grid' ? 'true' : 'false',
            onClick: () => setView('grid'),
          },
          icon('grid', 14),
          h('span', { text: '网格' })
        )
      )
    );
  }

  function renderBody() {
    if (state.loading && state.items.length === 0) {
      return h('div', { class: 'filetable' }, h('div', { class: 'filetable__body' }, skeletonRows(7)));
    }
    if (state.error) {
      return errorState({ message: state.error, onRetry: () => load(state.path) });
    }
    if (state.items.length === 0) {
      if (state.searching) {
        return emptyState({
          icon: 'search',
          title: '没有匹配的文件',
          desc: '试试更换关键词' + (state.recursive ? '' : '，或勾选「含子目录」扩大范围') + '。',
          action: h(
            'button',
            { class: 'btn', type: 'button', onClick: () => load(state.path) },
            icon('refresh', 15),
            h('span', { text: '返回目录' })
          ),
        });
      }
      return emptyState({
        icon: 'folder',
        title: '这个文件夹还是空的',
        desc: '把本地文件拖进来上传，或新建一个文件夹开始整理。',
        action: h(
          'button',
          { class: 'btn btn--primary', type: 'button', onClick: uploadHere },
          icon('upload', 15),
          h('span', { text: '上传文件' })
        ),
      });
    }

    const list = fileView({
      view: state.view,
      items: visibleItems(),
      selected: state.selected,
      onSelect,
      onActivate,
      onToggle,
      onToggleAll,
      sort: state.sort,
      onSort,
      actions: rowActions,
    });

    const wrap = h(
      'div',
      { class: 'page__body' },
      list,
      state.reachedCap
        ? h('div', {
            class: 'field__hint',
            style: { paddingTop: 'var(--sp-2)' },
            text: '结果已达上限，可能未展示全部匹配项。',
          })
        : null
    );
    return wrap;
  }

  function render() {
    mount(page, renderHead(), renderToolbar(), renderBody());
  }

  render();
  load('/');

  return {
    node: page,
    /** 进入页面时由外壳调用，用于从其他页面跳转后刷新。 */
    onEnter(params) {
      if (params && params.path) load(params.path);
    },
    /** 离开页面时清掉防抖定时器，避免在已卸载的页面上触发一次搜索。 */
    destroy() {
      clearTimeout(searchTimer);
    },
  };
}

function readView() {
  try {
    const v = localStorage.getItem(VIEW_KEY);
    return v === 'grid' ? 'grid' : 'list';
  } catch (err) {
    return 'list';
  }
}

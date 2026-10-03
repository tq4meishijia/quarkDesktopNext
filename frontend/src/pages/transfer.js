/**
 * transfer.js —— 传输任务页。
 *
 * 任务数据有两个来源：进入页面时拉一次全量（list），之后靠后端事件增量更新。
 * 事件到达时只替换同 id 的那一条，避免整体重排导致进度条闪烁。
 */
import { h, mount } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bridge, EVENTS } from '../bridge/index.js';
import { notify, toastError } from '../components/toast.js';
import { emptyState, loadingState } from '../components/empty.js';
import { taskRow } from '../components/taskrow.js';
import { bytes, speed as fmtSpeed } from '../core/format.js';

export const meta = { title: '传输' };

const FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'active', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'failed', label: '失败 / 取消' },
];

const ACTIVE = new Set(['pending', 'running', 'paused']);

export function transferPage(ctx) {
  const api = () => bridge();
  const state = { tasks: [], filter: 'all', loading: true };

  const page = h('div', { class: 'page' });

  async function load() {
    state.loading = true;
    render();
    try {
      state.tasks = (await api().transfer.list()) || [];
    } catch (err) {
      toastError(err, '任务列表加载失败');
      state.tasks = [];
    } finally {
      state.loading = false;
      render();
    }
  }

  async function act(fn, okMsg) {
    try {
      await fn();
      if (okMsg) notify.success(okMsg);
      await load();
    } catch (err) {
      toastError(err);
    }
  }

  // 增量合并后端推送的单条任务
  const offUpdate = api().on(EVENTS.transferUpdate, (task) => {
    if (!task || !task.id) return;
    const idx = state.tasks.findIndex((t) => t.id === task.id);
    if (idx >= 0) state.tasks[idx] = task;
    else state.tasks.push(task);
    render();
  });
  const offList = api().on(EVENTS.transferList, (list) => {
    if (Array.isArray(list)) {
      state.tasks = list;
      render();
    }
  });

  function matches(task) {
    const s = task.status || 'pending';
    if (state.filter === 'active') return ACTIVE.has(s);
    if (state.filter === 'done') return s === 'completed' || s === 'handedoff';
    if (state.filter === 'failed') return s === 'failed' || s === 'cancelled';
    return true;
  }

  function stats() {
    let active = 0;
    let done = 0;
    let totalSpeed = 0;
    for (const t of state.tasks) {
      const s = t.status || 'pending';
      if (ACTIVE.has(s)) active++;
      if (s === 'completed' || s === 'handedoff') done++;
      if (s === 'running') totalSpeed += t.speed || 0;
    }
    return { active, done, totalSpeed };
  }

  function renderSummary() {
    const s = stats();
    return h(
      'div',
      { class: 'transfer-summary' },
      h(
        'div',
        { class: 'transfer-summary__item' },
        h('span', { class: 'transfer-summary__label', text: '进行中' }),
        h('span', { class: 'transfer-summary__value', text: String(s.active) })
      ),
      h(
        'div',
        { class: 'transfer-summary__item' },
        h('span', { class: 'transfer-summary__label', text: '已完成' }),
        h('span', { class: 'transfer-summary__value', text: String(s.done) })
      ),
      h(
        'div',
        { class: 'transfer-summary__item' },
        h('span', { class: 'transfer-summary__label', text: '总速度' }),
        h('span', { class: 'transfer-summary__value', text: fmtSpeed(s.totalSpeed) })
      ),
      h('span', { class: 'grow' }),
      h(
        'button',
        { class: 'btn btn--sm', type: 'button', onClick: () => act(() => api().transfer.pauseAll(), '已暂停全部任务') },
        icon('pause', 15),
        h('span', { text: '全部暂停' })
      ),
      h(
        'button',
        { class: 'btn btn--sm', type: 'button', onClick: () => act(() => api().transfer.resumeAll(), '已继续全部任务') },
        icon('play', 15),
        h('span', { text: '全部继续' })
      ),
      h(
        'button',
        {
          class: 'btn btn--sm btn--danger',
          type: 'button',
          onClick: () => act(() => api().transfer.clearCompleted(), '已清除已结束的任务'),
        },
        icon('trash', 15),
        h('span', { text: '清除已结束' })
      )
    );
  }

  function renderBody() {
    if (state.loading) return loadingState('正在读取任务…');

    const visible = state.tasks.filter(matches);
    if (visible.length === 0) {
      return emptyState({
        icon: 'transfer',
        title: state.filter === 'all' ? '还没有传输任务' : '该分类下没有任务',
        desc:
          state.filter === 'all'
            ? '在文件页选择文件后点「下载」，或点「上传」选择本地文件，任务会出现在这里。'
            : '换一个筛选条件看看。',
        action:
          state.filter === 'all'
            ? h(
                'button',
                { class: 'btn', type: 'button', onClick: () => ctx.go('files') },
                icon('files', 15),
                h('span', { text: '去文件页' })
              )
            : null,
      });
    }

    return h(
      'div',
      { class: 'tasklist' },
      ...visible.map((task) =>
        taskRow({
          task,
          onPause: (id) => act(() => api().transfer.pause(id)),
          onResume: (id) => act(() => api().transfer.resume(id)),
          onCancel: (id) => act(() => api().transfer.cancel(id), '已取消任务'),
          onRetry: (id) => act(() => api().transfer.retry(id), '已重新入队'),
          onOpen: (id) => act(() => api().transfer.openDest(id)),
        })
      )
    );
  }

  function render() {
    mount(
      page,
      h(
        'div',
        { class: 'page__head' },
        h('div', { class: 'topbar__title', text: '传输任务' }),
        h('span', { class: 'grow' }),
        h(
          'div',
          { class: 'segmented', role: 'tablist', 'aria-label': '任务筛选' },
          ...FILTERS.map((f) =>
            h(
              'button',
              {
                class: 'segmented__item',
                type: 'button',
                role: 'tab',
                'aria-selected': state.filter === f.key ? 'true' : 'false',
                onClick: () => {
                  state.filter = f.key;
                  render();
                },
              },
              h('span', { text: f.label })
            )
          )
        ),
        h(
          'button',
          { class: 'btn btn--icon', type: 'button', title: '刷新', onClick: load },
          icon('refresh', 16)
        )
      ),
      renderSummary(),
      h('div', { class: 'page__body' }, renderBody())
    );
  }

  render();
  load();

  return {
    node: page,
    destroy() {
      if (offUpdate) offUpdate();
      if (offList) offList();
    },
  };
}

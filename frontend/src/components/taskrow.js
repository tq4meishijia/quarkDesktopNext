/**
 * taskrow.js —— 传输任务条目。
 *
 * 一条任务要在一行里同时说清：是什么、到哪了、多快、还要多久、能做什么操作。
 * 因此信息分层：主行（名称 + 状态徽标）→ 进度条 → 副行（速度 / 剩余 / 路径）。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bytes, speed as fmtSpeed, eta, percent } from '../core/format.js';

export const STATUS_TEXT = {
  pending: '等待中',
  running: '进行中',
  paused: '已暂停',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
  handedoff: '已移交下载器',
};

const STATUS_BADGE = {
  pending: '',
  running: 'badge--accent',
  paused: '',
  completed: 'badge--success',
  failed: 'badge--danger',
  cancelled: '',
  handedoff: 'badge--warn',
};

const DOT_CLASS = {
  pending: '',
  running: 'dot--running',
  paused: '',
  completed: 'dot--success',
  failed: 'dot--danger',
  cancelled: '',
  handedoff: 'dot--warn',
};

/**
 * @param {object} opts
 * @param {object} opts.task 任务 DTO
 * @param {(id:string)=>void} opts.onPause
 * @param {(id:string)=>void} opts.onResume
 * @param {(id:string)=>void} opts.onCancel
 * @param {(id:string)=>void} opts.onRetry
 * @param {(id:string)=>void} opts.onOpen 打开落盘目录
 */
export function taskRow({ task, onPause, onResume, onCancel, onRetry, onOpen }) {
  const isUpload = task.kind === 'upload';
  const status = task.status || 'pending';
  const active = status === 'running' || status === 'pending';
  const left = Math.max(0, (task.size || 0) - (task.done || 0));
  // 已移交外部下载器的任务本进程不再跟踪，不提供暂停/取消——给了也是假的。
  const handed = status === 'handedoff';
  const done = status === 'completed' || handed;

const progressClass =
    status === 'completed'
      ? 'progress progress--success'
      : status === 'failed'
        ? 'progress progress--danger'
        : status === 'paused'
          ? 'progress progress--paused'
          : handed
            ? 'progress progress--warn'
            : 'progress';

  const actions = [];
  if (status === 'running' || status === 'pending') {
    actions.push(
      iconBtn('pause', '暂停', () => onPause && onPause(task.id)),
      iconBtn('x', '取消', () => onCancel && onCancel(task.id), true)
    );
  } else if (status === 'paused') {
    actions.push(
      iconBtn('play', '继续', () => onResume && onResume(task.id)),
      iconBtn('x', '取消', () => onCancel && onCancel(task.id), true)
    );
  } else if (status === 'failed' || status === 'cancelled') {
    actions.push(iconBtn('retry', '重试', () => onRetry && onRetry(task.id)));
  }
  if (done && !isUpload && onOpen) {
    actions.push(iconBtn('folder', '打开所在目录', () => onOpen(task.id)));
  }

  return h(
    'div',
    { class: 'task', dataset: { id: task.id, status } },
    h(
      'div',
      { class: 'task__icon ' + (isUpload ? 'task__icon--upload' : 'task__icon--download') },
      icon(isUpload ? 'upload' : 'download', 17)
    ),
    h(
      'div',
      { class: 'task__body' },
      h(
        'div',
        { class: 'task__top' },
        h('div', { class: 'task__name', title: task.name, text: task.name }),
        h(
          'span',
          { class: 'badge ' + (STATUS_BADGE[status] || '') },
          h('span', { class: 'dot ' + (DOT_CLASS[status] || '') }),
          h('span', { text: STATUS_TEXT[status] || status })
        ),
        h('span', { class: 'task__stats', text: percent(task.progress || 0) })
      ),
      h('div', { class: progressClass }, h('div', { class: 'progress__bar', style: { width: (task.progress || 0) + '%' } })),
      h(
        'div',
        { class: 'task__top' },
        h('span', {
          class: 'task__stats',
          text: status === 'running'
            ? bytes(task.done) + ' / ' + bytes(task.size) + ' · ' + fmtSpeed(task.speed) + ' · 剩余 ' + eta(left, task.speed)
            : handed
              ? '已交给 ' + (task.engine || '外部下载器') + '，进度请在对应程序中查看'
              : bytes(task.done) + ' / ' + bytes(task.size) + (task.error ? ' · ' + task.error : ''),
        }),
        h('span', { class: 'grow' }),
        h('span', { class: 'task__stats', text: task.engine || (isUpload ? '上传' : '下载') })
      ),
      task.dest || task.remotePath
        ? h('div', {
            class: 'task__path',
            title: isUpload ? task.localPath : task.dest || task.remotePath,
            text: isUpload
              ? '本地 ' + (task.localPath || '')
              : (task.dest ? '本地 ' + task.dest : '网盘 ' + task.remotePath),
          })
        : null
    ),
    h('div', { class: 'task__actions' }, ...actions)
  );
}

function iconBtn(name, title, onClick, danger = false) {
  return h(
    'button',
    {
      class: 'btn btn--ghost btn--icon btn--sm' + (danger ? ' btn--danger' : ''),
      type: 'button',
      title,
      'aria-label': title,
      onClick,
    },
    icon(name, 15)
  );
}

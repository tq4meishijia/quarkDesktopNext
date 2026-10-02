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
};

const STATUS_BADGE = {
  pending: '',
  running: 'badge--accent',
  paused: '',
  completed: 'badge--success',
  failed: 'badge--danger',
  cancelled: '',
};

const DOT_CLASS = {
  pending: '',
  running: 'dot--running',
  paused: '',
  completed: 'dot--success',
  failed: 'dot--danger',
  cancelled: '',
};

/**
 * @param {object} opts
 * @param {object} opts.task 任务 DTO
 * @param {(id:string)=>void} opts.onPause
 * @param {(id:string)=>void} opts.onResume
 * @param {(id:string)=>void} opts.onCancel
 * @param {(id:string)=>void} opts.onRetry
 */
export function taskRow({ task, onPause, onResume, onCancel, onRetry }) {
  const isUpload = task.kind === 'upload';
  const status = task.status || 'pending';
  const active = status === 'running' || status === 'pending';
  const left = Math.max(0, (task.size || 0) - (task.done || 0));

  const progressClass =
    status === 'completed'
      ? 'progress progress--success'
      : status === 'failed'
      ? 'progress progress--danger'
      : status === 'paused'
      ? 'progress progress--paused'
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
            : bytes(task.done) + ' / ' + bytes(task.size) + (task.error ? ' · ' + task.error : ''),
        }),
        h('span', { class: 'grow' }),
        h('span', { class: 'task__stats', text: isUpload ? '上传' : '下载' })
      ),
      task.remotePath
        ? h('div', {
            class: 'task__path',
            title: isUpload ? task.localPath : task.remotePath,
            text: isUpload ? '本地 ' + (task.localPath || '') : '网盘 ' + task.remotePath,
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

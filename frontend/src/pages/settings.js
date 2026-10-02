/**
 * settings.js —— 设置页。
 *
 * 设置项改动即生效（主题立刻切换、并发数立刻下发给传输内核），不做「保存按钮」，
 * 避免出现改了一堆却忘记保存、界面与实际行为不一致的情况。
 */
import { h, mount } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bridge, bridgeMode } from '../bridge/index.js';
import { notify, toastError } from '../components/toast.js';
import { confirmDialog } from '../components/modal.js';
import { loadingState } from '../components/empty.js';

export const meta = { title: '设置' };

const THEMES = [
  { key: 'light', label: '浅色', icon: 'sun' },
  { key: 'dark', label: '深色', icon: 'moon' },
  { key: 'system', label: '跟随系统', icon: 'monitor' },
];

const POLICIES = [
  { key: 'skip', label: '跳过同名文件（默认）' },
  { key: 'overwrite', label: '覆盖同名文件' },
  { key: 'rsync', label: '仅覆盖大小不同的同名文件' },
];

const DEFAULTS = {
  downloadDir: '',
  concurrency: 3,
  theme: 'system',
  uploadPolicy: 'skip',
  startMinimized: false,
};

export function settingsPage(ctx) {
  const api = () => bridge();
  const state = { settings: null, dir: '', loading: true };

  const page = h('div', { class: 'page' });

  async function load() {
    state.loading = true;
    render();
    try {
      const [s, d] = await Promise.all([api().settings.get(), api().settings.dir()]);
      state.settings = s || { ...DEFAULTS };
      state.dir = d || '';
    } catch (err) {
      state.settings = { ...DEFAULTS };
      toastError(err, '设置加载失败');
    } finally {
      state.loading = false;
      render();
    }
  }

  async function save(patch, okMsg = '已保存') {
    try {
      const next = await api().settings.save({ ...state.settings, ...patch });
      state.settings = next;
      if (patch.theme) ctx.applyTheme(patch.theme);
      notify.success(okMsg);
      render();
    } catch (err) {
      toastError(err, '保存失败');
    }
  }

  // ---------- 行渲染 ----------

  function row(title, desc, control) {
    return h(
      'div',
      { class: 'setting-row' },
      h(
        'div',
        { class: 'setting-row__info' },
        h('div', { class: 'setting-row__title', text: title }),
        h('div', { class: 'setting-row__desc', text: desc })
      ),
      h('div', { class: 'setting-row__control' }, control)
    );
  }

  function downloadDirRow() {
    const input = h('input', {
      class: 'input input--mono',
      value: state.settings.downloadDir || '',
      placeholder: '留空则使用系统下载目录下的 QuarkDrive',
      onInput: (e) => {
        state.settings = { ...state.settings, downloadDir: e.target.value };
      },
      onKeydown: (e) => {
        if (e.key === 'Enter') save({ downloadDir: e.target.value });
      },
      onBlur: (e) => {
        if ((e.target.value || '') !== (state.settings.downloadDir || '')) {
          save({ downloadDir: e.target.value });
        }
      },
    });

    return row(
      '下载路径',
      '下载的文件默认保存到这个目录；留空时使用系统下载目录下的 QuarkDrive 文件夹。',
      h(
        'div',
        { class: 'input-group', style: { width: '100%' } },
        input,
        h(
          'button',
          {
            class: 'btn btn--icon',
            type: 'button',
            title: '选择目录',
            onClick: async () => {
              try {
                const dir = await api().transfer.pickDir();
                if (dir) await save({ downloadDir: dir });
              } catch (err) {
                toastError(err, '打开目录选择失败');
              }
            },
          },
          icon('folder', 16)
        )
      )
    );
  }

  function concurrencyRow() {
    const value = state.settings.concurrency || 3;
    const label = h('span', { class: 'badge badge--accent', text: value + ' 个' });
    const slider = h('input', {
      class: 'slider',
      type: 'range',
      min: '1',
      max: '16',
      value: String(value),
      onInput: (e) => {
        label.textContent = e.target.value + ' 个';
      },
      onChange: (e) => {
        save({ concurrency: Number(e.target.value) });
      },
    });
    return row(
      '同时传输数',
      '同时搬运的文件数量。带宽充足时可适当调高；网络较慢时调低更稳定。',
      h('div', { class: 'col', style: { gap: 'var(--sp-2)', width: '100%' } }, h('div', { class: 'row row--between' }, label, h('span', { class: 'field__hint', text: '1 – 16' })), slider)
    );
  }

  function themeRow() {
    return row(
      '界面主题',
      '选择「跟随系统」后，会随操作系统的亮/暗设置自动切换。',
      h(
        'div',
        { class: 'theme-options' },
        ...THEMES.map((t) => {
          const active = state.settings.theme === t.key;
          return h(
            'button',
            {
              class: 'theme-option',
              type: 'button',
              'aria-pressed': active ? 'true' : 'false',
              onClick: () => save({ theme: t.key }),
            },
            h(
              'span',
              {
                class: 'theme-option__preview',
                style: {
                  background: t.key === 'dark' ? '#171a21' : t.key === 'light' ? '#ffffff' : 'linear-gradient(90deg,#ffffff 50%,#171a21 50%)',
                },
              },
              h('i', { style: { background: t.key === 'dark' ? '#0f1115' : '#f5f6f8' } }),
              h('i', { style: { background: t.key === 'dark' ? '#272c36' : '#e2e6ea', flex: '1', height: 'auto' } })
            ),
            icon(t.icon, 14),
            h('span', { text: t.label })
          );
        })
      )
    );
  }

  function policyRow() {
    const select = h(
      'select',
      {
        class: 'select',
        onChange: (e) => save({ uploadPolicy: e.target.value }),
      },
      ...POLICIES.map((p) =>
        h('option', {
          value: p.key,
          text: p.label,
          selected: state.settings.uploadPolicy === p.key ? 'selected' : null,
        })
      )
    );
    return row('上传时的同名文件策略', '决定目标位置已存在同名文件时的处理方式。', select);
  }

  function aboutRows() {
    const mode = bridgeMode();
    return h(
      'div',
      { class: 'card' },
      h('div', { class: 'card__title', text: '关于' }),
      h(
        'div',
        { class: 'col', style: { gap: 'var(--sp-2)', marginTop: 'var(--sp-3)' } },
        infoLine('运行模式', mode === 'wails' ? '桌面客户端（已连接 Go 适配层）' : '预览模式（mock 数据，未连接后端）'),
        infoLine('配置目录', state.dir || '-'),
        infoLine('能力来源', 'github.com/zhangjingwei/kuake_cli/sdk'),
        h('div', {
          class: 'field__hint',
          text: '本客户端不实现网盘协议：所有网络与文件操作都转发到 kuake_cli 的 SDK，与 CLI 共用同一套能力。',
        }),
        h(
          'button',
          {
            class: 'btn btn--danger',
            type: 'button',
            style: { alignSelf: 'flex-start', marginTop: 'var(--sp-2)' },
            onClick: async () => {
              const ok = await confirmDialog({
                title: '恢复默认设置',
                message: '将把下载路径、并发数、主题与上传策略恢复为默认值。',
                confirmText: '恢复默认',
                danger: true,
              });
              if (!ok) return;
              await save({ ...DEFAULTS }, '已恢复默认设置');
            },
          },
          icon('retry', 15),
          h('span', { text: '恢复默认设置' })
        )
      )
    );
  }

  function infoLine(k, v) {
    return h(
      'div',
      { class: 'row row--between' },
      h('span', { class: 'text-3', text: k }),
      h('span', { class: 'truncate', style: { maxWidth: '70%' }, text: v, title: v })
    );
  }

  function render() {
    if (state.loading || !state.settings) {
      mount(page, h('div', { class: 'page__head' }, h('div', { class: 'topbar__title', text: '设置' })), loadingState('正在读取设置…'));
      return;
    }
    mount(
      page,
      h('div', { class: 'page__head' }, h('div', { class: 'topbar__title', text: '设置' })),
      h(
        'div',
        { class: 'settings' },
        h(
          'div',
          { class: 'card' },
          h('div', { class: 'card__title', text: '传输' }),
          h('div', { style: { marginTop: 'var(--sp-2)' } },
            downloadDirRow(),
            concurrencyRow(),
            policyRow()
          )
        ),
        h(
          'div',
          { class: 'card' },
          h('div', { class: 'card__title', text: '外观' }),
          h('div', { style: { marginTop: 'var(--sp-2)' } }, themeRow())
        ),
        aboutRows()
      )
    );
  }

  render();
  load();

  return { node: page };
}

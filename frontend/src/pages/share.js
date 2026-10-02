/**
 * share.js —— 分享链接解析与转存页。
 *
 * 左侧是「解析 → 选条目 → 选目标目录 → 转存」的主流程，
 * 右侧是「我的分享」列表。两侧互不阻塞：解析失败不影响查看已有分享。
 */
import { h, mount } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bridge } from '../bridge/index.js';
import { notify, toastError } from '../components/toast.js';
import { confirmDialog } from '../components/modal.js';
import { emptyState, loadingState } from '../components/empty.js';
import { bytes, datetime } from '../core/format.js';

export const meta = { title: '分享' };

export function sharePage(ctx) {
  const api = () => bridge();

  const state = {
    text: '',
    parsing: false,
    saving: false,
    preview: null,
    selected: new Set(),
    target: '/',
    result: '',
    myShares: [],
    myLoading: true,
  };

  const page = h('div', { class: 'page' });

  // ---------- 解析 ----------

  async function parse() {
    const raw = String(state.text || '').trim();
    if (!raw) {
      notify.warn('请先粘贴分享链接');
      return;
    }
    state.parsing = true;
    state.result = '';
    render();
    try {
      const preview = await api().share.parse(raw);
      state.preview = preview;
      state.selected = new Set((preview.items || []).map((it) => it.fid));
      if (!preview.items || preview.items.length === 0) {
        notify.warn('链接解析成功，但没有拿到可转存内容');
      }
    } catch (err) {
      state.preview = null;
      toastError(err, '解析失败');
    } finally {
      state.parsing = false;
      render();
    }
  }

  async function save(all) {
    const p = state.preview;
    if (!p) return;
    const picked = (p.items || []).filter((it) => state.selected.has(it.fid));
    if (!all && picked.length === 0) {
      notify.warn('请先勾选要转存的条目');
      return;
    }
    state.saving = true;
    render();
    try {
      let targetFid = '0';
      if (state.target && state.target !== '/') {
        try {
          targetFid = await api().files.resolveFid(state.target);
        } catch (err) {
          notify.warn('目标目录解析失败，将保存到根目录');
          targetFid = '0';
        }
      }
      const res = await api().share.save({
        pwdId: p.pwdId,
        stoken: p.stoken,
        fids: all ? [] : picked.map((it) => it.fid),
        tokens: all ? [] : picked.map((it) => it.token || ''),
        targetFid,
        saveAll: !!all,
      });
      state.result = (res && res.message) || '转存任务已提交';
      notify.success(state.result);
    } catch (err) {
      toastError(err, '转存失败');
    } finally {
      state.saving = false;
      render();
    }
  }

  // ---------- 我的分享 ----------

  async function loadMyShares() {
    state.myLoading = true;
    render();
    try {
      state.myShares = (await api().share.myShares(1, 50)) || [];
    } catch (err) {
      state.myShares = [];
    } finally {
      state.myLoading = false;
      render();
    }
  }

  async function removeShares() {
    const ok = await confirmDialog({
      title: '取消分享',
      message: '取消后该链接将立即失效，确定继续吗？',
      confirmText: '取消分享',
      danger: true,
    });
    if (!ok) return;
    try {
      await api().share.remove(state.myShares.map((s) => s.shareId));
      notify.success('已取消全部分享');
      await loadMyShares();
    } catch (err) {
      toastError(err, '取消分享失败');
    }
  }

  // ---------- 渲染 ----------

  function renderParseCard() {
    const textarea = h('textarea', {
      class: 'textarea input--mono',
      placeholder: '粘贴分享链接，可同时带上「提取码：xxxx」',
      value: state.text,
      onInput: (e) => {
        state.text = e.target.value;
      },
      onKeydown: (e) => {
        if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) parse();
      },
    });

    return h(
      'div',
      { class: 'card' },
      h(
        'div',
        { class: 'row row--between', style: { marginBottom: 'var(--sp-3)' } },
        h('div', { class: 'card__title', text: '解析分享链接' }),
        h('span', { class: 'badge', text: 'Ctrl + Enter 解析' })
      ),
      h(
        'div',
        { class: 'col', style: { gap: 'var(--sp-3)' } },
        textarea,
        h(
          'div',
          { class: 'row' },
          h(
            'button',
            { class: 'btn btn--primary', type: 'button', disabled: state.parsing, onClick: parse },
            state.parsing ? h('span', { class: 'spinner' }) : icon('search', 15),
            h('span', { text: state.parsing ? '解析中…' : '解析' })
          ),
          h('button', {
            class: 'btn btn--ghost',
            type: 'button',
            text: '清空',
            onClick: () => {
              state.text = '';
              state.preview = null;
              state.result = '';
              render();
            },
          })
        )
      )
    );
  }

  function renderPreviewCard() {
    const p = state.preview;
    if (!p) return null;

    const items = p.items || [];
    const allChecked = items.length > 0 && items.every((it) => state.selected.has(it.fid));

    return h(
      'div',
      { class: 'card', style: { display: 'flex', flexDirection: 'column', minHeight: '0', flex: '1' } },
      h(
        'div',
        { class: 'row row--between', style: { marginBottom: 'var(--sp-2)' } },
        h('div', { class: 'col' },
          h('div', { class: 'card__title', text: p.title || '分享内容' }),
          h('div', { class: 'card__desc', text: '共 ' + items.length + ' 项' + (p.passcode ? ' · 提取码 ' + p.passcode : '') })
        ),
        h(
          'button',
          {
            class: 'btn btn--sm',
            type: 'button',
            onClick: () => {
              state.selected = allChecked ? new Set() : new Set(items.map((it) => it.fid));
              render();
            },
          },
          h('span', { text: allChecked ? '取消全选' : '全选' })
        )
      ),

      h(
        'div',
        { class: 'share-list' },
        ...items.map((it) => {
          const checked = state.selected.has(it.fid);
          return h(
            'div',
            {
              class: 'share-item',
              dataset: { selected: checked ? 'true' : 'false' },
              onClick: () => {
                if (checked) state.selected.delete(it.fid);
                else state.selected.add(it.fid);
                render();
              },
            },
            h('div', { class: 'checkbox', dataset: checked ? { checked: 'true' } : {} }),
            h('div', { class: 'filerow__icon' + (it.isDir ? ' filerow__icon--dir' : '') }, icon(it.isDir ? 'folder' : 'file', 14)),
            h('div', { class: 'grow truncate', text: it.name }),
            h('span', { class: 'task__stats', text: it.isDir ? '—' : bytes(it.size) })
          );
        })
      ),

      h('hr', { class: 'divider', style: { margin: 'var(--sp-3) 0' } }),

      h(
        'div',
        { class: 'field' },
        h('label', { class: 'field__label', text: '保存到' }),
        h(
          'div',
          { class: 'input-group' },
          h('input', {
            class: 'input input--mono',
            value: state.target,
            placeholder: '/',
            onInput: (e) => {
              state.target = e.target.value || '/';
            },
          }),
          h('button', {
            class: 'btn',
            type: 'button',
            text: '根目录',
            onClick: () => {
              state.target = '/';
              render();
            },
          })
        ),
        h('div', { class: 'field__hint', text: '填写网盘目录路径，留空 / 表示保存到根目录。' })
      ),

      h(
        'div',
        { class: 'row', style: { marginTop: 'var(--sp-3)' } },
        h(
          'button',
          { class: 'btn btn--primary', type: 'button', disabled: state.saving, onClick: () => save(false) },
          state.saving ? h('span', { class: 'spinner' }) : icon('inbox', 15),
          h('span', { text: '转存所选' })
        ),
        h(
          'button',
          { class: 'btn', type: 'button', disabled: state.saving, onClick: () => save(true) },
          icon('download', 15),
          h('span', { text: '整包转存' })
        )
      ),

      state.result ? h('div', { class: 'share-result', style: { marginTop: 'var(--sp-3)' } }, icon('check', 15), h('span', { text: state.result })) : null,

      h('div', {
        class: 'field__hint',
        style: { marginTop: 'var(--sp-3)' },
        text: '提示：转存是服务端异步任务，提交后请稍等片刻再到文件页刷新查看。',
      })
    );
  }

  function renderMyShares() {
    return h(
      'div',
      { class: 'card', style: { display: 'flex', flexDirection: 'column', minHeight: '0', flex: '1' } },
      h(
        'div',
        { class: 'row row--between', style: { marginBottom: 'var(--sp-2)' } },
        h('div', { class: 'card__title', text: '我的分享' }),
        state.myShares.length
          ? h(
              'button',
              { class: 'btn btn--sm btn--danger', type: 'button', onClick: removeShares },
              icon('trash', 15),
              h('span', { text: '全部取消' })
            )
          : null
      ),
      state.myLoading
        ? loadingState('正在读取分享列表…')
        : state.myShares.length === 0
        ? emptyState({ icon: 'link', title: '还没有创建过分享', desc: '在文件页选中文件后点「创建分享」即可生成链接。' })
        : h(
            'div',
            { class: 'share-list' },
            ...state.myShares.map((s) =>
              h(
                'div',
                { class: 'share-item' },
                h('div', { class: 'filerow__icon filerow__icon--dir' }, icon('link', 14)),
                h(
                  'div',
                  { class: 'col grow' },
                  h('div', { class: 'truncate', text: s.title || '(未命名)' }),
                  h('div', {
                    class: 'task__stats',
                    text: '浏览 ' + (s.viewCnt || 0) + ' · 转存 ' + (s.saveCnt || 0) + ' · ' + datetime(s.createdAt),
                  })
                ),
                s.expired ? h('span', { class: 'badge badge--danger', text: '已失效' }) : s.passcode ? h('span', { class: 'badge', text: '提取码 ' + s.passcode }) : null
              )
            )
          )
    );
  }

  function render() {
    mount(
      page,
      h(
        'div',
        { class: 'page__head' },
        h('div', { class: 'topbar__title', text: '分享与转存' }),
        h('span', { class: 'grow' }),
        h(
          'button',
          { class: 'btn', type: 'button', onClick: () => ctx.go('files') },
          icon('files', 15),
          h('span', { text: '去文件页创建分享' })
        )
      ),
      h(
        'div',
        { class: 'share-layout' },
        h(
          'div',
          { class: 'share-col' },
          renderParseCard(),
          state.parsing ? loadingState('正在解析分享链接…') : renderPreviewCard()
        ),
        h('div', { class: 'share-col' }, renderMyShares())
      )
    );
  }

  render();
  loadMyShares();

  return { node: page };
}

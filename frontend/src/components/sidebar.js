/**
 * sidebar.js —— 左侧导航与账号区。
 *
 * 导航项与路由名一一对应；传输项上带一个进行中任务数的角标，
 * 让用户在其他页面也能感知后台传输。
 */
import { h } from '../core/dom.js';
import { icon } from '../core/icons.js';
import { bytes } from '../core/format.js';

export const NAV_ITEMS = [
  { name: 'files', label: '文件', icon: 'files' },
  { name: 'transfer', label: '传输', icon: 'transfer' },
  { name: 'share', label: '分享', icon: 'link' },
  { name: 'settings', label: '设置', icon: 'settings' },
];

/**
 * @param {object} opts
 * @param {string} opts.active 当前路由名
 * @param {(name:string)=>void} opts.onNavigate
 * @param {object} opts.auth 登录态
 * @param {number} opts.activeTaskCount 进行中的任务数
 * @param {()=>void} opts.onLogout
 */
export function sidebar({ active, onNavigate, auth, activeTaskCount = 0, onLogout }) {
  const profile = (auth && auth.profile) || null;

  return h(
    'aside',
    { class: 'sidebar' },
    h(
      'div',
      { class: 'brand' },
      h('div', { class: 'brand__mark', text: 'QK' }),
      h(
        'div',
        { class: 'col' },
        h('div', { class: 'brand__name', text: '夸克网盘' }),
        h('div', { class: 'brand__sub', text: '桌面版' })
      )
    ),

    h(
      'nav',
      { class: 'nav', 'aria-label': '主导航' },
      ...NAV_ITEMS.map((it) =>
        h(
          'button',
          {
            class: 'nav__item',
            type: 'button',
            'aria-current': active === it.name ? 'page' : 'false',
            onClick: () => onNavigate && onNavigate(it.name),
          },
          icon(it.icon, 18),
          h('span', { class: 'nav__label', text: it.label }),
          it.name === 'transfer' && activeTaskCount > 0
            ? h('span', { class: 'nav__count', text: String(activeTaskCount) })
            : null
        )
      )
    ),

    profile && profile.totalBytes > 0 ? quotaCard(profile) : null,

    h(
      'button',
      {
        class: 'user-chip',
        type: 'button',
        title: '退出登录',
        onClick: () => onLogout && onLogout(),
      },
      h(
        'div',
        { class: 'user-chip__avatar' },
        profile && profile.avatar
          ? h('img', { src: profile.avatar, alt: '' })
          : h('span', { text: (profile && profile.nickname ? profile.nickname : '?').slice(0, 1) })
      ),
      h(
        'div',
        { class: 'col grow' },
        h('div', { class: 'user-chip__name truncate', text: (profile && profile.nickname) || '未登录' }),
        h('div', { class: 'brand__sub', text: auth && auth.masked ? auth.masked : '点击退出' })
      ),
      icon('logout', 15)
    )
  );
}

function quotaCard(profile) {
  const used = profile.usedBytes || 0;
  const total = profile.totalBytes || 0;
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0;

  return h(
    'div',
    { class: 'quota' },
    h(
      'div',
      { class: 'quota__head' },
      h('span', { text: '存储空间' }),
      h('span', {
        class: 'quota__num',
        text: Math.round(pct) + '%',
      })
    ),
    h('div', { class: 'progress' }, h('div', { class: 'progress__bar', style: { width: pct + '%' } })),
    h('div', {
      class: 'quota__head',
    }, h('span', { class: 'text-3', text: bytes(used) + ' / ' + bytes(total) }))
  );
}

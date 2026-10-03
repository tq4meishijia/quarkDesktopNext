/**
 * icons.js —— 内联 SVG 图标集。
 *
 * 统一 24×24 视窗、描边式（stroke: currentColor），因此图标颜色自动跟随文字色，
 * 亮/暗主题无需各存一套。图标本体是路径数组，一个图标可含多段 path。
 */

const NS = 'http://www.w3.org/2000/svg';

const ICONS = {
  folder: ['M3 7.5A2.5 2.5 0 0 1 5.5 5h3.2l1.6 2h8.2A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9z'],
  folderPlus: [
    'M3 7.5A2.5 2.5 0 0 1 5.5 5h3.2l1.6 2h8.2A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9z',
    'M12 10.5v5',
    'M9.5 13h5',
  ],
  file: ['M13.5 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8.5L13.5 3z', 'M13.5 3v5.5H19'],
  image: ['M4 5h16v14H4z', 'M4 16.5l4.5-4.5 3.5 3.5 3-3L20 16.5'],
  video: ['M3 6.5h12v11H3z', 'M15 10.5l6-3.5v11L15 14.5'],
  audio: ['M4 14v-4h4l4-3.5v11L8 14H4z', 'M16.5 9.8a3.8 3.8 0 0 1 0 4.4'],
  archive: ['M3 5h18v4H3z', 'M5 9v10h14V9', 'M10 13h4'],
  doc: ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z', 'M14 3v5h5', 'M9 13h6', 'M9 16.5h4'],
  code: ['M9 8l-4 4 4 4', 'M15 8l4 4-4 4'],

  files: ['M3 8V5.5A1.5 1.5 0 0 1 4.5 4h4L10 5.5h6.5A1.5 1.5 0 0 1 18 7v1', 'M3 8h14.5A1.5 1.5 0 0 1 19 9.5v8A1.5 1.5 0 0 1 17.5 19h-13A1.5 1.5 0 0 1 3 17.5V8z'],
  transfer: ['M4 8h13', 'M13.5 5l3.5 3-3.5 3', 'M20 16H7', 'M10.5 13L7 16l3.5 3'],
  link: ['M10 13.6a4 4 0 0 0 5.6 0l2.6-2.6a4 4 0 0 0-5.6-5.6L11.5 6.5', 'M14 10.4a4 4 0 0 0-5.6 0l-2.6 2.6a4 4 0 0 0 5.6 5.6l1.1-1.1'],
  settings: ['M4 8h9', 'M17 8h3', 'M4 16h4', 'M12 16h8', 'M15.5 6v4', 'M7.5 14v4'],

  upload: ['M12 16V4', 'M7.5 8.5L12 4l4.5 4.5', 'M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3'],
  download: ['M12 4v12', 'M7.5 11.5L12 16l4.5-4.5', 'M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3'],
  refresh: ['M20 12a8 8 0 1 1-2.4-5.7', 'M20 4.5v4h-4'],
  retry: ['M3.5 12a8.5 8.5 0 1 1 2.6 6.1', 'M3.5 19.5v-4h4'],

  grid: ['M4 4h6.5v6.5H4z', 'M13.5 4H20v6.5h-6.5z', 'M4 13.5h6.5V20H4z', 'M13.5 13.5H20V20h-6.5z'],
  list: ['M4 6.5h16', 'M4 12h16', 'M4 17.5h16'],
  search: ['M10.5 4a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13z', 'M15.4 15.4L20 20'],

  trash: ['M4 7h16', 'M9.5 7V4.5h5V7', 'M6 7l1 13h10l1-13'],
  pencil: ['M4.5 19.5h4L19 9l-4-4L4.5 15.5v4z'],
  plus: ['M12 5.5v13', 'M5.5 12h13'],
  copy: ['M9 9h11v11H9z', 'M15.5 9V4H4v11h5'],
  move: ['M5 12h14', 'M13.5 6.5l6 5.5-6 5.5'],

  play: ['M8 5.5l11 6.5-11 6.5v-13z'],
  pause: ['M9.5 5.5v13', 'M14.5 5.5v13'],
  x: ['M6.5 6.5l11 11', 'M17.5 6.5l-11 11'],
  check: ['M5 12.5l4.5 4.5L19 7'],
  chevronRight: ['M9.5 5l7 7-7 7'],
  chevronDown: ['M5 9.5l7 7 7-7'],

  sun: ['M12 16.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z', 'M12 2v2', 'M12 20v2', 'M4.4 4.4l1.4 1.4', 'M18.2 18.2l1.4 1.4', 'M2 12h2', 'M20 12h2', 'M4.4 19.6l1.4-1.4', 'M18.2 5.8l1.4-1.4'],
  moon: ['M20 14.3A8.5 8.5 0 0 1 9.7 4 8.5 8.5 0 1 0 20 14.3z'],
  monitor: ['M3 5h18v10.5H3z', 'M9 19.5h6', 'M12 15.5v4'],

  logout: ['M15 5H6a1 1 0 0 0-1 1v12a1 1 0 0 0 1 1h9', 'M13 12h8', 'M18 9l3 3-3 3'],
  alert: ['M12 4.5l9 16H3l9-16z', 'M12 10v4', 'M12 17h.01'],
  info: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 11v5', 'M12 8h.01'],
  eye: ['M2.5 12S6 6.5 12 6.5 21.5 12 21.5 12 18 17.5 12 17.5 2.5 12 2.5 12z', 'M12 14.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z'],
  clock: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 7.5V12l3.5 2'],
  harddrive: ['M3 7h18v10H3z', 'M7 17v2.5', 'M17 17v2.5', 'M7.5 11.5h.01'],
  cloud: ['M7 18h10a4 4 0 0 0 .5-8 5.5 5.5 0 0 0-10.6 1.4A3.3 3.3 0 0 0 7 18z'],
  user: ['M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8z', 'M4.5 20a7.5 7.5 0 0 1 15 0'],
  inbox: ['M4 13h4l1.5 3h5L16 13h4', 'M4 13l2.5-8h11L20 13v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1v-5z'],
  zap: ['M13 3L5 14h6l-1 7 8-11h-6l1-7z'],
  // external：在系统文件管理器 / 默认程序中打开
  external: ['M14 4h6v6', 'M20 4l-8.5 8.5', 'M18 14v5a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 4 19V8a1.5 1.5 0 0 1 1.5-1.5H10'],
};

/**
 * 生成图标节点。
 * @param {keyof ICONS|string} name 图标名，未知名回落到 file
 * @param {number} size 边长（px）
 * @returns {SVGElement}
 */
export function icon(name, size = 18) {
  const paths = ICONS[name] || ICONS.file;
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('fill', 'none');
  svg.setAttribute('stroke', 'currentColor');
  svg.setAttribute('stroke-width', '1.8');
  svg.setAttribute('stroke-linecap', 'round');
  svg.setAttribute('stroke-linejoin', 'round');
  svg.setAttribute('aria-hidden', 'true');
  svg.classList.add('icon');
  for (const d of paths) {
    const p = document.createElementNS(NS, 'path');
    p.setAttribute('d', d);
    svg.appendChild(p);
  }
  return svg;
}

/** 图标名是否存在，供调用方做防御。 */
export function hasIcon(name) {
  return Object.prototype.hasOwnProperty.call(ICONS, name);
}

export { ICONS };

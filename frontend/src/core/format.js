/**
 * format.js —— 展示层格式化。所有单位、精度、本地化措辞都收敛在这里，
 * 组件里不再出现裸的 toFixed / 手写 "MB"。
 */

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** 字节数 → 人类可读体积。0 与未知（负数）统一显示 "-"。 */
export function bytes(n) {
  const v = Number(n);
  if (!Number.isFinite(v) || v <= 0) return '-';
  if (v < 1024) return v + ' B';
  let i = Math.floor(Math.log(v) / Math.log(1024));
  i = Math.min(i, UNITS.length - 1);
  const val = v / Math.pow(1024, i);
  // 1-10 保留一位小数，10-100 取整，观感更整齐
  return (val < 10 ? val.toFixed(1) : Math.round(val)) + ' ' + UNITS[i];
}

/** 字节/秒 → 速度串。 */
export function speed(bps) {
  const v = Number(bps);
  if (!Number.isFinite(v) || v <= 0) return '-';
  return bytes(v) + '/s';
}

/** 剩余秒数 → mm:ss 或 hh:mm:ss。 */
export function duration(seconds) {
  const s = Math.max(0, Math.round(Number(seconds) || 0));
  if (s < 60) return s + ' 秒';
  const m = Math.floor(s / 60);
  if (m < 60) return m + ' 分 ' + pad(s % 60) + ' 秒';
  const hh = Math.floor(m / 60);
  return hh + ' 小时 ' + pad(m % 60) + ' 分';
}

/** 剩余字节 + 速度 → 预计剩余时间的紧凑形式（mm:ss）。 */
export function eta(remainingBytes, bps) {
  const left = Number(remainingBytes);
  const v = Number(bps);
  if (!Number.isFinite(left) || left <= 0) return '--:--';
  if (!Number.isFinite(v) || v <= 0) return '--:--';
  const sec = Math.round(left / v);
  if (sec > 99 * 3600) return '--:--';
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  if (m >= 60) {
    return pad(Math.floor(m / 60)) + ':' + pad(m % 60) + ':' + pad(s);
  }
  return pad(m) + ':' + pad(s);
}

/**
 * Unix 秒 → 日期时间。同一年省略年份，今天/昨天用相对说法，列表里更易扫读。
 */
export function datetime(ts) {
  const sec = Number(ts);
  if (!Number.isFinite(sec) || sec <= 0) return '-';
  const d = new Date(sec * 1000);
  const now = new Date();
  const pad2 = pad;
  const hm = pad2(d.getHours()) + ':' + pad2(d.getMinutes());
  const sameDay = (a, b) =>
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();

  const yesterday = new Date(now.getTime() - 86400000);
  if (sameDay(d, now)) return '今天 ' + hm;
  if (sameDay(d, yesterday)) return '昨天 ' + hm;
  if (d.getFullYear() === now.getFullYear()) {
    return pad2(d.getMonth() + 1) + '-' + pad2(d.getDate()) + ' ' + hm;
  }
  return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate());
}

/** 0-100 的进度取一位小数，避免进度条文字抖动。 */
export function percent(p) {
  const v = Number(p);
  if (!Number.isFinite(v)) return '0%';
  return (Math.round(Math.min(100, Math.max(0, v)) * 10) / 10).toFixed(1) + '%';
}

function pad(n) {
  return String(n).padStart(2, '0');
}

/** 把区间外的数值夹回区间。 */
export function clamp(n, min, max) {
  return Math.min(max, Math.max(min, Number(n) || 0));
}

/** 按扩展名粗略归类，用于网格/列表的图标着色。 */
export function kindOf(name, isDir) {
  if (isDir) return 'dir';
  const ext = (String(name).split('.').pop() || '').toLowerCase();
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'heic'].includes(ext)) return 'image';
  if (['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm'].includes(ext)) return 'video';
  if (['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a'].includes(ext)) return 'audio';
  if (['zip', 'rar', '7z', 'tar', 'gz', 'bz2'].includes(ext)) return 'archive';
  if (['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'txt', 'md'].includes(ext)) return 'doc';
  if (['go', 'js', 'ts', 'py', 'json', 'yaml', 'yml', 'html', 'css', 'sh'].includes(ext)) return 'code';
  return 'file';
}

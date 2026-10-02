/**
 * index.js —— 适配层门面（前端唯一的数据出入口）。
 *
 * 页面代码只依赖这里导出的 bridge()，永远不知道自己跑在 Wails 壳里还是预览模式：
 *   - 检测到 window.go.* 绑定 → 用 wails.js 直连 Go 适配层；
 *   - 否则 → 用 mock.js 的假数据，保证界面可完整演示。
 * 两套实现方法签名完全一致，新增能力时必须两边同步补上。
 */

import { createWailsBridge } from './wails.js';
import { createMockBridge } from './mock.js';

/** 后端事件名，与 app/app.go 中的常量一一对应。 */
export const EVENTS = {
  transferUpdate: 'transfer:update',
  transferList: 'transfer:list',
  authChanged: 'auth:changed',
  toast: 'toast',
};

let impl = null;

/**
 * 初始化适配层，必须在渲染任何页面之前调用一次。
 * @returns {Promise<{mode: 'wails'|'mock', reason?: string}>}
 */
export async function initBridge() {
  try {
    impl = await createWailsBridge();
    return { mode: impl.mode };
  } catch (err) {
    impl = createMockBridge();
    return { mode: 'mock', reason: err && err.message ? err.message : String(err) };
  }
}

/** 取当前适配器。未初始化时抛错，避免静默产生 undefined 调用。 */
export function bridge() {
  if (!impl) throw new Error('适配层尚未初始化，请先调用 initBridge()');
  return impl;
}

/** 当前模式，供 UI 展示「预览模式」角标。 */
export function bridgeMode() {
  return impl ? impl.mode : 'unknown';
}

/** 订阅后端事件，返回取消订阅函数。 */
export function onEvent(event, cb) {
  return bridge().on(event, cb);
}

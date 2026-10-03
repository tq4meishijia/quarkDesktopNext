/**
 * store.js —— 极简状态容器。
 *
 * 只做三件事：读快照、浅合并写入、变更广播。刻意不做深层 diff 与 selector，
 * 本应用的状态量（登录态、任务列表、设置、当前路由）用浅合并足够。
 */
export function createStore(initial = {}) {
  let state = { ...initial };
  const subs = new Set();

  return {
    /** 返回当前状态快照。 */
    get() {
      return state;
    },
    /**
     * 写入状态。可传对象（浅合并）或函数（基于当前状态返回补丁）。
     * @param {object|((s:object)=>object)} patch
     */
    set(patch) {
      const next = typeof patch === 'function' ? patch(state) : patch;
      if (!next) return state;
      state = { ...state, ...next };
      for (const fn of Array.from(subs)) {
        try {
          fn(state);
        } catch (err) {
          console.error('[store] subscriber error', err);
        }
      }
      return state;
    },
    /**
     * 订阅变更，返回取消订阅函数。
     *
     * 当前无调用方（页面各自用 onEvent 订阅后端事件）。保留是因为 set() 的
     * 广播循环依赖它，删掉会让容器失去「变更通知」这一基本能力。
     */
    subscribe(fn) {
      subs.add(fn);
      return () => subs.delete(fn);
    },
  };
}

// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package transfer

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

// ErrNotFound 任务不存在。
var ErrNotFound = errors.New("任务不存在")

// ErrHandedOff 由 Runner 返回，表示任务已交给外部下载器，本进程停止跟踪。
// Manager 会把它落成「已移交」终态，而不是失败。
//
// 注意：internal/engine 也有一个同名同文案的 ErrHandedOff。Runner 实现必须
// 用本包的 ErrHandedOff（或显式做 errors.Is 桥接）返回，否则 Manager 匹配不到，
// 任务会被误判为「失败」。
var ErrHandedOff = errors.New("已移交给外部下载器")

// ErrPaused 由 Runner 返回，表示本次搬运是因用户暂停而中断，不是失败。
// Manager 遇到它会把任务放回「暂停」态并让出并发槽位，等待 Resume 重新调度。
//
// 为什么需要这个哨兵：内建下载器在暂停时通过 Gate 中断当前请求并返回，
// 若把它当成失败，任务会被错记为 failed 且再也续传不了。
var ErrPaused = errors.New("任务已暂停")

// maxConcurrency 是并行度硬上限，与 SetConcurrency 的钳制保持一致。
const maxConcurrency = 16

// Manager 是传输队列的调度中心：
//   - 固定数量的 worker 从 queue 取任务；
//   - 通过容量为 N 的信号量控制"同时进行的文件数"，N 可在运行时调整；
//   - 进度回调先过暂停闸门再做速度采样，最后按节流窗口向前端推事件。
type Manager struct {
	runner Runner
	emit   func(*Task)

	mu     sync.RWMutex
	tasks  map[string]*Task
	order  []string
	sample map[string]sample
	last   map[string]time.Time

	queue  chan *Task
	gate   *slotGate
	stopCh chan struct{}
	wg     sync.WaitGroup
	once   sync.Once

	// store 负责任务落盘与恢复；为 nil 时纯内存运行。
	store *persister
}

type sample struct {
	at   time.Time
	done int64
}

// NewManager 创建管理器。emit 用于把任务快照推送给 UI（可为 nil）。
func NewManager(runner Runner, emit func(*Task)) *Manager {
	m := &Manager{
		runner: runner,
		emit:   emit,
		tasks:  make(map[string]*Task),
		sample: make(map[string]sample),
		last:   make(map[string]time.Time),
		queue:  make(chan *Task, 1024),
		gate:   newSlotGate(1),
		stopCh: make(chan struct{}),
	}
	m.SetConcurrency(1)
	return m
}

// Start 启动 worker 池。maxWorkers 是"最多同时调度多少个任务"的硬上限，
// 实际并行数由 SetConcurrency 的信号量决定。
func (m *Manager) Start(maxWorkers int) {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	for i := 0; i < maxWorkers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

// EnablePersistence 开启任务持久化：statePath 为落盘文件路径。
//
// 必须在 Start 之前调用。开启后：
//   - 每次任务状态变化会（去抖后）异步落盘；
//   - Stop 时做最后一次同步落盘；
//   - 可用 RestoreTasks 读回上次残留的任务。
func (m *Manager) EnablePersistence(statePath string) {
	m.store = newPersister(statePath, true)
	m.store.snapshot = m.persistSnapshot
	m.store.start()
}

// RestoreTasks 读回上次残留的任务，全部标记为「已中断」等待用户续传。
//
// 不自动续传的理由：重启后立刻发起大量网络请求会消耗流量/费用，
// 且违背「用户点开始才下载」的预期。
//
// 返回读到的任务数。文件不存在或损坏时返回 0。
func (m *Manager) RestoreTasks() int {
	if m.store == nil {
		return 0
	}
	items := m.store.load()
	n := 0
	for _, it := range items {
		// 只恢复可续传的任务：上传需本地文件仍存在，下载只需 fid。
		if it.Kind == "upload" {
			if it.LocalPath == "" {
				continue
			}
			if _, err := os.Stat(it.LocalPath); err != nil {
				// 本地源文件已被删除/移动，无法续传，跳过。
				continue
			}
		}
		if it.Kind == "download" && it.Dest == "" {
			continue
		}
		t := restoreTask(it)
		m.mu.Lock()
		m.tasks[t.ID] = t
		m.order = append(m.order, t.ID)
		m.mu.Unlock()
		n++
	}
	if n > 0 {
		// 一次性推全量快照，让前端立刻看到历史任务。
		for _, t := range m.List() {
			m.push(t, true)
		}
	}
	return n
}

// persistSnapshot 导出当前任务快照（由 persister 调用）。
func (m *Manager) persistSnapshot() *persistedState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := &persistedState{Version: currentPersistVersion}
	for _, id := range m.order {
		t, ok := m.tasks[id]
		if !ok {
			continue
		}
		st.Tasks = append(st.Tasks, t.toPersisted())
	}
	return st
}

// SetConcurrency 运行时调整并行数（1-16）。已在进行中的任务不受影响：
// 调大后立即有新的 acquire 通过，调小则等在途任务自然结束才真正降下来。
func (m *Manager) SetConcurrency(n int) {
	m.gate.setLimit(n)
}

// Stop 停止调度并等待 worker 退出；不等待在途任务完成，调用方应先自行取消。
func (m *Manager) Stop() {
	m.once.Do(func() { close(m.stopCh) })
	m.wg.Wait()
	// 最后同步落盘一次，保证退出前的状态不丢。
	if m.store != nil {
		m.store.close()
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stopCh:
			return
		case t := <-m.queue:
			// 槽位是并发额度的唯一载体：暂停会让出槽位，好让别的任务顶上，
			// 否则「暂停全部任务」会把并发额度耗光，新任务永久排队。
			if !m.gate.acquire(m.stopCh) {
				return
			}
			if t.Status() == StatusCancelled {
				m.gate.release()
				continue
			}
			// 取出时若已被暂停，直接让出槽位等 Resume 重新入队，
			// 不做任何搬运，也不改状态（状态由 Pause/Resume 负责）。
			if t.Gate().Paused() {
				m.gate.release()
				continue
			}
			m.run(t)
			m.gate.release()
		}
	}
}

// Enqueue 入队一个新任务并立即返回其 ID 快照。
func (m *Manager) Enqueue(s Spec) *Task {
	t := newTask(s)
	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.mu.Unlock()
	m.push(t, true)
	select {
	case m.queue <- t:
	case <-m.stopCh:
	}
	return t
}

// run 搬运一个任务直到终态。
//
// 暂停的处理：Runner 在暂停时返回 ErrPaused（内建下载器由 Gate 中断当前请求），
// 此时任务回到 paused 态、进度保留，等待 Resume 重新入队续传。
func (m *Manager) run(t *Task) {
	t.setStatus(StatusRunning)
	m.push(t, true)

	prog := func(done int64) error {
		return m.onProgress(t, done)
	}

	var err error
	switch t.Kind {
	case "upload":
		err = m.runner.Upload(t.Context(), t, prog)
	case "download":
		err = m.runner.Download(t.Context(), t, prog)
	default:
		err = errors.New("未知任务类型: " + t.Kind)
	}

	m.mu.Lock()
	delete(m.sample, t.ID)
	delete(m.last, t.ID)
	m.mu.Unlock()

	switch {
	case errors.Is(err, ErrPaused):
		// 用户主动暂停：不是失败，也不算结束，保持可续传状态。
		t.setStatus(StatusPaused)
	case errors.Is(err, ErrHandedOff):
		// 外部下载器已接管：不是失败，进度不再由本进程汇报。
		t.setStatus(StatusHandedOff)
	case err == nil:
		if t.Size > 0 {
			t.setDone(t.Size)
		}
		t.setStatus(StatusCompleted)
	case errors.Is(err, context.Canceled):
		t.setStatus(StatusCancelled)
	default:
		// 用户取消但 Runner 无法中断时（见 app 包说明），优先记为取消。
		if t.Context().Err() != nil {
			t.setStatus(StatusCancelled)
		} else {
			t.fail(err)
		}
	}
	m.push(t, true)
}

// onProgress 是进度回调的统一入口：闸门 → 采样 → 节流推送。
func (m *Manager) onProgress(t *Task, done int64) error {
	if err := t.Gate().Wait(t.Context()); err != nil {
		return err
	}
	t.setDone(done)

	now := time.Now()
	m.mu.Lock()
	prev, ok := m.sample[t.ID]
	shouldEmit := false
	if !ok || now.Sub(prev.at) >= 400*time.Millisecond {
		if ok {
			elapsed := now.Sub(prev.at).Seconds()
			if elapsed > 0 {
				t.setSpeed(float64(done-prev.done) / elapsed)
			}
		}
		m.sample[t.ID] = sample{at: now, done: done}
	}
	if lastAt, ok2 := m.last[t.ID]; !ok2 || now.Sub(lastAt) >= 250*time.Millisecond {
		m.last[t.ID] = now
		shouldEmit = true
	}
	m.mu.Unlock()

	if shouldEmit {
		m.push(t, false)
	}
	return nil
}

// push 向 UI 推送任务快照。force=true 表示状态变化，跳过节流。
func (m *Manager) push(t *Task, force bool) {
	// 这里同时是状态变化的统一出口，因此落盘触发点也放在这：
	// force=true 时标记一次 dirty，由 persister 去抖写盘。
	if force && m.store != nil {
		m.store.mark()
	}
	if m.emit == nil {
		return
	}
	if force {
		m.mu.Lock()
		m.last[t.ID] = time.Now()
		m.mu.Unlock()
	}
	m.emit(t)
}

// Get 返回任务指针。
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok
}

// List 按创建顺序返回全部任务。
func (m *Manager) List() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Task, 0, len(m.order))
	for _, id := range m.order {
		if t, ok := m.tasks[id]; ok {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Pause 暂停任务。仅对 pending / running 生效。
func (m *Manager) Pause(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	st := t.Status()
	if st.IsTerminal() {
		return nil
	}
	t.Gate().Set(true)
	t.setStatus(StatusPaused)
	m.push(t, true)
	return nil
}

// Resume 继续任务。仅对 paused / pending 生效。
func (m *Manager) Resume(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	st := t.Status()
	if st.IsTerminal() || st == StatusRunning {
		return nil
	}
	t.Gate().Set(false)
	t.setStatus(StatusPending)
	m.push(t, true)
	// 重新入队，等待空闲槽位。
	select {
	case m.queue <- t:
	case <-m.stopCh:
	}
	return nil
}

// Cancel 取消任务：未开始的直接标记取消，进行中的通过 context 通知 Runner。
func (m *Manager) Cancel(id string) error {
	t, ok := m.Get(id)
	if !ok {
		return ErrNotFound
	}
	if t.Status().IsTerminal() {
		return nil
	}
	t.Gate().Set(false) // 先解除暂停，避免 Runner 卡在闸门里收不到取消信号
	t.requestCancel()
	t.setStatus(StatusCancelled)
	m.push(t, true)
	return nil
}

// Retry 把一个失败/取消的任务重新入队。
func (m *Manager) Retry(id string) error {
	m.mu.Lock()
	t, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	st := t.Status()
	if st != StatusFailed && st != StatusCancelled {
		return errors.New("只有失败或已取消的任务可以重试")
	}
	// 换新的 ctx / cancel / gate：旧 Runner 可能还持有旧 ctx 的引用，
	// 复用同一个闸门会让「上次遗留的暂停位」影响本次重试。
	t.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel
	t.gate = NewGate()
	t.done = 0
	t.errMsg = ""
	t.finishedAt = time.Time{}
	t.status = StatusPending
	t.mu.Unlock()
	m.push(t, true)
	select {
	case m.queue <- t:
	case <-m.stopCh:
	}
	return nil
}

// PauseAll 暂停所有非终态任务。
func (m *Manager) PauseAll() int {
	n := 0
	for _, t := range m.List() {
		if t.Status().IsTerminal() {
			continue
		}
		t.Gate().Set(true)
		if t.Status() == StatusRunning {
			t.setStatus(StatusPaused)
		}
		m.push(t, true)
		n++
	}
	return n
}

// ResumeAll 继续所有暂停任务。
func (m *Manager) ResumeAll() int {
	n := 0
	for _, t := range m.List() {
		if t.Status() != StatusPaused {
			continue
		}
		t.Gate().Set(false)
		t.setStatus(StatusPending)
		m.push(t, true)
		select {
		case m.queue <- t:
		case <-m.stopCh:
		}
		n++
	}
	return n
}

// CancelAll 取消所有非终态任务。
func (m *Manager) CancelAll() int {
	n := 0
	for _, t := range m.List() {
		if t.Status().IsTerminal() {
			continue
		}
		t.Gate().Set(false)
		t.requestCancel()
		t.setStatus(StatusCancelled)
		m.push(t, true)
		n++
	}
	return n
}

// ClearCompleted 清除所有终态任务，返回被清除的数量。
func (m *Manager) ClearCompleted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.order[:0]
	n := 0
	for _, id := range m.order {
		t, ok := m.tasks[id]
		if !ok {
			continue
		}
		if t.Status().IsTerminal() {
			delete(m.tasks, id)
			delete(m.sample, id)
			delete(m.last, id)
			n++
			continue
		}
		kept = append(kept, id)
	}
	m.order = kept
	return n
}

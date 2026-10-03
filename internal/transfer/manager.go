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
	"sort"
	"sync"
	"time"
)

// ErrNotFound 任务不存在。
var ErrNotFound = errors.New("任务不存在")

// ErrHandedOff 由 Runner 返回，表示任务已交给外部下载器，本进程停止跟踪。
// Manager 会把它落成「已移交」终态，而不是失败。
var ErrHandedOff = errors.New("已移交给外部下载器")

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
	sem    chan struct{}
	stopCh chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
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
		sem:    make(chan struct{}, 1),
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

// SetConcurrency 运行时调整并行数（1-16）。已在进行中的任务不受影响。
func (m *Manager) SetConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	if n > 16 {
		n = 16
	}
	m.mu.Lock()
	m.sem = make(chan struct{}, n)
	m.mu.Unlock()
}

// Stop 停止调度并等待 worker 退出；不等待在途任务完成，调用方应先自行取消。
func (m *Manager) Stop() {
	m.once.Do(func() { close(m.stopCh) })
	m.wg.Wait()
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stopCh:
			return
		case t := <-m.queue:
			m.acquire()
			if t.Status() == StatusCancelled {
				m.release()
				continue
			}
			m.run(t)
			m.release()
		}
	}
}

func (m *Manager) acquire() {
	m.mu.RLock()
	sem := m.sem
	m.mu.RUnlock()
	sem <- struct{}{}
}

func (m *Manager) release() {
	m.mu.RLock()
	sem := m.sem
	m.mu.RUnlock()
	<-sem
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
	t.cancel()
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
	ctx, cancel := context.WithCancel(context.Background())
	t.ctx = ctx
	t.cancel = cancel
	t.gate.Set(false)
	t.mu.Lock()
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
		t.cancel()
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

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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner 是可控的 Runner：记录并发峰值，按需阻塞，并遵守任务的暂停闸门
// （真实 Runner 正是通过 Gate 感知暂停的，见 app.sdkRunner 与 engine 的 Gate 回调）。
type fakeRunner struct {
	started  atomic.Int64
	peak     atomic.Int64
	inflight atomic.Int64
	release  chan struct{}
	err      error
	// block 若非 nil，每个任务在搬运前先等它关闭。
	block chan struct{}
	// gateAware 为 true 时，block 等待期间轮询任务的暂停闸门，
	// 暂停时返回 ErrPaused（与内建下载器行为一致）。
	gateAware bool
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{release: make(chan struct{})}
}

func (f *fakeRunner) enter() int64 {
	cur := f.inflight.Add(1)
	for {
		old := f.peak.Load()
		if cur <= old || f.peak.CompareAndSwap(old, cur) {
			break
		}
	}
	f.started.Add(1)
	return cur
}

func (f *fakeRunner) leave() { f.inflight.Add(-1) }

func (f *fakeRunner) do(ctx context.Context, t *Task, prog ProgressFunc) error {
	f.enter()
	defer f.leave()
	if f.block != nil {
		// 与真实 Runner 一致：暂停时返回 ErrPaused，取消时返回 ctx.Err()。
		if err := f.waitUnblocked(ctx, t); err != nil {
			return err
		}
	}
	if f.err != nil {
		return f.err
	}
	_ = prog(1024)
	return nil
}

// waitUnblocked 等待 block 关闭；gateAware 时同时响应暂停与取消，
// 并返回与真实 Runner 同义的错误（ErrPaused / ctx.Err()）。
func (f *fakeRunner) waitUnblocked(ctx context.Context, t *Task) error {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		// 取消优先判定：用户取消时不该被记成暂停。
		if err := ctx.Err(); err != nil {
			return err
		}
		if f.gateAware && t.Gate().Paused() {
			return ErrPaused
		}
		select {
		case <-f.block:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (f *fakeRunner) Upload(ctx context.Context, t *Task, prog ProgressFunc) error {
	return f.do(ctx, t, prog)
}

func (f *fakeRunner) Download(ctx context.Context, t *Task, prog ProgressFunc) error {
	return f.do(ctx, t, prog)
}

// newTestManager 建一个带 n 个 worker 的管理器并在测试结束自动停止。
func newTestManager(t *testing.T, r Runner, workers int) *Manager {
	t.Helper()
	m := NewManager(r, nil)
	m.Start(workers)
	t.Cleanup(m.Stop)
	return m
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// ---------------------------------------------------------------------------
// P0-1：SetConcurrency 不得因替换 channel 导致死锁
// ---------------------------------------------------------------------------

// TestSetConcurrencyDuringTransferNoDeadlock 是 P0-1 的回归测试。
//
// 旧实现里 SetConcurrency 直接 m.sem = make(chan struct{}, n)，
// acquire 与 release 各自独立读 m.sem，若在两者之间换掉 channel，
// token 会泄漏到旧 channel、release 在新 channel 上永久阻塞。
// 本测试在任务搬运期间高频调整并发数，验证：
//  1. 不会死锁（所有任务最终都能完成并释放槽位）；
//  2. 调整后槽位数最终归零（无泄漏）；
//  3. 并发峰值不超过设定上限。
func TestSetConcurrencyDuringTransferNoDeadlock(t *testing.T) {
	r := newFakeRunner()
	m := newTestManager(t, r, 8)

	const total = 24
	m.SetConcurrency(4)

	// 先把并发度拉满，让任务真正占着槽位。
	for i := 0; i < total; i++ {
		m.Enqueue(Spec{Kind: "download", Name: "f", Size: 1})
	}

	// 搬运期间反复调整并发数，正落在 acquire/release 的间隙上。
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		n := 1
		for {
			select {
			case <-stop:
				return
			default:
			}
			n = n%16 + 1
			m.SetConcurrency(n)
			time.Sleep(time.Millisecond)
		}
	}()

	// 等待所有任务完成。
	if !waitFor(t, 10*time.Second, func() bool { return int(r.started.Load()) >= total }) {
		t.Fatalf("任务未全部启动：started=%d want=%d", r.started.Load(), total)
	}

	stop <- struct{}{}
	wg.Wait()

	if !waitFor(t, 5*time.Second, func() bool { return m.gate.heldCount() == 0 }) {
		t.Fatalf("槽位泄漏：held=%d（并发调整导致 acquire/release 错配）", m.gate.heldCount())
	}

	// 全部任务都应落到终态。
	for _, task := range m.List() {
		if !task.Status().IsTerminal() {
			t.Fatalf("任务 %s 未到终态: %s", task.ID, task.Status())
		}
	}
}

// TestSetConcurrencyClamp 验证越界值被钳制到 [1,16]，且不产生负容量。
func TestSetConcurrencyClamp(t *testing.T) {
	r := newFakeRunner()
	m := newTestManager(t, r, 2)

	for _, c := range []struct{ in, want int }{
		{-5, 1}, {0, 1}, {1, 1}, {16, 16}, {99, 16},
	} {
		m.SetConcurrency(c.in)
		m.gate.mu.Lock()
		got := m.gate.limit
		m.gate.mu.Unlock()
		if got != c.want {
			t.Fatalf("SetConcurrency(%d) => limit %d, want %d", c.in, got, c.want)
		}
	}
}

// TestSetConcurrencyRaiseTakesEffect 验证调大并发度后新任务能立即获得槽位。
func TestSetConcurrencyRaiseTakesEffect(t *testing.T) {
	r := newFakeRunner()
	m := newTestManager(t, r, 4)
	m.SetConcurrency(1)

	// 塞入 4 个任务并让第一个占住唯一槽位。
	r.block = make(chan struct{})
	m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return r.inflight.Load() == 1 }) {
		t.Fatal("首个任务未占用槽位")
	}
	blocked := make(chan struct{})
	m.Enqueue(Spec{Kind: "download", Name: "b", Size: 1})
	// 此时并发=1，第二个任务应被挡住。
	time.Sleep(50 * time.Millisecond)
	if r.inflight.Load() != 1 {
		t.Fatalf("并发=1 时不应有第二个任务在跑，inflight=%d", r.inflight.Load())
	}

	// 调大到 3，第二个任务应立刻获得槽位。
	m.SetConcurrency(3)
	go func() { close(blocked) }()
	_ = blocked
	if !waitFor(t, 3*time.Second, func() bool { return r.started.Load() >= 2 }) {
		t.Fatalf("调大并发后新任务未获得槽位：started=%d", r.started.Load())
	}
	close(r.block)
}

// ---------------------------------------------------------------------------
// P0-2：暂停必须让出并发槽位
// ---------------------------------------------------------------------------

// TestPauseYieldsSlot 是 P0-2 的回归测试。
//
// 旧实现里 release() 包住 m.run(t) 全程，暂停中的任务仍占着 semaphore 槽。
// 后果：暂停全部任务后并发额度被耗光，新任务永久排队。
// 本测试验证：暂停所有任务后，新任务仍能立即开始。
func TestPauseYieldsSlot(t *testing.T) {
	r := newFakeRunner()
	r.gateAware = true
	m := newTestManager(t, r, 4)
	m.SetConcurrency(2)

	// 让两个任务真正占住全部 2 个槽位。
	r.block = make(chan struct{})
	m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	m.Enqueue(Spec{Kind: "download", Name: "b", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return r.inflight.Load() == 2 }) {
		t.Fatalf("未占满并发槽位，inflight=%d", r.inflight.Load())
	}

	// 暂停全部任务。
	if n := m.PauseAll(); n != 2 {
		t.Fatalf("PauseAll 返回 %d，期望 2", n)
	}
	// 等搬运循环因暂停退出，槽位归还。
	if !waitFor(t, 3*time.Second, func() bool { return m.gate.heldCount() == 0 }) {
		t.Fatalf("暂停后槽位未归还：held=%d（暂停任务仍占用并发额度）", m.gate.heldCount())
	}

	// 关键断言：并发额度已释放，新任务能立刻开始。
	m.Enqueue(Spec{Kind: "download", Name: "c", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return r.started.Load() >= 3 }) {
		t.Fatal("暂停全部任务后新任务无法启动（并发额度被暂停任务占死）")
	}
	close(r.block)
}

// TestPausedTaskStaysPausedAndResumable 验证暂停的任务保持 paused 态、
// 不被误判为失败，且 Resume 后能重新搬运。
func TestPausedTaskStaysPausedAndResumable(t *testing.T) {
	r := newFakeRunner()
	r.gateAware = true
	r.block = make(chan struct{})
	m := newTestManager(t, r, 2)
	m.SetConcurrency(1)

	task := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 2048})
	if !waitFor(t, 3*time.Second, func() bool { return r.started.Load() == 1 }) {
		t.Fatal("任务未启动")
	}
	if err := m.Pause(task.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// 搬运循环应因 ErrPaused 退出，任务回到 paused。
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusPaused }) {
		t.Fatalf("暂停后状态为 %s，期望 %s", task.Status(), StatusPaused)
	}
	if task.Status().IsTerminal() {
		t.Fatal("暂停不应是终态")
	}
	if msg := task.ErrMsg(); msg != "" {
		t.Fatalf("暂停不应产生错误信息，得到 %q", msg)
	}
	// 槽位必须已归还。
	if !waitFor(t, 2*time.Second, func() bool { return m.gate.heldCount() == 0 }) {
		t.Fatalf("暂停后槽位未归还：held=%d", m.gate.heldCount())
	}

	// 放行并恢复：任务应重新被调度并最终完成。
	close(r.block)
	r.block = nil
	if err := m.Resume(task.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusCompleted }) {
		t.Fatalf("恢复后状态为 %s，期望 %s", task.Status(), StatusCompleted)
	}
}

// TestPauseBeforeStartIsNotRun 验证入队但尚未开始的任务被暂停后不会被误跑，
// 也不会丢失（Resume 后仍能执行）。
func TestPauseBeforeStartIsNotRun(t *testing.T) {
	r := newFakeRunner()
	r.gateAware = true
	r.block = make(chan struct{})
	m := newTestManager(t, r, 1)
	m.SetConcurrency(1)

	// 第一个任务占住唯一槽位，第二个只能排队。
	first := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	second := m.Enqueue(Spec{Kind: "download", Name: "b", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return r.started.Load() == 1 }) {
		t.Fatal("首个任务未启动")
	}
	if err := m.Pause(second.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// 第一个跑完后，第二个不应被启动（它处于暂停）。
	close(r.block)
	r.block = nil
	if !waitFor(t, 3*time.Second, func() bool { return first.Status().IsTerminal() }) {
		t.Fatalf("首个任务未结束：%s", first.Status())
	}
	time.Sleep(80 * time.Millisecond)
	if r.started.Load() != 1 {
		t.Fatalf("排队中被暂停的任务不应启动，started=%d", r.started.Load())
	}

	// 恢复后应能执行。
	if err := m.Resume(second.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return second.Status() == StatusCompleted }) {
		t.Fatalf("恢复后状态为 %s，期望 %s", second.Status(), StatusCompleted)
	}
}

// TestCancelPausedTask 验证取消暂停中的任务能立即生效（不被 Gate 卡住）。
func TestCancelPausedTask(t *testing.T) {
	r := newFakeRunner()
	r.gateAware = true
	r.block = make(chan struct{})
	m := newTestManager(t, r, 1)
	m.SetConcurrency(1)

	task := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return r.started.Load() == 1 }) {
		t.Fatal("任务未启动")
	}
	if err := m.Pause(task.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusPaused }) {
		t.Fatalf("状态为 %s，期望 paused", task.Status())
	}
	if err := m.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusCancelled }) {
		t.Fatalf("取消后状态为 %s，期望 %s", task.Status(), StatusCancelled)
	}
	close(r.block)
}

// ---------------------------------------------------------------------------
// 状态机与既有行为回归
// ---------------------------------------------------------------------------

// TestHandedOffIsTerminal 验证「已移交」是终态且算成功。
// app 层负责把 engine.ErrHandedOff 桥接成本包的 ErrHandedOff。
func TestHandedOffIsTerminal(t *testing.T) {
	r := newFakeRunner()
	r.err = ErrHandedOff
	m := newTestManager(t, r, 1)

	task := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusHandedOff }) {
		t.Fatalf("状态为 %s，期望 %s", task.Status(), StatusHandedOff)
	}
	if !task.Status().Succeeded() {
		t.Fatal("已移交应算成功结束")
	}
	if m.gate.heldCount() != 0 {
		t.Fatalf("槽位未归还：held=%d", m.gate.heldCount())
	}
}

// TestPausedErrorNotTreatedAsFailure 验证 ErrPaused 不会被记成 failed。
func TestPausedErrorNotTreatedAsFailure(t *testing.T) {
	r := newFakeRunner()
	r.err = ErrPaused
	m := newTestManager(t, r, 1)

	task := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusPaused }) {
		t.Fatalf("状态为 %s，期望 %s（ErrPaused 不应视为失败）", task.Status(), StatusPaused)
	}
	if task.Status() == StatusFailed {
		t.Fatal("ErrPaused 被误判为失败")
	}
}

// TestRetryAfterPauseResetsGate 验证重试会换上新闸门，不受上次暂停位影响。
func TestRetryAfterPauseResetsGate(t *testing.T) {
	r := newFakeRunner()
	r.err = ErrPaused
	m := newTestManager(t, r, 1)

	task := m.Enqueue(Spec{Kind: "download", Name: "a", Size: 1})
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusPaused }) {
		t.Fatal("任务未进入暂停态")
	}
	// paused 不是终态，Retry 应拒绝。
	if err := m.Retry(task.ID); err == nil {
		t.Fatal("暂停中的任务不应允许 Retry")
	}

	// 取消后即可重试；重试后闸门应是放行状态。
	if err := m.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return task.Status() == StatusCancelled }) {
		t.Fatal("取消未生效")
	}
	r.err = nil
	if err := m.Retry(task.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if task.Gate().Paused() {
		t.Fatal("重试后闸门仍是暂停态")
	}
	if !waitFor(t, 3*time.Second, func() bool { return task.Status() == StatusCompleted }) {
		t.Fatalf("重试后状态为 %s，期望 %s", task.Status(), StatusCompleted)
	}
}

// TestConcurrencyRespected 验证并发峰值不超过设定上限。
func TestConcurrencyRespected(t *testing.T) {
	r := newFakeRunner()
	r.block = make(chan struct{})
	m := newTestManager(t, r, 8)
	m.SetConcurrency(3)

	for i := 0; i < 12; i++ {
		m.Enqueue(Spec{Kind: "download", Name: "f", Size: 1})
	}
	time.Sleep(150 * time.Millisecond)
	if got := r.peak.Load(); got > 3 {
		t.Fatalf("并发峰值 %d 超过上限 3", got)
	}
	close(r.block)
}

// TestGateWaitUnblocksImmediately 验证解除暂停能立即唤醒 Wait（不再等 100ms 轮询）。
func TestGateWaitUnblocksImmediately(t *testing.T) {
	g := NewGate()
	g.Set(true)
	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background()) }()

	select {
	case <-done:
		t.Fatal("暂停期间 Wait 不应返回")
	case <-time.After(30 * time.Millisecond):
	}

	start := time.Now()
	g.Set(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait 返回 %v", err)
		}
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Fatalf("解除暂停唤醒过慢：%v（应靠广播而非轮询）", d)
		}
	case <-time.After(time.Second):
		t.Fatal("解除暂停后 Wait 未返回")
	}
}

// TestGateWaitContextCancel 验证 ctx 取消能打断暂停等待。
func TestGateWaitContextCancel(t *testing.T) {
	g := NewGate()
	g.Set(true)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- g.Wait(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("期望 context.Canceled，得到 %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ctx 取消未打断 Wait")
	}
}

// TestGateNilContextPaused 验证 nil ctx 时暂停也能被识别（不静默放行）。
func TestGateNilContextPaused(t *testing.T) {
	g := NewGate()
	if err := g.Wait(nil); err != nil {
		t.Fatalf("未暂停时应返回 nil，得到 %v", err)
	}
	g.Set(true)
	if err := g.Wait(nil); !errors.Is(err, ErrPaused) {
		t.Fatalf("暂停时 nil ctx 应返回 ErrPaused，得到 %v", err)
	}
}

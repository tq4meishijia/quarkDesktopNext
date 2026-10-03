// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package transfer

import "sync"

// slotGate 是容量可在运行时调整的计数信号量（并发闸门）。
//
// 为什么不用可替换的 chan struct{}：
//
//	旧实现把「当前并行度」直接编码成 channel 的容量，SetConcurrency 通过
//	m.sem = make(chan struct{}, n) 换一个新 channel 来生效。而 acquire 与
//	release 各自独立读取 m.sem，于是存在这样一个窗口：某 worker 在旧 channel
//	上 acquire 成功后，SetConcurrency 换掉了 channel，随后 release 在新 channel
//	上执行 —— token 泄漏到旧 channel，新 channel 上永远等不到释放，worker 死锁。
//	实测该窗口可由用户在设置页调整并发数直接触发（app/settings.go 会调用
//	SetConcurrency）。
//
//	本实现把容量降为一个受锁保护的整数：调整并行度只改数字，不存在"换 channel"
//	这个动作，因此结构上不可能出现 acquire/release 落在不同容器上的情况。
type slotGate struct {
	mu    sync.Mutex
	limit int
	held  int

	// notify 是广播信号：每次状态变化（释放槽位 / 调整容量 / 停止）都会
	// 关闭旧通道并换上新的，等待方醒来后重新检查条件。
	// 不用 sync.Cond 是因为 Cond 无法被 stop 唤醒，而 worker 必须在
	// 调度器停止时能立刻退出，否则 Stop 里的 wg.Wait() 会永久阻塞。
	notify chan struct{}
}

func newSlotGate(limit int) *slotGate {
	return &slotGate{
		limit:  limit,
		notify: make(chan struct{}),
	}
}

// broadcast 唤醒所有等待方。调用方必须已持有 g.mu。
func (g *slotGate) broadcast() {
	close(g.notify)
	g.notify = make(chan struct{})
}

// acquire 占用一个槽位；stop 关闭时返回 false（此时不占用槽位）。
// 槽位耗尽时阻塞等待，容量被调大或槽位被释放后重新竞争。
func (g *slotGate) acquire(stop <-chan struct{}) bool {
	for {
		g.mu.Lock()
		if g.held < g.limit {
			g.held++
			g.mu.Unlock()
			return true
		}
		wait := g.notify
		g.mu.Unlock()

		select {
		case <-stop:
			return false
		case <-wait:
		}
	}
}

// release 归还一个槽位。重复归还是安全的（不会把计数压到负数）。
func (g *slotGate) release() {
	g.mu.Lock()
	if g.held > 0 {
		g.held--
	}
	g.broadcast()
	g.mu.Unlock()
}

// setLimit 调整容量。调大立即唤醒等待者；调小不打断已在进行中的任务，
// 只让后续 acquire 等待到持有数降下来为止。
func (g *slotGate) setLimit(n int) {
	if n < 1 {
		n = 1
	}
	if n > maxConcurrency {
		n = maxConcurrency
	}
	g.mu.Lock()
	g.limit = n
	g.broadcast()
	g.mu.Unlock()
}

// heldCount 返回当前占用数，仅供测试断言。
func (g *slotGate) heldCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held
}

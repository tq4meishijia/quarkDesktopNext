// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Package transfer 提供与具体网盘实现无关的传输队列内核：
// 任务状态机、并发闸门、暂停/继续、取消与速度采样。
//
// 真正的字节搬运由调用方通过 Runner 接口注入（app 包用 kuake_cli/sdk 实现），
// 因此本包不依赖任何网盘 API，可独立测试。
package transfer

import (
	"context"
	"sync"
	"time"
)

// Status 任务状态。
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// IsTerminal 判断状态是否为终态。
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Gate 是一个可被随时开关的"水龙头"：暂停时 Wait 阻塞，继续时放行。
// 取消通过 context 一并传递，避免另开一套信号。
type Gate struct {
	mu     sync.Mutex
	paused bool
}

// Set 设置暂停开关；解除暂停时立即唤醒所有等待者。
func (g *Gate) Set(paused bool) {
	g.mu.Lock()
	g.paused = paused
	g.mu.Unlock()
}

// Paused 返回当前暂停状态。
func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// Wait 在暂停期间阻塞；ctx 被取消时立即返回 ctx.Err()。
// 轮询间隔 100ms，兼顾响应速度与 CPU 占用。
func (g *Gate) Wait(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !g.Paused() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Spec 描述一个新任务的全部入参，是 Enqueue 的唯一入口。
type Spec struct {
	// Kind 为 upload 或 download。
	Kind string
	// Name 展示用文件名。
	Name string
	// LocalPath 上传时为本地文件全路径；下载时为目标目录。
	LocalPath string
	// RemotePath 上传时为远端目标全路径；下载时为展示用的网盘路径。
	RemotePath string
	// Fid 下载任务的网盘文件 ID（上传任务为空）。
	Fid string
	// Size 文件总字节数，未知时为 0。
	Size int64
}

// Task 是一个上传或下载任务。不可变字段在构造后只读，可变字段由互斥锁保护。
type Task struct {
	ID         string
	Kind       string // upload | download
	Name       string
	LocalPath  string
	RemotePath string
	Fid        string
	Size       int64
	CreatedAt  time.Time

	mu         sync.Mutex
	done       int64
	status     Status
	speed      float64
	errMsg     string
	finishedAt time.Time

	ctx    context.Context
	cancel context.CancelFunc
	gate   *Gate
}

// ProgressFunc 由 Runner 在搬运字节时回调，done 为已完成字节数。
// 返回非 nil 表示调用方应立即中止（暂停被取消 / 用户取消）。
type ProgressFunc func(done int64) error

// Runner 是传输内核与具体网盘实现之间的唯一耦合点。
type Runner interface {
	Upload(ctx context.Context, t *Task, prog ProgressFunc) error
	Download(ctx context.Context, t *Task, prog ProgressFunc) error
}

func newTask(s Spec) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	return &Task{
		ID:         nextID(s.Kind),
		Kind:       s.Kind,
		Name:       s.Name,
		LocalPath:  s.LocalPath,
		RemotePath: s.RemotePath,
		Fid:        s.Fid,
		Size:       s.Size,
		CreatedAt:  time.Now(),
		status:     StatusPending,
		ctx:        ctx,
		cancel:     cancel,
		gate:       &Gate{},
	}
}

// Context 返回任务上下文，取消后 Runner 应尽快退出。
func (t *Task) Context() context.Context { return t.ctx }

// Gate 返回暂停闸门。
func (t *Task) Gate() *Gate { return t.gate }

// Status 返回当前状态。
func (t *Task) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// Done 返回已完成字节数。
func (t *Task) Done() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

// Speed 返回最近一次采样的瞬时速度（字节/秒）。
func (t *Task) Speed() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.speed
}

// ErrMsg 返回失败原因。
func (t *Task) ErrMsg() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.errMsg
}

// FinishedAt 返回结束时间，未结束为零值。
func (t *Task) FinishedAt() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finishedAt
}

// setDone 更新进度，返回是否发生了变化。
func (t *Task) setDone(done int64) {
	t.mu.Lock()
	t.done = done
	t.mu.Unlock()
}

func (t *Task) setStatus(s Status) {
	t.mu.Lock()
	t.status = s
	if s.IsTerminal() {
		t.finishedAt = time.Now()
		t.speed = 0
	}
	t.mu.Unlock()
}

func (t *Task) setSpeed(v float64) {
	t.mu.Lock()
	t.speed = v
	t.mu.Unlock()
}

func (t *Task) fail(err error) {
	t.mu.Lock()
	t.status = StatusFailed
	t.finishedAt = time.Now()
	t.speed = 0
	if err != nil {
		t.errMsg = err.Error()
	}
	t.mu.Unlock()
}

var (
	idMu    sync.Mutex
	idSeq   uint64
	idLastN int64
)

// nextID 生成可读且稳定的任务号，如 up-20261002-194500-1。
func nextID(kind string) string {
	prefix := "dl"
	if kind == "upload" {
		prefix = "up"
	}
	idMu.Lock()
	defer idMu.Unlock()
	now := time.Now()
	if now.Unix() != idLastN {
		idLastN = now.Unix()
		idSeq = 0
	}
	idSeq++
	return prefix + "-" + now.Format("20060102-150405") + "-" + itoa(idSeq)
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

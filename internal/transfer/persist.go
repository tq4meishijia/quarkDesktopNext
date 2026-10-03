// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 任务持久化。
//
// 为什么需要：原先任务只存在于内存，应用重启后全部丢失，用户看不到任何
// 历史记录，也无法知道上次那几个大文件传到哪了。
//
// 恢复语义（重要）：重启后**不自动续传**，只把任务恢复成「已中断」状态。
// 理由是自动续传会在用户不知情时发起大量网络请求（可能产生流量与费用），
// 与「用户点开始才下载」的预期相悖。用户点 Resume 即可续传 ——
// 内建下载器有 .part + .state 机制，SDK 上传有状态文件，都能接上。
//
// 落盘时机：状态变化时异步落盘（去抖），避免每个进度回调都写文件。
// 刻意**不落盘进度百分比**：进度频繁变化且恢复价值低（内建下载器自己的
// state 文件已经记了已完成分片），落它只会让写放大。

// persistedTask 是任务的落盘形态。
//
// 只保存恢复必需的字段：可重新入队的 Spec + 展示所需信息 + 终态。
// 不保存 ctx / cancel / gate / done / speed —— 这些都是进程内状态。
type persistedTask struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	LocalPath  string    `json:"localPath,omitempty"`
	Dest       string    `json:"dest,omitempty"`
	RemotePath string    `json:"remotePath,omitempty"`
	Fid        string    `json:"fid,omitempty"`
	Size       int64     `json:"size"`
	Engine     string    `json:"engine,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	Status     Status    `json:"status"`
	ErrMsg     string    `json:"errMsg,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Interrupted 标记「本次恢复来自上次进程的残留」。
	Interrupted bool `json:"interrupted"`
}

// persistedState 是落盘文件的整体结构。
type persistedState struct {
	Version int             `json:"version"`
	Tasks   []persistedTask `json:"tasks"`
}

// currentPersistVersion 是落盘格式版本。
// 结构不兼容变更时递增，旧文件会被忽略而不是误解析。
const currentPersistVersion = 1

// persister 负责任务落盘与恢复。
type persister struct {
	mu     sync.Mutex
	path   string
	dirty  bool
	closed bool
	lastWr time.Time
	// writeDelay 是去抖间隔：状态变化后这段时间内的重复变化不重复写。
	writeDelay time.Duration
	// wake 通知等待中的落盘协程。
	wake chan struct{}
	// done 落盘协程退出信号。
	done chan struct{}
	// enabled 为 false 时完全不落盘（测试可用）。
	enabled bool
	// snapshot 由 Manager 注入，返回当前任务快照。
	snapshot func() *persistedState
}

// newPersister 建一个落盘器；path 为空或 enabled=false 时禁用。
func newPersister(path string, enabled bool) *persister {
	if path == "" {
		enabled = false
	}
	return &persister{
		path:       path,
		writeDelay: 300 * time.Millisecond,
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		enabled:    enabled,
	}
}

// DefaultStatePath 返回默认的任务状态文件路径。
// 由调用方（app 层）决定基目录，这里只做拼接。
func DefaultStatePath(dir string) string {
	return filepath.Join(dir, "transfer-tasks.json")
}

// start 启动后台落盘协程。
func (p *persister) start() {
	if !p.enabled {
		return
	}
	go p.loop()
}

// close 停止后台协程并做最后一次同步落盘。
func (p *persister) close() {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	close(p.done)
	// 最后再写一次，确保退出前的状态落盘。
	p.writeNow()
}

// mark 标记有变化，触发（去抖后的）落盘。
func (p *persister) mark() {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	p.dirty = true
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default: // 已有待处理信号
	}
}

func (p *persister) loop() {
	for {
		select {
		case <-p.done:
			return
		case <-p.wake:
			// 去抖：等待一小段时间，合并密集的状态变化。
			t := time.NewTimer(p.writeDelay)
		debounce:
			for {
				select {
				case <-t.C:
					break debounce
				case <-p.done:
					t.Stop()
					return
				case <-p.wake:
					// 又一次变化，重置计时。
					//
					// 注意：这里**不能**写「if !t.Stop() { <-t.C }」——
					// 那种写法在计时器已到期且值已被 select 消费时会永久阻塞
					// （channel 已空，无人再写），配合 close() 关闭 done 的
					// 时机就是一次死锁，且 -race 未必能报出（是逻辑死锁，
					// 不是数据竞争）。Stop 后直接 Reset 即可，Go 1.23+
					// 的 Timer 保证 Reset 前已 Stop 不会残留值。
					t.Stop()
					t.Reset(p.writeDelay)
				}
			}
			p.writeNow()
		}
	}
}

// writeNow 立即落盘一次。
func (p *persister) writeNow() {
	if !p.enabled {
		return
	}
	// 注意：以下读取（dirty / path / snapshot）必须在锁内完成。
	// writeNow 有两个并发来源 —— 去抖协程（loop）与 Stop() 里的同步落盘，
	// 二者会同时进入；把字段读到锁外会与 close()/mark() 形成数据竞争
	// （-race 会直接报出来）。
	p.mu.Lock()
	if !p.dirty {
		p.mu.Unlock()
		return
	}
	p.dirty = false
	p.lastWr = time.Now()
	path := p.path
	snapshotFn := p.snapshot
	p.mu.Unlock()

	if snapshotFn == nil {
		return
	}
	snapshot := snapshotFn()
	if snapshot == nil {
		return
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	// 原子写：临时文件 + rename，避免崩溃留下半截 JSON
	// （本项目的 config 包也采用同样做法）。
	tmp, err := os.CreateTemp(dir, ".transfer-tasks*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 失败路径清残留
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows 上目标存在时 Rename 会失败，先删再改。
		if os.Remove(path) == nil {
			_ = os.Rename(tmpName, path)
		}
	}
}

// load 读取历史任务；文件不存在或损坏时返回 nil（不阻断启动）。
func (p *persister) load() []persistedTask {
	if !p.enabled {
		return nil
	}
	p.mu.Lock()
	path := p.path
	p.mu.Unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st persistedState
	if err := json.Unmarshal(raw, &st); err != nil {
		// 损坏就当没有，不阻断启动（与 config.Load 的容错策略一致）。
		return nil
	}
	if st.Version != currentPersistVersion {
		return nil
	}
	return st.Tasks
}

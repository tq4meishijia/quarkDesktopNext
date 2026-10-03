// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// pollInterval 是外部下载器进度采样间隔：外部程序没有统一的进度接口，
// 只能观察目标文件长了多少。
const pollInterval = 500 * time.Millisecond

// runProcess 启动外部命令行下载器，等到它退出；期间按目标文件大小汇报进度。
//
// 为什么用「看文件大小」而不是解析 stdout：aria2 / wget / curl 的进度输出格式各不相同，
// 且都可能被用户重定向，静默观察产物是唯一在三者上都成立的做法。
func runProcess(ctx context.Context, d Descriptor, bin string, args []string, r Request) error {
	if err := os.MkdirAll(filepath.Dir(r.Dest), 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// 不弹控制台窗口：命令行下载器应当安静地在后台跑。
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &sysProcHidden
	}
	logPath := filepath.Join(os.TempDir(), "quark-desktop-"+d.ID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		defer logFile.Close()
		cmd.Stdout, cmd.Stderr = logFile, logFile
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", d.Label, err)
	}

	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		var prev int64
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				cur := fileSize(r.Dest)
				if r.Progress != nil && cur >= prev {
					_ = r.Progress(cur)
				}
				prev = cur
			}
		}
	}()

	waitErr := cmd.Wait()
	close(stop)
	<-finished

	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case waitErr != nil:
		return fmt.Errorf("%s 下载失败: %v（详细输出见 %s）", d.Label, waitErr, logPath)
	}
	if r.Progress != nil {
		_ = r.Progress(fileSize(r.Dest))
	}
	return nil
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// launchDetached 启动 GUI 下载器后不等待它退出：这类程序会长期驻留，
// 等进程结束没有意义，进程回收交给系统。
func launchDetached(ctx context.Context, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动外部下载器失败: %w", err)
	}
	return nil
}

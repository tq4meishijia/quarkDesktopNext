// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"os"
	"path/filepath"
	"time"

	"kuake-desktop/internal/config"
)

// currentSettings 返回当前生效设置的快照（并发安全）。
func (a *App) currentSettings() config.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// GetSettings 供设置页初始化表单。
func (a *App) GetSettings() config.Settings {
	return a.currentSettings()
}

// SaveSettings 保存设置：落盘后立即把并发数等运行时项应用到传输内核。
func (a *App) SaveSettings(s config.Settings) (config.Settings, error) {
	if err := a.store.UpdateSettings(s); err != nil {
		return a.currentSettings(), err
	}
	a.mu.Lock()
	a.settings = a.store.Settings()
	applied := a.settings
	a.mu.Unlock()
	a.tm.SetConcurrency(applied.Concurrency)
	return applied, nil
}

// ConfigDir 返回配置文件所在目录，设置页用于展示。
func (a *App) ConfigDir() string {
	return a.store.Dir()
}

// EnsureDownloadDir 检查（必要时创建）下载目录，返回绝对路径。
// 设置页保存前与任务入队前都会调用，避免下载时才报路径不存在。
func (a *App) EnsureDownloadDir(dir string) (string, error) {
	d := dir
	if d == "" {
		d = a.currentSettings().DownloadDir
	}
	if d == "" {
		home, _ := os.UserHomeDir()
		d = filepath.Join(home, "Downloads", "QuarkDrive")
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// nowUnix 当前 Unix 秒，用于新建文件夹等本地构造条目。
func nowUnix() int64 { return time.Now().Unix() }

// configCredentialsEmpty 返回一份空凭证，用于登出时清空会话文件。
func configCredentialsEmpty() config.Credentials { return config.Credentials{} }

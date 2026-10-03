// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"kuake-desktop/internal/config"
	"kuake-desktop/internal/engine"
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

// DownloaderInfo 是设置页展示用的下载器视图：注册表条目 + 本机探测结果。
type DownloaderInfo struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Note    string   `json:"note"`
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	Ready   bool     `json:"ready"`
	Default []string `json:"defaultArgs"`
}

// ListDownloaders 返回全部可选下载器，并标注哪些在本机已就绪。
// 设置页据此提示「未检测到」，避免用户选了才发现不能用。
func (a *App) ListDownloaders() []DownloaderInfo {
	list := engine.List()
	out := make([]DownloaderInfo, 0, len(list))
	for _, d := range list {
		p, ok := d.Available()
		// 自定义了可执行文件路径时按设置判断，而不是自动探测
		if custom := strings.TrimSpace(a.currentSettings().DownloaderExec); custom != "" && d.ID != engine.BuiltinID {
			if st, err := os.Stat(custom); err == nil && !st.IsDir() {
				p, ok = custom, true
			} else {
				p, ok = "", false
			}
		}
		out = append(out, DownloaderInfo{
			ID:      d.ID,
			Label:   d.Label,
			Note:    d.Note,
			Kind:    d.Kind,
			Path:    p,
			Ready:   ok,
			Default: d.Args,
		})
	}
	return out
}

// PickDownloaderExec 让用户手动指定外部下载器的可执行文件。
func (a *App) PickDownloaderExec() (string, error) {
	if a.ctx == nil {
		return "", errors.New("窗口尚未就绪")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择下载器可执行文件",
	})
}

// RevealLocalDir 在系统文件管理器里打开一个本地目录（下载完成后使用）。
func (a *App) RevealLocalDir(dir string) error {
	d := strings.TrimSpace(dir)
	if d == "" {
		return errors.New("路径为空")
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return errors.New("目录不存在：" + abs)
	}
	return openInFileManager(abs)
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

// copyToClipboard 把文本写入系统剪贴板：外部 GUI 下载器接管前，
// 用户可以直接在下载器里粘贴这个直链。
func (a *App) copyToClipboard(text string) {
	if a.ctx == nil || text == "" {
		return
	}
	runtime.ClipboardSetText(a.ctx, text)
}

// openInFileManager 在系统文件管理器里打开目录（存在则选中该目录）。
func openInFileManager(abs string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", abs)
	case "darwin":
		cmd = exec.Command("open", abs)
	default:
		cmd = exec.Command("xdg-open", abs)
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Start()
}

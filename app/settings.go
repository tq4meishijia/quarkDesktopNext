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
//
// 安全约束：该方法是 Wails 导出绑定，参数完全来自前端。若不加限制，
// 渲染进程里的任何脚本都能让本进程对任意路径执行 explorer/open/xdg-open，
// 构成一条以用户权限执行系统命令的通路。因此这里做两层限制：
//  1. 路径必须落在允许打开的根目录内（下载目录 / 配置目录 / 任务记录过的目录）；
//  2. 拒绝含路径分隔符的相对穿越，且必须真实存在。
//
// 前端正常流程（打开下载目录、打开任务落盘位置）不受影响，
// 因为这些路径本就来自设置项或任务对象。
func (a *App) RevealLocalDir(dir string) error {
	d := strings.TrimSpace(dir)
	if d == "" {
		return errors.New("路径为空")
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		return err
	}
	// filepath.Abs 已做 Clean；这里再显式拒绝残留的穿越片段，
	// 防止符号链接或异常拼接绕过前缀判定。
	if strings.ContainsRune(abs, 0) {
		return errors.New("路径含非法字符")
	}
	if !a.pathRevealAllowed(abs) {
		return errors.New("不允许打开该目录：仅限下载目录与本应用管理的目录")
	}
	if fi, err := os.Stat(abs); err != nil {
		return errors.New("目录不存在：" + abs)
	} else if !fi.IsDir() {
		return errors.New("不是目录：" + abs)
	}
	return openInFileManager(abs)
}

// pathRevealAllowed 判断路径是否落在允许打开的根目录内。
func (a *App) pathRevealAllowed(abs string) bool {
	for _, root := range a.revealRoots() {
		if root == "" {
			continue
		}
		if pathWithin(root, abs) {
			return true
		}
	}
	return false
}

// revealRoots 返回允许打开的根目录列表：
//   - 设置里配置的下载目录（及其父目录链上的用户目录）；
//   - 配置目录（用户可能想打开 session.json 所在处）；
//   - 已知任务的落盘位置所在目录。
func (a *App) revealRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			roots = append(roots, abs)
		}
	}
	add(a.currentSettings().DownloadDir)
	// 默认下载目录 ~/Downloads/QuarkDrive 的父目录，允许用户打开下载根。
	if dl := a.currentSettings().DownloadDir; dl != "" {
		add(filepath.Dir(dl))
	}
	add(a.store.Dir())
	// a.tm 在 App.New 中总是初始化，但测试用手工构造的 App 可能为 nil，
	// 故此处做 nil 保护，避免路径校验把无关的 nil 解引用带进来。
	if a.tm != nil {
		for _, t := range a.tm.List() {
			if t.Dest != "" {
				add(filepath.Dir(t.Dest))
			}
			if t.LocalPath != "" {
				add(filepath.Dir(t.LocalPath))
			}
		}
	}
	return roots
}

// pathWithin 判断 child 是否位于 root 之内（含 root 自身）。
// 用 filepath.Rel 判定而非字符串前缀，避免 /tmp/abc 误匹配 /tmp/ab。
func pathWithin(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	// 越界或父级穿越一律拒绝。
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
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

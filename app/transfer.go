// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zhangjingwei/kuake_cli/sdk"

	"kuake-desktop/internal/engine"
	"kuake-desktop/internal/transfer"
)

// DownloadItem 描述一个待下载条目，由文件浏览页的多选结果构造。
type DownloadItem struct {
	Fid        string `json:"fid"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	RemotePath string `json:"remotePath"`
}

// 取消/暂停说明：
//   - 内建下载器直接消费任务 context 与暂停闸门，暂停/取消都立即生效；
//   - 上传复用 sdk.UploadFile（秒传、分片、断点续传由 SDK 负责），
//     但 SDK 的进度回调没有中断钩子，取消会在下一个进度回调边界生效；
//   - 外部下载器（IDM/迅雷/浏览器等）不受本进程控制，任务会立刻标记为「已移交」。

// sdkRunner 是 transfer.Runner 的实现。
//
// 刻意使用未导出类型：Wails 会把 *App 的所有导出方法生成前端绑定，
// 若让 *App 自己实现 Runner，Upload/Download 这两个只应被 Go 内部调用的方法
// 就会泄漏到前端 API 里（且参数含 *transfer.Task，前端根本无法构造）。
type sdkRunner struct {
	app *App
}

// Upload 实现 transfer.Runner：把本地文件传到远端路径。
func (r *sdkRunner) Upload(ctx context.Context, t *transfer.Task, prog transfer.ProgressFunc) error {
	qc, err := r.app.requireClient()
	if err != nil {
		return err
	}
	policy := sdk.UploadPolicy(r.app.currentSettings().UploadPolicy)

	cb := func(p *sdk.UploadProgress) {
		if p == nil {
			return
		}
		// 先汇报进度，再过暂停闸门：暂停时阻塞在这里，SDK 的读取循环随之停住。
		_ = prog(p.Uploaded)
		_ = t.Gate().Wait(ctx)
	}

	resp, err := qc.UploadFile(t.LocalPath, t.RemotePath, cb, &sdk.UploadOptions{Policy: policy})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Success {
		return errors.New(respMessage(resp))
	}
	return nil
}

// Download 实现 transfer.Runner。
//
// 走 internal/engine：内建下载器是多连接分片 + 断点续传；选了外部下载器则把直链
// 交给它并立刻返回 transfer.ErrHandedOff（外部程序不接受外部 context，
// 继续等待只会让暂停/取消失真）。
func (r *sdkRunner) Download(ctx context.Context, t *transfer.Task, prog transfer.ProgressFunc) error {
	qc, err := r.app.requireClient()
	if err != nil {
		return err
	}
	dlURL, err := qc.GetDownloadURL(t.Fid)
	if err != nil {
		return err
	}
	set := r.app.currentSettings()

	desc, ok := engine.Lookup(set.Downloader)
	if !ok {
		return errors.New("未知的下载器：" + set.Downloader)
	}
	req := engine.Request{
		URL:      dlURL,
		Dest:     t.Dest,
		Size:     t.Size,
		Cookie:   cookieHeader(qc.GetCookies()),
		Referer:  sdk.PAN_DOMAIN + "/",
		UA:       engine.UA,
		Segments: set.Segments,
		Progress: prog,
		Gate:     func() error { return t.Gate().Wait(ctx) },
	}

	if desc.ID == engine.BuiltinID {
		return (&engine.Native{}).Fetch(ctx, req)
	}
	return engine.Run(ctx, desc, engine.Options{
		Exec:    set.DownloaderExec,
		Args:    set.DownloaderArgs,
		CopyURL: r.app.copyToClipboard,
	}, req)
}

// cookieHeader 把 cookie 字典拼成请求头用的 "a=1; b=2" 形式，键名排序保证可复现。
func cookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	keys := make([]string, 0, len(cookies))
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}

// ---------- 前端可直接调用的任务 API ----------

// PickUploadFiles 弹出系统多选文件对话框，返回本地文件绝对路径列表。
func (a *App) PickUploadFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, errors.New("窗口尚未就绪")
	}
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
}

// PickDownloadDir 弹出系统目录对话框，用于设置默认下载路径。
func (a *App) PickDownloadDir() (string, error) {
	if a.ctx == nil {
		return "", errors.New("窗口尚未就绪")
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择默认下载目录",
		DefaultDirectory: a.currentSettings().DownloadDir,
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// EnqueueUploads 把一批本地文件加入上传队列。
func (a *App) EnqueueUploads(localPaths []string, remoteDir string) ([]TaskDTO, error) {
	if _, err := a.requireClient(); err != nil {
		return nil, err
	}
	if len(localPaths) == 0 {
		return nil, errors.New("没有选择文件")
	}
	dir := normalizeDir(remoteDir)
	out := make([]TaskDTO, 0, len(localPaths))
	for _, lp := range localPaths {
		lp = strings.TrimSpace(lp)
		if lp == "" {
			continue
		}
		st, err := os.Stat(lp)
		if err != nil {
			a.notify("error", "无法读取文件："+filepath.Base(lp))
			continue
		}
		if st.IsDir() {
			a.notify("warn", "暂不支持上传整个文件夹："+filepath.Base(lp))
			continue
		}
		name := filepath.Base(lp)
		t := a.tm.Enqueue(transfer.Spec{
			Kind:       "upload",
			Name:       name,
			LocalPath:  lp,
			Dest:       lp,
			RemotePath: joinRemote(dir, name),
			Size:       st.Size(),
			Engine:     "内建上传",
		})
		out = append(out, taskToDTO(t))
	}
	if len(out) == 0 {
		return nil, errors.New("没有可上传的文件")
	}
	return out, nil
}

// EnqueueDownloads 把一批网盘文件加入下载队列。
// dir 为空时用设置页的默认下载路径；非空时本次下载落到该目录（界面的「下载到…」）。
// keepTree 为 true 时按网盘目录层级建子目录。
func (a *App) EnqueueDownloads(items []DownloadItem, dir string, keepTree bool) ([]TaskDTO, error) {
	if _, err := a.requireClient(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("没有选择文件")
	}
	set := a.currentSettings()
	base := strings.TrimSpace(dir)
	if base == "" {
		base = set.DownloadDir
	}
	if strings.TrimSpace(base) == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "Downloads", "QuarkDrive")
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	desc, ok := engine.Lookup(set.Downloader)
	if !ok {
		return nil, errors.New("未知的下载器：" + set.Downloader)
	}

	out := make([]TaskDTO, 0, len(items))
	var lastErr error
	for _, it := range items {
		if it.Fid == "" {
			continue
		}
		name := strings.TrimSpace(it.Name)
		if name == "" {
			name = it.Fid
		}
		target := absBase
		if keepTree {
			target = uniqueRemoteDir(absBase, dirOf(it.RemotePath))
		}
		dest, derr := resolveDest(target, name, set.SameName)
		if derr != nil {
			a.notify("warn", name+"："+derr.Error())
			lastErr = derr
			continue
		}
		t := a.tm.Enqueue(transfer.Spec{
			Kind:       "download",
			Name:       name,
			LocalPath:  target,
			Dest:       dest,
			RemotePath: it.RemotePath,
			Fid:        it.Fid,
			Size:       it.Size,
			Engine:     desc.Label,
		})
		out = append(out, taskToDTO(t))
	}
	if len(out) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("没有可下载的文件")
	}
	return out, nil
}

// ListTasks 返回全部任务（含已完成），用于页面首次渲染。
func (a *App) ListTasks() []TaskDTO {
	list := a.tm.List()
	out := make([]TaskDTO, 0, len(list))
	for _, t := range list {
		out = append(out, taskToDTO(t))
	}
	return out
}

// PauseTask 暂停单个任务。
func (a *App) PauseTask(id string) (bool, error) {
	if err := a.tm.Pause(id); err != nil {
		return false, err
	}
	return true, nil
}

// ResumeTask 继续单个任务。
func (a *App) ResumeTask(id string) (bool, error) {
	if err := a.tm.Resume(id); err != nil {
		return false, err
	}
	return true, nil
}

// CancelTask 取消单个任务。
func (a *App) CancelTask(id string) (bool, error) {
	if err := a.tm.Cancel(id); err != nil {
		return false, err
	}
	return true, nil
}

// RetryTask 重试失败或已取消的任务。
func (a *App) RetryTask(id string) (bool, error) {
	if err := a.tm.Retry(id); err != nil {
		return false, err
	}
	return true, nil
}

// PauseAllTasks 暂停全部进行中的任务。
func (a *App) PauseAllTasks() int { return a.tm.PauseAll() }

// ResumeAllTasks 继续全部暂停的任务。
func (a *App) ResumeAllTasks() int { return a.tm.ResumeAll() }

// CancelAllTasks 取消全部未完成的任务。
func (a *App) CancelAllTasks() int { return a.tm.CancelAll() }

// ClearCompletedTasks 清除已完成/失败/取消的任务。
func (a *App) ClearCompletedTasks() int { return a.tm.ClearCompleted() }

// RevealLocal 校验本地路径存在，供下载完成后「打开所在目录」按钮做前置检查。
func (a *App) RevealLocal(localPath string) (string, error) {
	p := strings.TrimSpace(localPath)
	if p == "" {
		return "", errors.New("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", errors.New("文件不存在：" + abs)
	}
	return abs, nil
}

// OpenTaskDest 在系统文件管理器里打开某个下载任务的落盘目录。
func (a *App) OpenTaskDest(id string) (bool, error) {
	t, ok := a.tm.Get(id)
	if !ok {
		return false, transfer.ErrNotFound
	}
	dir := t.Dest
	if dir == "" {
		dir = t.LocalPath
	}
	if err := a.RevealLocalDir(dir); err != nil {
		return false, err
	}
	return true, nil
}

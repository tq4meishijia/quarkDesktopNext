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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zhangjingwei/kuake_cli/sdk"

	"kuake-desktop/internal/transfer"
)

// DownloadItem 描述一个待下载条目，由文件浏览页的多选结果构造。
type DownloadItem struct {
	Fid        string `json:"fid"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	RemotePath string `json:"remotePath"`
}

// 取消/暂停说明（与 README「已知限制」一致）：
//   - 下载走本文件里的自建 HTTP 循环，暂停与取消都能立即生效；
//   - 上传复用 sdk.UploadFile（秒传、分片、断点续传由 SDK 负责），
//     但 SDK 的进度回调没有中断钩子，取消会在下一个进度回调边界生效。

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
// 这里没有直接用 sdk.DownloadFile，而是用 sdk.GetDownloadURL 拿到直链后自建下载循环，
// 原因是 GUI 需要真正的暂停/取消能力，而 SDK 的下载循环不接受外部 context。
// 请求头（UA / Referer / Cookie）与 SDK 保持一致，避免被 OSS 边缘策略拒绝。
func (r *sdkRunner) Download(ctx context.Context, t *transfer.Task, prog transfer.ProgressFunc) error {
	qc, err := r.app.requireClient()
	if err != nil {
		return err
	}
	dlURL, err := qc.GetDownloadURL(t.Fid)
	if err != nil {
		return err
	}
	dest := filepath.Join(t.LocalPath, t.Name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("创建本地目录失败: %w", err)
	}

	// 先写 .kuake-part，成功后原子改名，避免中断留下半截文件。
	part := dest + ".kuake-part"
	out, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}
	committed := false
	defer func() {
		out.Close()
		if !committed {
			_ = os.Remove(part)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Referer", sdk.PAN_DOMAIN+"/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	if cookies := qc.GetCookies(); len(cookies) > 0 {
		keys := make([]string, 0, len(cookies))
		for k := range cookies {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+cookies[k])
		}
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 256*1024)
	var written int64
	lastReport := time.Now()
	for {
		// 暂停闸门放在读之前：暂停时不再向服务端要数据。
		if err := t.Gate().Wait(ctx); err != nil {
			return err
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			if time.Since(lastReport) >= 200*time.Millisecond {
				lastReport = time.Now()
				if perr := prog(written); perr != nil {
					return perr
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}

	if perr := prog(written); perr != nil {
		return perr
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dest) // Windows 下 Rename 不能覆盖已存在文件
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("写入最终文件失败: %w", err)
	}
	committed = true
	return nil
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
			RemotePath: joinRemote(dir, name),
			Size:       st.Size(),
		})
		out = append(out, taskToDTO(t))
	}
	if len(out) == 0 {
		return nil, errors.New("没有可上传的文件")
	}
	return out, nil
}

// EnqueueDownloads 把一批网盘文件加入下载队列，落盘目录取设置页的下载路径。
func (a *App) EnqueueDownloads(items []DownloadItem) ([]TaskDTO, error) {
	if _, err := a.requireClient(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("没有选择文件")
	}
	dir := a.currentSettings().DownloadDir
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	out := make([]TaskDTO, 0, len(items))
	for _, it := range items {
		if it.Fid == "" {
			continue
		}
		name := strings.TrimSpace(it.Name)
		if name == "" {
			name = it.Fid
		}
		t := a.tm.Enqueue(transfer.Spec{
			Kind:       "download",
			Name:       name,
			LocalPath:  dir,
			RemotePath: it.RemotePath,
			Fid:        it.Fid,
			Size:       it.Size,
		})
		out = append(out, taskToDTO(t))
	}
	if len(out) == 0 {
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

// RevealLocal 校验本地文件是否存在，供下载完成后「打开所在目录」按钮做前置检查。
// 真正的唤起文件管理器由前端自行处理，避免 Go 侧引入平台相关的 exec 调用。
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

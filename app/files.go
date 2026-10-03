// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"errors"
	"strings"

	"github.com/zhangjingwei/kuake_cli/sdk"
)

// 递归搜索的保护阈值：防止一次搜索把整个网盘翻一遍。
const (
	searchMaxDepth = 5
	searchMaxNodes = 3000
	searchMaxHits  = 300
)

// ListDir 列出目录内容。path 传 "/" 或空表示根目录。
func (a *App) ListDir(p string) (DirListing, error) {
	qc, err := a.requireClient()
	if err != nil {
		return DirListing{}, err
	}
	if strings.TrimSpace(p) == "" {
		p = "/"
	}
	items, err := a.listAt(qc, p)
	if err != nil {
		return DirListing{}, err
	}
	// 目录优先、其次按名称升序，保证列表视图稳定。
	sortItems(items)
	return DirListing{Path: p, Items: items}, nil
}

// Search 搜索文件。
//
// kuake_cli/sdk 没有提供服务端搜索接口，这里的实现是客户端侧遍历：
//   - recursive=false：只匹配当前目录；
//   - recursive=true：广度优先下钻，受 searchMaxDepth / searchMaxNodes / searchMaxHits 三重保护，
//     命中上限时 DirListing.ReachedCap 置 true，前端据此提示"结果可能被截断"。
func (a *App) Search(keyword, root string, recursive bool) (DirListing, error) {
	qc, err := a.requireClient()
	if err != nil {
		return DirListing{}, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return DirListing{Path: root, Items: []FileItem{}}, nil
	}
	if strings.TrimSpace(root) == "" {
		root = "/"
	}

	hits := make([]FileItem, 0, 32)
	reachedCap := false

	current, err := a.listAt(qc, root)
	if err != nil {
		return DirListing{}, err
	}
	for _, it := range current {
		if strings.Contains(strings.ToLower(it.Name), kw) {
			hits = append(hits, it)
			if len(hits) >= searchMaxHits {
				reachedCap = true
				break
			}
		}
	}

	if recursive && !reachedCap {
		type level struct {
			fid   string
			path  string
			depth int
		}
		queue := make([]level, 0, 64)
		for _, it := range current {
			if it.IsDir {
				queue = append(queue, level{fid: it.Fid, path: it.Path, depth: 1})
			}
		}
		nodes := 0
		for len(queue) > 0 && !reachedCap {
			lv := queue[0]
			queue = queue[1:]
			if lv.depth > searchMaxDepth {
				continue
			}
			children, lerr := a.listAt(qc, lv.fid)
			if lerr != nil {
				continue // 单个目录失败不影响整体搜索
			}
			nodes += len(children)
			if nodes > searchMaxNodes {
				reachedCap = true
				break
			}
			for _, c := range children {
				if strings.Contains(strings.ToLower(c.Name), kw) {
					hits = append(hits, c)
					if len(hits) >= searchMaxHits {
						reachedCap = true
						break
					}
				}
				if c.IsDir {
					queue = append(queue, level{fid: c.Fid, path: c.Path, depth: lv.depth + 1})
				}
			}
		}
	}

	sortItems(hits)
	return DirListing{Path: root, Items: hits, ReachedCap: reachedCap}, nil
}

// CreateFolder 在 parent 目录下新建文件夹，返回新建条目。
func (a *App) CreateFolder(parent, name string) (FileItem, error) {
	qc, err := a.requireClient()
	if err != nil {
		return FileItem{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return FileItem{}, errors.New("文件夹名称不能为空")
	}
	if strings.Contains(name, "/") {
		return FileItem{}, errors.New("文件夹名称不能包含 /")
	}
	pdirFid, err := a.resolveFid(qc, parent)
	if err != nil {
		return FileItem{}, err
	}
	resp, err := qc.CreateFolder(name, pdirFid)
	if err != nil {
		return FileItem{}, err
	}
	if resp == nil || !resp.Success {
		return FileItem{}, a.respErr(resp)
	}
	fid := ""
	if resp.Data != nil {
		fid, _ = resp.Data["fid"].(string)
	}
	return FileItem{
		Fid:   fid,
		Name:  name,
		Path:  joinRemote(parent, name),
		IsDir: true,
		Mtime: nowUnix(),
	}, nil
}

// Rename 重命名文件/文件夹，newName 只传名称不带路径。
func (a *App) Rename(remotePath, newName string) (bool, error) {
	qc, err := a.requireClient()
	if err != nil {
		return false, err
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return false, errors.New("新名称不能为空")
	}
	resp, err := qc.Rename(remotePath, newName)
	if err != nil {
		return false, err
	}
	if resp == nil || !resp.Success {
		return false, a.respErr(resp)
	}
	return true, nil
}

// Delete 批量删除，返回成功条数。单条失败不中断，最终统一汇报。
func (a *App) Delete(remotePaths []string) (int, error) {
	qc, err := a.requireClient()
	if err != nil {
		return 0, err
	}
	if len(remotePaths) == 0 {
		return 0, errors.New("请先选择要删除的文件")
	}
	// 前置策略校验：命中黑名单的路径直接拒绝，不下发到网盘。
	g := loadGuardConfig()
	if err := g.checkOp("delete"); err != nil {
		return 0, err
	}
	ok := 0
	var lastErr error
	for _, p := range remotePaths {
		if err := g.checkRemotePath(p); err != nil {
			lastErr = err
			continue
		}
		resp, derr := qc.Delete(p)
		if derr != nil {
			lastErr = derr
			continue
		}
		if resp == nil || !resp.Success {
			lastErr = a.respErr(resp)
			continue
		}
		ok++
	}
	if ok == 0 && lastErr != nil {
		return 0, lastErr
	}
	return ok, nil
}

// Move 移动到目标目录（destDir 为目录路径）。
func (a *App) Move(src, destDir string) (bool, error) {
	return a.relocate("move", src, destDir)
}

// Copy 复制到目标目录（destDir 为目录路径）。
func (a *App) Copy(src, destDir string) (bool, error) {
	return a.relocate("copy", src, destDir)
}

func (a *App) relocate(kind, src, destDir string) (bool, error) {
	qc, err := a.requireClient()
	if err != nil {
		return false, err
	}
	src = strings.TrimSpace(src)
	dest := normalizeDir(destDir)
	if src == "" || dest == "" {
		return false, errors.New("源路径与目标目录不能为空")
	}
	if dirOf(src) == dest {
		return false, errors.New("源与目标目录相同")
	}
	// 前置策略校验：源与目标都不允许落在被禁用的远端路径下。
	g := loadGuardConfig()
	if err := g.checkOp(kind); err != nil {
		return false, err
	}
	if err := g.checkRemotePath(src); err != nil {
		return false, err
	}
	if err := g.checkRemotePath(dest); err != nil {
		return false, err
	}
	var resp *sdk.StandardResponse
	if kind == "move" {
		resp, err = qc.Move(src, dest)
	} else {
		resp, err = qc.Copy(src, dest)
	}
	if err != nil {
		return false, err
	}
	if resp == nil || !resp.Success {
		return false, a.respErr(resp)
	}
	return true, nil
}

// ResolveFid 把远端路径解析为 fid（根目录为 "0"），供转存等操作使用。
func (a *App) ResolveFid(remotePath string) (string, error) {
	qc, err := a.requireClient()
	if err != nil {
		return "", err
	}
	return a.resolveFid(qc, remotePath)
}

// resolveFid 内部实现：根目录直接返回 "0"，其余走 GetFileInfo。
func (a *App) resolveFid(qc *sdk.QuarkClient, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "0", nil
	}
	resp, err := qc.GetFileInfo(p)
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success {
		return "", a.respErr(resp)
	}
	if resp.Data == nil {
		return "", errors.New("无法解析目录 fid：返回数据为空")
	}
	fid, _ := resp.Data["fid"].(string)
	if fid == "" {
		return "", errors.New("无法解析目录 fid")
	}
	return fid, nil
}

// listAt 列出某个路径或 fid 下的条目，并统一转换为 FileItem。
func (a *App) listAt(qc *sdk.QuarkClient, pathOrFid string) ([]FileItem, error) {
	resp, err := qc.List(pathOrFid)
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.Success {
		return nil, a.respErr(resp)
	}
	raw, _ := resp.Data["list"].([]sdk.QuarkFileInfo)
	items := make([]FileItem, 0, len(raw))
	for _, f := range raw {
		items = append(items, FileItem{
			Fid:   f.Fid,
			Name:  f.Name,
			Path:  f.Path,
			Size:  f.Size,
			IsDir: f.IsDirectory,
			Mtime: f.ModifyTime,
			Ctime: f.CreateTime,
			Ext:   extOf(f.Name),
		})
	}
	return items, nil
}

// sortItems 目录在前、同名按字典序，保证翻页与刷新后顺序稳定。
func sortItems(items []FileItem) {
	n := len(items)
	for i := 1; i < n; i++ {
		for j := i; j > 0; j-- {
			a, b := items[j-1], items[j]
			if lessFile(b, a) {
				items[j-1], items[j] = b, a
				continue
			}
			break
		}
	}
}

func lessFile(x, y FileItem) bool {
	if x.IsDir != y.IsDir {
		return x.IsDir
	}
	return strings.ToLower(x.Name) < strings.ToLower(y.Name)
}

// normalizeDir 规整目录路径为以 / 结尾的形式，SDK 的移动/复制依赖这个约定。
func normalizeDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "/"
	}
	dir = strings.ReplaceAll(dir, "\\", "/")
	if !strings.HasPrefix(dir, "/") {
		dir = "/" + dir
	}
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	return dir
}

// respMessage 从 StandardResponse 提取可读错误。
func respMessage(r *sdk.StandardResponse) string {
	if r == nil {
		return "请求失败：空响应"
	}
	if r.Message != "" {
		if r.Code != "" && r.Code != "OK" {
			return r.Message + "（" + r.Code + "）"
		}
		return r.Message
	}
	if r.Code != "" {
		return "请求失败：" + r.Code
	}
	return "请求失败"
}

// respErr 构造业务失败错误，并顺带判定是否由会话失效引起。
//
// 会话失效时（凭证静默过期、接口权限变化）走 invalidateSession 统一收口：
// 内存态清空 + 删 session.json + 推 auth:changed 事件，
// 前端据此跳回登录页，无需自己识别错误文案。
func (a *App) respErr(resp *sdk.StandardResponse) error {
	a.noteAuthFailure(resp, nil)
	return errors.New(respMessage(resp))
}

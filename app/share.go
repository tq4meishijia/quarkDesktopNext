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
)

// ParseShare 解析分享链接文本，返回可直接转存的预览结构。
//
// 链路与 CLI 的 share-save 一致：
// GetShareInfo（提取 pwd_id / 提取码）→ GetShareStoken（换 stoken）→ GetShareList（列目录）。
func (a *App) ParseShare(text string) (SharePreview, error) {
	qc, err := a.requireClient()
	if err != nil {
		return SharePreview{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return SharePreview{}, errors.New("请粘贴分享链接")
	}
	info, err := qc.GetShareInfo(text)
	if err != nil {
		return SharePreview{}, errors.New("链接解析失败：" + err.Error())
	}
	if info == nil || info.PwdID == "" {
		return SharePreview{}, errors.New("未能从文本中识别分享 ID")
	}

	stokenData, err := qc.GetShareStoken(info.PwdID, info.Passcode)
	if err != nil {
		return SharePreview{}, errors.New("获取分享凭证失败：" + err.Error())
	}
	stoken := ""
	if stokenData != nil {
		stoken = mapStr(stokenData, "stoken")
	}
	if stoken == "" {
		return SharePreview{}, errors.New("分享凭证为空，可能需要提取码或链接已失效")
	}

	listData, err := qc.GetShareList(info.PwdID, stoken, "0", 1, 100, "file_name", "asc")
	if err != nil {
		return SharePreview{}, errors.New("获取分享内容失败：" + err.Error())
	}

	preview := SharePreview{
		PwdID:    info.PwdID,
		Passcode: info.Passcode,
		Stoken:   stoken,
		Items:    []ShareNode{},
	}
	if listData != nil {
		preview.Title = mapStr(listData, "title", "share_title", "name")
		preview.Items = parseShareNodes(listData)
	}
	return preview, nil
}

// parseShareNodes 从分享列表响应里提取可转存条目。
// 上游字段在不同接口版本间有别名，这里逐项做宽松解析，缺字段不会中断整体。
func parseShareNodes(data map[string]interface{}) []ShareNode {
	if data == nil {
		return []ShareNode{}
	}
	rawList, ok := data["list"].([]interface{})
	if !ok {
		if arr, ok2 := data["list"].([]map[string]interface{}); ok2 {
			for _, it := range arr {
				rawList = append(rawList, it)
			}
		}
	}
	out := make([]ShareNode, 0, len(rawList))
	for _, item := range rawList {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		node := ShareNode{
			Fid:   mapStr(m, "fid"),
			Name:  mapStr(m, "file_name", "name", "title"),
			Size:  mapNum(m, "size"),
			IsDir: mapBool(m, "dir", "is_dir", "folder"),
			Token: mapStr(m, "share_fid_token", "share_token", "token"),
		}
		if node.Name == "" && node.Fid == "" {
			continue
		}
		out = append(out, node)
	}
	return out
}

// SaveShare 转存分享内容到自己的网盘。
func (a *App) SaveShare(req SaveShareRequest) (SaveShareResult, error) {
	qc, err := a.requireClient()
	if err != nil {
		return SaveShareResult{}, err
	}
	if strings.TrimSpace(req.PwdID) == "" || strings.TrimSpace(req.Stoken) == "" {
		return SaveShareResult{}, errors.New("分享信息不完整，请重新解析链接")
	}
	target := strings.TrimSpace(req.TargetFid)
	if target == "" {
		target = "0"
	}
	saveAll := req.SaveAll || len(req.Fids) == 0

	data, err := qc.SaveShareFile(req.PwdID, req.Stoken, req.Fids, req.Tokens, target, saveAll)
	if err != nil {
		return SaveShareResult{}, err
	}
	res := SaveShareResult{OK: true, Message: "转存任务已提交"}
	if data != nil {
		if tid := mapStr(data, "task_id"); tid != "" {
			res.TaskID = tid
			res.Message = "转存任务已提交，可在文件页刷新查看结果"
		}
	}
	return res, nil
}

// CreateShareLink 为网盘文件/文件夹创建分享链接。
// days 取值为 0（永久）、1、7、30；needPasscode 为 true 时由服务端生成提取码。
func (a *App) CreateShareLink(remotePath string, days int, needPasscode bool) (ShareLink, error) {
	qc, err := a.requireClient()
	if err != nil {
		return ShareLink{}, err
	}
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return ShareLink{}, errors.New("请先选择要分享的文件")
	}
	info, err := qc.CreateShare(remotePath, days, needPasscode)
	if err != nil {
		return ShareLink{}, err
	}
	if info == nil {
		return ShareLink{}, errors.New("创建分享失败：返回为空")
	}
	return ShareLink{
		URL:       info.ShareURL,
		Passcode:  info.Passcode,
		PwdID:     info.PwdID,
		ExpiresAt: info.ExpiresAt,
	}, nil
}

// ListMyShares 列出我创建的分享。
func (a *App) ListMyShares(page, size int) ([]MyShareItem, error) {
	qc, err := a.requireClient()
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 50
	}
	data, err := qc.GetMyShareList(page, size, "created_at", "desc")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return []MyShareItem{}, nil
	}
	rawList, _ := data["list"].([]interface{})
	out := make([]MyShareItem, 0, len(rawList))
	for _, item := range rawList {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, MyShareItem{
			ShareID:   mapStr(m, "share_id", "shareid", "id"),
			Title:     mapStr(m, "title", "file_name", "name"),
			URL:       mapStr(m, "share_url", "url", "share_link"),
			Passcode:  mapStr(m, "passcode", "pwd"),
			ViewCnt:   mapNum(m, "view_count", "view_cnt", "views"),
			SaveCnt:   mapNum(m, "save_count", "save_cnt", "saves"),
			CreatedAt: mapNum(m, "created_at", "ctime"),
			Expired:   mapBool(m, "expired", "is_expired"),
		})
	}
	return out, nil
}

// DeleteShare 批量取消分享。
func (a *App) DeleteShare(shareIDs []string) (bool, error) {
	qc, err := a.requireClient()
	if err != nil {
		return false, err
	}
	if len(shareIDs) == 0 {
		return false, errors.New("请先选择要取消的分享")
	}
	if err := qc.DeleteShare(shareIDs); err != nil {
		return false, err
	}
	return true, nil
}

// mapBool 从 map 中提取布尔字段，兼容 bool / 数字 / "true" 字符串。
func mapBool(m map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch b := v.(type) {
		case bool:
			return b
		case float64:
			return b != 0
		case int:
			return b != 0
		case string:
			s := strings.ToLower(strings.TrimSpace(b))
			return s == "true" || s == "1" || s == "yes"
		}
	}
	return false
}

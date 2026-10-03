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
	"path/filepath"
	"strconv"
	"strings"
)

// 下载路径解析的两个明确错误，界面据此给出可读提示。
var (
	errEmptyDir       = errors.New("下载目录为空")
	errSameNameExists = errors.New("目标位置已存在同名文件")
)

// windowsReserved 是 Windows 文件名保留字符，外加控制字符与路径分隔符。
// 网盘里存在带这些字符的文件名，不清洗会直接创建失败或写到别处。
const windowsReserved = `<>:"|?*\/`

// cleanFileName 把网盘文件名转成安全的本地文件名。
// 保留扩展名，避免清洗后丢掉类型信息。
func cleanFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20:
			// 控制字符直接丢弃
		case strings.ContainsRune(windowsReserved, r):
			b.WriteRune('_')
		case r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	// Windows 不允许以空格或点结尾
	out = strings.TrimRight(out, " .")
	// 保留设备名（CON / NUL / COM1…）在 Windows 上无法创建
	if isReservedDevice(out) {
		out = "_" + out
	}
	if out == "" {
		return "未命名文件"
	}
	return out
}

func isReservedDevice(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5",
		"COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5",
		"LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// resolveDest 决定一个下载任务的最终本地路径。
//
// policy：rename（同名自动加 (1) 序号，默认）/ overwrite（直接覆盖）/ skip（同名则失败）。
func resolveDest(dir, name, policy string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errEmptyDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, cleanFileName(name))
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest, nil
	}
	// 同名已存在：按策略处理
	switch policy {
	case "overwrite":
		return dest, nil
	case "skip":
		return "", errSameNameExists
	}
	ext := filepath.Ext(dest)
	stem := strings.TrimSuffix(dest, ext)
	for i := 1; i < 10000; i++ {
		cand := stem + "(" + strconv.Itoa(i) + ")" + ext
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand, nil
		}
	}
	return "", errSameNameExists
}

// uniqueRemoteDir 把网盘目录映射到本地子目录（保留目录层级）。
// 用 "全角_" 前缀是网盘里真实存在的目录名，避免和数字序号混淆。
func uniqueRemoteDir(base, remoteDir string) string {
	remoteDir = strings.Trim(strings.TrimSpace(remoteDir), "/")
	if remoteDir == "" {
		return base
	}
	parts := strings.Split(remoteDir, "/")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		cleaned = append(cleaned, cleanFileName(p))
	}
	if len(cleaned) == 0 {
		return base
	}
	return filepath.Join(append([]string{base}, cleaned...)...)
}

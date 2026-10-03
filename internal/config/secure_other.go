// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE.

//go:build !windows

package config

// hardenSessionFile 在非 Windows 平台是空操作。
//
// POSIX 系的文件权限位由内核强制执行：store.go 以 0600 创建 session.json 后
// 只有属主可读写，访问控制语义已经正确，无需额外处理。
func hardenSessionFile(path string) error { return nil }

// secureSessionFileOnLoad 在非 Windows 平台是空操作（无需补做权限收紧）。
func secureSessionFileOnLoad(path string) {}

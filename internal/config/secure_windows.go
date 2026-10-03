// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE.

//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows 下 session.json 的访问控制。
//
// 背景：store.go 以 0600 创建 session.json，但 Windows/NTFS 不实施 POSIX 权限位 ——
// os.WriteFile 的 perm 在 Windows 上仅映射为只读属性，不表达 ACL。仅靠 0600
// 无法阻止同机其他用户账户读取明文 Cookie（它等价于账号登录态）。
//
// 这里补一层真实生效的访问控制：把文件 DACL 收紧为「仅当前用户 + SYSTEM +
// Administrators 可访问」，并对 DACL 加 PROTECTED 标记阻止继承，
// 否则父目录的宽松 ACL 会合并进来、收紧等于无效。
//
// 实现用 golang.org/x/sys/windows —— 该模块本已在 go.mod 依赖图中
//（v0.30.0，由 Wails 间接引入），此处只是把已在图中的间接依赖提升为直接依赖，
// 版本不变、不新增供应链，不违反 HANDOFF §7.2「不引入新第三方依赖」。
//
// 策略：尽力而为。ACL 收紧失败不阻断登录（凭证仍可用，只是访问控制较弱）。

// hardenSessionFile 在文件创建后收紧其 ACL。
func hardenSessionFile(path string) error {
	// SYSTEM 与 Administrators 用 SID 字符串构造，避免手工拼二进制字面量。
	sysSID, err := windows.StringToSid("S-1-5-18") // LocalSystem
	if err != nil {
		return err
	}
	adminSID, err := windows.StringToSid("S-1-5-32-544") // Builtin\Administrators
	if err != nil {
		return err
	}

	// 允许当前用户、SYSTEM、Administrators 完全控制，且不参与继承。
	explicits := make([]windows.EXPLICIT_ACCESS, 0, 3)
	for _, sid := range []*windows.SID{currentUserSID(), sysSID, adminSID} {
		if sid == nil {
			continue
		}
		explicits = append(explicits, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	if len(explicits) == 0 {
		return errNoSubject
	}

	acl, err := windows.ACLFromEntries(explicits, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	)
}

type winError string

func (e winError) Error() string { return string(e) }

const errNoSubject = winError("无法构造访问控制主体")

// currentUserSID 取当前进程令牌中的用户 SID；取不到返回 nil（调用方跳过该主体）。
func currentUserSID() *windows.SID {
	tok, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil
	}
	defer tok.Close()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return nil
	}
	return tu.User.Sid
}

// secureSessionFileOnLoad 在加载阶段补做一次 ACL 收紧。
//
// 升级到本版本前创建的 session.json 是在旧逻辑（仅 0600、无 ACL 保护）下写入的，
// 需要在下次启动时就补上收紧，而不必等用户重新登录。
func secureSessionFileOnLoad(path string) {
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		return
	}
	// 收紧是幂等的；失败静默，不阻断启动。
	_ = hardenSessionFile(path)
}

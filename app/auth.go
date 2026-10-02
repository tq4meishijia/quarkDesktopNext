// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"errors"

	"github.com/zhangjingwei/kuake_cli/sdk"
)

// AuthStatus 返回当前登录态。前端在启动与收到 auth:changed 事件时调用。
func (a *App) AuthStatus() AuthState {
	a.mu.Lock()
	qc := a.client
	source := a.source
	profile := a.profile
	a.mu.Unlock()

	if qc == nil {
		return AuthState{LoggedIn: false, Message: "未登录"}
	}
	cookies := qc.GetCookies()
	raw := ""
	// 优先取 __pus 作为回显主体，取不到就退化成第一个非空值。
	if v, ok := cookies["__pus"]; ok && v != "" {
		raw = "__pus=" + v
	} else {
		for k, v := range cookies {
			if v != "" {
				raw = k + "=" + v
				break
			}
		}
	}
	return AuthState{
		LoggedIn: true,
		Source:   source,
		Masked:   maskCookie(raw),
		Message:  "已登录",
		Profile:  profile,
	}
}

// Login 用界面粘贴的 Cookie 登录。成功后凭证会写入本地会话文件（权限 0600）。
func (a *App) Login(cookie string) (AuthState, error) {
	return a.connect(cookie, "manual")
}

// LoginFromEnv 用 KUAKE_COOKIE / KUAKE_PUS + KUAKE_PUUS 登录，行为与 CLI 一致。
// 环境变量属于临时凭证，不会写盘。
func (a *App) LoginFromEnv() (AuthState, error) {
	raw := sdk.ResolveEnvCookieString()
	if raw == "" {
		return AuthState{}, errors.New("未检测到 KUAKE_COOKIE 或 KUAKE_PUS / KUAKE_PUUS")
	}
	return a.connect(raw, "env")
}

// Logout 清掉内存客户端与本地会话文件。
func (a *App) Logout() (bool, error) {
	a.mu.Lock()
	a.client = nil
	a.source = ""
	a.profile = Profile{}
	a.mu.Unlock()
	if err := a.store.SaveCredentials(configCredentialsEmpty()); err != nil {
		return false, err
	}
	a.emitAuth()
	return true, nil
}

// GetProfile 重新拉取一次用户资料（容量会随使用变化，需要可刷新）。
func (a *App) GetProfile() (Profile, error) {
	qc, err := a.requireClient()
	if err != nil {
		return Profile{}, err
	}
	resp, err := qc.GetUserInfo()
	if err != nil {
		return Profile{}, err
	}
	if resp == nil || !resp.Success {
		return Profile{}, errors.New("获取用户信息失败")
	}
	p := parseProfile(resp.Data)
	a.mu.Lock()
	a.profile = p
	a.mu.Unlock()
	a.emitAuth()
	return p, nil
}

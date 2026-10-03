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

// Logout 主动退出登录：清空内存会话、落盘凭证，并要求前端清掉本地存储。
//
// 前端的 localStorage / sessionStorage / Cookie 清理在 Wails 侧无法直接做
// （Go 拿不到 WebView 的存储），因此由前端在收到 auth:changed 后统一执行；
// 这里通过返回 true 告知「会话已失效」，前端必须把本地痕迹一并清掉。
func (a *App) Logout() (bool, error) {
	if err := a.invalidateSession(SessionLogout); err != nil {
		return false, err
	}
	a.notify("info", "已退出登录")
	return true, nil
}

// 会话失效的原因，供前端决定提示文案与是否需要跳登录页。
const (
	SessionLogout     = "logout"      // 用户主动退出
	SessionExpired    = "expired"     // 会话过期 / 令牌失效
	SessionAuthFailed = "authFailed"  // 鉴权校验失败
	SessionCleared    = "credCleared" // 用户清除了本机凭证
)

// invalidateSession 是所有「会话失效」路径的唯一收口：
// 断开内存客户端、清空用户信息缓存、删除落盘凭证、广播 auth:changed。
//
// 覆盖的入口：主动退出（Logout）、会话过期与鉴权失败（HandleSessionInvalid）、
// 清除本机凭证（ClearCredentials）。三者的内存与磁盘清理逻辑完全一致，
// 集中在这里是为了避免将来新增入口时漏掉某一步。
func (a *App) invalidateSession(reason string) error {
	a.mu.Lock()
	a.client = nil
	a.source = ""
	a.profile = Profile{}
	// 凭证失效后，下一次交互式登录必须要求重新登录：浏览器里那份 Cookie
	// 还在，不置这个标记会跳过登录直接进入。
	a.requireFreshLogin = true
	a.mu.Unlock()

	// SaveCredentials 传空值时走 os.Remove 分支，删掉整个 session.json。
	if err := a.store.SaveCredentials(configCredentialsEmpty()); err != nil {
		return err
	}
	a.emitAuthReason(reason)
	return nil
}

// HandleSessionInvalid 供前端在发现会话失效时调用（令牌过期、接口鉴权失败、
// 登录态校验不通过等），执行与主动退出完全一致的清理。
//
// 前端在调用前应先清掉自己的 localStorage/sessionStorage/Cookie；
// 本方法负责内存态与磁盘凭证，保证清理后任何需要鉴权的接口都会因
// requireClient 返回错误而失败。
func (a *App) HandleSessionInvalid(reason string) (bool, error) {
	switch reason {
	case SessionLogout, SessionExpired, SessionAuthFailed, SessionCleared:
	default:
		reason = SessionExpired
	}
	if err := a.invalidateSession(reason); err != nil {
		return false, err
	}
	if reason == SessionExpired || reason == SessionAuthFailed {
		a.notify("warn", "登录状态已失效，请重新登录")
	}
	return true, nil
}

// CredentialInfo 描述本机凭证的落盘情况，供设置页展示与确认。
type CredentialInfo struct {
	// HasCredential 为 true 表示 session.json 存在且含非空 cookie。
	HasCredential bool `json:"hasCredential"`
	// Source 是凭证来源：interactive / manual / env / unknown。
	Source string `json:"source"`
	// ItemCount 是凭证里的 Cookie 条目数，便于用户判断「存了些什么」。
	ItemCount int `json:"itemCount"`
	// SessionFile 是凭证文件的完整路径。
	SessionFile string `json:"sessionFile"`
	// LoggedIn 表示当前内存里是否已有可用会话。
	LoggedIn bool `json:"loggedIn"`
}

// CredentialState 返回本机凭证状态。只读，不触发任何登录动作。
func (a *App) CredentialState() CredentialInfo {
	a.mu.Lock()
	live := a.client != nil
	a.mu.Unlock()

	cred := a.store.Credentials()
	cookie := strings.TrimSpace(cred.Cookie)
	return CredentialInfo{
		HasCredential: cookie != "",
		Source:        cred.Source,
		ItemCount:     len(parseCookieNames(cookie)),
		SessionFile:   a.store.SessionFilePath(),
		LoggedIn:      live,
	}
}

// ClearCredentials 清除本机保存的登录凭证：删除 session.json 并断开内存会话。
//
// 与 Logout 的区别：两者都会清内存会话，Logout 是「退出登录」语义（界面回到登录页），
// 清除凭证额外承诺「落盘文件一定被删掉」，即便当前没登录、文件却残留也会一并清掉
// （比如 app/ 被强杀、Cookie 已失效但文件还在的情况）。返回清除后的状态，
// 前端无需再查一次即可判断是否还有残留。
func (a *App) ClearCredentials() (CredentialInfo, error) {
	// 与退出登录走同一套清理：断开内存会话、清空用户信息、删 session.json，
	// 并置 requireFreshLogin 让下一次交互式登录必须真的重新登录。
	if err := a.invalidateSession(SessionCleared); err != nil {
		return CredentialInfo{}, err
	}
	a.notify("success", "已清除本机保存的登录凭证")
	return a.CredentialState(), nil
}

// parseCookieNames 从 Cookie 串里取出条目名，忽略 $Version 之类的附加属性。
// 只用于统计条数展示，不参与任何鉴权判断。
func parseCookieNames(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.HasPrefix(name, "$") {
			continue
		}
		out = append(out, name)
	}
	return out
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

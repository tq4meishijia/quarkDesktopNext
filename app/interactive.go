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
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"kuake-desktop/internal/loginproxy"
)

// interactiveTimeout 是等待用户完成浏览器登录的上限。
const interactiveTimeout = 5 * time.Minute

// StartInteractiveLogin 启动本地代理并在系统浏览器打开夸克网盘。
//
// 流程：
//  1. 起一个只代理 *.quark.cn 的临时本地服务，返回入口地址并调起系统浏览器；
//  2. 用户在浏览器里照常登录（密码只走浏览器与夸克，不经过本进程）；
//  3. 代理在转发过程中从 Cookie 里取到凭证，自动完成登录并保存；
//  4. 登录成功后广播 auth:changed，界面切换到主窗口。
//
// 通过 InteractiveLoginStatus 轮询进度，CancelInteractiveLogin 可随时中止并释放端口。
func (a *App) StartInteractiveLogin() (InteractiveLoginState, error) {
	a.mu.Lock()
	// 重复点击时先收掉上一轮，避免端口泄漏
	if a.loginSession != nil {
		a.loginSession.Close()
		a.loginSession = nil
	}
	requireNew := a.requireFreshLogin
	session, err := loginproxy.StartWith(interactiveTimeout, loginproxy.Options{
		RequireFreshLogin: requireNew,
	})
	if err != nil {
		a.mu.Unlock()
		return InteractiveLoginState{}, err
	}
	a.loginSession = session
	a.loginPhase = PhaseWaiting
	a.loginHint = "已打开浏览器，请在弹出的夸克网盘页面完成登录。"
	if requireNew {
		a.loginHint = "已打开浏览器。此前清除过凭证，请在页面里重新登录一次（若浏览器已是登录态，需先退出夸克）。"
	}
	ctx := a.ctx
	a.mu.Unlock()

	if ctx != nil {
		// 打不开也没关系：登录页会展示地址，用户可以手动复制
		runtime.BrowserOpenURL(ctx, session.URL())
	}

	go a.awaitInteractiveLogin(session)
	return a.interactiveState(), nil
}

// OpenInteractiveLoginURL 重新在系统浏览器打开登录页。
// 浏览器没自动弹出、或用户误关掉了窗口时，用它把入口再要一次——
// 不必重新走一遍整个登录流程。
func (a *App) OpenInteractiveLoginURL() bool {
	a.mu.Lock()
	session := a.loginSession
	ctx := a.ctx
	a.mu.Unlock()
	if session == nil || ctx == nil {
		return false
	}
	runtime.BrowserOpenURL(ctx, session.URL())
	return true
}

// InteractiveLoginStatus 返回当前交互式登录进度，供界面轮询。
func (a *App) InteractiveLoginStatus() InteractiveLoginState {
	return a.interactiveState()
}

// CancelInteractiveLogin 中止等待并释放本地端口。
func (a *App) CancelInteractiveLogin() bool {
	a.mu.Lock()
	session := a.loginSession
	a.loginSession = nil
	if session != nil {
		a.loginPhase = PhaseCancelled
		a.loginHint = "已取消登录。"
	}
	a.mu.Unlock()
	if session == nil {
		return false
	}
	session.Close()
	return true
}

// awaitInteractiveLogin 等待代理捕获凭证，随后复用既有登录流程。
func (a *App) awaitInteractiveLogin(session *loginproxy.Session) {
	cookie, err := session.Wait(context.Background())

	a.mu.Lock()
	if a.loginSession == session {
		a.loginSession = nil
	}
	// 本轮无论成败，强制重登标记都用完了：它只针对「清完凭证后的第一次」，
	// 否则用户重新登录成功后，下次再点交互式登录又被要求重登一遍。
	a.requireFreshLogin = false
	a.mu.Unlock()

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return // CancelInteractiveLogin 已经写好状态，避免重复覆盖
		}
		a.setLoginPhase(PhaseError, "交互式登录失败："+err.Error())
		a.notify("error", "交互式登录失败："+err.Error())
		return
	}

	// 抓到了凭证但校验还没过，先把界面切到「正在校验」，
	// 否则这段几百毫秒里界面毫无反馈，用户会以为按钮没生效。
	a.setLoginPhase(PhaseCapturing, "已捕获登录凭证，正在校验…")

	if _, cerr := a.connect(cookie, "interactive"); cerr != nil {
		a.setLoginPhase(PhaseError, cerr.Error())
		a.notify("error", "登录校验失败："+cerr.Error())
		return
	}
	a.setLoginPhase(PhaseSuccess, "登录成功，凭证已保存到本机。")
	a.notify("success", "已通过浏览器登录，凭证已保存到本机")
}

// interactiveState 组装进度快照。
func (a *App) interactiveState() InteractiveLoginState {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := InteractiveLoginState{
		Active: a.loginSession != nil,
		Phase:  a.loginPhase,
		Hint:   a.loginHint,
	}
	if a.loginSession != nil {
		st.URL = a.loginSession.URL()
		_, collected := a.loginSession.Snapshot()
		st.Collected = collected
		st.Remaining = a.loginSession.Remaining()
		// 强制重新登录模式下检测到浏览器已有登录态：用户干等也不会收敛，
		// 必须明确告诉他下一步做什么。
		if a.loginSession.StaleExistingCredential() {
			st.StaleExisting = true
			if a.loginPhase == PhaseWaiting {
				st.Hint = "检测到浏览器里已有夸克登录态，不会自动进入。请在该页面退出夸克账号后重新登录，或手动退出浏览器 Cookie 后重试。"
			}
		}
	}
	return st
}

// setLoginPhase 更新阶段与提示文案。
func (a *App) setLoginPhase(phase, hint string) {
	a.mu.Lock()
	a.loginPhase = phase
	a.loginHint = hint
	a.mu.Unlock()
}

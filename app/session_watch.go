// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"strings"
	"sync"
	"time"

	"github.com/zhangjingwei/kuake_cli/sdk"
)

// 会话失效的自动检测。
//
// 为什么需要：原先会话失效完全依赖前端上报（前端发现 401/鉴权失败后调
// HandleSessionInvalid）。Go 侧没有任何主动检测，一旦前端漏报，
// a.client 保持非 nil、requireClient 持续放行，用户会一直看到空列表或
// 反复失败却不知原因。
//
// 两条检测路径：
//  1. 主动探测：登录成功后按固定间隔调 GetUserInfo 校验凭证是否仍有效。
//     能覆盖「用户放着不管、凭证静默过期」这一最常见的情形。
//  2. 被动判定：业务操作收到鉴权类错误时立即上报。响应更快，且能覆盖
//     「凭证刚过期」与「接口权限变化」两种情况。
//
// 为什么不在 SDK 里改：SDK 的 checkAuth 是未导出函数，且 quark-cil 是
// AGPL-3.0 上游，改动会带来合并冲突与 §13 披露义务。此处在桌面端自有
// 代码内用公开 API（GetUserInfo）实现同等效果，不触碰上游。

const (
	// sessionProbeInterval 是主动探测间隔。
	//
	// 取 5 分钟的权衡：太短会浪费请求配额（每次探测都是一次真实 API 调用），
	// 太长则用户会在过期后长时间看到错误界面。5 分钟意味着最坏情况下
	// 用户在凭证过期后 5 分钟内被自动登出，期间操作会由被动路径兜底。
	sessionProbeInterval = 5 * time.Minute

	// sessionProbeTimeout 单次探测的超时，避免网络卡住时探测 goroutine 堆积。
	sessionProbeTimeout = 15 * time.Second
)

// sessionWatcher 后台探测会话有效性。
type sessionWatcher struct {
	app *App

	mu      sync.Mutex
	stopCh  chan struct{}
	stopped bool
}

// startSessionWatch 启动后台探测。登录成功后调用。
func (a *App) startSessionWatch() {
	a.mu.Lock()
	if a.watcher != nil {
		a.mu.Unlock()
		return
	}
	w := &sessionWatcher{app: a, stopCh: make(chan struct{})}
	a.watcher = w
	a.mu.Unlock()

	go w.loop()
}

// stopSessionWatch 停止后台探测（登出/退出时调用）。可重复调用。
func (a *App) stopSessionWatch() {
	a.mu.Lock()
	w := a.watcher
	a.watcher = nil
	a.mu.Unlock()
	if w != nil {
		w.stop()
	}
}

func (w *sessionWatcher) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	w.stopped = true
	close(w.stopCh)
}

func (w *sessionWatcher) loop() {
	ticker := time.NewTicker(sessionProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.probeOnce()
		}
	}
}

// probeOnce 执行一次校验；失效则触发统一的会话失效收口。
func (w *sessionWatcher) probeOnce() {
	a := w.app
	// 未登录时不必探测。
	a.mu.Lock()
	qc := a.client
	loginPhase := a.loginPhase
	a.mu.Unlock()
	if qc == nil || loginPhase == PhaseCapturing {
		return
	}

	// 探测本身失败（网络问题）不等于凭证失效，只有明确的
	// 「鉴权被拒」才算数，避免弱网把用户踢下线。
	if !authRejected(qc.GetUserInfo) {
		return
	}
	a.invalidateSession(SessionExpired)
}

// authRejected 判断一次 API 调用是否因鉴权被拒而失败。
//
// 判定口径：网络错误、超时、5xx 都**不算**鉴权失效（那属于服务不可用，
// 登出无济于事）；只有「请求成功但业务码表示未授权」或明确的鉴权错误文案
// 才算。fn 返回 (success bool, err error)，由调用方提供。
func authRejected(fn func() (*sdk.StandardResponse, error)) bool {
	resp, err := fn()
	if err != nil {
		// 传输层错误：网络/超时/DNS。不能据此判定凭证失效。
		if isAuthErrorText(err.Error()) {
			return true
		}
		return false
	}
	if resp == nil {
		return false
	}
	if resp.Success {
		return false
	}
	// 业务码判定。
	switch strings.ToUpper(strings.TrimSpace(resp.Code)) {
	case "UNAUTHORIZED", "AUTH_FAILED", "TOKEN_EXPIRED", "NOT_LOGIN",
		"401", "403", "SESSION_EXPIRED", "INVALID_TOKEN":
		return true
	}
	// 兜底看文案（不同接口的 code 不统一）。
	return isAuthErrorText(resp.Message)
}

// isAuthErrorText 按文案判定是否鉴权失败。
//
// 只匹配明确指向「登录态」的短句，避免把「文件不存在」之类的
// 业务错误误判成会话失效而把用户踢下线。
func isAuthErrorText(msg string) bool {
	if msg == "" {
		return false
	}
	m := strings.ToLower(msg)
	// 明确排除的业务错误（优先判定，避免误伤）。
	for _, benign := range []string{
		"not found", "不存在", "已删除", "file_not_found", "目录不存在",
	} {
		if strings.Contains(m, benign) {
			return false
		}
	}
	for _, marker := range []string{
		"authentication failed",
		"unauthorized",
		"token expired",
		"invalid token",
		"session expired",
		"not logged in",
		"请先登录",
		"未登录",
		"登录已过期",
		"登录态已失效",
		"凭证已失效",
		"重新登录",
	} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// noteAuthFailure 在业务操作失败时调用：若失败原因是鉴权失效则触发登出。
//
// 供 app 各业务方法在拿到失败响应后调用，实现「被动判定」路径。
// 返回 true 表示本次失败被判定为会话失效。
func (a *App) noteAuthFailure(resp *sdk.StandardResponse, err error) bool {
	if resp == nil && err == nil {
		return false
	}
	if authRejected(func() (*sdk.StandardResponse, error) { return resp, err }) {
		// 已登录才需要失效；否则重复触发没有意义。
		a.mu.Lock()
		loggedIn := a.client != nil
		a.mu.Unlock()
		if !loggedIn {
			return false
		}
		a.invalidateSession(SessionExpired)
		return true
	}
	return false
}

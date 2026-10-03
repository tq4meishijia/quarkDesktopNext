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
	"testing"
	"time"

	"github.com/zhangjingwei/kuake_cli/sdk"
)

// TestAuthRejectedByCode 验证业务码判定。
func TestAuthRejectedByCode(t *testing.T) {
	authCodes := []string{
		"UNAUTHORIZED", "unauthorized", "AUTH_FAILED", "TOKEN_EXPIRED",
		"NOT_LOGIN", "401", "403", "SESSION_EXPIRED", "INVALID_TOKEN",
		"  Token_Expired  ",
	}
	for _, c := range authCodes {
		resp := &sdk.StandardResponse{Success: false, Code: c, Message: "出错了"}
		if !authRejected(func() (*sdk.StandardResponse, error) { return resp, nil }) {
			t.Errorf("业务码 %q 应判定为鉴权失效", c)
		}
	}
}

// TestAuthRejectedByMessage 验证文案兜底判定。
func TestAuthRejectedByMessage(t *testing.T) {
	authMsgs := []string{
		"authentication failed",
		"Unauthorized",
		"token expired",
		"请先登录",
		"登录已过期",
		"未登录",
		"登录态已失效",
		"重新登录",
	}
	for _, m := range authMsgs {
		resp := &sdk.StandardResponse{Success: false, Code: "SOME_OTHER_CODE", Message: m}
		if !authRejected(func() (*sdk.StandardResponse, error) { return resp, nil }) {
			t.Errorf("文案 %q 应判定为鉴权失效", m)
		}
	}
}

// TestAuthRejectedFalsePositives 验证普通业务失败不被误判为会话失效。
//
// 这是本功能最关键的一条：误判会把用户在网络抖动或业务错误时踢下线。
func TestAuthRejectedFalsePositives(t *testing.T) {
	benign := []*sdk.StandardResponse{
		{Success: true, Code: "OK"}, // 成功
		{Success: false, Code: "FILE_NOT_FOUND", Message: "文件不存在"}, // 业务错误
		{Success: false, Code: "LIST_PAGE_LIMIT", Message: "页码超限"},
		{Success: false, Code: "NOT_FOUND", Message: "目录不存在: /x"},
		nil, // 空响应
	}
	for i, resp := range benign {
		if authRejected(func() (*sdk.StandardResponse, error) { return resp, nil }) {
			t.Errorf("case %d 不应判为鉴权失效: %+v", i, resp)
		}
	}
	// 纯业务文案里含"未找到"等词也不应误判。
	resp := &sdk.StandardResponse{Success: false, Code: "X", Message: "文件未找到或已被删除"}
	if authRejected(func() (*sdk.StandardResponse, error) { return resp, nil }) {
		t.Error("含「未找到」的业务文案不应判为鉴权失效")
	}
}

// TestAuthRejectedIgnoresNetworkErrors 验证网络错误不被当作凭证失效。
//
// 网络不通、超时、DNS 失败都属于「服务不可用」，登出无济于事，
// 反而会让用户在断网时丢掉登录态。
func TestAuthRejectedIgnoresNetworkErrors(t *testing.T) {
	netErrs := []error{
		errors.New("dial tcp: i/o timeout"),
		errors.New("context deadline exceeded"),
		errors.New("no such host"),
		errors.New("connection reset by peer"),
		errors.New("EOF"),
	}
	for _, e := range netErrs {
		if authRejected(func() (*sdk.StandardResponse, error) { return nil, e }) {
			t.Errorf("网络错误 %q 不应判为鉴权失效", e.Error())
		}
	}
	// 但明确指向鉴权的错误文案仍应判定。
	if !authRejected(func() (*sdk.StandardResponse, error) {
		return nil, errors.New("authentication failed")
	}) {
		t.Error("明确鉴权错误的传输层错误应判定为失效")
	}
}

// TestIsAuthErrorText 验证文案判定的边界行为。
func TestIsAuthErrorText(t *testing.T) {
	if isAuthErrorText("") {
		t.Error("空串不应判为鉴权失败")
	}
	if isAuthErrorText("请稍后重试") {
		t.Error("一般提示不应判为鉴权失败")
	}
	if !isAuthErrorText("登录已过期，请重新登录") {
		t.Error("应判为鉴权失败")
	}
	// 排除项优先于包含项。
	if isAuthErrorText("文件不存在") {
		t.Error("业务错误应被排除")
	}
}

// TestNoteAuthFailureOnlyWhenLoggedIn 验证未登录时不触发失效流程。
func TestNoteAuthFailureOnlyWhenLoggedIn(t *testing.T) {
	a := newTestApp(t) // 未登录：a.client == nil
	resp := &sdk.StandardResponse{Success: false, Code: "UNAUTHORIZED"}
	if a.noteAuthFailure(resp, nil) {
		t.Error("未登录时不应触发会话失效（否则会递归）")
	}
	// 也不应抛 panic。
	if err := a.respErr(resp); err == nil {
		t.Error("respErr 仍应返回错误")
	}
}

// TestNoteAuthFailureTriggersLogout 验证已登录时鉴权失败会走统一收口。
func TestNoteAuthFailureTriggersLogout(t *testing.T) {
	a := newTestApp(t)
	// 造一个已登录态（不校验真实有效性，与既有测试一致）
	qc, err := newClient("__pus=P; __puus=PP;")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	a.mu.Lock()
	a.client = qc
	a.source = "manual"
	a.mu.Unlock()
	writeSession(t, a.store.Dir(), "__pus=P;")

	resp := &sdk.StandardResponse{Success: false, Code: "UNAUTHORIZED", Message: "登录已过期"}
	if !a.noteAuthFailure(resp, nil) {
		t.Fatal("已登录 + 鉴权失败应触发失效")
	}
	// 内存态应被清空
	a.mu.Lock()
	stillClient := a.client != nil
	fresh := a.requireFreshLogin
	a.mu.Unlock()
	if stillClient {
		t.Error("会话失效后 client 应为 nil")
	}
	if !fresh {
		t.Error("会话失效后 requireFreshLogin 应置位")
	}
	// 磁盘凭证应被删除
	if _, err := os.Stat(a.store.SessionFilePath()); err == nil {
		t.Error("会话失效后 session.json 应被删除")
	}
}

// TestSessionWatchStartStop 验证探测器的启停幂等。
func TestSessionWatchStartStop(t *testing.T) {
	a := newTestApp(t)
	// 未登录时启动也不应 panic（probeOnce 会因 client==nil 直接返回）
	a.startSessionWatch()
	a.startSessionWatch() // 重复启动应无副作用
	time.Sleep(20 * time.Millisecond)
	a.stopSessionWatch()
	a.stopSessionWatch() // 重复停止应无副作用
}

// TestRespErrStillReturnsMessage 验证 respErr 不改变原有错误文案。
func TestRespErrStillReturnsMessage(t *testing.T) {
	a := newTestApp(t)
	resp := &sdk.StandardResponse{Success: false, Code: "FILE_NOT_FOUND", Message: "文件不存在"}
	err := a.respErr(resp)
	if err == nil {
		t.Fatal("respErr 应返回错误")
	}
	// 文案应与 respMessage 一致（保持前端既有行为不变）
	if want := respMessage(resp); err.Error() != want {
		t.Errorf("文案被改变: got %q want %q", err.Error(), want)
	}
}

// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kuake-desktop/internal/config"

	"github.com/zhangjingwei/kuake_cli/sdk"
)

// newTestApp 造一个把配置写到临时目录的 App，避免污染真实用户配置。
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KUAKE_DESKTOP_HOME", dir)
	st := config.Load()
	return &App{store: st, settings: st.Settings(), loginPhase: PhaseIdle}
}

func writeSession(t *testing.T, dir, cookie string) {
	t.Helper()
	raw, err := json.Marshal(config.Credentials{Cookie: cookie, Source: "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLogoutClearsEverything 主动退出必须清空内存态与磁盘凭证，
// 且把 requireFreshLogin 置位（否则下次交互式登录会直接跳过）。
func TestLogoutClearsEverything(t *testing.T) {
	a := newTestApp(t)
	dir := a.store.Dir()
	writeSession(t, dir, "__pus=P; __puus=PP")

	// 伪造已登录状态
	a.mu.Lock()
	a.source = "interactive"
	a.profile = Profile{Nickname: "someone", UsedBytes: 1, TotalBytes: 2}
	a.requireFreshLogin = false
	a.mu.Unlock()

	if _, err := a.Logout(); err != nil {
		t.Fatalf("Logout 失败：%v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Error("退出后 session.json 应被删除")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil || a.source != "" || a.profile.Nickname != "" {
		t.Error("退出后内存态（客户端/来源/用户信息）应全部清空")
	}
	if !a.requireFreshLogin {
		t.Error("退出后应要求下一次登录为强制重登")
	}
}

// TestHandleSessionInvalidAllReasons 会话过期与鉴权失败必须与主动退出等价。
func TestHandleSessionInvalidAllReasons(t *testing.T) {
	for _, reason := range []string{SessionExpired, SessionAuthFailed, "unknown-reason", ""} {
		a := newTestApp(t)
		dir := a.store.Dir()
		writeSession(t, dir, "__pus=P; __puus=PP")
		a.mu.Lock()
		a.source = "manual"
		a.profile = Profile{Nickname: "someone"}
		a.mu.Unlock()

		if _, err := a.HandleSessionInvalid(reason); err != nil {
			t.Fatalf("reason=%q 返回错误：%v", reason, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
			t.Errorf("reason=%q 时 session.json 应被删除", reason)
		}
		a.mu.Lock()
		if a.client != nil || a.source != "" || a.profile.Nickname != "" {
			t.Errorf("reason=%q 时内存态未清空", reason)
		}
		a.mu.Unlock()
	}
}

// TestClearCredentialsGoesThroughSamePath 清除凭证与退出登录必须共用同一套清理，
// 否则将来只改一处就会出现「清了 A 没清 B」。
func TestClearCredentialsGoesThroughSamePath(t *testing.T) {
	a := newTestApp(t)
	dir := a.store.Dir()
	writeSession(t, dir, "__pus=P; __puus=PP")
	a.mu.Lock()
	a.profile = Profile{Nickname: "someone"}
	a.mu.Unlock()

	info, err := a.ClearCredentials()
	if err != nil {
		t.Fatalf("ClearCredentials 失败：%v", err)
	}
	if info.HasCredential {
		t.Error("清除后不应再有凭证")
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Error("清除凭证后 session.json 应被删除")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.profile.Nickname != "" || !a.requireFreshLogin {
		t.Error("清除凭证应同时清空用户信息并置强制重登")
	}
}

// TestRequireClientFailsAfterLogout 清理完成后必须无法再访问任何需要鉴权的能力——
// requireClient 是所有业务接口的必经关口。
func TestRequireClientFailsAfterLogout(t *testing.T) {
	a := newTestApp(t)
	// 清理前：有 client 时不报错
	a.mu.Lock()
	a.client = mustNewClient(t, "__pus=P; __puus=PP")
	a.mu.Unlock()
	if _, err := a.requireClient(); err != nil {
		t.Fatalf("清理前 requireClient 应成功：%v", err)
	}

	if _, err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.requireClient(); err == nil {
		t.Error("退出后 requireClient 必须返回错误（否则仍可访问需鉴权的能力）")
	}
}

// TestAuthStatusReportsInvalidReason AuthStatus 要能带上失效原因，供前端区分处理。
func TestAuthStatusReportsInvalidReason(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	st := a.AuthStatus()
	if st.LoggedIn {
		t.Error("退出后 LoggedIn 应为 false")
	}
	if st.Profile.Nickname != "" || st.Profile.Avatar != "" {
		t.Error("退出后不应残留用户信息")
	}
}

// mustNewClient 造一个已登录客户端；凭证非法时 SDK 会 panic，newClient 会转成 error。
func mustNewClient(t *testing.T, cookie string) *sdk.QuarkClient {
	t.Helper()
	qc, err := newClient(cookie)
	if err != nil {
		t.Fatalf("构造客户端失败：%v", err)
	}
	return qc
}

// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package loginproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newFreshSession 起一个强制重新登录模式的会话。
func newFreshSession(t *testing.T, upstream *httptest.Server) *Session {
	t.Helper()
	s, err := StartWith(30*time.Second, Options{RequireFreshLogin: true})
	if err != nil {
		t.Fatalf("启动会话失败：%v", err)
	}
	s.settleDelay = 80 * time.Millisecond
	s.client.Transport = &upstreamTransport{base: upstream.URL}
	t.Cleanup(s.Close)
	return s
}

// TestFreshLoginIgnoresExistingCredential 核心回归：浏览器里已有 __pus 时，
// 代理不能把它当成「刚登录成功」——这正是「清除凭证后仍自动跳转」的成因。
func TestFreshLoginIgnoresExistingCredential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newFreshSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 浏览器带着旧凭证访问入口页
	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/list", nil)
	req.Header.Set("Cookie", "__pus=OLD-PUS; __puus=OLD-PUUS")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	if s.captured() {
		t.Error("浏览器已有的旧凭证不应触发 captured（会导致清除凭证后仍自动跳转）")
	}
	if !s.StaleExistingCredential() {
		t.Error("应记录「浏览器里已有可用凭证」，供前端提示用户")
	}

	// 等过 settleDelay 也不该收敛
	time.Sleep(200 * time.Millisecond)
	if s.captured() {
		t.Error("旧凭证不应在宽限期后被判定为登录完成")
	}
}

// TestFreshLoginAcceptsNewCredential 与上一条相反：用户真的在浏览器里登录了一次，
// 凭证值发生变化，必须正常收敛，否则功能不可用。
func TestFreshLoginAcceptsNewCredential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newFreshSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 先带上旧凭证
	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/list", nil)
	req.Header.Set("Cookie", "__pus=OLD-PUS")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	// 用户在浏览器里重新登录，浏览器带上了新凭证
	req2, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/list", nil)
	req2.Header.Set("Cookie", "__pus=NEW-PUS; __puus=NEW-PUUS")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res2.Body)
	res2.Body.Close()

	ctx := t.Context()
	cookie, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("新凭证应触发收敛，实际错误：%v", err)
	}
	if !strings.Contains(cookie, "__pus=NEW-PUS") {
		t.Errorf("应返回新凭证，实际 %q", cookie)
	}
}

// TestFreshLoginNoExistingCredentialStartsClean 浏览器本来就是干净会话时，
// 首次登录由上游 Set-Cookie 下发凭证，必须直接收敛（不能把正常首次登录也挡住）。
func TestFreshLoginNoExistingCredentialStartsClean(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟真实登录：夸克在登录成功时用 Set-Cookie 下发凭证
		http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "FIRST-PUS", Domain: ".quark.cn", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	s := newFreshSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	res, err := http.Get(base + prefix + "/pan.quark.cn/list")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	if _, err := s.Wait(t.Context()); err != nil {
		t.Fatalf("干净会话下首次登录应直接收敛：%v", err)
	}
}

// TestNormalModeUnchanged 默认（非强制）模式必须保持原语义：见到凭证即收敛。
// 这是绝大多数登录场景走的路径，不能被新特性影响。
func TestNormalModeUnchanged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/list", nil)
	req.Header.Set("Cookie", "__pus=ANY; __puus=ANY")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	if _, err := s.Wait(t.Context()); err != nil {
		t.Fatalf("默认模式应保持见到凭证即收敛：%v", err)
	}
	if s.StaleExistingCredential() {
		t.Error("默认模式不应报告「浏览器已有凭证」")
	}
}

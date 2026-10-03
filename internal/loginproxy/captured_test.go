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

// TestCapturedServesSubresourcesFromUpstream 是本轮白屏问题的核心回归。
//
// 扫码/短信登录成功后，夸克页面会自行跳到 /list 并继续加载它的 JS/CSS/接口。
// 代理若在 captured 之后把子资源也换成收尾页 HTML，浏览器会把 HTML 当 JS 解析，
// 页面直接白屏且没有任何提示——表现为「扫码成功后不跳转、卡在白屏」。
func TestCapturedServesSubresourcesFromUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, "console.log('upstream-js')")
		case strings.HasSuffix(r.URL.Path, ".css"):
			w.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(w, "body{color:red}")
		case strings.HasPrefix(r.URL.Path, "/api/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"list":[]}`)
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html><body>upstream-page</body></html>")
		}
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 走真实路径让会话进入已捕获状态
	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/", nil)
	req.Header.Set("Cookie", "__pus=PUS-A; __puus=PUUS-A")
	req.Header.Set("Sec-Fetch-Dest", "document")
	res0, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res0.Body)
	res0.Body.Close()
	if !s.captured() {
		t.Fatal("凭证已下发，应进入已捕获状态")
	}

	cases := []struct {
		path       string
		dest       string
		wantSubstr string
		wantHTML   bool
	}{
		// 文档导航仍应拿到收尾页，告诉用户可以关窗口了
		{"/__proxy/pan.quark.cn/list", "document", "登录成功", true},
		// 子资源必须回到上游，否则浏览器把 HTML 当 JS/CSS 解析 → 白屏
		{"/__proxy/pan.quark.cn/static/js/app.js", "script", "upstream-js", false},
		{"/__proxy/pan.quark.cn/static/css/main.css", "style", "body{color:red}", false},
		{"/__proxy/pan.quark.cn/api/list", "empty", `{"list":[]}`, false},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodGet, base+c.path, nil)
		r.Header.Set("Sec-Fetch-Dest", c.dest)
		r.Header.Set("Referer", base+prefix+"/pan.quark.cn/list")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatalf("%s 请求失败：%v", c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		got := string(body)
		if !strings.Contains(got, c.wantSubstr) {
			t.Errorf("%s (dest=%s) 内容不符合预期，期望包含 %q，实际 %q",
				c.path, c.dest, c.wantSubstr, truncate(got, 60))
		}
		if c.wantHTML && !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s 导航请求应返回 HTML", c.path)
		}
	}
}

// TestRootPathSubresourceAfterCaptured 根绝对路径（不带 /__proxy 前缀）的子资源
// 在已捕获状态下也必须转发，不能被收尾页顶掉——SPA 的接口都是这种形态。
func TestRootPathSubresourceAfterCaptured(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		_, _ = io.WriteString(w, "<html><body>upstream</body></html>")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/", nil)
	req.Header.Set("Cookie", "__pus=PUS-B; __puus=PUUS-B")
	req.Header.Set("Sec-Fetch-Dest", "document")
	res0, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res0.Body)
	res0.Body.Close()

	r, _ := http.NewRequest(http.MethodGet, base+"/api/account/info", nil)
	r.Header.Set("Sec-Fetch-Dest", "empty")
	r.Header.Set("Referer", base+prefix+"/pan.quark.cn/list")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("已捕获后根路径接口请求失败：%v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `{"ok":true}`) {
		t.Errorf("已捕获后根路径接口应转发到上游，实际 %q", truncate(string(body), 60))
	}
}

// TestIsDocumentNavigation 纯函数：Sec-Fetch-Dest 是判定的第一依据，
// 缺失时退化为 Accept 启发式。
func TestIsDocumentNavigation(t *testing.T) {
	mk := func(dest, accept, method string) *http.Request {
		r := httptest.NewRequest(method, "/", nil)
		if dest != "" {
			r.Header.Set("Sec-Fetch-Dest", dest)
		}
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		return r
	}
	yes := []*http.Request{
		mk("document", "", "GET"),
		mk("iframe", "", "GET"),
		mk("", "text/html,application/xhtml+xml", "GET"),
	}
	for _, r := range yes {
		if !isDocumentNavigation(r) {
			t.Errorf("应判定为文档导航：dest=%q accept=%q", r.Header.Get("Sec-Fetch-Dest"), r.Header.Get("Accept"))
		}
	}
	no := []*http.Request{
		mk("script", "*/*", "GET"),
		mk("style", "text/css,*/*", "GET"),
		mk("empty", "*/*", "GET"),
		mk("image", "image/*", "GET"),
		mk("script", "text/html", "GET"), // Sec-Fetch-Dest 优先于 Accept
	}
	for _, r := range no {
		if isDocumentNavigation(r) {
			t.Errorf("不应判定为文档导航：dest=%q accept=%q", r.Header.Get("Sec-Fetch-Dest"), r.Header.Get("Accept"))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestGracePeriodCoversSPALoadTime 回归：凭证捕获后的宽限期必须够 SPA 加载完。
//
// 实测问题：宽限期原为 8 秒，而凭证捕获后夸克页面会自行跳到 /list 并陆续加载
// 自己的 JS/CSS/接口。8 秒太紧，浏览器表现为「文档 HTML 加载到了、脚本没跑起来」
// 的白屏，network 里是 ERR_CONNECTION_REFUSED。30 秒足够整个 SPA 加载完。
func TestGracePeriodCoversSPALoadTime(t *testing.T) {
	if gracePeriod < 20*time.Second {
		t.Errorf("宽限期 %v 太短，登录后跳转 /list 加载资源时会被拒绝连接（表现为白屏）", gracePeriod)
	}
	// 也不能无限长：代理只在登录期间存活，停太久会让本机端口长时间监听。
	if gracePeriod > defaultTimeout/2 {
		t.Errorf("宽限期 %v 过长，接近会话超时上限，本机端口会长时间监听", gracePeriod)
	}
}

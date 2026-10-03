// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package loginproxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// 修复点 1+2：Origin 还原 + 请求体缓冲（消除 chunked）
// ---------------------------------------------------------------------------

// TestForwardFixesOriginAndBody 复现扫码/短信接口的两个失败点：
// 浏览器把本地代理当同源，POST 带 Origin: http://127.0.0.1:<port>，且透传 r.Body
// 会让上游收到 chunked 无长度请求体。两者都会让夸克接口拒绝。
func TestForwardFixesOriginAndBody(t *testing.T) {
	var (
		mu        sync.Mutex
		gotOrigin string
		gotCL     int64
		gotTE     []string
		gotBody   string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotOrigin = r.Header.Get("Origin")
		gotCL = r.ContentLength
		gotTE = r.TransferEncoding
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	payload := "phone=13700000000&code=123456"
	req, err := http.NewRequest(http.MethodPost, base+prefix+"/passport.quark.cn/api/sms/verify", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", base) // 浏览器视角的同源
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotBody, "code=123456") {
		t.Errorf("上游未收到完整请求体（可能被 chunked 吞掉）：%q", gotBody)
	}
	if gotCL <= 0 {
		t.Errorf("上游应收到带 Content-Length 的请求，实际 %d", gotCL)
	}
	if len(gotTE) > 0 {
		t.Errorf("上游不应收到 chunked 请求体，实际 TransferEncoding=%v", gotTE)
	}
	if gotOrigin != "https://passport.quark.cn" {
		t.Errorf("Origin 应还原成上游真实源，实际 %q", gotOrigin)
	}
}

// ---------------------------------------------------------------------------
// 修复点 4：根绝对路径回落 + 会话 Cookie 回注
// ---------------------------------------------------------------------------

// TestRootPathFallsBackToRefererHost 验证 SPA 用根绝对路径请求接口时，
// 代理能靠 Referer 推断目标主机、并从已采集的会话 Cookie 回注，避免上游当成未登录。
func TestRootPathFallsBackToRefererHost(t *testing.T) {
	var (
		mu           sync.Mutex
		verifyCookie string
		verifyHit    bool
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login": // 下发一个非凭证的会话 Cookie（不触发收敛，便于继续测第二步）
			http.SetCookie(w, &http.Cookie{Name: "login_sid", Value: "SID-XYZ", Domain: ".quark.cn", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html><body>login</body></html>")
		case "/api/sms/verify":
			mu.Lock()
			verifyCookie = r.Header.Get("Cookie")
			verifyHit = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	browser := newBrowser(t)
	base := "http://" + s.ln.Addr().String()

	// 第一步：访问登录页，代理采集到 login_sid
	res, err := browser.Get(base + prefix + "/passport.quark.cn/login")
	if err != nil {
		t.Fatalf("访问登录页失败：%v", err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	// 第二步：SPA 用根绝对路径请求校验接口，Referer 指向登录页。
	// 浏览器不会把 path-scoped 的 login_sid 发到 /api/...，代理需自行回注。
	req, err := http.NewRequest(http.MethodPost, base+"/api/sms/verify", strings.NewReader("code=1"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Referer", base+prefix+"/passport.quark.cn/login")
	res2, err := browser.Do(req)
	if err != nil {
		t.Fatalf("根路径接口请求失败：%v", err)
	}
	io.Copy(io.Discard, res2.Body)
	res2.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if !verifyHit {
		t.Fatal("根路径请求未被转发到上游 /api/sms/verify")
	}
	if !strings.Contains(verifyCookie, "login_sid=SID-XYZ") {
		t.Errorf("根路径请求未回注会话 Cookie，上游收到：%q", verifyCookie)
	}
}

// TestBannerNotInjectedTwice 手机登录表单在 iframe 里加载，iframe 文档会再走一遍
// 代理；重复注入提示条会把登录框往下挤一大截。
func TestBannerNotInjectedTwice(t *testing.T) {
	once := injectBanner([]byte("<html><body><div>x</div></body></html>"))
	if !bytes.Contains(once, []byte(bannerMarker)) {
		t.Fatalf("首次注入应带标记")
	}
	twice := injectBanner(once)
	if bytes.Count(twice, []byte("__quark_proxy_banner")) != 1 {
		t.Errorf("提示条不应被重复注入，出现 %d 次", bytes.Count(twice, []byte("__quark_proxy_banner")))
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("二次注入后内容应保持不变")
	}
}

// TestLocalizeSkipsScanLandingHost 回归测试：扫码落地页地址不能被本地化。
// bundle 用 Hk(scanLoginPage, {token,...}) 把它拼进二维码内容，手机扫码后要能
// 真正打开夸克页面；一旦被改写成 /__proxy/su.quark.cn/...，二维码就指向
// 127.0.0.1，扫码必然失败。同时 alicdn.com 必须被本地化（bundle 在该 CDN）。
func TestLocalizeSkipsScanLandingHost(t *testing.T) {
	keep := []string{
		"https://su.quark.cn/4_eMHBJ?token=abc&client_id=532&ssb=weblogin",
		"//su.quark.cn/4_eMHBJ?token=abc",
		"https:\\/\\/su.quark.cn\\/4_eMHBJ?token=abc",
	}
	for _, in := range keep {
		if got := localize(in); got != in {
			t.Errorf("扫码落地页地址不应被改写：\n in = %q\nout = %q", in, got)
		}
	}

	rewrite := []struct{ in, want string }{
		// bundle 托管在 g.alicdn.com，必须本地化，否则内部硬编码接口地址无从改写
		{"https://g.alicdn.com/quark-cloud-drive/x/0.1.78/js/index.js", "/__proxy/g.alicdn.com/quark-cloud-drive/x/0.1.78/js/index.js"},
		{"//g.alicdn.com/a.js", "/__proxy/g.alicdn.com/a.js"},
		{"https://img.alicdn.com/x.png", "/__proxy/img.alicdn.com/x.png"},
		// 扫码取码 / 轮询 / 手机登录页：必须本地化，否则 XHR 被 CORS 拦掉
		{"https://uop.quark.cn/cas/ajax/getTokenForQrcodeLogin?client_id=532", "/__proxy/uop.quark.cn/cas/ajax/getTokenForQrcodeLogin?client_id=532"},
		{"https://uop.quark.cn/cas/custom/login?custom_login_type=mobile", "/__proxy/uop.quark.cn/cas/custom/login?custom_login_type=mobile"},
		{"https://uop.quark.cn/cas/ajax/getServiceTicketByQrcodeToken", "/__proxy/uop.quark.cn/cas/ajax/getServiceTicketByQrcodeToken"},
		// 转义写法同样要本地化（bundle 是压缩 JS，字符串里常见 \/\/）
		{"https:\\/\\/uop.quark.cn\\/cas\\/ajax\\/x", "/__proxy/uop.quark.cn\\/cas\\/ajax\\/x"},
		// 非白名单域仍然不动
		{"https://track.lc.quark.cn/collect", "/__proxy/track.lc.quark.cn/collect"},
		{"https://example.com/a", "https://example.com/a"},
	}
	for _, c := range rewrite {
		if got := localize(c.in); got != c.want {
			t.Errorf("localize(%q)\n got = %q\nwant = %q", c.in, got, c.want)
		}
	}
}

// TestAllowedHostCoversLoginBundle 白名单必须覆盖登录页 bundle 所在的 CDN，
// 同时不放行任意第三方域。
func TestAllowedHostCoversLoginBundle(t *testing.T) {
	yes := []string{"g.alicdn.com", "img.alicdn.com", "pan.quark.cn", "uop.quark.cn", "su.quark.cn", "px.wpk.quark.cn"}
	for _, h := range yes {
		if !allowedHost(h) {
			t.Errorf("应允许代理：%q", h)
		}
	}
	no := []string{"example.com", "g.alicdn.com.evil.com", "alicdn.com.evil.io", "", "evilquark.cn"}
	for _, h := range no {
		if allowedHost(h) {
			t.Errorf("不应允许代理：%q", h)
		}
	}
}

// TestMergeBrowserAndJarCookies 验证合并回注：浏览器自写 Cookie 与已采集会话
// 都必须出现在转发给上游的 Cookie 头里。
func TestMergeBrowserAndJarCookies(t *testing.T) {
	s := &Session{jar: map[string]string{"login_sid": "SID-1", "ctoken": "OLD"}}
	s.absorb("ctoken=NEW; from_browser=1")
	got := s.mergedCookie()
	for _, want := range []string{"login_sid=SID-1", "ctoken=NEW", "from_browser=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("合并串缺少 %q，实际 %q", want, got)
		}
	}
}

// TestRootPathWithoutRefererIs404 保证没有可信 Referer 时仍拒绝，
// 根路径回落不能把代理变成任意主机的转发器。
func TestRootPathWithoutRefererIs404(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should-not-reach")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 无 Referer
	res, err := http.Get(base + "/static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("无 Referer 的根路径应 404，实际 %d", res.StatusCode)
	}

	// Referer 指向非本机（诱导）
	req, _ := http.NewRequest(http.MethodGet, base+"/x", nil)
	req.Header.Set("Referer", "https://evil.example.com/__proxy/passport.quark.cn/login")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Errorf("外部 Referer 不应被采信，实际 %d", res2.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 纯函数
// ---------------------------------------------------------------------------

func TestIsLocalOrigin(t *testing.T) {
	yes := []string{"http://127.0.0.1:53211", "http://localhost:80", "http://[::1]:9000"}
	no := []string{"https://passport.quark.cn", "http://127.0.0.1.evil.com", "https://quark.cn", ""}
	for _, v := range yes {
		if !isLocalOrigin(v) {
			t.Errorf("应判定为本机 Origin：%q", v)
		}
	}
	for _, v := range no {
		if isLocalOrigin(v) {
			t.Errorf("不应判定为本机 Origin：%q", v)
		}
	}
}

func TestHostFromReferer(t *testing.T) {
	mk := func(referer string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if referer != "" {
			r.Header.Set("Referer", referer)
		}
		return r
	}
	if h := hostFromReferer(mk("http://127.0.0.1:5000/__proxy/passport.quark.cn/login")); h != "passport.quark.cn" {
		t.Errorf("应从本机 Referer 解析出主机，实际 %q", h)
	}
	if h := hostFromReferer(mk("https://evil.com/__proxy/passport.quark.cn/login")); h != "" {
		t.Errorf("非本机 Referer 不应采信，实际 %q", h)
	}
	if h := hostFromReferer(mk("http://127.0.0.1:5000/no-proxy-prefix")); h != "" {
		t.Errorf("无代理前缀应返回空，实际 %q", h)
	}
	if h := hostFromReferer(mk("")); h != "" {
		t.Errorf("空 Referer 应返回空，实际 %q", h)
	}
}

func TestReadForwardBody(t *testing.T) {
	// 正常
	r1 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("hello")))
	b1, err := readForwardBody(r1)
	if err != nil || string(b1) != "hello" {
		t.Errorf("正常请求体读取错误：%q %v", b1, err)
	}
	// nil body
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	b2, err := readForwardBody(r2)
	if err != nil || len(b2) != 0 {
		t.Errorf("无 body 应返回空：%q %v", b2, err)
	}
	// 超限
	big := strings.Repeat("a", maxRequestBody+1)
	r3 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
	if _, err := readForwardBody(r3); err == nil {
		t.Error("超过上限应返回错误")
	}
}

// TestMergedCookieReflectsJar 验证回注用的合并串确实来自已采集的 jar。
func TestMergedCookieReflectsJar(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	s := newTestSession(t, upstream)
	s.absorbOne("login_sid", "SID-1")
	if got := s.mergedCookie(); !strings.Contains(got, "login_sid=SID-1") {
		t.Errorf("mergedCookie 未包含已采集项：%q", got)
	}
	// 收敛前不应误判为已捕获
	if s.captured() {
		t.Error("非凭证 Cookie 不应触发 captured")
	}
}

// ---------------------------------------------------------------------------
// 修复点 5：根路径回落用「最近文档主机」兜底（pushState / no-referrer 场景）
// ---------------------------------------------------------------------------

// TestFallbackUsesLastKnownHost 复现 SPA 客户端路由后的请求：登录页经
// pushState 把地址栏改成根绝对路径（或声明 no-referrer）后，后续请求的
// Referer 不再含 /__proxy/<host>/ 前缀。代理应回退到最近一次文档主机继续转发，
// 并把缺前缀的本机 Referer 还原成上游真实地址。
func TestFallbackUsesLastKnownHost(t *testing.T) {
	var (
		mu         sync.Mutex
		gotPaths   []string
		gotReferer []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		gotReferer = append(gotReferer, r.Header.Get("Referer"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>ok</body></html>")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 第一步：正常访问带前缀的登录页，会话记录下最近文档主机。
	res, err := http.Get(base + prefix + "/pan.quark.cn/login")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	// 第二步：pushState 到 /login 后，静态资源请求完全不带 Referer。
	res2, err := http.Get(base + "/static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()

	// 第三步：接口请求带「缺前缀」的本机 Referer。
	req, err := http.NewRequest(http.MethodPost, base+"/api/sms/verify", strings.NewReader("code=1"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Referer", base+"/login")
	res3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res3.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(gotPaths) != 3 {
		t.Fatalf("上游应收到 3 次请求，实际 %d：%v", len(gotPaths), gotPaths)
	}
	if gotPaths[1] != "/static/js/app.js" {
		t.Errorf("无 Referer 的根路径应按最近主机转发，实际 %q", gotPaths[1])
	}
	if gotPaths[2] != "/api/sms/verify" {
		t.Errorf("缺前缀 Referer 的请求应按最近主机转发，实际 %q", gotPaths[2])
	}
	if gotReferer[2] != "https://pan.quark.cn/login" {
		t.Errorf("本机 Referer 应还原成上游真实地址，实际 %q", gotReferer[2])
	}
}

// ---------------------------------------------------------------------------
// 修复点 6：根路径回落的 Cookie 合并回注
// ---------------------------------------------------------------------------

// TestFallbackMergesBrowserCookies 复现 SPA 自写本机 Cookie 的场景：页面用
// document.cookie 写入 ctoken 后，根绝对路径请求会带上这个 Cookie 头。旧逻辑
// 只在 Cookie 头为空时才回注已采集会话，上游因此收到残缺会话，短信校验报
// 「验证码无效」。修复后应把浏览器 Cookie 与已采集会话合并。
func TestFallbackMergesBrowserCookies(t *testing.T) {
	var verifyCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login": // 下发一个会话 Cookie（不触发收敛，便于继续测第二步）
			http.SetCookie(w, &http.Cookie{Name: "login_sid", Value: "SID-XYZ", Domain: ".quark.cn", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html><body>login</body></html>")
		case "/api/sms/verify":
			verifyCookie = r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	res, err := http.Get(base + prefix + "/pan.quark.cn/login")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	// 浏览器视角：SPA 自写了 ctoken，请求带上它，但 path-scoped 的 login_sid 不会来。
	req, err := http.NewRequest(http.MethodPost, base+"/api/sms/verify", strings.NewReader("code=1"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Referer", base+prefix+"/pan.quark.cn/login")
	req.Header.Set("Cookie", "ctoken=JS-WRITTEN")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res2.Body)
	res2.Body.Close()

	if !strings.Contains(verifyCookie, "ctoken=JS-WRITTEN") {
		t.Errorf("浏览器自带的 Cookie 不应被丢弃，上游收到：%q", verifyCookie)
	}
	if !strings.Contains(verifyCookie, "login_sid=SID-XYZ") {
		t.Errorf("已采集的会话 Cookie 不应被浏览器 Cookie 顶掉，上游收到：%q", verifyCookie)
	}
}

// ---------------------------------------------------------------------------
// 修复点 7：跨主机接口的 Origin 还原为页面源站
// ---------------------------------------------------------------------------

// TestOriginForCrossHostCallUsesPageOrigin 复现扫码登录的 CAS 调用：登录页在
// pan.quark.cn，取码/轮询接口在 uop.quark.cn。上游按页面源站校验 Origin，
// 按目标主机还原（https://uop.quark.cn）会被拒绝，必须还原成 https://pan.quark.cn。
func TestOriginForCrossHostCallUsesPageOrigin(t *testing.T) {
	var (
		mu        sync.Mutex
		gotOrigin string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotOrigin = r.Header.Get("Origin")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":2000000}`)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	req, err := http.NewRequest(http.MethodGet, base+prefix+"/uop.quark.cn/cas/ajax/getTokenForQrcodeLogin?client_id=532", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", base) // 浏览器视角：页面与代理同源
	req.Header.Set("Referer", base+prefix+"/pan.quark.cn/login")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if gotOrigin != "https://pan.quark.cn" {
		t.Errorf("跨主机接口的 Origin 应还原为页面源站，实际 %q", gotOrigin)
	}
}

// TestOriginSameHostStillWorks 保证同主机接口的 Origin 还原不受影响。
func TestOriginSameHostStillWorks(t *testing.T) {
	var gotOrigin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	req, err := http.NewRequest(http.MethodPost, base+prefix+"/passport.quark.cn/api/sms/send", strings.NewReader("phone=13700000000"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", base)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	if gotOrigin != "https://passport.quark.cn" {
		t.Errorf("同主机接口 Origin 应为目标主机，实际 %q", gotOrigin)
	}
}

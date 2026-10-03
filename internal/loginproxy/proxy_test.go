// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package loginproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 纯函数：域名白名单与地址改写
// ---------------------------------------------------------------------------

func TestAllowedHost(t *testing.T) {
	allow := []string{"pan.quark.cn", "drive-pc.quark.cn", "drive-h.quark.cn"}
	deny := []string{"", "evil.com", "quark.cn", "pan.quark.cn.evil.com", "user@pan.quark.cn", "pan.quark.cn:8080"}

	for _, h := range allow {
		if !allowedHost(h) {
			t.Fatalf("应当允许：%q", h)
		}
	}
	for _, h := range deny {
		if allowedHost(h) {
			t.Fatalf("应当拒绝：%q", h)
		}
	}
}

func TestLocalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://pan.quark.cn/list", "/__proxy/pan.quark.cn/list"},
		{"http://drive-pc.quark.cn/1/file/share?a=1", "/__proxy/drive-pc.quark.cn/1/file/share?a=1"},
		{"//drive-h.quark.cn/x", "/__proxy/drive-h.quark.cn/x"},
		// JSON / JS 字符串里的 "\/" 等价于 "/"，保留转义是安全且更省事的写法
		{"https:\\/\\/pan.quark.cn\\/api", "/__proxy/pan.quark.cn\\/api"},
		{"https://example.com/a", "https://example.com/a"},
		{"relative/path", "relative/path"},
	}
	for _, c := range cases {
		if got := localize(c.in); got != c.want {
			t.Errorf("localize(%q)\n got = %q\nwant = %q", c.in, got, c.want)
		}
	}
}

func TestRemoteize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://127.0.0.1:1234/__proxy/pan.quark.cn/list", "https://pan.quark.cn/list"},
		{"/__proxy/drive-pc.quark.cn/x", "https://drive-pc.quark.cn/x"},
		{"https://example.com/a", "https://example.com/a"},
	}
	for _, c := range cases {
		if got := remoteize(c.in); got != c.want {
			t.Errorf("remoteize(%q)\n got = %q\nwant = %q", c.in, got, c.want)
		}
	}
}

func TestRewriteCookie(t *testing.T) {
	c := &http.Cookie{
		Name:     "__pus",
		Value:    "abc",
		Domain:   ".quark.cn",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
	}
	got := rewriteCookie(c, "pan.quark.cn")
	if got.Domain != "" {
		t.Errorf("Domain 应当被剥离，实际 %q", got.Domain)
	}
	if got.Path != "/__proxy/pan.quark.cn/" {
		t.Errorf("Path 应当圈到代理前缀，实际 %q", got.Path)
	}
	if got.Secure {
		t.Error("本地是 http，Secure 必须为 false")
	}
	if got.SameSite == http.SameSiteNoneMode {
		t.Error("SameSite=None 必须降级，否则浏览器会丢弃该 Cookie")
	}
	if !got.HttpOnly {
		t.Error("HttpOnly 应当保留")
	}
}

func TestRelocate(t *testing.T) {
	cases := []struct {
		location, host, want string
	}{
		{"https://passport.quark.cn/login?r=1", "pan.quark.cn", "/__proxy/passport.quark.cn/login?r=1"},
		{"/sign", "pan.quark.cn", "/__proxy/pan.quark.cn/sign"},
		{"/__proxy/pan.quark.cn/done", "pan.quark.cn", "/__proxy/pan.quark.cn/done"},
		{"https://example.com/x", "pan.quark.cn", "https://example.com/x"},
		{"", "pan.quark.cn", ""},
	}
	for _, c := range cases {
		if got := relocate(c.location, c.host); got != c.want {
			t.Errorf("relocate(%q, %q)\n got = %q\nwant = %q", c.location, c.host, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Cookie 解析与合并
// ---------------------------------------------------------------------------

func TestParseCookieHeader(t *testing.T) {
	got := parseCookieHeader(`__pus=abc; __puus=def; _UP_A=x%20y; broken; =novalue; tfstk="quoted"`)
	if got["__pus"] != "abc" || got["__puus"] != "def" {
		t.Fatalf("关键凭证未解析出来：%#v", got)
	}
	if got["_UP_A"] != "x%20y" {
		t.Errorf("_UP_A 解析错误：%q", got["_UP_A"])
	}
	if got["tfstk"] != "quoted" {
		t.Errorf("引号未处理：%q", got["tfstk"])
	}
	if len(got["broken"]) != 0 {
		t.Error("无等号片段应当被忽略")
	}
}

func TestJoinCookiesSorted(t *testing.T) {
	got := joinCookies(map[string]string{"b": "2", "a": "1", "empty": ""})
	if got != "a=1; b=2;" {
		t.Fatalf("拼接结果不符合预期：%q", got)
	}
	if joinCookies(nil) != "" {
		t.Error("空 map 应返回空字符串")
	}
}

func TestHasCredential(t *testing.T) {
	if !hasCredential(map[string]string{"__pus": "x"}) {
		t.Error("含 __pus 应判定为已登录")
	}
	if !hasCredential(map[string]string{"__puus": "x"}) {
		t.Error("含 __puus 应判定为已登录")
	}
	if hasCredential(map[string]string{"other": "x"}) {
		t.Error("无关 Cookie 不应判定为已登录")
	}
}

// ---------------------------------------------------------------------------
// 端到端：伪浏览器 -> 本地代理 -> 伪装的上游站点
// ---------------------------------------------------------------------------

// upstreamTransport 把对 https://*.quark.cn 的请求改写到 httptest 站点，
// 这样不必真的联网，也能完整走一遍 net/http 客户端的行为。
type upstreamTransport struct {
	base string // 形如 http://127.0.0.1:PORT
}

func (u *upstreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := u.base + req.URL.RequestURI()
	r, err := http.NewRequest(req.Method, target, req.Body)
	if err != nil {
		return nil, err
	}
	for k, vs := range req.Header {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	return http.DefaultTransport.RoundTrip(r)
}

// newTestSession 返回一个把上游指向 upstream 的会话，并把收敛宽限期缩短以便测试。
func newTestSession(t *testing.T, upstream *httptest.Server) *Session {
	t.Helper()
	s, err := Start(30 * time.Second)
	if err != nil {
		t.Fatalf("启动会话失败：%v", err)
	}
	s.settleDelay = 80 * time.Millisecond
	s.client.Transport = &upstreamTransport{base: upstream.URL}
	t.Cleanup(s.Close)
	return s
}

// TestConcurrentTraffic 用并发流量压一遍：多个主机同时下发 Cookie，
// 期间还有 goroutine 并发读取快照。本机 race detector 跑不起来，用这个兜底。
func TestConcurrentTraffic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "P", Domain: ".quark.cn", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "host_" + strings.ReplaceAll(r.Host, ".", "_"), Value: r.URL.Path, Domain: ".quark.cn", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="https://drive-pc.quark.cn/x">x</a>`)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := http.Get(base + "/__proxy/host" + strconv.Itoa(i) + ".quark.cn/some/path")
			if err != nil {
				t.Errorf("并发请求失败：%v", err)
				return
			}
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}()
	}
	// 并发读取状态，验证锁的覆盖
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = s.Snapshot()
				time.Sleep(time.Millisecond)
			}
		}
	}()
	wg.Wait()
	close(stop)

	cookie, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("并发场景下等待失败：%v", err)
	}
	if !strings.Contains(cookie, "__pus=P") {
		t.Errorf("并发场景下未捕获到凭证：%q", cookie)
	}
}

// newBrowser 模拟真实浏览器：自带 Cookie Jar，会把 Set-Cookie 按 path 回传。
func newBrowser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar 失败：%v", err)
	}
	return &http.Client{Jar: jar}
}

// TestEndToEndCapture 验证完整流程：
//  1. 浏览器访问入口页（上游下发若干 Set-Cookie）；
//  2. 浏览器带上这些 Cookie 再请求另一台主机；
//  3. 其中出现凭证后会话应当收敛，并把两台主机的 Cookie 合并成整段返回。
func TestEndToEndCapture(t *testing.T) {
	var seenReferer string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenReferer = r.Header.Get("Referer")
		switch {
		case r.URL.Path == "/": // 入口页：下发无凭证的普通 Cookie
			http.SetCookie(w, &http.Cookie{Name: "tfstk", Value: "T123", Domain: ".quark.cn", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "b-user-id", Value: "u-1", Domain: ".quark.cn", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<a href="https://drive-pc.quark.cn/next">next</a>`)
		case r.URL.Path == "/next": // 第二跳：下发真正的登录凭证
			http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "P-US", Domain: ".quark.cn", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "__puus", Value: "P-UUS", Domain: ".quark.cn", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "_UP_A", Value: "up-a", Domain: ".quark.cn", Path: "/", SameSite: http.SameSiteNoneMode})
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"api":"https:\/\/drive-h.quark.cn\/v1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	browser := newBrowser(t)

	// 第 1 步：打开入口页
	res, err := browser.Get(s.URL())
	if err != nil {
		t.Fatalf("访问入口页失败：%v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), `/__proxy/drive-pc.quark.cn/next`) {
		t.Errorf("页面里的绝对域名未被改写：%s", body)
	}

	// 第 2 步：跟随改写后的链接。手动带上 Referer（Go 客户端不会自动携带），
	// 目的是验证代理会把它还原成上游绝对地址。
	req, err := http.NewRequest("GET", "http://"+s.ln.Addr().String()+"/__proxy/drive-pc.quark.cn/next", nil)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	req.Header.Set("Referer", s.URL())
	res2, err := browser.Do(req)
	if err != nil {
		t.Fatalf("访问第二跳失败：%v", err)
	}
	body2, _ := io.ReadAll(res2.Body)
	res2.Body.Close()
	if !strings.Contains(string(body2), "/__proxy/drive-h.quark.cn") {
		t.Errorf("转义形式的域名未被本地化：%s", body2)
	}
	if seenReferer != "https://pan.quark.cn/" {
		t.Errorf("回传给上游的 Referer 未被还原：%q", seenReferer)
	}

	cookie, err := s.Wait(context.Background())
	if err != nil {
		t.Fatalf("等待登录结果失败：%v", err)
	}
	for _, want := range []string{"__pus=P-US", "__puus=P-UUS", "tfstk=T123", "b-user-id=u-1", "_UP_A=up-a"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("捕获结果缺少 %s：%q", want, cookie)
		}
	}
}

// TestRejectNonWhitelisted 确保本机代理不会变成任意网站的转发器。
func TestRejectNonWhitelisted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	res, err := http.Get("http://" + s.ln.Addr().String() + "/__proxy/example.com/steal")
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("白名单外的域名应当 403，实际 %d", res.StatusCode)
	}
}

// TestTimeout 验证超时会自动收摊，不会让端口长期挂着。
func TestTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s, err := Start(600 * time.Millisecond)
	if err != nil {
		t.Fatalf("启动会话失败：%v", err)
	}
	defer s.Close()
	s.client.Transport = &upstreamTransport{base: upstream.URL}

	// 只访问不带任何 Cookie 的页面，不该触发收敛，只能等到超时
	if _, err := http.Get(s.URL()); err != nil {
		t.Fatalf("访问失败：%v", err)
	}
	if _, err := s.Wait(context.Background()); err == nil {
		t.Fatal("超时场景下应当返回 error")
	}
}

// TestSnapshotShowsProgress 验证界面轮询能拿到「已收敛 / 收集条数」。
func TestSnapshotShowsProgress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "P", Domain: ".quark.cn", Path: "/"})
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	browser := newBrowser(t)

	ready, collected := s.Snapshot()
	if ready || collected != 0 {
		t.Fatalf("初始状态应为空：ready=%v collected=%d", ready, collected)
	}

	if _, err := browser.Get(s.URL()); err != nil {
		t.Fatalf("访问失败：%v", err)
	}
	// 首次请求只带 Set-Cookie，真正携带 Cookie 的是下一次请求
	if _, err := browser.Get(s.URL()); err != nil {
		t.Fatalf("第二次访问失败：%v", err)
	}

	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("等待失败：%v", err)
	}
	ready, collected = s.Snapshot()
	if !ready {
		t.Error("完成后 ready 应为 true")
	}
	if collected == 0 {
		t.Error("应当至少收集到一条 Cookie")
	}
}

// TestRemainingCountsDown 验证倒计时随时间递减且不为负。
func TestRemainingCountsDown(t *testing.T) {
	s, err := Start(90 * time.Second)
	if err != nil {
		t.Fatalf("启动会话失败：%v", err)
	}
	defer s.Close()
	if r := s.Remaining(); r < 80 || r > 91 {
		t.Errorf("剩余秒数应在 90 附近，实际 %d", r)
	}
	s.deadline = time.Now().Add(-time.Second)
	if r := s.Remaining(); r != 0 {
		t.Errorf("已过期应返回 0，实际 %d", r)
	}
}

// TestBannerInjectedIntoHTML 验证被代理的 HTML 页面带上了说明横幅。
func TestBannerInjectedIntoHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body><div>hi</div></body></html>")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	res, err := http.Get(s.URL())
	if err != nil {
		t.Fatalf("访问失败：%v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "__quark_proxy_banner") {
		t.Errorf("页面未注入横幅：%s", body)
	}
	// 注入点在 <body> 之后，原有内容必须还在
	if !strings.Contains(string(body), "<div>hi</div>") {
		t.Errorf("注入横幅破坏了原页面：%s", body)
	}
}

// TestDonePageAfterCapture 验证捕获凭证后浏览器拿到收尾页，
// 而不是代理已关停导致的「无法访问此网站」。
func TestDonePageAfterCapture(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "P", Domain: ".quark.cn", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>login</body></html>")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	browser := newBrowser(t)
	base := "http://" + s.ln.Addr().String()

	// 首访问下发凭证，第二次请求才带上 Cookie（与真实浏览器一致）
	for i := 0; i < 3; i++ {
		res, err := browser.Get(base + prefix + "/pan.quark.cn/")
		if err != nil {
			t.Fatalf("第 %d 次访问失败：%v", i+1, err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if !s.captured() {
			continue
		}
		break
	}
	if _, err := s.Wait(context.Background()); err != nil {
		t.Fatalf("等待凭证失败：%v", err)
	}

	res, err := http.Get(base + prefix + "/pan.quark.cn/")
	if err != nil {
		t.Fatalf("收尾页访问失败：%v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "登录成功") {
		t.Errorf("捕获后应返回收尾页，实际：%s", body)
	}
}

// TestInjectBannerKeepsNonHTML 验证没有 <body> 的内容不会被改坏。
func TestInjectBannerKeepsNonHTML(t *testing.T) {
	in := []byte(`{"a":"https://pan.quark.cn/x"}`)
	got := string(injectBanner(in))
	if !strings.Contains(got, "pan.quark.cn") {
		t.Errorf("无 body 的内容应原样返回：%s", got)
	}
	if strings.Contains(got, "__quark_proxy_banner") {
		t.Error("无 body 时不应注入横幅")
	}
}

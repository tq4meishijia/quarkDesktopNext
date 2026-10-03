// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Package loginproxy 提供「交互式登录」所需的本地临时反向代理。
//
// 背景：夸克没有面向第三方应用的 OAuth 授权入口，可用凭证只有浏览器登录后那段
// Cookie。让用户手抄有两个问题：一是操作繁琐，二是手抄往往只粘到 __pus/__puus，
// 而部分下载链接的回调校验需要网页上那一整段（含 _UP_*、tfstk 等 HttpOnly 字段，
// 在页面里根本看不到）。
//
// 做法：在本机起一个只代理 *.quark.cn 的临时反向代理，让用户照常在浏览器里登录，
// 由代理从请求头的 Cookie 里拿到「整段」凭证。
//
// 安全边界（三条都不能松）：
//  1. 只监听 127.0.0.1 的随机空闲端口，外部机器连不上；
//  2. 只转发 allowedSuffix 列出的域名后缀，其余一律 403，避免沦为开放代理；
//  3. 只在登录期间存活：完成、取消或超时后立即关闭服务器。
package loginproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// prefix 是区分不同上游主机的路径前缀：/__proxy/<host>/<path>。
	// 每台主机用独立路径前缀，浏览器才能按 path 隔离地回传各自的 Cookie。
	prefix = "/__proxy"

	// defaultTimeout 是登录等待上限，超时后自动收摊。
	defaultTimeout = 5 * time.Minute

	// settleDelay 是探测到凭证后继续收集的宽限期。登录成功的一瞬间浏览器通常会
	// 连续发出若干请求，多等一会儿才能把 _UP_* / tfstk 之类一并收齐。
	settleDelay = 1500 * time.Millisecond

	// maxRewriteBody 是改写响应体的上限，超过就直接原样转发（登录页远小于此）。
	maxRewriteBody = 8 << 20

	// gracePeriod 是捕获凭证后继续服务的时间：这段时间里浏览器会拿到
	// 「登录成功」的收尾页，而不是连接被拒绝的白屏。
	gracePeriod = 8 * time.Second
)

// donePage 是捕获凭证后返回给浏览器的收尾页。
// 刻意做成不依赖外部资源的内联页面：此刻代理已收工，任何外部资源都取不到。
const donePage = `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">
<title>登录成功</title><style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
font:15px/1.7 -apple-system,"Segoe UI","Microsoft YaHei",sans-serif;background:#0f1115;color:#e6e8eb}
.box{text-align:center;padding:40px}
h1{font-size:20px;margin:0 0 10px}
p{color:#9aa1ab;margin:4px 0}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:#3ecf8e;margin-right:8px}
</style></head><body><div class="box">
<h1><span class="dot"></span>登录成功</h1>
<p>凭证已保存到本机，这个窗口可以直接关闭。</p>
<p>若窗口没有自动关闭，请手动关掉它。</p>
</div></body></html>`

// bannerHTML 是注入到被代理页面顶部的提示条。
// 目的很单纯：让用户知道「这个 127.0.0.1 页面是本客户端开的，不是可疑站点」，
// 以及登录完成后会发生什么——否则代理登录很容易被当成钓鱼页面而中断。
const bannerHTML = `<div id="__quark_proxy_banner" style="position:fixed;z-index:2147483647;
top:0;left:0;right:0;padding:8px 14px;background:#0f1115;color:#e6e8eb;
font:13px/1.5 -apple-system,'Segoe UI','Microsoft YaHei',sans-serif;
border-bottom:1px solid #2b3038;box-shadow:0 2px 8px rgba(0,0,0,.35)">
本窗口由夸克网盘桌面版临时打开，用于安全读取登录凭证（仅限 quark.cn）。登录成功后会自动关闭。
</div>`

// allowedSuffix 是可代理的域名后缀。
var allowedSuffix = []string{".quark.cn"}

// hostRe 匹配绝对 URL 中的 quark.cn 主机，用于把页面里的地址改回本地代理路径。
var hostRe = regexp.MustCompile(`(?i)(?:https?:)?//([a-z0-9][a-z0-9.-]*\.quark\.cn)`)

// escapedHostRe 匹配 JS / JSON 里被转义过的地址，例如 https:\/\/pan.quark.cn。
var escapedHostRe = regexp.MustCompile(`(?i)(?:https?:)?(?:\\/)+([a-z0-9][a-z0-9.-]*\.quark\.cn)`)

// Result 是一次登录流程的结局，Cookie 是合并后的整段凭证。
type Result struct {
	Cookie string
	Err    error
}

// Session 是一次交互式登录的会话，并发安全。
type Session struct {
	ln     net.Listener
	srv    *http.Server
	client *http.Client

	timeout     time.Duration
	settleDelay time.Duration
	deadline    time.Time

	mu    sync.Mutex
	jar   map[string]string // 合并后的 Cookie：name -> value
	ready bool

	resultOnce sync.Once
	readyOnce  sync.Once
	closeOnce  sync.Once
	resultCh   chan Result
	done       chan struct{}
}

// Start 起一个本地代理并返回会话；调用方最终必须调用 Close 释放端口。
func Start(timeout time.Duration) (*Session, error) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Session{
		ln:          ln,
		jar:         make(map[string]string),
		resultCh:    make(chan Result, 1),
		done:        make(chan struct{}),
		timeout:     timeout,
		settleDelay: settleDelay,
		deadline:    time.Now().Add(timeout),
	}
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 15 * time.Second}
	s.client = &http.Client{
		Timeout: 30 * time.Second,
		// 重定向必须手动处理：Location 要改写回本地路径，不能直接放浏览器去跳。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	go func() { _ = s.srv.Serve(ln) }()
	go s.watchdog()
	return s, nil
}

// URL 是让用户打开的入口地址。
func (s *Session) URL() string {
	return "http://" + s.ln.Addr().String() + prefix + "/pan.quark.cn/"
}

// Wait 阻塞到登录完成、失败或超时；ctx 取消时同样返回。
func (s *Session) Wait(ctx context.Context) (string, error) {
	select {
	case res := <-s.resultCh:
		return res.Cookie, res.Err
	case <-ctx.Done():
		s.finish(Result{Err: ctx.Err()})
		return "", ctx.Err()
	}
}

// Snapshot 非阻塞地查看进度，供界面轮询展示。
func (s *Session) Snapshot() (ready bool, collected int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready, len(s.jar)
}

// Remaining 返回距离超时的剩余秒数，已收敛或已关闭时返回 0。
// 界面用它显示倒计时，避免用户对着一个不动的进度条猜要等多久。
func (s *Session) Remaining() int {
	left := time.Until(s.deadline).Seconds()
	if left <= 0 {
		return 0
	}
	return int(left) + 1
}

// Close 关闭会话与端口，可重复调用。
func (s *Session) Close() {
	s.closeOnce.Do(func() { close(s.done) })
	_ = s.srv.Close()
}

// Done 返回一个在会话结束（成功 / 失败 / 取消 / 超时）后关闭的 channel。
func (s *Session) Done() <-chan struct{} { return s.done }

// watchdog 到期自动结束，避免端口一直挂着。
func (s *Session) watchdog() {
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		s.finish(Result{Err: errors.New("等待登录超时")})
	case <-s.done:
	}
}

// captured 返回是否已捕获登录凭证（用于决定是否进入收尾页阶段）。
func (s *Session) captured() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// finish 落地结果，保证只写一次。
//
// 成功时不立即关停：留一段宽限期让浏览器把收尾页显示出来，
// 否则用户会看到「无法访问此网站」，误以为登录失败又点一遍。
func (s *Session) finish(res Result) {
	s.resultOnce.Do(func() { s.resultCh <- res })
	if res.Err != nil {
		s.Close()
		return
	}
	timer := time.NewTimer(gracePeriod)
	go func() {
		defer timer.Stop()
		select {
		case <-timer.C:
			s.Close()
		case <-s.done:
		}
	}()
}

// scheduleFinish 在探测到凭证后再收集一小会儿，然后收摊。
func (s *Session) scheduleFinish() {
	s.readyOnce.Do(func() {
		s.mu.Lock()
		s.ready = true
		s.mu.Unlock()
		go func() {
			timer := time.NewTimer(s.settleDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-s.done:
				return
			}
			s.mu.Lock()
			cookie := joinCookies(s.jar)
			s.mu.Unlock()
			if cookie == "" {
				s.finish(Result{Err: errors.New("未捕获到任何 Cookie")})
				return
			}
			s.finish(Result{Cookie: cookie})
		}()
	})
}

// absorbOne 合并单个 Cookie（通常来自上游的 Set-Cookie），返回是否首次发现凭证。
func (s *Session) absorbOne(name, value string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(value) == "" {
		return false
	}
	s.mu.Lock()
	s.jar[name] = value
	already := s.ready
	s.mu.Unlock()
	if already {
		return false
	}
	_, ok := s.jar[name]
	return ok && isCredential(name)
}

// ---------- HTTP ----------

func (s *Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 凭证已捕获后的宽限期内，一律返回收尾页，避免浏览器撞上连接被拒。
	if s.captured() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, donePage)
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, prefix+"/pan.quark.cn/", http.StatusFound)
		return
	}
	if !strings.HasPrefix(r.URL.Path, prefix+"/") {
		http.Error(w, "未找到", http.StatusNotFound)
		return
	}
	host, rest := splitHost(strings.TrimPrefix(r.URL.Path, prefix+"/"))
	if !allowedHost(host) {
		// 白名单之外一律拒绝：本机代理绝不能变成任意网站的转发器。
		http.Error(w, "该域名不在允许范围内", http.StatusForbidden)
		return
	}
	if raw := r.Header.Get("Cookie"); raw != "" && s.absorb(raw) {
		s.scheduleFinish()
	}
	s.forward(w, r, host, rest)
}

// splitHost 把 "pan.quark.cn/xxx" 拆成 ("pan.quark.cn", "/xxx")。
func splitHost(p string) (string, string) {
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return "", ""
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], p[i:]
	}
	return p, "/"
}

// allowedHost 判断主机是否允许被代理。
func allowedHost(host string) bool {
	host = strings.ToLower(host)
	if host == "" || strings.ContainsAny(host, "@: ") {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, suffix := range allowedSuffix {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}

// forward 把请求发往 https://<host><rest>，并在回程改写响应。
func (s *Session) forward(w http.ResponseWriter, r *http.Request, host, rest string) {
	target := "https://" + host + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		http.Error(w, "构造上游请求失败", http.StatusBadGateway)
		return
	}
	if r.Body != nil {
		defer r.Body.Close()
	}

	copyRequestHeader(req.Header, r.Header)
	req.Header.Set("Host", host)
	if v := req.Header.Get("Referer"); v != "" {
		req.Header.Set("Referer", remoteize(v))
	}
	// 不请求压缩：改写 HTML 需要明文，identity 最省事也最稳。
	req.Header.Del("Accept-Encoding")

	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "访问上游失败："+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	s.writeResponse(w, resp, host)
}

// proxyHostRe 与 hostRe 互为反向：localize 负责 remote -> local，remoteize 反之。
// 锚定开头是安全的：Referer 这类要回上游的值，本身就以 scheme 或 "/" 起始。
var proxyHostRe = regexp.MustCompile(`(?i)^(?:(?:[a-z][a-z0-9+.-]*)://[^/]+)?/__proxy/([a-z0-9][a-z0-9.-]*\.quark\.cn)`)

// remoteize 把本地代理路径还原成上游绝对地址，用于 Referer 等需要回上游的头。
func remoteize(s string) string {
	return proxyHostRe.ReplaceAllString(s, "https://$1")
}

func (s *Session) writeResponse(w http.ResponseWriter, resp *http.Response, host string) {
	dst := w.Header()
	for k, values := range resp.Header {
		switch strings.ToLower(k) {
		case "content-length", "location", "set-cookie",
			"content-security-policy", "content-security-policy-report-only",
			"strict-transport-security", "transfer-encoding", "connection",
			"keep-alive", "upgrade":
			// 这些要么会被改写，要么不适合透传（CSP 会把资源引回真实域名）
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}

	location := resp.Header.Get("Location")
	if location != "" {
		dst.Set("Location", relocate(location, host))
	}
	for _, c := range resp.Cookies() {
		http.SetCookie(w, rewriteCookie(c, host))
		// 直接从下发动作采集，是最及时也最完整的信号：
		// 浏览器第一次访问某台主机时请求头里还没有 Cookie，只有等下一次才会回传。
		if s.absorbOne(c.Name, c.Value) {
			s.scheduleFinish()
		}
	}

	ct := resp.Header.Get("Content-Type")
	var body []byte
	if shouldRewrite(ct) {
		limited := io.LimitReader(resp.Body, maxRewriteBody+1)
		buf, err := io.ReadAll(limited)
		if err != nil {
			http.Error(w, "读取上游响应失败", http.StatusBadGateway)
			return
		}
		if len(buf) <= maxRewriteBody {
			body = injectBanner(localizeBytes(buf))
		} else {
			body = buf
		}
	}

	w.WriteHeader(resp.StatusCode)
	if body != nil {
		_, _ = w.Write(body)
		return
	}
	_, _ = io.Copy(w, resp.Body)
}

// copyRequestHeader 逐头复制：丢掉 hop-by-hop 头，Cookie 单独处理。
func copyRequestHeader(dst, src http.Header) {
	for k, values := range src {
		lower := strings.ToLower(k)
		if lower == "host" || lower == "accept-encoding" {
			continue
		}
		if hopByHop[lower] {
			continue
		}
		if lower == "cookie" {
			dst["Cookie"] = append([]string{}, values...)
			continue
		}
		dst[k] = append([]string{}, values...)
	}
}

var hopByHop = map[string]bool{
	"connection":          true,
	"proxy-connection":    true,
	"keep-alive":          true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"expect":              true,
	"content-length":      true,
}

// rewriteCookie 去掉 Domain、把 Path 圈到本主机的代理前缀下，
// 这样浏览器会「按路径」分别为每台主机保存并回传 Cookie。
func rewriteCookie(c *http.Cookie, host string) *http.Cookie {
	out := *c
	out.Domain = ""
	out.Secure = false
	// SameSite=None 必须配合 Secure 使用，本地是 http，直接降级为不声明。
	if out.SameSite == http.SameSiteNoneMode {
		out.SameSite = http.SameSiteDefaultMode
	}
	out.Path = prefix + "/" + host + "/"
	return &out
}

// relocate 把上游重定向地址改写回本地代理路径。
func relocate(location, curHost string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return location
	}
	if !strings.Contains(location, "://") {
		switch {
		case strings.HasPrefix(location, prefix+"/"):
			return location
		case strings.HasPrefix(location, "/"):
			return prefix + "/" + curHost + location
		default:
			return location
		}
	}
	return localize(location)
}

// localize 把 quark.cn 的绝对地址换成本地代理路径。
func localize(s string) string {
	s = escapedHostRe.ReplaceAllString(s, prefix+"/$1")
	return hostRe.ReplaceAllString(s, prefix+"/$1")
}

// localizeBytes 同 localize，作用于响应体。
func localizeBytes(b []byte) []byte {
	b = escapedHostRe.ReplaceAll(b, []byte(prefix+"/$1"))
	return hostRe.ReplaceAll(b, []byte(prefix+"/$1"))
}

// injectBanner 在 HTML 的 <body> 后插入提示条。
// 找不到 <body> 时直接放弃注入：提示条只是体验增强，不值得为此改坏页面结构。
func injectBanner(b []byte) []byte {
	lower := bytes.ToLower(b)
	i := bytes.Index(lower, []byte("<body"))
	if i < 0 {
		return b
	}
	j := bytes.IndexByte(b[i:], '>')
	if j < 0 {
		return b
	}
	at := i + j + 1
	out := make([]byte, 0, len(b)+len(bannerHTML))
	out = append(out, b[:at]...)
	out = append(out, bannerHTML...)
	out = append(out, b[at:]...)
	return out
}

// shouldRewrite 判断响应体是否需要改写域名。
func shouldRewrite(contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, needle := range []string{"text/html", "application/javascript", "text/javascript", "application/json", "text/css"} {
		if strings.Contains(ct, needle) {
			return true
		}
	}
	return false
}

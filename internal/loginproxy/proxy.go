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
	"net/url"
	"regexp"
	"strconv"
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
	//
	// 必须留足：凭证捕获后夸克页面会自行跳到 /list 并继续加载它自己的
	// JS/CSS/接口，这些子资源会陆续发出好几秒。8 秒太紧，浏览器表现为
	// 「文档 HTML 加载到了、脚本没跑起来」的白屏（实测 ERR_CONNECTION_REFUSED）。
	// 30 秒足够整个 SPA 加载完，又远小于 defaultTimeout，不会让端口长时间挂着。
	gracePeriod = 30 * time.Second

	// maxRequestBody 是转发请求体的上限。登录表单体积极小，这个上限只是防御性的，
	// 避免本机代理被当成任意大文件的转发器。
	maxRequestBody = 4 << 20
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
<p>凭证已保存到本机，客户端已自动进入主界面。</p>
<p>这个页面之后会自动关闭；若没有关闭，请手动关掉它即可。</p>
<p>代理在稍后会自行退出，本机地址将不再可用，属正常现象。</p>
</div></body></html>`

// bannerMarker 是提示条的自定义属性名，用作「本页已注入」的标记。
// 手机登录表单在 iframe 里加载（/cas/custom/login），iframe 文档会再走一遍代理，
// 没有这个标记就会重复注入，把登录框往下挤一大截。
const bannerMarker = "__quarkProxyBannerInjected"

// bannerHTML 是注入到被代理页面顶部的提示条。
// 目的很单纯：让用户知道「这个 127.0.0.1 页面是本客户端开的，不是可疑站点」，
// 以及登录完成后会发生什么——否则代理登录很容易被当成钓鱼页面而中断。
const bannerHTML = `<div id="__quark_proxy_banner" data-` + bannerMarker + `="1" style="position:fixed;z-index:2147483647;
top:0;left:0;right:0;padding:8px 14px;background:#0f1115;color:#e6e8eb;
font:13px/1.5 -apple-system,'Segoe UI','Microsoft YaHei',sans-serif;
border-bottom:1px solid #2b3038;box-shadow:0 2px 8px rgba(0,0,0,.35)">
本窗口由夸克网盘桌面版临时打开，用于安全读取登录凭证（仅限 quark.cn）。登录成功后会自动关闭。
</div>`

// allowedSuffix 是可代理的域名后缀。
//
// 除 quark.cn 外必须放行 g.alicdn.com：登录页的 JS bundle（1.6MB，含取码/发码/
// 校验等全部接口地址）就托管在这个 CDN 上，而 bundle 不经代理，代理就永远没机会
// 改写它内部硬编码的 https://uop.quark.cn/... 与 https://pan.quark.cn/api/...。
// 浏览器随后从 127.0.0.1 页面直连这两个域，夸克只对 *.quark.cn 源返回
// Access-Control-Allow-Origin，于是全部 XHR 被 CORS 拦掉——表现就是二维码区域
// 「网络异常」、短信验证码提交后无响应。img.alicdn.com 是同一 CDN 的图片域。
var allowedSuffix = []string{".quark.cn", ".alicdn.com"}

// scanHostRe 匹配「手机端扫码落地页」主机：这类地址会被拼进二维码图片的内容里
// （bundle 里 Hk(scanLoginPage, {token, client_id, ssb}) 生成指向
// https://su.quark.cn/4_eMHBJ?token=... 的码），由用户手机访问、不经过本机浏览器。
// 改写成 127.0.0.1 路径后二维码内容即被污染，手机扫码会指向打不开的本机地址。
// Go 的 RE2 不支持负向前瞻，因此 localize 采取「占位保护 → 改写 → 还原」三步走。
// 同一条规则要同时覆盖正常 URL 与 bundle 里转义过的 https:\/\/su.quark.cn\/...：
// 写成「可选反斜杠 + 斜杠」即可同时命中 \/\/ 与 // 两种形态。
var scanHostRe = regexp.MustCompile(`(?i)(?:https?:)?\\?/\\?/(?:su|qr|scan)\.quark\.cn`)

// localizeHostRe 是 localize 实际使用的主机匹配（remote -> local）；
// proxyHostRe 是它的反向（local -> remote），见 remoteize。
var localizeHostRe = regexp.MustCompile(`(?i)(?:https?:)?//([a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com))`)

// localizeEscapedHostRe 同上，处理 JS / JSON 里转义过的地址。
var localizeEscapedHostRe = regexp.MustCompile(`(?i)(?:https?:)?(?:\\/)+([a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com))`)

// scanPlaceholder 是扫码落地页在改写期间的占位前缀。\x00 不可能出现在合法 URL 里，
// 拼上序号即可精确还原被摘走的原文。
const scanPlaceholder = "\x00scanlanding-"

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

	// lastHost 记录最近一次可信的代理目标主机。SPA 登录页普遍用客户端路由
	// （history.pushState 到根绝对路径）或声明 referrer-policy=no-referrer，
	// 之后浏览器发出的根绝对路径请求不再带可用的 /__proxy/<host>/ Referer，
	// 此时用「最近一次文档所属主机」兜底推断，否则二维码、短信等接口全部 404。
	lastHost string

	// requireNew 为 true 时启用「强制重新登录」：见 Options.RequireFreshLogin。
	requireNew bool
	// baseline 记录会话开始时浏览器已持有的凭证值，用于识别「这次真的登录了」。
	baseline map[string]string
	// staleExisting 为 true 表示检测到浏览器里已有可用凭证（因而跳过了自动收敛），
	// 前端据此提示用户：要么去浏览器里退出夸克，要么接受直接进入。
	staleExisting bool

	resultOnce sync.Once
	readyOnce  sync.Once
	closeOnce  sync.Once
	resultCh   chan Result
	done       chan struct{}
}

// Options 是 Start 的可选参数，缺省值即为零值语义。
type Options struct {
	// RequireFreshLogin 为 true 时，只接受「本次会话中新出现的凭证」。
	//
	// 背景：用户在客户端里清除了凭证，但浏览器里那份夸克 Cookie 还在。
	// 代理起来后第一个请求就带着旧的 __pus/__puus，会被立刻判定为「登录完成」，
	// 界面直接跳进主界面——用户以为要重新登录，实际什么都没发生。
	// 打开该选项后，启动时浏览器已有的凭证只记为基线，不算登录成功；
	// 必须等到浏览器完成一次真实登录、下发与基线不同的新凭证，才收敛。
	RequireFreshLogin bool
}

// Start 起一个本地代理并返回会话；调用方最终必须调用 Close 释放端口。
func Start(timeout time.Duration) (*Session, error) {
	return StartWith(timeout, Options{})
}

// StartWith 与 Start 相同，但接受额外选项。
func StartWith(timeout time.Duration, opts Options) (*Session, error) {
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
		baseline:    make(map[string]string),
		resultCh:    make(chan Result, 1),
		done:        make(chan struct{}),
		timeout:     timeout,
		settleDelay: settleDelay,
		deadline:    time.Now().Add(timeout),
		requireNew:  opts.RequireFreshLogin,
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
//
// 结尾不带斜杠：/__proxy/pan.quark.cn。实测上游 https://pan.quark.cn 与
// https://pan.quark.cn/ 返回完全一致的 SSR 首页（均为 200 / 42463 字节），
// 而 splitHost 对无斜杠路径会正确回落到 "/"、HasPrefix 判断也照样匹配，
// 因此这里用更简洁的形式，不必担心少一个斜杠导致解析异常。
func (s *Session) URL() string {
	return "http://" + s.ln.Addr().String() + prefix + "/pan.quark.cn"
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

// StaleExistingCredential 报告是否在强制重新登录模式下检测到「浏览器里已有可用凭证」。
// 出现这种情况说明代理不会自动收敛：用户要么去浏览器退出夸克后重登，
// 要么显式选择沿用现有凭证。前端据此给出明确提示，避免用户干等。
func (s *Session) StaleExistingCredential() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.staleExisting
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

// mergedCookie 返回当前已采集 Cookie 的合并串，供根路径请求回注。
// 根路径请求（/api/xxx、/static/xxx）不带 /__proxy/<host>/ 前缀，
// 浏览器不会把 path-scoped 的会话 Cookie 发出来，代理需自己补上。
func (s *Session) mergedCookie() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return joinCookies(s.jar)
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
	// Set-Cookie 走「上游刚写下」的语义：基线里没有该凭证名时即本次登录产生。
	fresh := s.detectFreshFromSetCookieLocked(name, value)
	s.mu.Unlock()
	if already {
		return false
	}
	return fresh
}

// ---------- HTTP ----------

func (s *Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 凭证已捕获后的宽限期内，只对「文档导航」返回收尾页。
	//
	// 这里必须区分导航与子资源：登录成功（扫码或短信）后夸克页面会自行跳到
	// /list 等路径，并继续加载它的 JS/CSS/接口。若把子资源也换成收尾页 HTML，
	// 浏览器会把一段 HTML 当 JS 解析，页面白屏，且用户看不到任何「可以关窗口了」
	// 的提示——这正是「扫码成功后不跳转、卡在白屏」的直接原因。
	if s.captured() && isDocumentNavigation(r) {
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
		// SPA 登录页会用根绝对路径请求资源与接口（/static/js/app.js、/api/xxx），
		// 浏览器把它们解析成 http://127.0.0.1:<port>/... —— 不带 /__proxy/<host> 前缀。
		// 直接 404 会导致资源加载失败、页面白屏或接口「网络异常」。
		// 推断顺序：Referer 里的 /__proxy/<host>/ 前缀 → 会话记录的最近文档主机
		// （覆盖 pushState 后 Referer 失去前缀、referrerpolicy=no-referrer 两种场景）。
		host := hostFromReferer(r)
		if host == "" {
			host = s.lastKnownHost()
		}
		if host != "" && allowedHost(host) {
			s.rememberHost(host)
			// 本机 Referer 若缺前缀（pushState 后的地址栏），还原成上游真实地址，
			// 上游按页面源站做 Referer 校验，收到 127.0.0.1 会拒绝。
			normalizeLocalReferer(r, host)
			// 合并回注：浏览器带来的任意本机 Cookie（如 SPA 自写的 ctoken）不能
			// 顶掉已采集的会话 Cookie——只判断 Cookie 头为空才回注的话，上游会
			// 拿到一个残缺会话，短信校验这类需要完整流程 Cookie 的接口报「验证码无效」。
			if raw := r.Header.Get("Cookie"); raw != "" && s.absorb(raw) {
				s.scheduleFinish()
			}
			if inj := s.mergedCookie(); inj != "" {
				r.Header.Set("Cookie", inj)
			}
			s.forward(w, r, host, r.URL.Path)
			return
		}
		http.Error(w, "未找到", http.StatusNotFound)
		return
	}
	host, rest := splitHost(strings.TrimPrefix(r.URL.Path, prefix+"/"))
	if !allowedHost(host) {
		// 白名单之外一律拒绝：本机代理绝不能变成任意网站的转发器。
		http.Error(w, "该域名不在允许范围内", http.StatusForbidden)
		return
	}
	s.rememberHost(host)
	if raw := r.Header.Get("Cookie"); raw != "" && s.absorb(raw) {
		s.scheduleFinish()
	}
	s.forward(w, r, host, rest)
}

// refererProxyRe 从 Referer 里提取 /__proxy/<host>/ 的主机段。
var refererProxyRe = regexp.MustCompile(`(?i)/__proxy/([a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com))/`)

// hostFromReferer 从 Referer 头推断当前页所属的上游主机；取不到返回空串。
// 只在 Referer 本身指向本机代理时采信，避免被任意外部来源诱导。
func hostFromReferer(r *http.Request) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || !isLocalHost(u.Hostname()) {
		return ""
	}
	m := refererProxyRe.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

// isLocalHost 判断主机名是否指向本机。
func isLocalHost(h string) bool {
	switch strings.ToLower(h) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// rememberHost 记录最近一次可信的代理目标主机（已通过 allowedHost 校验）。
func (s *Session) rememberHost(host string) {
	s.mu.Lock()
	s.lastHost = strings.ToLower(host)
	s.mu.Unlock()
}

// lastKnownHost 返回最近记录的代理目标主机；尚无记录时返回空串。
func (s *Session) lastKnownHost() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHost
}

// normalizeLocalReferer 把指向本机但缺少 /__proxy/<host>/ 前缀的 Referer
// 还原成上游真实地址（https://<host><path>）。SPA 客户端路由把地址栏改成
// http://127.0.0.1:<port>/login 之类的根路径后，浏览器后续请求的 Referer
// 会丢失代理前缀；直接透传的话上游按页面源站校验 Referer 会失败。
// 带前缀的 Referer 不动，由 forward 里的 remoteize 统一还原。
func normalizeLocalReferer(r *http.Request, host string) {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return
	}
	u, err := url.Parse(ref)
	if err != nil || !isLocalHost(u.Hostname()) {
		return
	}
	if strings.HasPrefix(u.Path, prefix+"/") {
		return
	}
	normalized := "https://" + host + u.Path
	if u.RawQuery != "" {
		normalized += "?" + u.RawQuery
	}
	r.Header.Set("Referer", normalized)
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
	// 注意：此处刻意不做 net.SplitHostPort 剥离端口。
	// 上面的 ContainsAny 已拒绝任何含 ":" 的 host，而 SplitHostPort 成功的
	// 前提正是含 ":"，故该分支在当前调用路径上不可达（历史遗留）。
	// 保留拒绝含 ":" 的行为即可覆盖带端口的输入（proxy_test.go 已断言）。
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

	// 请求体必须先读进内存再转发。直接透传 r.Body（服务器侧的不透明类型）会让
	// Go 客户端改用 Transfer-Encoding: chunked 并丢掉 Content-Length——
	// passport / 短信这类接口普遍按 Content-Length 读取，收到 chunked 会读成空 body，
	// 于是「发码成功但校验时参数为空」，前端表现为「验证码无效 / 网络异常」。
	body, err := readForwardBody(r)
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusRequestEntityTooLarge)
		return
	}

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, reqBody)
	if err != nil {
		http.Error(w, "构造上游请求失败", http.StatusBadGateway)
		return
	}
	if len(body) == 0 && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		// 明确表达「有 body 字段但为空」，避免某些服务端把缺 Content-Length 当异常。
		req.ContentLength = 0
	}

	copyRequestHeader(req.Header, r.Header)
	req.Header.Set("Host", host)
	if v := req.Header.Get("Referer"); v != "" {
		req.Header.Set("Referer", remoteize(v))
	}
	// Origin 必须还原成「页面源站」，而不是目标主机。登录页在 pan.quark.cn，
	// 但扫码登录的 CAS 接口在 uop.quark.cn：浏览器同源视角下 Origin 是本地代理，
	// 上游按页面源站（pan.quark.cn）做 Origin/CSRF 校验——按目标主机还原会被
	// 拒绝或缺失 CORS 放行头，扫码取码、轮询扫码状态这些调用全部落空。
	// 同主机接口两者等价，因此统一按页面源站还原。
	if v := req.Header.Get("Origin"); v != "" && isLocalOrigin(v) {
		req.Header.Set("Origin", "https://"+pageOriginHost(req.Header.Get("Referer"), host))
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

// readForwardBody 读取并限长待转发的请求体。登录表单体积极小，
// 上限只是防御性的，避免被当成任意大文件的转发器。
func readForwardBody(r *http.Request) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxRequestBody {
		return nil, errors.New("请求体过大")
	}
	return b, nil
}

// localOriginRe 匹配指向本机代理的 Origin（http://127.0.0.1:port 或 localhost）。
var localOriginRe = regexp.MustCompile(`(?i)^https?://(127\.0\.0\.1|localhost|\[::1\])(:\d+)?$`)

// isLocalOrigin 判断 Origin 是否指向本地代理本身。
func isLocalOrigin(v string) bool {
	return localOriginRe.MatchString(strings.TrimSpace(v))
}

// proxyHostRe 与 hostRe 互为反向：localize 负责 remote -> local，remoteize 反之。
// 锚定开头是安全的：Referer 这类要回上游的值，本身就以 scheme 或 "/" 起始。
var proxyHostRe = regexp.MustCompile(`(?i)^(?:(?:[a-z][a-z0-9+.-]*)://[^/]+)?/__proxy/([a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com))`)

// remoteize 把本地代理路径还原成上游绝对地址，用于 Referer 等需要回上游的头。
// rootHostNoSlashRe 匹配「还原后主机后面没有任何路径」的情况，例如
// https://pan.quark.cn（由入口地址 /__proxy/pan.quark.cn 还原而来）。
var rootHostNoSlashRe = regexp.MustCompile(`(?i)^(https://[a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com))$`)

func remoteize(s string) string {
	out := proxyHostRe.ReplaceAllString(s, "https://$1")
	// 入口地址不带尾斜杠（/__proxy/pan.quark.cn），还原后成了 https://pan.quark.cn。
	// 上游按 Referer 做同源校验，主机根路径必须带尾斜杠，否则会被判成跨源。
	// 只在「恰好是主机根」时补 /，/list 这类带路径的不受影响。
	return rootHostNoSlashRe.ReplaceAllString(out, "$1/")
}

// pageOriginHost 从（已还原成上游地址的）Referer 里提取发起页面的源站主机，
// 作为跨主机接口的 Origin 还原值；Referer 缺失或不可信时退回目标主机。
func pageOriginHost(referer, fallback string) string {
	if referer == "" {
		return fallback
	}
	if u, err := url.Parse(referer); err == nil && allowedHost(u.Hostname()) {
		return strings.ToLower(u.Hostname())
	}
	return fallback
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
	// 上游偶尔会返回「没有 Location 的重定向」（实测 pan.quark.cn/list 未登录时就是
	// 302 + Location 为空）。这种响应原样透传会让浏览器无从跟随：地址栏停在
	// /__proxy/pan.quark.cn/list，页面一片空白。补一个同主机根路径兜底即可。
	if location == "" && isRedirectWithoutLocation(resp.StatusCode) {
		location = "/"
	}
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
			// HTML 里内联着 SSR 数据，地址改写要区别对待：展示用文本（如把图片
			// 地址当标题的 title 字段）保持原样，否则会渲染出一行代理路径。
			if strings.Contains(strings.ToLower(ct), "text/html") {
				body = injectBanner(localizeHTML(buf))
			} else {
				body = injectBanner(localizeBytes(buf))
			}
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

// isDocumentNavigation 判断请求是否为「文档导航」（浏览器整页跳转到新地址），
// 而不是页面内部的子资源请求。
//
// 依据是浏览器必发的 Sec-Fetch-Dest：导航为 document，脚本/样式/接口分别是
// script、style、empty。该头不存在时（旧浏览器、被 curl 之类省略）退化为
// 「只要不是明显的脚本或接口就当导航」，宁可多显示一次收尾页，也不要把
// 收尾页 HTML 当成 JS 返回导致整页崩掉。
func isDocumentNavigation(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest"))) {
	case "document", "iframe", "frame":
		return true
	case "script", "style", "empty", "font", "image", "audio", "video", "manifest", "worker":
		return false
	}
	// 没有该头时用 Accept 兜底：HTML 导航请求会带 text/html。
	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" {
		// 连 Accept 都没有（裸 GET），仍按导航处理：这是最常见的 curl/简单请求形态。
		return r.Method == http.MethodGet
	}
	return strings.Contains(accept, "text/html")
}

// isRedirectWithoutLocation 判断是否为「缺 Location 的重定向」。
// 浏览器对这种响应无法跟随，等同于白屏，必须补一个 Location。
func isRedirectWithoutLocation(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
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

// localize 把 quark.cn / alicdn.com 的绝对地址换成本地代理路径。
// 扫码落地页主机（su/quark 等）必须原样保留，见 scanHostRe 的说明。
func localize(s string) string {
	return localizeExceptScanHosts(s)
}

// localizeBytes 同 localize，作用于响应体。
func localizeBytes(b []byte) []byte {
	return []byte(localize(string(b)))
}

// fetchableKeys 是「这个键的值会被浏览器真正发起请求」的 JSON 键名白名单。
//
// SSR 页面把配置数据内联在 <script> 里，其中既有要请求的地址
// （imageUrl / actionLink / backgroundImage / videoUrl…），
// 也有纯粹给人看的文本（title —— 夸克把图片地址直接当标题文案用）。
// 早先无差别改写会把 title 里的地址也换成 127.0.0.1 路径，
// 于是页面中部直接渲染出一行 /__proxy/image.quark.cn/...(png) 文字。
//
// 取舍：宁可漏改（图片加载失败只是少一张图），不可错改
// （把展示文案变成路径，视觉上明显且让人困惑）。
var fetchableKeys = map[string]bool{
	"src": true, "url": true, "href": true, "link": true, "action": true,
	"imageurl": true, "image": true, "icon": true, "iconurl": true,
	"backgroundimage": true, "background": true, "cover": true, "poster": true,
	"actionlink": true, "redirecturl": true, "callbackurl": true, "returnurl": true,
	"videourl": true, "logourl": true, "avatar": true, "avatarurl": true,
}

// jsonURLValueRe 匹配 JSON 字符串值里的绝对地址。
//
// 两个容易踩的坑，都已在注释里标明：
//  1. 斜杠前用 \\? 表示「可选的反斜杠」，这样 https://…、https:\/\/…、//…
//     三种形态能一次匹配。写成 (?:\\/)+ 是「两个反斜杠」，会漏掉全部转义写法。
//  2. 两侧引号必须显式捕获（组 1 与组 3）。若像 "(\w+)"(\s*:\s*")…(") 那样
//     只捕获键名与冒号，拼接回去时会把开引号丢掉，JSON 直接损坏。
var jsonURLValueRe = regexp.MustCompile(`(?i)(")([a-z0-9_]{1,24})(")(\s*:\s*")((?:https?:)?(?:\\?/+)*[a-z0-9][a-z0-9.-]*\.(?:quark\.cn|alicdn\.com)[^"]*)(")`)

// localizeHTML 用于 text/html 响应：先把「纯展示用」的 JSON 字段原样保护起来，
// 整体改写其余内容，最后再把保护住的字段放回。
//
// 顺序很关键：必须先保护再改写。反过来（先整体改写、再按键甄别）会失效——
// 等 rewriteJSONData 运行时，title 里的地址早已变成 /__proxy/… 路径，
// 正则再也匹配不到它，也就没机会把它还原回去。
func localizeHTML(b []byte) []byte {
	// 1) 摘出所有「键不在白名单」里的地址，替换成占位符。
	//    捕获组：1=开引号 2=键名 3=闭引号 4=冒号+开引号 5=地址 6=闭引号
	var kept []string
	protected := jsonURLValueRe.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := jsonURLValueRe.FindSubmatch(m)
		if sub == nil {
			return m
		}
		if fetchableKeys[strings.ToLower(string(sub[2]))] {
			return m // 会被请求的地址，留在原地等整体改写
		}
		kept = append(kept, string(sub[5]))
		return []byte(string(sub[1]) + string(sub[2]) + string(sub[3]) + string(sub[4]) +
			jsonURLPlaceholder + strconv.Itoa(len(kept)-1) + "\x00" + string(sub[6]))
	})

	// 2) 整体改写（含 HTML 属性、JS 字符串、CSS url()）
	protected = localizeBytes(protected)

	// 3) 放回被保护的原文
	for i, orig := range kept {
		protected = bytes.ReplaceAll(protected,
			[]byte(jsonURLPlaceholder+strconv.Itoa(i)+"\x00"), []byte(orig))
	}
	return protected
}

// jsonURLPlaceholder 是展示用地址在改写期间的占位前缀。
// \x00 不可能出现在合法 URL 里，拼上序号即可精确还原被摘走的原文。
const jsonURLPlaceholder = "\x00jsonurl-"

// localizeExceptScanHosts 执行真正的地址改写。扫码落地页地址先摘出来换成占位符，
// 改写完成后再按原文放回——RE2 没有负向前瞻，这是等价且更直白的做法。
func localizeExceptScanHosts(s string) string {
	if !strings.Contains(strings.ToLower(s), "quark.cn") && !strings.Contains(strings.ToLower(s), "alicdn.com") {
		return s
	}
	var kept []string
	keep := func(m string) string {
		kept = append(kept, m)
		return scanPlaceholder + strconv.Itoa(len(kept)-1) + "\x00"
	}
	s = scanHostRe.ReplaceAllStringFunc(s, keep)
	s = localizeEscapedHostRe.ReplaceAllString(s, prefix+"/$1")
	s = localizeHostRe.ReplaceAllString(s, prefix+"/$1")
	for i, orig := range kept {
		s = strings.ReplaceAll(s, scanPlaceholder+strconv.Itoa(i)+"\x00", orig)
	}
	return s
}

// injectBanner 在 HTML 的 <body> 后插入提示条。
// 找不到 <body> 时直接放弃注入：提示条只是体验增强，不值得为此改坏页面结构。
// 已注入过则跳过，见 bannerMarker。
func injectBanner(b []byte) []byte {
	if bytes.Contains(b, []byte(bannerMarker)) {
		return b
	}
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

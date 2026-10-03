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
)

// TestRedirectWithoutLocationGetsFallback 回归：实测 pan.quark.cn/list 未登录时
// 返回「302 且 Location 为空」。这种响应原样透传会让浏览器无从跟随，
// 地址栏停在 /__proxy/pan.quark.cn/list 并白屏。代理必须补一个 Location。
func TestRedirectWithoutLocationGetsFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 刻意不写 Location，模拟真实上游行为
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 浏览器不会自动跟随，用 CheckRedirect 拦下来看 Location
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(base + prefix + "/pan.quark.cn/list")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	loc := res.Header.Get("Location")
	if loc == "" {
		t.Fatal("缺 Location 的重定向必须补上兜底地址，否则浏览器白屏")
	}
	if loc != prefix+"/pan.quark.cn/" {
		t.Errorf("兜底应指向同主机根路径，实际 %q", loc)
	}
}

// TestRedirectWithLocationStillRewritten 确认有 Location 时改写逻辑没被破坏。
func TestRedirectWithLocationStillRewritten(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://uop.quark.cn/cas/custom/login", http.StatusFound)
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(base + prefix + "/pan.quark.cn/login")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()

	if loc := res.Header.Get("Location"); loc != prefix+"/uop.quark.cn/cas/custom/login" {
		t.Errorf("跨主机重定向应被本地化，实际 %q", loc)
	}
}

// TestIsRedirectWithoutLocation 纯函数：只有 3xx 家族才算「缺 Location 的重定向」。
func TestIsRedirectWithoutLocation(t *testing.T) {
	yes := []int{301, 302, 303, 307, 308}
	for _, c := range yes {
		if !isRedirectWithoutLocation(c) {
			t.Errorf("%d 应判定为重定向", c)
		}
	}
	no := []int{200, 201, 204, 400, 401, 403, 404, 500}
	for _, c := range no {
		if isRedirectWithoutLocation(c) {
			t.Errorf("%d 不应判定为重定向", c)
		}
	}
}

// TestCapturedReturnsDonePageForNavigation 凭证捕获后，文档导航应返回收尾页（子资源必须继续转发，见 captured_test.go）：
// 登录成功后页面会自行跳转并继续加载 JS/CSS，若子资源拿到上游内容会显得
// 页面还在正常工作，用户就不会知道可以关闭窗口了。
func TestCapturedReturnsDonePageForSubResources(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") {
			_, _ = io.WriteString(w, "console.log('upstream')")
			return
		}
		_, _ = io.WriteString(w, "<html><body>upstream</body></html>")
	}))
	defer upstream.Close()

	s := newTestSession(t, upstream)
	base := "http://" + s.ln.Addr().String()

	// 走真实路径：带凭证的请求触发 absorb → scheduleFinish → ready
	req, _ := http.NewRequest(http.MethodGet, base+prefix+"/pan.quark.cn/", nil)
	req.Header.Set("Cookie", "__pus=PUS-FOR-DONEPAGE")
	res0, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res0.Body)
	res0.Body.Close()
	if !s.captured() {
		t.Fatal("凭证已下发，应进入已捕获状态")
	}

	for _, p := range []string{"/__proxy/pan.quark.cn/", "/__proxy/pan.quark.cn/list"} {
		req, _ := http.NewRequest(http.MethodGet, base+p, nil)
		req.Header.Set("Referer", base+prefix+"/pan.quark.cn/login")
		req.Header.Set("Sec-Fetch-Dest", "document")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s 请求失败：%v", p, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !strings.Contains(string(body), "登录成功") {
			t.Errorf("%s 在已捕获状态下应返回收尾页，实际返回了上游内容", p)
		}
	}
}

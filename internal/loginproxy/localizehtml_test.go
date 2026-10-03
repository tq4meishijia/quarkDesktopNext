// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package loginproxy

import (
	"strings"
	"testing"
)

// TestLocalizeHTMLSkipsDisplayText 回归：SSR 把图片地址当 title 文案用，
// 无差别改写会让页面中部渲染出一行 /__proxy/image.quark.cn/...(png) 文字。
// 展示用字段必须保持原样，而真正会被请求的字段仍要改写。
func TestLocalizeHTMLSkipsDisplayText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "title 字段是展示文案，不改写",
			in:   `{"title":"https://image.quark.cn/s/x/202608/abc.png","description":"配文"}`,
			want: `{"title":"https://image.quark.cn/s/x/202608/abc.png","description":"配文"}`,
		},
		{
			name: "imageUrl 会被请求，要改写",
			in:   `{"imageUrl":"https://image.quark.cn/s/x/202608/def.png"}`,
			want: `{"imageUrl":"/__proxy/image.quark.cn/s/x/202608/def.png"}`,
		},
		{
			name: "actionLink 会被请求，要改写",
			in:   `{"actionLink":"https://b.quark.cn/apps/xxx"}`,
			want: `{"actionLink":"/__proxy/b.quark.cn/apps/xxx"}`,
		},
		{
			name: "backgroundImage 会被请求，要改写",
			in:   `{"backgroundImage":"https://image.quark.cn/s/y.png"}`,
			want: `{"backgroundImage":"/__proxy/image.quark.cn/s/y.png"}`,
		},
		{
			name: "多个字段混排时各自按规则处理",
			in:   `{"title":"https://image.quark.cn/a.png","imageUrl":"https://image.quark.cn/b.png","actionLink":"https://b.quark.cn/c"}`,
			want: `{"title":"https://image.quark.cn/a.png","imageUrl":"/__proxy/image.quark.cn/b.png","actionLink":"/__proxy/b.quark.cn/c"}`,
		},
	}
	for _, c := range cases {
		if got := string(localizeHTML([]byte(c.in))); got != c.want {
			t.Errorf("%s\n in = %s\nout = %s\nwant = %s", c.name, c.in, got, c.want)
		}
	}
}

// TestLocalizeHTMLStillRewritesHTMLAttrs HTML 属性不在 JSON 语法内，
// 必须仍被改写，否则图片/脚本都取不到。
func TestLocalizeHTMLStillRewritesHTMLAttrs(t *testing.T) {
	in := `<img src="https://image.quark.cn/s/a.png"><a href="https://b.quark.cn/x">x</a>`
	got := string(localizeHTML([]byte(in)))
	if !strings.Contains(got, `src="/__proxy/image.quark.cn/s/a.png"`) {
		t.Errorf("img src 未被改写：%s", got)
	}
	if !strings.Contains(got, `href="/__proxy/b.quark.cn/x"`) {
		t.Errorf("a href 未被改写：%s", got)
	}
}

// TestLocalizeHTMLKeepsQRCodeLandingURL 扫码落地页豁免必须在新的 HTML 路径上继续生效。
func TestLocalizeHTMLKeepsQRCodeLandingURL(t *testing.T) {
	in := `{"actionLink":"https://su.quark.cn/4_eMHBJ?token=abc"}`
	if got := string(localizeHTML([]byte(in))); got != in {
		t.Errorf("扫码落地页不应被改写：%s", got)
	}
}

// TestLocalizeBytesUnchangedForNonHTML 非 HTML 响应（独立 JS / JSON）仍整体改写，
// 因为那里的地址几乎都会被 fetch/XHR 用到。
func TestLocalizeBytesUnchangedForNonHTML(t *testing.T) {
	in := `{"anything":"https://uop.quark.cn/cas/ajax/x"}`
	if got := string(localizeBytes([]byte(in))); got != `{"anything":"/__proxy/uop.quark.cn/cas/ajax/x"}` {
		t.Errorf("非 HTML 响应应整体改写，实际 %s", got)
	}
}

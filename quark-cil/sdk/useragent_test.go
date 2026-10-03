// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: AGPL-3.0
//
// 本文件属于 quark-cil（上游 kuake_cli，AGPL-3.0）。
// 注意：本工程在编译期链接本模块，整体分发产物须遵循 AGPL-3.0。

package sdk

import (
	"net/http"
	"strings"
	"testing"
)

// TestSetDefaultAPIHeadersUsesClientUA 验证请求头使用夸克客户端 UA。
//
// 回归测试：原实现用浏览器 UA（Chrome/142），导致服务端对大文件返回
// 错误码 23018（"超过文件下载大小限制"）。用户下载几百 MB 的视频时
// 必然失败，且提示"请使用客户端下载"——而本工程本身就是客户端。
// 该问题在多个开源实现中的一致解法是改用官方客户端 UA。
func TestSetDefaultAPIHeadersUsesClientUA(t *testing.T) {
	qc := &QuarkClient{cookies: map[string]string{"__pus": "P"}}
	req, err := http.NewRequest("POST", "https://drive-pc.quark.cn/1/clouddrive/file/download", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	qc.setDefaultAPIHeaders(req)

	ua := req.Header.Get("User-Agent")
	if ua != ClientUserAgent {
		t.Errorf("UA 不等于 ClientUserAgent：\n  got  %q\n  want %q", ua, ClientUserAgent)
	}
	// 关键特征：缺了就等于退回浏览器 UA，限制会回来。
	if !strings.Contains(ua, "quark-cloud-drive/") {
		t.Error("UA 缺少 quark-cloud-drive 标识，服务端会按浏览器策略限制大小")
	}
	if !strings.Contains(ua, "Channel/pckk_other_ch") {
		t.Error("UA 缺少 Channel/pckk_other_ch 标识，这是客户端渠道的关键标记")
	}
	// 反向断言：不能是浏览器 UA。
	if strings.Contains(ua, "Chrome/") && !strings.Contains(ua, "Electron/") {
		t.Error("UA 看起来是浏览器形态（只有 Chrome 没有 Electron）")
	}
}

// TestSetDefaultAPIHeadersNoClientHints 验证不发送浏览器 Client Hints 头。
//
// Sec-Ch-Ua-* 是浏览器特有头。UA 声称自己是 Electron 100 却发着
// Chrome 142 的 Client Hints，自相矛盾，容易被判定为伪造 UA 而收紧策略。
func TestSetDefaultAPIHeadersNoClientHints(t *testing.T) {
	qc := &QuarkClient{cookies: map[string]string{"__pus": "P"}}
	req, _ := http.NewRequest("POST", "https://drive-pc.quark.cn/1/clouddrive/file/download", nil)
	qc.setDefaultAPIHeaders(req)

	for _, h := range []string{
		"Sec-Ch-Ua", "Sec-Ch-Ua-Arch", "Sec-Ch-Ua-Bitness", "Sec-Ch-Ua-Full-Version",
		"Sec-Ch-Ua-Full-Version-List", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Model",
		"Sec-Ch-Ua-Platform", "Sec-Ch-Ua-Platform-Version", "Sec-Ch-Ua-Wow64",
	} {
		if v := req.Header.Get(h); v != "" {
			t.Errorf("不应发送 %s（浏览器 Client Hints，与客户端 UA 矛盾）：%q", h, v)
		}
	}
	// 基本头仍应存在。
	if req.Header.Get("Cookie") == "" {
		t.Error("Cookie 头丢失")
	}
	if req.Header.Get("Referer") == "" {
		t.Error("Referer 头丢失")
	}
}

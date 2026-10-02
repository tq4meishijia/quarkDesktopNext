// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package loginproxy

import (
	"sort"
	"strings"
)

// credentialKeys 是判定「已经登录」的依据：出现任意一个即认为拿到了有效凭证。
var credentialKeys = []string{"__pus", "__puus"}

// parseCookieHeader 把请求头的 "a=1; b=2" 解析成 map，同名以最后一次为准。
// 只关心 name=value 结构，忽略 $Version、$Path 之类的附加属性。
func parseCookieHeader(raw string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "$") {
			continue
		}
		out[name] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return out
}

// joinCookies 按名字排序拼成 "a=1; b=2"，输出稳定便于测试与比对。
func joinCookies(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	names := make([]string, 0, len(m))
	for name, value := range m {
		if value == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(m[name])
	}
	b.WriteString(";")
	return b.String()
}

// hasCredential 判断是否已经出现登录所需的凭证字段。
func hasCredential(m map[string]string) bool {
	for name := range m {
		if isCredential(name) {
			return true
		}
	}
	return false
}

// isCredential 判断单个 Cookie 名是否属于登录凭证。
func isCredential(name string) bool {
	for _, key := range credentialKeys {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// absorb 合并一段请求头里的 Cookie，返回是否首次探测到登录凭证。
func (s *Session) absorb(raw string) bool {
	got := parseCookieHeader(raw)
	if len(got) == 0 {
		return false
	}
	s.mu.Lock()
	for k, v := range got {
		s.jar[k] = v
	}
	already := s.ready
	s.mu.Unlock()
	return !already && hasCredential(got)
}

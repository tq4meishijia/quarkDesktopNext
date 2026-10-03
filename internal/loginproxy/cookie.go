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

// absorb 合并一段请求头里的 Cookie，返回是否首次探测到「本次登录」的凭证。
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
	fresh := s.detectFreshLocked(got)
	s.mu.Unlock()
	return !already && fresh
}

// detectFreshLocked 在持锁状态下判断刚收到的一批 Cookie 是否代表「一次新的登录」。
//
// 强制重新登录模式（requireNew）下，浏览器里原有的 __pus/__puus 会在代理启动后
// 的第一个请求里就出现，与用户是否真的登录无关。非强制模式保持原语义：见到凭证
// 即视为登录成功。
//
// 强制模式下的判定规则：
//   - 首次见到某个凭证名：只记基线，不算登录完成。浏览器在用户登录之前不会有凭证，
//     所以第一次看到的东西恰恰可能是「旧的」。
//     例外：如果这批 Cookie 是随 Set-Cookie 首次下发（浏览器原本没有），
//     那就是本次登录产生的，由 absorbOne 用 Set-Cookie 语义单独判定。
//   - 同一凭证名但值变了：说明登录态被刷新，即刚刚发生了新登录，算成功。
//   - 与基线完全一致：只是浏览器里早就有的旧凭证，不算。
func (s *Session) detectFreshLocked(got map[string]string) bool {
	if !s.requireNew {
		return hasCredential(got)
	}
	fresh := false
	hasCred := false
	for name, value := range got {
		if !isCredential(name) {
			continue
		}
		hasCred = true
		old, seen := s.baseline[name]
		switch {
		case !seen:
			// 第一次见到这个名字，先记基线。
			s.baseline[name] = value
		case old != value:
			// 值变了：登录态被刷新，确实刚发生过新登录。
			s.baseline[name] = value
			fresh = true
		}
	}
	if hasCred && !fresh {
		// 只有旧凭证、没有任何变化：记下来供前端提示，避免用户干等。
		s.staleExisting = true
	}
	return fresh
}

// detectFreshFromSetCookieLocked 判定上游下发的这个凭证是否代表一次新登录。
//
// 与请求头路径的区别：Set-Cookie 是上游「刚刚」写下的，若浏览器此前并无该凭证
// （基线里没有这个名字），那它就是本次登录动作产生的，直接算成功。
// 值与基线完全相同的重复下发（服务端刷新会话时常见）则不算。
func (s *Session) detectFreshFromSetCookieLocked(name, value string) bool {
	if !s.requireNew {
		return isCredential(name)
	}
	old, seen := s.baseline[name]
	s.baseline[name] = value
	if !seen {
		s.staleExisting = false
		return isCredential(name)
	}
	return old != value && isCredential(name)
}

// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// dnsRetryDelay 是 DNS 解析失败后的重试间隔。
// 取值偏短：解析只需一次往返，200ms 足以覆盖瞬时抖动而不至于让用户明显等待。
const dnsRetryDelay = 200 * time.Millisecond

// 下载直链的 SSRF 防护。
//
// 为什么不直接复用 quark-cil/sdk 的 isSSRFProtectedURL：
//  1. 它是包内私有函数，未导出，桌面端无法调用；
//  2. 它只对 host 做字面 IP 判定（net.ParseIP(host) != nil 才查私有段），
//     域名主机（evil.com 解析到 10.0.0.1）完全绕过；
//  3. 它不含链路本地地址（169.254.0.0/16）与 CGNAT（100.64.0.0/10）判定。
//
// 这里在桌面端自有代码里实现更严格的版本，不修改 AGPL 上游：
//   - 字面 IP：直接判定私有段；
//   - 域名：解析后逐个检查 A/AAAA 记录，防止 DNS 指向内网；
//   - 解析失败：拒绝（fail-closed），不因为解析不了就放行内网直连。
//
// 判定发生在「拿到直链之后、交给任何下载器之前」，因此内建与外部两条路径都受保护。

// metadataHosts 是云厂商元数据服务地址，命中即拒绝。
// 精确匹配字面量不足以覆盖实际用法（如 metadata.google.internal），
// 故另有 metadataSuffixes 做后缀匹配。
var metadataHosts = []string{
	"169.254.169.254", // AWS / 阿里云 / GCP 通用元数据
	"100.100.100.200", // 阿里云 ECS 元数据
}

// metadataSuffixes 是元数据服务主机名的后缀特征。
var metadataSuffixes = []string{
	"metadata.google",
	"metadata.goog",
	"metadata.azure.com",
	"instance-data", // EC2 风格
}

// guardDownloadURL 校验下载直链是否可安全请求。
func guardDownloadURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("下载直链为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("下载直链无法解析: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("下载直链协议不受支持: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("下载直链缺少主机名")
	}
	return checkHost(host)
}

// checkHost 对主机做 SSRF 判定。
func checkHost(host string) error {
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "" {
		return errors.New("下载直链缺少主机名")
	}
	for _, mh := range metadataHosts {
		if lower == mh {
			return fmt.Errorf("已阻止访问云元数据服务: %s", host)
		}
	}
	for _, ms := range metadataSuffixes {
		if strings.Contains(lower, ms) {
			return fmt.Errorf("已阻止访问云元数据服务: %s", host)
		}
	}

	// 字面 IP：直接判私有段。
	if ip := net.ParseIP(lower); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("已阻止访问内网地址: %s", host)
		}
		return nil
	}

	// localhost 及其子域。
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("已阻止访问本机地址: %s", host)
	}

	// 域名：解析后逐一检查，防止 DNS 指向内网（rebinding / 内网域名）。
	//
	// 策略：fail-closed —— 解析失败即拒绝，理由是「无法确认安全」与「确认安全」
	// 不等价，放行等于把判定交给攻击者控制的 DNS。
	// 为避免临时 DNS 抖动误伤正常下载，失败后重试一次再下结论。
	ips, err := resolveHostWithRetry(lower)
	if err != nil {
		return fmt.Errorf("已阻止访问无法确认安全的目标（%s 解析失败）: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("已阻止访问内网地址: %s", host)
		}
	}
	return nil
}

// resolveHostWithRetry 解析主机名，失败时重试一次以容忍瞬时 DNS 故障。
func resolveHostWithRetry(host string) ([]net.IP, error) {
	ips, err := resolveHost(host)
	if err == nil {
		return ips, nil
	}
	// 短暂等待后重试一次；仍失败则交由调用方 fail-closed 处理。
	time.Sleep(dnsRetryDelay)
	return resolveHost(host)
}

// resolveHost 解析主机名，返回全部 A/AAAA 记录。
func resolveHost(host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errors.New("无解析结果")
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// isPrivateIP 判断 IP 是否属于不应由直链访问的网段。
// 比 SDK 版本多覆盖链路本地（169.254.0.0/16）与 CGNAT（100.64.0.0/10），
// 并补充 IPv6 的唯一本地地址 fc00::/7。
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10: // 10.0.0.0/8
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31: // 172.16.0.0/12
			return true
		case v4[0] == 192 && v4[1] == 168: // 192.168.0.0/16
			return true
		case v4[0] == 127: // 127.0.0.0/8
			return true
		case v4[0] == 169 && v4[1] == 254: // 169.254.0.0/16 链路本地
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10 CGNAT
			return true
		case v4[0] == 0: // 0.0.0.0/8 当前主机
			return true
		}
		return false
	}
	// IPv6 唯一本地地址 fc00::/7。
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return true
	}
	// IPv4 映射的 IPv6（如 ::ffff:127.0.0.1）。
	if v4 := ip.To4(); v4 != nil {
		return isPrivateIP(v4)
	}
	return false
}

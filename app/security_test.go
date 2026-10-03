// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// S-1：下载直链的 SSRF 防护
// ---------------------------------------------------------------------------

// TestGuardDownloadURLBlocksInternal 验证直链指向内网/本机时被拦截。
//
// 修复前：app/transfer.go 把 GetDownloadURL 的返回值直接交给 engine，
// 没有任何校验，SDK 的 isSSRFProtectedURL 只覆盖 OSS 上传路径。
func TestGuardDownloadURLBlocksInternal(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"本机回环", "http://127.0.0.1:8080/x"},
		{"localhost", "http://localhost/x"},
		{"子域localhost", "http://api.localhost/x"},
		{"私有A段", "http://10.0.0.1/x"},
		{"私有B段", "http://172.16.0.1/x"},
		{"私有C段", "http://192.168.1.1/x"},
		{"链路本地", "http://169.254.1.1/x"},
		{"CGNAT", "http://100.64.0.1/x"},
		{"零地址", "http://0.0.0.0/x"},
		{"未指定地址", "http://0.0.0.0:8080/x"},
		{"AWS元数据", "http://169.254.169.254/latest/meta-data/"},
		{"阿里云元数据", "http://100.100.100.200/latest/"},
		{"GCP元数据", "http://metadata.google.internal/x"},
		{"IPv6回环", "http://[::1]/x"},
		{"IPv6唯一本地", "http://[fc00::1]/x"},
		{"IPv4映射回环", "http://[::ffff:127.0.0.1]/x"},
		{"非http协议", "file:///etc/passwd"},
		{"空直链", ""},
		{"仅空白", "   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := guardDownloadURL(c.url); err == nil {
				t.Fatalf("直链 %q 应被拦截，但通过了", c.url)
			}
		})
	}
}

// TestGuardDownloadURLAllowsPublic 验证正常公网直链不被误伤（不降低可用性）。
//
// 注意：fail-closed 策略下，未知域名会因 DNS 解析失败而被拒，
// 故这里只用「必然可解析」的公共 DNS 名（Google DNS 是 FQDN，本身无需解析），
// 以及字面公网 IP。真实 CDN 域名在有网络的环境下才可解析。
func TestGuardDownloadURLAllowsPublic(t *testing.T) {
	allow := []string{
		"http://8.8.8.8/x",           // 字面公网 IPv4
		"http://[2606:4700::1111]/",  // 字面公网 IPv6
		"https://dns.google/resolve", // 公网 FQDN
	}
	for _, u := range allow {
		if err := guardDownloadURL(u); err != nil {
			t.Fatalf("公网直链 %q 不应被拦截：%v", u, err)
		}
	}
}

// TestGuardDownloadURLFailsClosedOnDNSError 验证 DNS 解析失败时拒绝（fail-closed）。
//
// 这是本轮的安全策略变更：旧实现解析失败即放行，等于把「是否安全」的
// 判定交给攻击者控制的 DNS。字面 IP 与本机名仍不受 DNS 影响。
func TestGuardDownloadURLFailsClosedOnDNSError(t *testing.T) {
	// .invalid 是 RFC 2606 保留 TLD，永久解析失败。
	err := guardDownloadURL("http://definitely-not-a-real-host.invalid/x")
	if err == nil {
		t.Fatal("DNS 解析失败时应拒绝（fail-closed）")
	}
	if !strings.Contains(err.Error(), "无法确认安全") {
		t.Fatalf("错误信息应说明无法确认安全，得到 %q", err.Error())
	}
	// 字面 IP 不依赖 DNS，仍应正常判定。
	if err := guardDownloadURL("http://8.8.8.8/x"); err != nil {
		t.Fatalf("字面公网 IP 不应被 DNS 策略影响：%v", err)
	}
}

// TestIsPrivateIP 覆盖各类私网/保留网段判定。
func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.0",
		"::1", "fc00::1", "::ffff:127.0.0.1", "fe80::1",
	}
	for _, s := range private {
		ip := parseIPOrFail(t, s)
		if !isPrivateIP(ip) {
			t.Errorf("%s 应判为私有地址", s)
		}
	}
	public := []string{"1.1.1.1", "8.8.8.8", "172.32.0.1", "100.63.255.255", "2606:4700::1111"}
	for _, s := range public {
		ip := parseIPOrFail(t, s)
		if isPrivateIP(ip) {
			t.Errorf("%s 不应判为私有地址", s)
		}
	}
	// nil 不得 panic。
	if isPrivateIP(nil) {
		t.Error("nil IP 应判为非私有")
	}
}

// ---------------------------------------------------------------------------
// S-2：打开目录的路径白名单
// ---------------------------------------------------------------------------

// TestRevealLocalDirRejectsOutsidePaths 验证不得打开白名单之外的目录。
//
// 修复前：RevealLocalDir 只做 os.Stat 判存在，随后直接 exec 系统命令，
// 前端传入任意路径都能被打开。
func TestRevealLocalDirRejectsOutsidePaths(t *testing.T) {
	a := newTestApp(t)
	a.settings.DownloadDir = t.TempDir()

	// 造一个真实存在、但既不在下载目录也不在配置目录下的目录。
	// 用 os.MkdirTemp 而非 t.TempDir()：后者与 newTestApp 的 home 同根，
	// 会让「配置目录」这条白名单规则把它覆盖掉。
	parent := os.TempDir()
	outside, err := os.MkdirTemp(parent, "reveal-outside-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	// 解析软链接（macOS 的 /var → /private/var 会让路径形态变化）。
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}

	// 前置条件：确认它确实不在白名单内，否则本用例失去意义。
	if a.pathRevealAllowed(outside) {
		t.Skipf("测试前提不成立：%s 落在白名单内（下载目录=%s 配置目录=%s）",
			outside, a.settings.DownloadDir, a.store.Dir())
	}

	err = a.RevealLocalDir(outside)
	if err == nil {
		t.Fatal("白名单外的目录不应被允许打开")
	}
	// 必须是「白名单拒绝」而不是「目录不存在」——
	// 后者说明判定顺序有问题（会先探测任意路径的存在性）。
	if !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("应在白名单校验处拒绝（而非报不存在），得到 %q", err.Error())
	}
}

// TestRevealLocalDirAcceptsDownloadDir 验证下载目录本身仍可打开（不破坏正常流程）。
func TestRevealLocalDirAcceptsDownloadDir(t *testing.T) {
	a := newTestApp(t)
	dl := t.TempDir()
	a.settings.DownloadDir = dl
	// 目标是真实存在的目录，但为避免真的启动资源管理器，
	// 这里只校验白名单判定逻辑，不执行最终 exec。
	if !a.pathRevealAllowed(dl) {
		t.Fatal("下载目录应被允许打开")
	}
	// 子目录也应允许。
	sub := filepath.Join(dl, "sub", "deep")
	if !a.pathRevealAllowed(sub) {
		t.Fatal("下载目录的子目录应被允许")
	}
}

// TestPathWithinBoundary 验证前缀判定以路径边界为界。
func TestPathWithinBoundary(t *testing.T) {
	root := filepath.FromSlash("/tmp/dl")
	if !pathWithin(root, root) {
		t.Error("根目录自身应命中")
	}
	if !pathWithin(root, filepath.Join(root, "a", "b")) {
		t.Error("子目录应命中")
	}
	// 经典前缀误匹配：/tmp/dl2 不应被 /tmp/dl 命中。
	if pathWithin(root, filepath.FromSlash("/tmp/dl2")) {
		t.Error("/tmp/dl2 不应被 /tmp/dl 包含（前缀误匹配）")
	}
	if pathWithin(root, filepath.FromSlash("/tmp/other")) {
		t.Error("无关目录不应命中")
	}
	// 父级穿越必须拒绝。
	if pathWithin(root, filepath.FromSlash("/tmp/dl/../etc")) {
		t.Error("路径穿越应被拒绝")
	}
}

// TestRevealLocalDirEmptyPath 验证空路径被拒绝。
func TestRevealLocalDirEmptyPath(t *testing.T) {
	a := newTestApp(t)
	if err := a.RevealLocalDir("   "); err == nil {
		t.Fatal("空路径应被拒绝")
	}
}

// ---------------------------------------------------------------------------
// S-3：外部下载器的 Cookie 最小化
// ---------------------------------------------------------------------------

// TestDownloadCookieHeaderDropsAccountCredentials 验证账号级凭证不会离开进程。
//
// 修复前：cookieHeader 把整张 cookie map 交给外部下载器，
// 其中 __pus 是账号登录态凭证。
func TestDownloadCookieHeaderDropsAccountCredentials(t *testing.T) {
	cookies := map[string]string{
		"__pus":     "SECRET-ACCOUNT-TOKEN",
		"__puus":    "SECRET-PUUS",
		"tfstk":     "tfstk-value",
		"b-user-id": "user-123",
		"_UP_A":     "up-a",
		"_UP_B":     "up-b",
		"__other":   "other-secret",
		"sessionid": "sid",
		"access_ck": "ak",
	}
	got := downloadCookieHeader(cookies)

	for _, forbidden := range []string{"__pus=", "SECRET-ACCOUNT-TOKEN", "__other", "sessionid", "access_ck"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("外部下载器 Cookie 不应包含 %q，实际=%q", forbidden, got)
		}
	}
	// 下载直链真正需要的键必须保留，否则功能不可用。
	for _, required := range []string{"__puus=", "tfstk=", "b-user-id=", "_UP_A=", "_UP_B="} {
		if !strings.Contains(got, required) {
			t.Fatalf("外部下载器 Cookie 应保留 %q，实际=%q", required, got)
		}
	}
}

// TestDownloadCookieHeaderEmptyCases 验证空输入与全过滤场景返回空串。
func TestDownloadCookieHeaderEmptyCases(t *testing.T) {
	if got := downloadCookieHeader(nil); got != "" {
		t.Errorf("nil 应返回空串，得到 %q", got)
	}
	if got := downloadCookieHeader(map[string]string{}); got != "" {
		t.Errorf("空 map 应返回空串，得到 %q", got)
	}
	// 只有账号凭证时应被过滤为空。
	got := downloadCookieHeader(map[string]string{"__pus": "x", "sessionid": "y"})
	if got != "" {
		t.Errorf("无可保留键时应返回空串，得到 %q", got)
	}
}

// TestCookieHeaderKeepsAllForBuiltin 验证内建下载器仍拿完整凭证（不降低可用性）。
func TestCookieHeaderKeepsAllForBuiltin(t *testing.T) {
	cookies := map[string]string{"__pus": "P", "__puus": "PP", "tfstk": "T"}
	got := cookieHeader(cookies)
	for _, k := range []string{"__pus=P", "__puus=PP", "tfstk=T"} {
		if !strings.Contains(got, k) {
			t.Fatalf("内建下载器 Cookie 应包含 %q，实际=%q", k, got)
		}
	}
}

// ---------------------------------------------------------------------------
// S-5：危险操作的前置校验
// ---------------------------------------------------------------------------

// TestCheckSensitiveLocalPathBlocks 验证系统/凭证路径被拦截。
func TestCheckSensitiveLocalPathBlocks(t *testing.T) {
	blocked := []string{
		"/etc/passwd",
		"/etc/shadow",
		"/root/.bashrc",
		"/proc/self/environ",
		"/sys/kernel/config",
		"/var/log/syslog",
		"/dev/sda",
		"/boot/vmlinuz",
	}
	for _, p := range blocked {
		if err := checkSensitiveLocalPath(p); err == nil {
			t.Errorf("路径 %q 应被拦截", p)
		}
	}
	// 凭证类文件（按 basename 判定，放在任意目录都应拦截）。
	for _, base := range []string{"id_rsa", "id_ed25519", ".netrc", ".pgpass", ".my.cnf", ".env", "master.key"} {
		p := filepath.Join("/tmp/somewhere", base)
		if err := checkSensitiveLocalPath(p); err == nil {
			t.Errorf("凭证文件 %q 应被拦截", base)
		}
	}
	// 凭证目录。
	for _, dir := range []string{"/home/u/.ssh/id_x", "/home/u/.aws/credentials", "/home/u/.kube/config"} {
		if err := checkSensitiveLocalPath(dir); err == nil {
			t.Errorf("凭证目录 %q 应被拦截", dir)
		}
	}
	// Windows 系统路径。
	for _, p := range []string{`c:/windows/system32/config/sam`, `c:/pagefile.sys`, `c:/bootmgr`} {
		if err := checkSensitiveLocalPath(p); err == nil {
			t.Errorf("Windows 系统路径 %q 应被拦截", p)
		}
	}
	// 空路径。
	if err := checkSensitiveLocalPath("  "); err == nil {
		t.Error("空路径应被拒绝")
	}
}

// TestCheckSensitiveLocalPathAllowsNormal 验证普通用户文件不被误伤。
func TestCheckSensitiveLocalPathAllowsNormal(t *testing.T) {
	allowed := []string{
		"/home/user/Documents/报告.pdf",
		"/home/user/Videos/movie.mp4",
		"/tmp/download/photo.jpg",
		"/home/user/project/src/main.go",
		"/home/user/Downloads/QuarkDrive/data.bin",
	}
	for _, p := range allowed {
		if err := checkSensitiveLocalPath(p); err != nil {
			t.Errorf("普通文件 %q 不应被拦截：%v", p, err)
		}
	}
}

// TestCheckRemoteFileNameBlocksTraversal 验证远端文件名的路径穿越被拒绝。
func TestCheckRemoteFileNameBlocksTraversal(t *testing.T) {
	bad := []string{
		"", "   ", "..", ".",
		"../../.ssh/authorized_keys", "..\\windows\\system32\\cmd.exe",
		`..\..\windows\system32\cmd.exe`,
		"a/b", `a\b`,
		"nul\x00.txt",
	}
	for _, n := range bad {
		if err := checkRemoteFileName(n); err == nil {
			t.Errorf("远端文件名 %q 应被拒绝", n)
		}
	}
	for _, ok := range []string{"a.txt", "报告.pdf", "movie.mp4", "..hidden", "file..name.txt"} {
		if err := checkRemoteFileName(ok); err != nil {
			t.Errorf("正常文件名 %q 不应被拒绝：%v", ok, err)
		}
	}
}

// TestGuardConfigFromEnv 验证环境变量驱动的黑名单生效。
func TestGuardConfigFromEnv(t *testing.T) {
	t.Setenv("KUAKE_DENY_OPS", "delete:move")
	t.Setenv("KUAKE_DENY_PATHS", "/backup:/secret")
	t.Setenv("KUAKE_DENY_EXTS", "pem:key")
	t.Setenv("KUAKE_MAX_UPLOAD_MB", "1")
	g := loadGuardConfig()

	if err := g.checkOp("delete"); err == nil {
		t.Error("delete 应被禁用")
	}
	if err := g.checkOp("move"); err == nil {
		t.Error("move 应被禁用")
	}
	if err := g.checkOp("copy"); err != nil {
		t.Errorf("copy 不应被禁用：%v", err)
	}
	if err := g.checkRemotePath("/backup/2024/x"); err == nil {
		t.Error("/backup 前缀应被拦截")
	}
	// 前缀边界：/backup2 不应被 /backup 误伤。
	if err := g.checkRemotePath("/backup2/x"); err != nil {
		t.Errorf("/backup2 不应被 /backup 拦截：%v", err)
	}
	if err := g.checkUploadFile("/tmp/x.pem", "x.pem", 1); err == nil {
		t.Error(".pem 应被拦截")
	}
	if err := g.checkUploadFile("/tmp/big.bin", "big.bin", 5*1024*1024); err == nil {
		t.Error("超过 1MB 上限应被拦截")
	}
	if err := g.checkUploadFile("/tmp/small.bin", "small.bin", 1024); err != nil {
		t.Errorf("未超限应放行：%v", err)
	}
}

// TestGuardConfigDefaultIsPermissive 验证默认配置不干扰普通用户。
func TestGuardConfigDefaultIsPermissive(t *testing.T) {
	t.Setenv("KUAKE_DENY_OPS", "")
	t.Setenv("KUAKE_DENY_PATHS", "")
	t.Setenv("KUAKE_DENY_EXTS", "")
	t.Setenv("KUAKE_MAX_UPLOAD_MB", "")
	g := loadGuardConfig()
	if err := g.checkOp("delete"); err != nil {
		t.Errorf("默认不应禁用任何操作：%v", err)
	}
	if err := g.checkRemotePath("/any/path"); err != nil {
		t.Errorf("默认不应禁用任何路径：%v", err)
	}
	if err := g.checkUploadFile("/tmp/a.txt", "a.txt", 1<<40); err != nil {
		t.Errorf("默认不应有大小上限：%v", err)
	}
	if err := g.checkDownloadTarget(t.TempDir()); err != nil {
		t.Errorf("默认不应限制下载目录：%v", err)
	}
}

// TestPathHasPrefixBoundary 验证路径前缀判定以边界为界。
func TestPathHasPrefixBoundary(t *testing.T) {
	if !pathHasPrefix("/backup/x", "/backup") {
		t.Error("应命中")
	}
	if !pathHasPrefix("/backup", "/backup") {
		t.Error("相等应命中")
	}
	if pathHasPrefix("/backup2", "/backup") {
		t.Error("/backup2 不应被 /backup 命中")
	}
	if pathHasPrefix("", "/backup") {
		t.Error("空前缀不应命中")
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// parseIPOrFail 解析 IP 字面量，失败即测试失败。
func parseIPOrFail(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("无法解析 IP: %s", s)
	}
	return ip
}

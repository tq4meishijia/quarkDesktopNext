// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 危险操作的前置校验（对应 HANDOFF §6.1 第 3 项「接入 guard 前置校验」）。
//
// 为什么不在桌面端直接复用 quark-cil/internal/guard：
// Go 语言层面禁止跨 module import 内部包（internal/ 规则），实测报错
// “use of internal package ... not allowed”。而修改上游把 guard 提升为
// 公开包会破坏其 internal 语义、且带来持续的合并冲突（AGPL 上游）。
// 因此这里在桌面端自有代码（MIT 侧）实现等价校验，规则与上游保持一致，
// 不修改 AGPL 上游任何文件，也就不触发 §13 的披露义务。
//
// 校验分两层：
//  1. 本地上传路径黑名单 —— 防止把 SSH 私钥、系统文件等敏感文件传到网盘。
//     这是最关键的一层：桌面端是 GUI 工具，用户可能对路径意图不清，
//     而 SDK 侧只校验扩展名与大小，不看文件本身是什么。
//  2. 可配置的操作/路径黑名单 —— 与上游对齐，环境变量驱动，默认全放行
//     （保持普通用户零干扰；不降低现有安全等级，只增加可选的收紧手段）。

// guardConfig 是校验规则的运行时快照。
type guardConfig struct {
	denyOps   map[string]bool
	denyPaths []string
	denyExts  map[string]bool
	maxUpload int64
}

// loadGuardConfig 读取环境变量中的黑名单配置。
//
// 环境变量（与上游 kuake_cli 同名，便于用户统一配置）：
//
//	KUAKE_DENY_OPS       冒号分隔的操作名黑名单，如 "delete:move"
//	KUAKE_DENY_PATHS     冒号分隔的远端路径前缀黑名单
//	KUAKE_DENY_EXTS      冒号分隔的上传扩展名黑名单，如 "pem:key"
//	KUAKE_MAX_UPLOAD_MB  单文件上传上限（MB），0 表示不限制
func loadGuardConfig() guardConfig {
	g := guardConfig{
		denyOps:  map[string]bool{},
		denyExts: map[string]bool{},
	}
	for _, op := range splitGuardList(os.Getenv("KUAKE_DENY_OPS")) {
		g.denyOps[strings.ToLower(op)] = true
	}
	g.denyPaths = splitGuardList(os.Getenv("KUAKE_DENY_PATHS"))
	for _, ext := range splitGuardList(os.Getenv("KUAKE_DENY_EXTS")) {
		g.denyExts[strings.ToLower(strings.TrimPrefix(ext, "."))] = true
	}
	if v := strings.TrimSpace(os.Getenv("KUAKE_MAX_UPLOAD_MB")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			g.maxUpload = n * 1024 * 1024
		}
	}
	return g
}

func splitGuardList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ":") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// checkOp 拦截被禁用的操作名。
func (g guardConfig) checkOp(op string) error {
	if len(g.denyOps) == 0 {
		return nil
	}
	if g.denyOps[strings.ToLower(op)] {
		return fmt.Errorf("操作已被策略禁用: %s", op)
	}
	return nil
}

// checkRemotePath 拦截被禁用的远端路径前缀。
func (g guardConfig) checkRemotePath(p string) error {
	if len(g.denyPaths) == 0 {
		return nil
	}
	clean := pathCleanSlash(p)
	for _, deny := range g.denyPaths {
		if pathHasPrefix(clean, pathCleanSlash(deny)) {
			return fmt.Errorf("目标路径已被策略禁用: %s", p)
		}
	}
	return nil
}

// pathCleanSlash 统一分隔符并做词法归一（不触碰文件系统）。
func pathCleanSlash(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = filepath.ToSlash(p)
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// pathHasPrefix 以「路径边界」为界判断前缀，避免 /backup2 被 /backup 误伤。
func pathHasPrefix(target, prefix string) bool {
	if prefix == "" {
		return false
	}
	if target == prefix {
		return true
	}
	return strings.HasPrefix(target, prefix+"/")
}

// checkUploadFile 校验单个待上传文件：本地路径黑名单 + 扩展名 + 大小。
func (g guardConfig) checkUploadFile(localPath, displayName string, size int64) error {
	if err := checkSensitiveLocalPath(localPath); err != nil {
		return err
	}
	if len(g.denyExts) > 0 {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(displayName), "."))
		if ext != "" && g.denyExts[ext] {
			return fmt.Errorf("该文件类型已被策略禁用: .%s", ext)
		}
	}
	if g.maxUpload > 0 && size > g.maxUpload {
		return fmt.Errorf("文件超过策略允许的大小上限（%d MB）", g.maxUpload/1024/1024)
	}
	return nil
}

// checkDownloadTarget 校验下载落盘目录是否被沙箱限制。
func (g guardConfig) checkDownloadTarget(dir string) error {
	root := strings.TrimSpace(os.Getenv("KUAKE_DOWNLOAD_DIR"))
	if root == "" {
		return nil // 未配置沙箱则不限制
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	if !pathWithin(absRoot, absDir) {
		return fmt.Errorf("下载目录超出允许范围: %s", dir)
	}
	return nil
}

// checkRemoteFileName 校验远端返回的文件名，拒绝路径穿越。
//
// 远端文件名是不可信输入：恶意分享可以把条目名伪装成 "../../.ssh/authorized_keys"，
// 若直接拼到落盘目录上就能写到下载目录之外。与上游 guard.CheckRemoteFileName 同义。
//
// 注意只拦「作为路径段的 ..」与分隔符，不做子串匹配：
// "..hidden"、"file..name.txt" 是合法文件名，粗暴拒绝会误伤正常文件。
func checkRemoteFileName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return fmt.Errorf("远端文件名为空")
	}
	if strings.ContainsAny(n, `/\`) {
		return fmt.Errorf("远端文件名含非法路径分隔符: %s", n)
	}
	// 整个名字就是 "." 或 ".."：两者都是「目录」而非文件，落盘会造成语义错误。
	if n == "." || n == ".." {
		return fmt.Errorf("远端文件名非法: %s", n)
	}
	// 以 ".." 开头且第三位是分隔符（"../x"、"..\x"）是明确的路径穿越。
	// 注意 "..hidden" / "file..name.txt" 是合法文件名，不得误伤。
	if strings.HasPrefix(n, "..") && len(n) > 2 && isPathSeparator(n[2]) {
		return fmt.Errorf("远端文件名含路径穿越: %s", n)
	}
	// 兜底：控制字符（NUL 等）会截断后续路径处理。
	if strings.ContainsRune(n, 0) {
		return fmt.Errorf("远端文件名含非法字符")
	}
	return nil
}

func isPathSeparator(c byte) bool { return c == '/' || c == '\\' }

// ---------- 本地敏感文件拦截（与上游规则对齐） ----------

// uploadDenyPrefixesPOSIX 是禁止上传的系统路径前缀。
// /var/ 按子目录枚举而非整体封禁，刻意放行 macOS 的 /var/folders/...。
var uploadDenyPrefixesPOSIX = []string{
	"/etc/", "/proc/", "/sys/", "/dev/",
	"/root/", "/var/log/", "/var/lib/", "/var/spool/", "/var/db/", "/var/root/",
	"/private/etc/", "/private/var/db/",
	"/boot/", "/snap/",
}

// uploadDenyPrefixesWindows 是 Windows 系统路径前缀（小写，比较前会剥盘符）。
var uploadDenyPrefixesWindows = []string{
	`\windows\`, `\program files\`, `\program files (x86)\`, `\programdata\`,
	`\system volume information\`, `\$recycle.bin\`, `\recovery\`,
}

// windowsRootDenyBasenames 是 Windows 根目录下禁止的系统文件。
var windowsRootDenyBasenames = map[string]bool{
	"pagefile.sys": true, "hiberfil.sys": true, "swapfile.sys": true,
	"bootmgr": true, "bootnxt": true, "ntldr": true, "ntdetect.com": true,
}

// uploadDenySegments 是路径中出现的敏感目录段（凭证目录）。
var uploadDenySegments = []string{
	"/.ssh/", "/.aws/", "/.gnupg/", "/.kube/", "/.docker/", "/.config/gh/",
}

// uploadDenyBasenames 是禁止上传的敏感文件名。
var uploadDenyBasenames = map[string]bool{
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"id_rsa.pub": true, "id_ed25519.pub": true, "id_ecdsa.pub": true,
	".netrc": true, "_netrc": true, ".pgpass": true, ".my.cnf": true,
	".bash_history": true, ".zsh_history": true, ".git-credentials": true,
	".npmrc": true, ".pypirc": true, ".env": true, "credentials": true,
	"shadow": true, "master.key": true,
}

// checkSensitiveLocalPath 拦截上传本地敏感文件。
//
// 威胁模型：用户（或被诱导的自动化流程）把 SSH 私钥、凭证文件、系统文件
// 误传到网盘。SDK 侧只校验扩展名与大小，不看文件本身，故必须在 GUI 侧拦。
func checkSensitiveLocalPath(localPath string) error {
	if strings.TrimSpace(localPath) == "" {
		return fmt.Errorf("本地路径为空")
	}
	// 先解析符号链接，堵住「无害文件 → /etc/passwd」的跳转；
	// 文件不存在时用词法绝对路径兜底。
	abs := localPath
	if resolved, err := filepath.EvalSymlinks(localPath); err == nil {
		abs = resolved
	} else if a, err := filepath.Abs(localPath); err == nil {
		abs = a
	}
	// 归一化：统一分隔符、去盘符、转小写。
	//
	// Windows 上 filepath.Abs 会把 "/etc/passwd" 解析成 "<当前盘符>:\etc\passwd"，
	// 因此必须准备两种形态：
	//   slash   —— "c:/etc/passwd"，用于含盘符的判定；
	//   trimmed —— "etc/passwd"（去盘符、去前导斜杠），用于无盘符的词法判定。
	// 只匹配其中一种会漏：只匹配 slash 则 "\etc\passwd"（从资源管理器复制的
	// 无盘符绝对路径）逃逸；只匹配 trimmed 则 "c:/windows/..." 逃逸。
	lower := strings.ToLower(abs)
	slash := filepath.ToSlash(lower)
	trimmed := drivePrefixRe.ReplaceAllString(slash, "")
	trimmed = strings.TrimLeft(trimmed, "/")

	// 1) 系统路径前缀
	for _, pre := range uploadDenyPrefixesPOSIX {
		// 前缀表以 "/etc/" 形式书写，比较对象 trimmed 无前导斜杠，
		// 故两端都要剥掉再比。
		p := strings.Trim(strings.ToLower(pre), "/")
		if p == "" {
			continue
		}
		if strings.HasPrefix(trimmed, p+"/") || trimmed == p {
			return fmt.Errorf("拒绝上传系统路径下的文件: %s", localPath)
		}
	}
	for _, pre := range uploadDenyPrefixesWindows {
		winPre := strings.ReplaceAll(strings.ToLower(pre), `\`, "/")
		if strings.Contains("/"+trimmed+"/", winPre) {
			return fmt.Errorf("拒绝上传系统路径下的文件: %s", localPath)
		}
	}

	// 2) Windows 根目录系统文件（剥掉盘符再比对）
	if isWindowsSystemPath(slash) {
		return fmt.Errorf("拒绝上传系统文件: %s", localPath)
	}

	// 3) 凭证目录段（两段式边界匹配，避免 /a/.ssh/b 误判为 /a/.sshx）
	padded := "/" + trimmed + "/"
	for _, seg := range uploadDenySegments {
		s := strings.Trim(seg, "/") + "/"
		if strings.Contains(padded, "/"+s) || strings.HasPrefix(padded, "/"+s) {
			return fmt.Errorf("拒绝上传凭证目录下的文件: %s", localPath)
		}
	}

	// 4) 敏感文件名
	base := strings.ToLower(filepath.Base(abs))
	if uploadDenyBasenames[base] {
		return fmt.Errorf("拒绝上传敏感文件: %s", localPath)
	}
	return nil
}

// isWindowsSystemPath 判断是否为 Windows 系统根目录下的系统文件，
// 或位于 Windows 系统目录（\windows\、\program files\ 等）之下。
func isWindowsSystemPath(lowerSlash string) bool {
	// 去掉盘符前缀（如 c:/），得到相对根目录的路径。
	trimmed := drivePrefixRe.ReplaceAllString(lowerSlash, "")
	trimmed = strings.TrimLeft(trimmed, "/")
	if trimmed == "" {
		return false
	}
	segs := strings.Split(trimmed, "/")
	// 根目录下的系统文件（pagefile.sys / bootmgr 等）：路径只有一段。
	if len(segs) == 1 && windowsRootDenyBasenames[segs[0]] {
		return true
	}
	// 位于系统目录之下：\windows\...、\program files\... 等。
	for _, pre := range uploadDenyPrefixesWindows {
		winPre := strings.ReplaceAll(strings.ToLower(pre), `\`, "/")
		if strings.HasPrefix(trimmed, winPre) {
			return true
		}
	}
	return false
}

// drivePrefixRe 匹配 Windows 盘符前缀（如 c:/ 或 C:）。
var drivePrefixRe = regexp.MustCompile(`^[a-z]:`)

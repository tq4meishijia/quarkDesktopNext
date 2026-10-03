// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Package engine 负责「用什么方式把字节搬到本地」。
//
// 两类下载器：
//   - 内建（Native）：纯 Go 实现的多连接分片下载器，行为对齐 aria2 ——
//     支持 HTTP Range 并发分段、.part 断点续传、暂停/取消，无需任何外部程序。
//   - 外部（Descriptor）：把任务交接给系统上已安装的下载器。
//     分三种接管方式，见 Kind 常量。外部下载器的实际行为不受本工程控制，
//     因此只有 KindProcess 能汇报进度，另外两种交接后即视为「已移交」。
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Kind 描述下载器的接管方式。
const (
	// KindProcess 由本进程启动命令行程序，产物落在请求的目标路径，可跟踪进度。
	KindProcess = "process"
	// KindLaunch 唤起 GUI 下载器（IDM / 迅雷 / Motrix…），无法跟踪进度。
	KindLaunch = "launch"
	// KindURL 只把直链写入剪贴板并用系统默认程序打开，等价于「复制链接」。
	KindURL = "url"
)

// BuiltinID 是内建下载器在设置里的标识。
const BuiltinID = "builtin"

// 参数模板支持的占位符，替换见 substitute。
const (
	phURL     = "{url}"
	phDir     = "{dir}"
	phFile    = "{file}"
	phCookie  = "{cookie}"
	phReferer = "{referer}"
	phUA      = "{ua}"
)

// UA 是内建与外部下载器统一使用的 User-Agent，与原 HTTP 下载循环保持一致。
const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Descriptor 是一个外部下载器的静态描述。同一份描述既用于设置页展示，
// 也用于运行期查找，避免「界面显示的」与「实际执行的」不一致。
type Descriptor struct {
	// ID 存进 settings.json 的稳定标识。
	ID string `json:"id"`
	// Label 界面展示名。
	Label string `json:"label"`
	// Note 一句话说明这个下载器的接管方式与限制。
	Note string `json:"note"`
	// Kind 接管方式。
	Kind string `json:"kind"`
	// Exec 候选可执行文件，按序探测（PATH → 常见安装目录）。
	Exec []string `json:"exec"`
	// Args 参数模板；为空表示必须在设置里手填命令行。
	Args []string `json:"args"`
	// Hints 常见安装目录（相对 %ProgramFiles% 等环境变量根）。
	Hints []string `json:"hints"`
	// Platform 限定平台，空表示不限。
	Platform string `json:"platform"`
}

// Available 返回可执行文件的绝对路径。找不到时返回 ("", false)，
// 设置页据此把该项标成「未检测到」。
func (d Descriptor) Available() (string, bool) {
	if d.ID == BuiltinID {
		return BuiltinID, true
	}
	for _, name := range d.Exec {
		if p, err := lookPath(name); err == nil {
			return p, true
		}
	}
	for _, hint := range d.Hints {
		if platformMismatch(d.Platform) {
			continue
		}
		for _, root := range installRoots() {
			p := filepath.Join(expandEnv(root), filepath.FromSlash(hint))
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, true
			}
		}
	}
	return "", false
}

// Request 是一次下载任务的输入。Dest 是最终文件路径。
type Request struct {
	URL      string
	Dest     string
	Size     int64
	Cookie   string
	Referer  string
	UA       string
	Segments int
	// Progress 汇报已完成字节数；返回非 nil 表示应立即中止。
	Progress func(done int64) error
	// Gate 暂停闸门；返回非 nil 表示应立即中止。
	Gate func() error
}

// substitute 把参数模板里的占位符替换成实参。
// 模板是「参数数组」而非一整条命令行，因此路径含空格也不需要引号技巧。
func substitute(args []string, r Request) []string {
	dir := filepath.Dir(r.Dest)
	repl := strings.NewReplacer(
		phURL, r.URL,
		phDir, dir,
		phFile, filepath.Base(r.Dest),
		phCookie, r.Cookie,
		phReferer, r.Referer,
		phUA, r.UA,
	)
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, repl.Replace(a))
	}
	return out
}

// expandEnv 展开 %VAR% 形式的环境变量（Windows 风格），未知变量原样保留。
func expandEnv(s string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			b.WriteByte(s[i])
			continue
		}
		name := s[i+1 : i+1+j]
		if v := os.Getenv(name); v != "" {
			b.WriteString(v)
		} else {
			b.WriteString(s[i : i+j+2])
		}
		i += j + 1
	}
	return b.String()
}

// installRoots 返回常见安装目录根，按探测优先级排列。
func installRoots() []string {
	roots := []string{"%ProgramFiles%", "%ProgramFiles(x86)%", "%LOCALAPPDATA%", "%APPDATA%"}
	if runtime.GOOS != "windows" {
		roots = []string{"/usr/bin", "/usr/local/bin", "/opt/homebrew/bin"}
	}
	return roots
}

func platformMismatch(platform string) bool {
	return platform != "" && platform != runtime.GOOS
}

// Options 是一次调用的可覆盖参数：留空则用注册表里的默认命令行。
type Options struct {
	// Exec 覆盖自动探测到的可执行文件路径（设置里手填的路径）。
	Exec string
	// Args 覆盖默认参数模板。
	Args []string
	// CopyURL 用于 KindLaunch：把直链写入系统剪贴板。
	CopyURL func(string)
}

// Run 按描述执行一次下载。KindURL 与 KindLaunch 会立刻返回 ErrHandedOff，
// 表示任务已移交、本进程不再跟踪进度。
func Run(ctx context.Context, d Descriptor, opt Options, r Request) error {
	if len(opt.Args) == 0 {
		opt.Args = d.Args
	}
	if len(opt.Args) == 0 {
		return errNoArgs(d)
	}
	exec := opt.Exec
	if exec == "" {
		p, ok := d.Available()
		if !ok {
			return errNotFound(d)
		}
		exec = p
	}
	switch d.Kind {
	case KindURL:
		if opt.CopyURL != nil {
			opt.CopyURL(r.URL)
		}
		if err := openURL(ctx, r.URL); err != nil {
			return fmt.Errorf("打开直链失败: %w", err)
		}
		return ErrHandedOff
	case KindLaunch:
		if err := launchDetached(ctx, exec, substitute(opt.Args, r)); err != nil {
			return err
		}
		if opt.CopyURL != nil {
			opt.CopyURL(r.URL)
		}
		return ErrHandedOff
	default:
		return runProcess(ctx, d, exec, substitute(opt.Args, r), r)
	}
}

// ErrHandedOff 表示任务已移交给外部下载器，本进程不再跟踪进度。
// app 层据此把任务标记为「已移交」终态。
var ErrHandedOff = errors.New("已移交给外部下载器")

// Lookup 按 ID 取下载器描述。找不到返回 ok=false。
func Lookup(id string) (Descriptor, bool) {
	if id == BuiltinID || id == "" {
		return Descriptor{ID: BuiltinID, Label: "内建下载器", Kind: KindProcess}, true
	}
	for _, d := range registry {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

// List 返回全部可选下载器（含内建），供设置页渲染。
func List() []Descriptor {
	out := make([]Descriptor, 0, len(registry)+1)
	out = append(out, Descriptor{
		ID:    BuiltinID,
		Label: "内建下载器（多线程分片）",
		Note:  "纯 Go 实现，支持分片并发与断点续传，无需安装任何外部程序。",
		Kind:  KindProcess,
	})
	out = append(out, registry...)
	return out
}

// registry 是外部下载器清单。新增一个下载器只需要在这里加一条描述 +
// 在 Fetch 里给出默认参数，界面与执行路径会自动同步。
var registry = []Descriptor{
	{
		ID:    "aria2c",
		Label: "aria2",
		Note:  "命令行多线程下载器，进度可跟踪；需要本机已安装 aria2。",
		Kind:  KindProcess,
		Exec:  []string{"aria2c", "aria2c.exe"},
		Args: []string{
			"--continue=true",
			"--max-connection-per-server=8",
			"--split=8",
			"--min-split-size=1M",
			"--file-allocation=none",
			"--summary-interval=0",
			"--console-log-level=warn",
			"--header=" + phUA + ": " + phUA,
			"--header=Referer: " + phReferer,
			"--header=Cookie: " + phCookie,
			"--dir=" + phDir,
			"--out=" + phFile,
			phURL,
		},
	},
	{
		ID:    "wget",
		Label: "Wget",
		Note:  "类 Unix 环境常见；Windows 需自行安装。进度可跟踪。",
		Kind:  KindProcess,
		Exec:  []string{"wget", "wget.exe"},
		Args:  []string{"-c", "--header=Cookie: " + phCookie, "--header=Referer: " + phReferer, "-O", phFile, phURL},
	},
	{
		ID:    "curl",
		Label: "cURL",
		Note:  "几乎所有系统自带；断点续传由 -C - 提供。进度可跟踪。",
		Kind:  KindProcess,
		Exec:  []string{"curl", "curl.exe"},
		Args:  []string{"-L", "-C", "-", "--retry", "3", "-H", "Cookie: " + phCookie, "-H", "Referer: " + phReferer, "-o", phFile, phURL},
	},
	{
		ID:       "idm",
		Label:    "Internet Download Manager",
		Note:     "唤起 IDM 接管下载；IDM 不接受自定义请求头，遇到需要 Cookie 的直链可能失败。进度不可跟踪。",
		Kind:     KindLaunch,
		Platform: "windows",
		Exec:     []string{"IDMan.exe"},
		Hints:    []string{"Internet Download Manager/IDMan.exe"},
		Args:     []string{"/d", phURL, "/p", phDir, "/f", phFile, "/n", "/a"},
	},
	{
		ID:       "thunder",
		Label:    "迅雷",
		Note:     "唤起迅雷接管下载；直链已写入剪贴板，可在迅雷里直接粘贴。进度不可跟踪。",
		Kind:     KindLaunch,
		Platform: "windows",
		Exec:     []string{"Thunder.exe", "ThunderStart.exe"},
		Hints:    []string{"Thunder Network/Thunder/Thunder.exe", "Thunder/Thunder.exe"},
		Args:     []string{phURL},
	},
	{
		ID:    "motrix",
		Label: "Motrix",
		Note:  "唤起 Motrix 接管下载；直链已写入剪贴板。进度不可跟踪。",
		Kind:  KindLaunch,
		Exec:  []string{"Motrix.exe", "motrix"},
		Hints: []string{"Motrix/Motrix.exe", "Programs/Motrix/Motrix.exe"},
		Args:  []string{phURL},
	},
	{
		ID:    "fdm",
		Label: "Free Download Manager",
		Note:  "唤起 FDM 接管下载；直链已写入剪贴板。进度不可跟踪。",
		Kind:  KindLaunch,
		Exec:  []string{"fdm.exe", "fdm"},
		Hints: []string{"SoftCash International/Free Download Manager/fdm.exe", "Free Download Manager/fdm.exe"},
		Args:  []string{phURL},
	},
	{
		ID:    "jdownloader",
		Label: "JDownloader",
		Note:  "唤起 JDownloader 接管下载；直链已写入剪贴板。进度不可跟踪。",
		Kind:  KindLaunch,
		Exec:  []string{"JDownloader.exe", "jdownloader"},
		Hints: []string{"JDownloader/JDownloader.exe"},
		Args:  []string{phURL},
	},
	{
		ID:       "browser",
		Label:    "系统浏览器",
		Note:     "复制直链并用默认浏览器打开，适合临时取一个文件。",
		Kind:     KindURL,
		Platform: "",
	},
}

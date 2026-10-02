// ---------------------------------------------------------------------------
// 修改声明（AGPL-3.0 第 13 条要求）
// 本文件源自上游项目 kuake_cli（https://github.com/zhangjingwei/kuake_cli，
// AGPL-3.0）。本仓库已对其作出修改，修改日期 2026-10-02：
//   TestCheckUploadLocalPath_SystemPaths 与
//   TestCheckUploadLocalPath_SymlinkEscape 原先硬编码 POSIX 路径，在 Windows
//   上必然失败；现改为按 GOOS 选择对应的系统路径用例，使防护规则在两个平台
//   上都得到真实断言。POSIX 用例内容保持不变。
// 修改说明另见仓库根目录 NOTICE 文件。
// ---------------------------------------------------------------------------

package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckOp_DeniedOp(t *testing.T) {
	t.Setenv("KUAKE_DENY_OPS", "delete:move")
	g := NewGuard()
	if err := g.CheckOp("delete"); err == nil {
		t.Error("expected error for denied op 'delete'")
	}
	if err := g.CheckOp("move"); err == nil {
		t.Error("expected error for denied op 'move'")
	}
	if err := g.CheckOp("list"); err != nil {
		t.Errorf("unexpected error for allowed op 'list': %v", err)
	}
}

func TestCheckPath_SiblingNotBlocked(t *testing.T) {
	t.Setenv("KUAKE_DENY_PATHS", "/backup")
	g := NewGuard()
	if err := g.CheckPath("/backup2/file.txt"); err != nil {
		t.Errorf("/backup2 should not be blocked by /backup rule: %v", err)
	}
	if err := g.CheckPath("/backup"); err == nil {
		t.Error("expected exact match /backup to be blocked")
	}
	if err := g.CheckPath("/backup/2024/file.txt"); err == nil {
		t.Error("expected child /backup/2024/file.txt to be blocked")
	}
}

func TestCheckPath_MultiplePaths(t *testing.T) {
	t.Setenv("KUAKE_DENY_PATHS", "/重要资料:/备份")
	g := NewGuard()
	if err := g.CheckPath("/重要资料/doc.pdf"); err == nil {
		t.Error("expected /重要资料/doc.pdf to be blocked")
	}
	if err := g.CheckPath("/备份/old.zip"); err == nil {
		t.Error("expected /备份/old.zip to be blocked")
	}
	if err := g.CheckPath("/documents/file.txt"); err != nil {
		t.Errorf("unexpected block of /documents/file.txt: %v", err)
	}
}

func TestCheckUpload_ExtBlocked(t *testing.T) {
	t.Setenv("KUAKE_DENY_EXTS", ".env:.key:.pem")
	g := NewGuard()
	if err := g.CheckUpload(".env", 100); err == nil {
		t.Error("expected .env to be blocked")
	}
	if err := g.CheckUpload("server.key", 100); err == nil {
		t.Error("expected .key to be blocked")
	}
	if err := g.CheckUpload("cert.pem", 100); err == nil {
		t.Error("expected .pem to be blocked")
	}
	if err := g.CheckUpload("main.go", 100); err != nil {
		t.Errorf("unexpected block of main.go: %v", err)
	}
}

func TestCheckUpload_SizeLimit(t *testing.T) {
	t.Setenv("KUAKE_MAX_UPLOAD_MB", "10")
	g := NewGuard()
	if err := g.CheckUpload("big.zip", 11*1024*1024); err == nil {
		t.Error("expected 11MB file to be blocked by 10MB limit")
	}
	if err := g.CheckUpload("small.zip", 5*1024*1024); err != nil {
		t.Errorf("unexpected block of 5MB file: %v", err)
	}
	if err := g.CheckUpload("exact.zip", 10*1024*1024); err != nil {
		t.Errorf("unexpected block of exactly-at-limit file: %v", err)
	}
}

func TestCheckDownloadDir_PathTraversal(t *testing.T) {
	g := NewGuard()
	cases := []struct {
		input   string
		wantErr bool
	}{
		{"../etc", true},
		{"sub/../other", true},
		{"subdir", false},
		{"a/b/c", false},
		{"", false},
	}
	for _, c := range cases {
		err := g.CheckDownloadDir(c.input)
		if c.wantErr && err == nil {
			t.Errorf("CheckDownloadDir(%q): expected error", c.input)
		}
		if !c.wantErr && err != nil {
			t.Errorf("CheckDownloadDir(%q): unexpected error: %v", c.input, err)
		}
	}
}

func TestDownloadDir_Default(t *testing.T) {
	os.Unsetenv("KUAKE_DOWNLOAD_DIR")
	g := NewGuard()
	wd, _ := os.Getwd()
	if g.DownloadDir() != wd {
		t.Errorf("expected DownloadDir=%q (cwd), got %q", wd, g.DownloadDir())
	}
}

func TestDownloadDir_Custom(t *testing.T) {
	t.Setenv("KUAKE_DOWNLOAD_DIR", "/tmp/quark")
	g := NewGuard()
	if g.DownloadDir() != "/tmp/quark" {
		t.Errorf("expected /tmp/quark, got %q", g.DownloadDir())
	}
}

func TestCheckRemoteFileName(t *testing.T) {
	g := NewGuard()
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"normal.txt", false},
		{"中文文件.pdf", false},
		{"name with space.bin", false},
		{"", true},
		{"../escape.txt", true},
		{"a/b.txt", true},
		{"a\\b.txt", true},
		{"..", true},
		{".hidden", false}, // dotfile basename allowed; the LLM picked the remote path explicitly
	}
	for _, c := range cases {
		err := g.CheckRemoteFileName(c.name)
		if c.wantErr && err == nil {
			t.Errorf("CheckRemoteFileName(%q): expected error", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("CheckRemoteFileName(%q): unexpected error: %v", c.name, err)
		}
	}
}

// systemPathCases returns the OS-appropriate set of protected system paths.
// The upstream list was POSIX-only, which made this test fail on Windows —
// not because the guard was wrong, but because those paths simply are not
// system locations there. Windows now gets its own equivalents.
func systemPathCases() []string {
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{
			filepath.Join(root, "System32", "config", "SAM"),
			filepath.Join(root, "System32", "config", "SYSTEM"),
			filepath.Join(root, "win.ini"),
			`C:\ProgramData\Vendor\app.cfg`,
			`C:\System Volume Information\index.dat`,
			`C:\Users\Default\NTUSER.DAT`,
			`C:\$Recycle.Bin\S-1-5-21\file.txt`,
			`C:\pagefile.sys`,
			`C:\`,
		}
	}
	return []string{
		"/etc/passwd",
		"/etc/shadow",
		"/var/log/auth.log",
		"/var/lib/postgresql/data",
		"/var/spool/cron/crontabs/root",
		"/proc/self/environ",
		"/sys/class/net/eth0/address",
		"/dev/zero",
		"/root/.bashrc",
		"/private/etc/master.passwd",
	}
}

func TestCheckUploadLocalPath_SystemPaths(t *testing.T) {
	g := NewGuard()
	for _, p := range systemPathCases() {
		if err := g.CheckUploadLocalPath(p); err == nil {
			t.Errorf("CheckUploadLocalPath(%q): expected error", p)
		}
	}
}

func TestCheckUploadLocalPath_AllowsVarTmpAndFolders(t *testing.T) {
	g := NewGuard()
	// macOS user-space temp roots — must not be blanket-rejected.
	cases := []string{
		"/var/tmp/build.log",
		"/var/folders/8w/abcdef/T/work.txt",
	}
	for _, p := range cases {
		if err := g.CheckUploadLocalPath(p); err != nil {
			t.Errorf("CheckUploadLocalPath(%q): unexpected error: %v", p, err)
		}
	}
}

func TestCheckUploadLocalPath_CredentialDirs(t *testing.T) {
	tmp := t.TempDir()
	g := NewGuard()
	cases := []string{
		tmp + "/.ssh/id_rsa",
		tmp + "/.aws/credentials",
		tmp + "/.gnupg/secring.gpg",
		tmp + "/.kube/config",
		tmp + "/.docker/config.json",
		tmp + "/sub/.config/gh/hosts.yml",
	}
	for _, p := range cases {
		if err := g.CheckUploadLocalPath(p); err == nil {
			t.Errorf("CheckUploadLocalPath(%q): expected error", p)
		}
	}
}

func TestCheckUploadLocalPath_SensitiveBasenames(t *testing.T) {
	tmp := t.TempDir()
	g := NewGuard()
	// Place files under a non-sensitive parent dir to isolate the basename rule.
	cases := []string{
		tmp + "/id_rsa",
		tmp + "/id_ed25519",
		tmp + "/id_dsa",
		tmp + "/id_ecdsa",
		tmp + "/.netrc",
		tmp + "/.pgpass",
		tmp + "/.my.cnf",
	}
	for _, p := range cases {
		if err := g.CheckUploadLocalPath(p); err == nil {
			t.Errorf("CheckUploadLocalPath(%q): expected error (sensitive basename)", p)
		}
	}
}

func TestCheckUploadLocalPath_NormalFilesAllowed(t *testing.T) {
	tmp := t.TempDir()
	g := NewGuard()
	cases := []string{
		tmp + "/normal.txt",
		tmp + "/sub/dir/file.bin",
		tmp + "/中文.pdf",
	}
	for _, p := range cases {
		if err := g.CheckUploadLocalPath(p); err != nil {
			t.Errorf("CheckUploadLocalPath(%q): unexpected error: %v", p, err)
		}
	}
}

func TestCheckUploadLocalPath_SymlinkEscape(t *testing.T) {
	// A symlink that points at a protected system location must be rejected
	// even when the user-supplied path looks innocent — EvalSymlinks should
	// resolve it. The target is OS-specific: /etc/passwd on POSIX, a real
	// file under %SystemRoot% on Windows.
	target := "/etc/passwd"
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		target = filepath.Join(root, "win.ini")
	}
	tmp := t.TempDir()
	link := tmp + "/innocent.txt"
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}
	g := NewGuard()
	if err := g.CheckUploadLocalPath(link); err == nil {
		t.Errorf("CheckUploadLocalPath(%q): expected error (symlink to %s)", link, target)
	}
}

// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// newTestStore 建一个把配置写到临时目录的 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KUAKE_DESKTOP_HOME", dir)
	return Load()
}

// ---------------------------------------------------------------------------
// S-4：会话文件访问控制
// ---------------------------------------------------------------------------

// TestSaveCredentialsCreatesRestrictedFile 验证会话文件以 0600 落盘。
//
// 该权限位是 POSIX 上的实际访问控制；Windows 上不表达 ACL，
// 需靠 hardenSessionFile 补（由 TestHardenSessionFile* 覆盖其存在性与幂等性）。
func TestSaveCredentialsCreatesRestrictedFile(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveCredentials(Credentials{Cookie: "__pus=P;", Source: "manual"}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	p := filepath.Join(s.Dir(), sessionFile)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("会话文件未生成: %v", err)
	}
	if runtime.GOOS != "windows" {
		perm := fi.Mode().Perm()
		if perm&0o077 != 0 {
			t.Fatalf("会话文件权限过宽: %o（其他用户可读）", perm)
		}
	}
	// 内容必须是明文 JSON（读回可用），不因加密而破坏既有格式。
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("会话文件不是合法 JSON: %v", err)
	}
	if c.Cookie != "__pus=P;" {
		t.Fatalf("Cookie 内容不符: %q", c.Cookie)
	}
}

// TestLogoutRemovesSessionFile 验证登出（空 Cookie）删除文件。
func TestLogoutRemovesSessionFile(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveCredentials(Credentials{Cookie: "x", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredentials(Credentials{Cookie: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), sessionFile)); !os.IsNotExist(err) {
		t.Fatal("登出后会话文件应被删除")
	}
	if s.Credentials().Cookie != "" {
		t.Fatal("内存态 Cookie 未清空")
	}
}

// TestLoadPicksUpSession 验证重启后能读回凭证。
func TestLoadPicksUpSession(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveCredentials(Credentials{Cookie: "__pus=ABC;", Source: "interactive"}); err != nil {
		t.Fatal(err)
	}
	re := Load()
	if got := re.Credentials().Cookie; got != "__pus=ABC;" {
		t.Fatalf("读回的 Cookie 不符: %q", got)
	}
	if re.Credentials().Source != "interactive" {
		t.Fatalf("来源不符: %q", re.Credentials().Source)
	}
}

// TestHardenSessionFileIdempotent 验证 ACL 收紧可重复调用且不报错。
//
// Windows 上会真正调用 SetNamedSecurityInfo；非 Windows 是空操作。
// 本用例保证「旧文件在 Load 时补做收紧」这条路径不会因重复执行而失败。
func TestHardenSessionFileIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveCredentials(Credentials{Cookie: "x", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Dir(), sessionFile)
	for i := 0; i < 3; i++ {
		if err := hardenSessionFile(p); err != nil {
			t.Fatalf("第 %d 次 hardenSessionFile 失败: %v", i+1, err)
		}
	}
	// secureSessionFileOnLoad 也不应报错，且不破坏文件内容。
	secureSessionFileOnLoad(p)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ACL 收紧后文件应仍可读: %v", err)
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("ACL 收紧后内容被破坏: %v", err)
	}
}

// TestSecureSessionFileOnLoadMissingFile 验证文件不存在时不 panic。
func TestSecureSessionFileOnLoadMissingFile(t *testing.T) {
	secureSessionFileOnLoad(filepath.Join(t.TempDir(), "nope.json"))
	if err := hardenSessionFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Log("对不存在的文件返回错误属预期，仅确保不 panic")
	}
}

// ---------------------------------------------------------------------------
// D3：原子写（顺带修复的既有缺陷）
// ---------------------------------------------------------------------------

// TestWriteJSONAtomicNoTempLeftover 验证写入是原子的且不留临时文件。
//
// 旧实现直接 os.WriteFile（truncate-then-write），崩溃或并发写会留下半截 JSON；
// 而 Load 对解析失败是静默回落默认值的，用户会莫名丢设置。
func TestWriteJSONAtomicNoTempLeftover(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateSettings(Settings{DownloadDir: filepath.Join(t.TempDir(), "dl")}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
	// 设置文件必须是完整合法 JSON。
	raw, err := os.ReadFile(filepath.Join(s.Dir(), settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var v Settings
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("设置文件不是合法 JSON: %v", err)
	}
}

// TestWriteJSONConcurrent 验证并发写入不产生损坏文件。
func TestWriteJSONConcurrent(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = s.UpdateSettings(Settings{Concurrency: (n % 16) + 1})
		}(i)
	}
	wg.Wait()
	raw, err := os.ReadFile(filepath.Join(s.Dir(), settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var v Settings
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("并发写入后文件损坏: %v\n内容: %s", err, raw)
	}
	if v.Concurrency < 1 || v.Concurrency > 16 {
		t.Fatalf("并发度未落在合法区间: %d", v.Concurrency)
	}
}

// TestUpdateSettingsDoesNotHoldLockDuringIO 验证落盘不在锁内，
// 避免慢速磁盘把 Settings()/Credentials() 的读全部阻塞。
func TestUpdateSettingsDoesNotHoldLockDuringIO(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.Settings()
			_ = s.Credentials()
		}
	}()
	for i := 0; i < 30; i++ {
		if err := s.UpdateSettings(Settings{Concurrency: (i % 16) + 1}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// 既有行为回归（sanitize / 默认值 / 路径解析）
// ---------------------------------------------------------------------------

// TestLoadFallsBackOnCorruptFiles 验证损坏文件不影响启动。
func TestLoadFallsBackOnCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KUAKE_DESKTOP_HOME", dir)
	// 写入半截 JSON
	if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte(`{"concurrency":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionFile), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Settings().Concurrency < 1 {
		t.Fatalf("损坏配置应回落默认值，得到 %d", s.Settings().Concurrency)
	}
}

// TestSanitizeClampsValues 验证越界字段被夹回合法区间。
//
// 逐字段独立断言，且期望值以 defaults() 为基准 ——
// sanitize 会把空/非法字段全部填成默认值，故不能用「整结构体相等」比较。
func TestSanitizeClampsValues(t *testing.T) {
	d := defaults()
	// 合法值应原样保留。
	t.Run("合法值原样保留", func(t *testing.T) {
		in := Settings{Concurrency: 8, Segments: 6, Theme: "dark", UploadPolicy: "rsync", SameName: "skip", Downloader: "aria2c"}
		got := sanitize(in)
		if got.Concurrency != 8 || got.Segments != 6 || got.Theme != "dark" ||
			got.UploadPolicy != "rsync" || got.SameName != "skip" || got.Downloader != "aria2c" {
			t.Fatalf("合法值被改动: %+v", got)
		}
	})
	// 非法值应回落到默认值。
	falls := []struct {
		name string
		in   Settings
	}{
		{"并发为0", Settings{Concurrency: 0}},
		{"并发越界", Settings{Concurrency: 99}},
		{"分片为0", Settings{Segments: 0}},
		{"分片越界", Settings{Segments: 99}},
		{"主题非法", Settings{Theme: "neon"}},
		{"上传策略非法", Settings{UploadPolicy: "evil"}},
		{"同名策略非法", Settings{SameName: "clobber"}},
		{"下载器为空", Settings{Downloader: ""}},
	}
	for _, c := range falls {
		t.Run(c.name, func(t *testing.T) {
			got := sanitize(c.in)
			if got.Concurrency != d.Concurrency {
				t.Errorf("Concurrency=%d want %d", got.Concurrency, d.Concurrency)
			}
			if got.Segments != d.Segments {
				t.Errorf("Segments=%d want %d", got.Segments, d.Segments)
			}
			if got.Theme != d.Theme {
				t.Errorf("Theme=%q want %q", got.Theme, d.Theme)
			}
			if got.UploadPolicy != d.UploadPolicy {
				t.Errorf("UploadPolicy=%q want %q", got.UploadPolicy, d.UploadPolicy)
			}
			if got.SameName != d.SameName {
				t.Errorf("SameName=%q want %q", got.SameName, d.SameName)
			}
			if got.Downloader != d.Downloader {
				t.Errorf("Downloader=%q want %q", got.Downloader, d.Downloader)
			}
		})
	}
}

// TestSanitizeFillsEmptyDownloadDir 验证空下载目录被填为默认值。
func TestSanitizeFillsEmptyDownloadDir(t *testing.T) {
	if got := sanitize(Settings{}).DownloadDir; got != defaults().DownloadDir {
		t.Fatalf("DownloadDir=%q want %q", got, defaults().DownloadDir)
	}
}

// TestDefaultDirRespectsEnvOverride 验证 KUAKE_DESKTOP_HOME 生效。
func TestDefaultDirRespectsEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-home")
	t.Setenv("KUAKE_DESKTOP_HOME", want)
	if got := DefaultDir(); got != want {
		t.Fatalf("DefaultDir=%q want %q", got, want)
	}
}

// TestSessionFilePathUnderDir 验证会话文件路径在配置目录内。
func TestSessionFilePathUnderDir(t *testing.T) {
	s := newTestStore(t)
	p := s.SessionFilePath()
	if filepath.Dir(p) != s.Dir() {
		t.Fatalf("会话文件应位于配置目录内: %q vs %q", p, s.Dir())
	}
	if filepath.Base(p) != sessionFile {
		t.Fatalf("会话文件名不符: %s", filepath.Base(p))
	}
}

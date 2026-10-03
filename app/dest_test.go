// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一并编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanFileName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"正常文件.mp4", "正常文件.mp4"},
		{`a/b\c.txt`, "a_b_c.txt"},
		{`bad:name?.mp4`, "bad_name_.mp4"},
		{"trailing. ", "trailing"},
		{"CON", "_CON"},
		{"con.txt", "_con.txt"},
		{"", "未命名文件"},
		{"   ", "未命名文件"},
		{"a\x00b.txt", "ab.txt"},
	}
	for _, c := range cases {
		if got := cleanFileName(c.in); got != c.want {
			t.Errorf("cleanFileName(%q)\n got = %q\nwant = %q", c.in, got, c.want)
		}
	}
}

func TestResolveDestRename(t *testing.T) {
	dir := t.TempDir()
	first, err := resolveDest(dir, "movie.mp4", "rename")
	if err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	if filepath.Base(first) != "movie.mp4" {
		t.Fatalf("首次不应改名，实际 %q", filepath.Base(first))
	}
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := resolveDest(dir, "movie.mp4", "rename")
	if err != nil {
		t.Fatalf("重名解析失败: %v", err)
	}
	if filepath.Base(second) != "movie(1).mp4" {
		t.Fatalf("重名应加序号，实际 %q", filepath.Base(second))
	}
}

func TestResolveDestSkipAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDest(dir, "a.txt", "skip"); !errors.Is(err, errSameNameExists) {
		t.Errorf("skip 策略应报同名错误，实际 %v", err)
	}
	got, err := resolveDest(dir, "a.txt", "overwrite")
	if err != nil {
		t.Fatalf("overwrite 策略不应报错：%v", err)
	}
	if got != dest {
		t.Errorf("overwrite 应复用原路径，%q != %q", got, dest)
	}
	if _, err := resolveDest("", "a.txt", "rename"); !errors.Is(err, errEmptyDir) {
		t.Errorf("空目录应报错，实际 %v", err)
	}
}

func TestResolveDestCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "深", "层", "目录")
	if _, err := resolveDest(dir, "x.bin", "rename"); err != nil {
		t.Fatalf("应自动创建目录: %v", err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("目录未创建: %v", err)
	}
}

func TestUniqueRemoteDir(t *testing.T) {
	base := t.TempDir()
	if got := uniqueRemoteDir(base, "/"); got != base {
		t.Errorf("根目录应返回 base，实际 %q", got)
	}
	got := uniqueRemoteDir(base, "/视频/2026")
	want := filepath.Join(base, "视频", "2026")
	if got != want {
		t.Errorf("层级映射错误：%q != %q", got, want)
	}
}

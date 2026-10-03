// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newPersistManager 建一个开启了持久化的 Manager，状态文件在临时目录。
func newPersistManager(t *testing.T) (*Manager, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "transfer-tasks.json")
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	return m, p
}

// waitFile 等待落盘文件出现且内容可解析。
func waitFile(t *testing.T, path string, d int) *persistedState {
	t.Helper()
	var st *persistedState
	deadline := time.Now().Add(time.Duration(d) * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			var s persistedState
			if json.Unmarshal(raw, &s) == nil {
				st = &s
				return st
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// TestPersistenceWritesOnStop 验证 Stop 时做最后一次同步落盘。
func TestPersistenceWritesOnStop(t *testing.T) {
	m, p := newPersistManager(t)
	m.Start(2)
	m.SetConcurrency(2)
	m.Enqueue(Spec{Kind: "download", Name: "a.zip", Dest: filepath.Join(t.TempDir(), "a.zip"), Size: 100, Fid: "f1"})
	// Stop 触发同步落盘，无需等待去抖窗口。
	m.Stop()

	st := waitFile(t, p, 3)
	if st == nil {
		t.Fatal("Stop 后应已落盘且内容可解析")
	}
	if st.Version != currentPersistVersion {
		t.Errorf("版本号不符: %d", st.Version)
	}
	if len(st.Tasks) != 1 {
		t.Fatalf("任务数不符: %d", len(st.Tasks))
	}
	got := st.Tasks[0]
	if got.Name != "a.zip" || got.Fid != "f1" || got.Size != 100 {
		t.Errorf("任务字段不符: %+v", got)
	}
	if got.Dest == "" {
		t.Error("Dest 未持久化（恢复时无法续传）")
	}
}

// TestRestoreMarksInterruptedAsPaused 验证恢复的任务统一置为 paused（可续传）。
func TestRestoreMarksInterruptedAsPaused(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "transfer-tasks.json")
	dest := filepath.Join(t.TempDir(), "big.zip")

	// 第一次运行：造两个任务并落盘。
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	m.Enqueue(Spec{Kind: "download", Name: "big.zip", Dest: dest, Size: 999, Fid: "fid-1"})
	m.Enqueue(Spec{Kind: "download", Name: "done.zip", Dest: dest, Size: 1, Fid: "fid-2"})
	if !waitFor(t, 3*time.Second, func() bool { return len(m.List()) == 2 }) {
		t.Fatal("任务未入队")
	}
	// 第二个标记为完成，验证终态任务按原样恢复。
	if err := m.Cancel("nonexistent"); err == nil {
		t.Error("取消不存在的任务应报错")
	}
	m.Stop()

	// 第二次运行：恢复。
	m2 := NewManager(newFakeRunner(), nil)
	m2.EnablePersistence(p)
	m2.Start(1)
	n := m2.RestoreTasks()
	m2.Stop()
	if n != 2 {
		t.Fatalf("应恢复 2 个任务，实际 %d", n)
	}
	for _, task := range m2.List() {
		if !task.Status().IsTerminal() && task.Status() != StatusPaused {
			t.Errorf("非终态任务应恢复为 paused，实际 %s", task.Status())
		}
	}
}

// TestRestoreSkipsUploadWithMissingLocalFile 验证本地源文件已删的上传任务不恢复。
//
// 恢复一个源文件不存在的上传任务是无效的：Runner 会在 os.Open 阶段直接失败，
// 用户只会看到一个注定失败的历史记录。
func TestRestoreSkipsUploadWithMissingLocalFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "transfer-tasks.json")

	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	missing := filepath.Join(dir, "does-not-exist.bin")
	m.Enqueue(Spec{Kind: "upload", Name: "gone.bin", LocalPath: missing, RemotePath: "/x/gone.bin", Size: 10})
	m.Stop()

	m2 := NewManager(newFakeRunner(), nil)
	m2.EnablePersistence(p)
	m2.Start(1)
	n := m2.RestoreTasks()
	m2.Stop()
	if n != 0 {
		t.Fatalf("源文件已消失的上传任务不应恢复，实际恢复 %d 个", n)
	}
}

// TestRestoreKeepsUploadWithExistingLocalFile 验证源文件仍在的上传任务正常恢复。
func TestRestoreKeepsUploadWithExistingLocalFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "transfer-tasks.json")
	src := filepath.Join(dir, "present.bin")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	m.Enqueue(Spec{Kind: "upload", Name: "present.bin", LocalPath: src, RemotePath: "/x/present.bin", Size: 4})
	m.Stop()

	m2 := NewManager(newFakeRunner(), nil)
	m2.EnablePersistence(p)
	m2.Start(1)
	if n := m2.RestoreTasks(); n != 1 {
		t.Fatalf("应恢复 1 个上传任务，实际 %d", n)
	}
	m2.Stop()
}

// TestRestoreCorruptFileIsSafe 验证损坏的状态文件不阻断启动。
func TestRestoreCorruptFileIsSafe(t *testing.T) {
	p := filepath.Join(t.TempDir(), "transfer-tasks.json")
	if err := os.WriteFile(p, []byte(`{"version":1,"tasks":[{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	if n := m.RestoreTasks(); n != 0 {
		t.Fatalf("损坏文件应恢复 0 个任务，实际 %d", n)
	}
	m.Stop()
}

// TestRestoreVersionMismatchIgnored 验证版本不匹配时忽略旧文件。
func TestRestoreVersionMismatchIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "transfer-tasks.json")
	raw := `{"version":999,"tasks":[{"id":"x","kind":"download","status":"running"}]}`
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	if n := m.RestoreTasks(); n != 0 {
		t.Fatalf("版本不匹配应忽略，实际恢复 %d 个", n)
	}
	m.Stop()
}

// TestPersistenceNoTempLeftover 验证原子写不留临时文件。
func TestPersistenceNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "transfer-tasks.json")
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	for i := 0; i < 5; i++ {
		m.Enqueue(Spec{Kind: "download", Name: "f", Dest: filepath.Join(dir, "f"), Size: 1, Fid: "f"})
	}
	m.Stop()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("状态文件未生成: %v", err)
	}
}

// TestPersistenceDisabledByDefault 验证未开启持久化时不产生任何文件。
func TestPersistenceDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(newFakeRunner(), nil)
	m.Start(1)
	m.Enqueue(Spec{Kind: "download", Name: "a", Dest: filepath.Join(dir, "a"), Size: 1, Fid: "f"})
	m.Stop()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("未开启持久化时不应写文件，实际有 %d 个", len(entries))
	}
	if n := m.RestoreTasks(); n != 0 {
		t.Errorf("未开启持久化时 RestoreTasks 应返回 0，实际 %d", n)
	}
}

// TestDefaultStatePath 验证状态文件落在指定目录内。
func TestDefaultStatePath(t *testing.T) {
	got := DefaultStatePath("/some/dir")
	if filepath.Dir(got) != filepath.FromSlash("/some/dir") {
		t.Errorf("状态文件应在指定目录内: %s", got)
	}
	if filepath.Ext(got) != ".json" {
		t.Errorf("应为 .json 文件: %s", got)
	}
}

// TestCloseIdempotent 验证重复 close 不 panic。
func TestCloseIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "transfer-tasks.json")
	m := NewManager(newFakeRunner(), nil)
	m.EnablePersistence(p)
	m.Start(1)
	m.Stop()
	// 再次调用不应 panic
	m.store.close()
}

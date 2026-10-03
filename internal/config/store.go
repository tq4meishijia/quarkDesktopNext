// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Package config 负责桌面端的本地配置与会话凭证持久化。
//
// 设计约束：
//  1. 不写入 kuake_cli 的任何文件，只在桌面端自己的配置目录下读写；
//  2. 凭证与配置分离：settings.json 可自由备份，session.json 权限收紧为 0600；
//  3. 所有默认值集中在 defaults()，避免上层散落魔法值。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
)

// Settings 是设置页可编辑项的权威定义，字段与前端 settings 页一一对应。
type Settings struct {
	// DownloadDir 下载落盘根目录。
	DownloadDir string `json:"downloadDir"`
	// Concurrency 同时进行的文件传输数（1-16）。
	Concurrency int `json:"concurrency"`
	// Theme 界面主题：light / dark / system。
	Theme string `json:"theme"`
	// UploadPolicy 同名文件策略：skip / overwrite / rsync。
	UploadPolicy string `json:"uploadPolicy"`
	// StartMinimized 启动时是否最小化到托盘。
	StartMinimized bool `json:"startMinimized"`
	// Downloader 下载器 ID：builtin（内建）或注册表里的外部下载器。
	Downloader string `json:"downloader"`
	// DownloaderExec 外部下载器可执行文件路径，留空表示自动探测。
	DownloaderExec string `json:"downloaderExec"`
	// DownloaderArgs 外部下载器参数模板 JSON 数组，留空表示用内置模板。
	DownloaderArgs []string `json:"downloaderArgs"`
	// Segments 内建下载器的分片并发数（1-16）。
	Segments int `json:"segments"`
	// SameName 同名文件策略：rename（自动加序号，默认）/ overwrite / skip。
	SameName string `json:"sameName"`
}

// Credentials 保存当前会话凭证的来源与原始 Cookie 串。
type Credentials struct {
	Cookie string `json:"cookie"`
	Source string `json:"source"` // env | manual
}

// Store 是配置与凭证的统一入口，方法均并发安全。
type Store struct {
	mu   sync.Mutex
	dir  string
	sets Settings
	cred Credentials
}

const (
	settingsFile = "settings.json"
	sessionFile  = "session.json"
)

// DefaultDir 返回桌面端配置目录：优先 KUAKE_DESKTOP_HOME，其次 os.UserConfigDir/kuake-desktop。
func DefaultDir() string {
	if v := os.Getenv("KUAKE_DESKTOP_HOME"); v != "" {
		return v
	}
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "kuake-desktop")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kuake-desktop")
}

func defaults() Settings {
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, "Downloads", "QuarkDrive")
	}
	return Settings{
		DownloadDir:  dir,
		Concurrency:  3,
		Theme:        "system",
		UploadPolicy: "skip",
		Downloader:   "builtin",
		Segments:     4,
		SameName:     "rename",
	}
}

// Load 读取磁盘配置；文件缺失或字段非法时回落到默认值，不返回错误，
// 保证 GUI 任何情况下都能启动。
func Load() *Store {
	dir := DefaultDir()
	s := &Store{dir: dir, sets: defaults()}
	if raw, err := os.ReadFile(filepath.Join(dir, settingsFile)); err == nil {
		var v Settings
		if json.Unmarshal(raw, &v) == nil {
			s.sets = sanitize(v)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, sessionFile)); err == nil {
		var c Credentials
		if json.Unmarshal(raw, &c) == nil {
			s.cred = c
		}
		// 旧版本写下的 session.json 只靠 0600 保护，在 Windows 上不生效。
		// 加载时补做一次访问控制收紧，不必等用户重新登录。
		secureSessionFileOnLoad(filepath.Join(dir, sessionFile))
	}
	return s
}

// sanitize 把越界/非法字段夹回合法区间，避免坏配置把 UI 带崩。
func sanitize(v Settings) Settings {
	d := defaults()
	if v.DownloadDir == "" {
		v.DownloadDir = d.DownloadDir
	}
	if v.Concurrency < 1 || v.Concurrency > 16 {
		v.Concurrency = d.Concurrency
	}
	switch v.Theme {
	case "light", "dark", "system":
	default:
		v.Theme = d.Theme
	}
	switch v.UploadPolicy {
	case "skip", "overwrite", "rsync":
	default:
		v.UploadPolicy = d.UploadPolicy
	}
	switch v.SameName {
	case "rename", "overwrite", "skip":
	default:
		v.SameName = d.SameName
	}
	// 未识别的下载器一律回落到内建：外部下载器随时可能被卸载，
	// 坏配置不该让下载功能整体不可用。
	if v.Downloader == "" {
		v.Downloader = d.Downloader
	}
	if v.Segments < 1 || v.Segments > 16 {
		v.Segments = d.Segments
	}
	return v
}

// Dir 暴露配置目录，供设置页展示。
func (s *Store) Dir() string { return s.dir }

// SessionFilePath 返回会话凭证文件的完整路径。
// 设置页要把它展示给用户——「凭证存在哪、删掉的是哪个文件」必须可见，
// 否则用户只能猜，也没法自己手动清理。
func (s *Store) SessionFilePath() string {
	return filepath.Join(s.dir, sessionFile)
}

// Settings 返回当前配置快照。
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets
}

// UpdateSettings 合并式更新配置并落盘。
func (s *Store) UpdateSettings(v Settings) error {
	s.mu.Lock()
	s.sets = sanitize(v)
	snapshot := s.sets
	s.mu.Unlock()
	return s.writeJSON(settingsFile, snapshot, 0o644, false)
}

// Credentials 返回已保存的会话凭证。
func (s *Store) Credentials() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cred
}

// SaveCredentials 写入会话凭证（0600）；cookie 为空表示登出，直接删除文件。
//
// 权限说明：0600 在 POSIX 上由内核强制执行；Windows 上权限位不表达 ACL，
// 因此额外调用 hardenSessionFile 收紧 DACL（见 secure_windows.go）。
func (s *Store) SaveCredentials(c Credentials) error {
	s.mu.Lock()
	s.cred = c
	s.mu.Unlock()
	if c.Cookie == "" {
		_ = os.Remove(filepath.Join(s.dir, sessionFile))
		return nil
	}
	return s.writeJSON(sessionFile, c, 0o600, true)
}

// writeJSON 把 v 原子地写入 name。
//
// 先写同目录下的临时文件再 rename，避免 truncate-then-write 在崩溃或
// 并发写入时留下半截 JSON（配置解析失败会静默回落默认值，用户会莫名丢设置）。
// rename 在同一目录内是原子操作；Windows 上 os.Rename 不能覆盖已存在文件，
// 故先删除目标再改名。
func (s *Store) writeJSON(name string, v any, perm os.FileMode, sensitive bool) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.dir, name)
	tmp, err := os.CreateTemp(s.dir, "."+name+".tmp*")
	if err != nil {
		// 退化：临时文件不可用时回落到直接写，保证功能不中断。
		return os.WriteFile(final, raw, perm)
	}
	tmpName := tmp.Name()
	defer func() {
		// 任何失败路径都清掉临时文件，不留垃圾。
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// 临时文件的权限由 CreateTemp 以 0600 创建，符合最严要求。
	if err := os.Chmod(tmpName, perm); err != nil && goruntime.GOOS != "windows" {
		// POSIX 上失败要报错；Windows 上 Chmod 语义不同，忽略。
		return err
	}
	if sensitive {
		// 收紧在改名之前做：此时临时文件已存在且内容已落盘。
		// ACL 收紧失败不阻断：内容已受限在 0600（POSIX）或由父目录 ACL 兜底。
		_ = hardenSessionFile(tmpName)
	}
	if err := os.Rename(tmpName, final); err != nil {
		// Windows 上目标存在时 Rename 失败，先删再改。
		if rerr := os.Remove(final); rerr != nil {
			return err
		}
		if rerr := os.Rename(tmpName, final); rerr != nil {
			return rerr
		}
	}
	return nil
}

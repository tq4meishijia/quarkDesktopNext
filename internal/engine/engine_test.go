// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rangeServer 是一个严格遵守 HTTP Range 的静态文件服务器，
// 用来验证分片下载与断点续传是否真的按字节对齐。
type rangeServer struct {
	data    []byte
	hits    atomic.Int64
	ranges  atomic.Int64 // 收到的 206 响应数
	refused atomic.Bool  // 置位后一律返回 200（模拟不支持 Range 的服务端）
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	if s.refused.Load() {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.data)
		return
	}
	rangeSpec := r.Header.Get("Range")
	if rangeSpec == "" {
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(s.data))
		return
	}
	var start, end int64
	if _, err := fmt.Sscanf(rangeSpec, "bytes=%d-%d", &start, &end); err != nil {
		http.Error(w, "bad range", http.StatusBadRequest)
		return
	}
	if end >= int64(len(s.data)) {
		end = int64(len(s.data)) - 1
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.data)))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	s.ranges.Add(1)
	_, _ = w.Write(s.data[start : end+1])
}

func newTestServer(t *testing.T, size int) (*rangeServer, *httptest.Server) {
	t.Helper()
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("生成随机数据失败: %v", err)
	}
	rs := &rangeServer{data: buf}
	srv := httptest.NewServer(rs)
	t.Cleanup(srv.Close)
	return rs, srv
}

func newRequest(t *testing.T, srv *httptest.Server, dest string, segments int) Request {
	t.Helper()
	var mu sync.Mutex
	var last int64
	return Request{
		URL:      srv.URL + "/f.bin",
		Dest:     dest,
		Segments: segments,
		Progress: func(done int64) error {
			mu.Lock()
			if done > last {
				last = done
			}
			mu.Unlock()
			return nil
		},
		Gate: func() error { return nil },
	}
}

func TestNativeSplitDownload(t *testing.T) {
	const size = 3*minSegmentSize + 12345
	rs, srv := newTestServer(t, size)
	dir := t.TempDir()
	dest := filepath.Join(dir, "video.mp4")

	n := &Native{}
	r := newRequest(t, srv, dest, 4)
	if err := n.Fetch(context.Background(), r); err != nil {
		t.Fatalf("分片下载失败: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !bytes.Equal(got, rs.data) {
		t.Fatalf("产物与源数据不一致：长度 %d vs %d", len(got), len(rs.data))
	}
	if rs.ranges.Load() < 4 {
		t.Errorf("应当发出至少 4 个 Range 请求，实际 %d", rs.ranges.Load())
	}
	// 完成后不应残留中间文件
	for _, suffix := range []string{partSuffix, stateSuffix} {
		if _, err := os.Stat(dest + suffix); err == nil {
			t.Errorf("完成后仍残留 %s", suffix)
		}
	}
}

func TestNativeSplitResumesFromState(t *testing.T) {
	const size = 4*minSegmentSize + 777
	rs, srv := newTestServer(t, size)
	dir := t.TempDir()
	dest := filepath.Join(dir, "resume.bin")
	part := dest + partSuffix

	// 预置状态：第 0、2 片已完成，第 1、3 片未完成
	segSize := int64((size + 3) / 4)
	st := state{URL: srv.URL + "/f.bin", Size: size, Segments: 4, SegSize: segSize,
		Done: []bool{true, false, true, false}}
	saveState(part, st)
	// 把已完成的两片按正确字节写进 .part
	out, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("创建 part 失败: %v", err)
	}
	if err := out.Truncate(size); err != nil {
		t.Fatalf("预分配失败: %v", err)
	}
	for i, done := range st.Done {
		if !done {
			continue
		}
		start := int64(i) * segSize
		end := start + segSize - 1
		if end > size-1 {
			end = size - 1
		}
		if _, err := out.WriteAt(rs.data[start:end+1], start); err != nil {
			t.Fatalf("预置分片 %d 失败: %v", i, err)
		}
	}
	out.Close()

	rs.hits.Store(0)
	n := &Native{}
	if err := n.Fetch(context.Background(), newRequest(t, srv, dest, 4)); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	// 只应重新拉取未完成的 2 片（外加 1 次探测）
	if hits := rs.hits.Load(); hits > 4 {
		t.Errorf("续传应跳过已完成分片，实际请求 %d 次", hits)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !bytes.Equal(got, rs.data) {
		t.Error("续传产物与源数据不一致")
	}
}

func TestNativeStaleStateIsIgnored(t *testing.T) {
	const size = 2 * minSegmentSize
	_, srv := newTestServer(t, size)
	dir := t.TempDir()
	dest := filepath.Join(dir, "stale.bin")
	part := dest + partSuffix

	// URL 不匹配的状态必须被丢弃，否则会把两个文件的数据拼在一起
	saveState(part, state{URL: "http://example.com/other", Size: size, Segments: 2,
		Done: []bool{true, true}})
	n := &Native{}
	if err := n.Fetch(context.Background(), newRequest(t, srv, dest, 2)); err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if _, err := os.Stat(part); err == nil {
		t.Error("废弃状态下的 .part 应当已被改名消费")
	}
}

func TestNativeSingleWhenRangeUnsupported(t *testing.T) {
	const size = 2 * minSegmentSize
	rs, srv := newTestServer(t, size)
	rs.refused.Store(true)
	dir := t.TempDir()
	dest := filepath.Join(dir, "plain.bin")

	n := &Native{}
	if err := n.Fetch(context.Background(), newRequest(t, srv, dest, 8)); err != nil {
		t.Fatalf("单连接下载失败: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !bytes.Equal(got, rs.data) {
		t.Error("单连接产物与源数据不一致")
	}
}

func TestNativeGateStopsDownload(t *testing.T) {
	const size = 2 * minSegmentSize
	_, srv := newTestServer(t, size)
	dest := filepath.Join(t.TempDir(), "stopped.bin")

	r := newRequest(t, srv, dest, 2)
	r.Gate = func() error { return context.Canceled }

	err := (&Native{}).Fetch(context.Background(), r)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("闸门中止应返回 context.Canceled，实际 %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("中止后不应留下正式文件")
	}
}

func TestSubstitute(t *testing.T) {
	r := Request{
		URL:     "https://cdn.example.com/f?sign=1",
		Dest:    filepath.Join("/tmp", "dir with space", "a b.mp4"),
		Cookie:  "__pus=1; __puus=2",
		Referer: "https://pan.quark.cn/",
		UA:      UA,
	}
	got := substitute([]string{"--dir=" + phDir, "--out=" + phFile, "--cookie=" + phCookie, phURL}, r)
	want := []string{
		"--dir=" + filepath.Join("/tmp", "dir with space"),
		"--out=a b.mp4",
		"--cookie=__pus=1; __puus=2",
		"https://cdn.example.com/f?sign=1",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("占位符替换错误：\n got = %q\nwant = %q", got, want)
	}
}

func TestRegistryLookup(t *testing.T) {
	if d, ok := Lookup(""); !ok || d.ID != BuiltinID {
		t.Errorf("空 ID 应回落到内建下载器，实际 %+v", d)
	}
	if _, ok := Lookup("aria2c"); !ok {
		t.Error("aria2c 应在清单内")
	}
	if _, ok := Lookup("不存在的下载器"); ok {
		t.Error("未知 ID 应当返回 ok=false")
	}
	list := List()
	if len(list) < 2 || list[0].ID != BuiltinID {
		t.Fatalf("列表首项应为内建下载器，实际 %d 项", len(list))
	}
	for _, d := range list {
		if d.Note == "" {
			t.Errorf("下载器 %s 缺少说明文案", d.ID)
		}
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("QUARK_TEST_ROOT", "/opt/x")
	if got := expandEnv("%QUARK_TEST_ROOT%/bin"); got != "/opt/x/bin" {
		t.Errorf("环境变量展开错误：%s", got)
	}
	if got := expandEnv("%QUARK_MISSING_ROOT%/bin"); got != "%QUARK_MISSING_ROOT%/bin" {
		t.Errorf("未知变量应原样保留：%s", got)
	}
	if got := expandEnv("/plain/path"); got != "/plain/path" {
		t.Errorf("无百分号时应原样返回：%s", got)
	}
}

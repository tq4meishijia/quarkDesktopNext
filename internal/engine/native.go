// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 分片下载的参数与文件命名。
const (
	// partSuffix 未完成文件的扩展名；完成后原子改名为正式文件。
	partSuffix = ".kuake-part"
	// stateSuffix 断点续传状态文件，记录每一片是否已完成。
	stateSuffix = ".kuake-state"
	// chunkSize 单次读取缓冲，256KB 是吞吐与内存的平衡点。
	chunkSize = 256 << 10
	// minSegmentSize 小于该值的分片不值得单独一个连接。
	minSegmentSize = 1 << 20
	// progressInterval 进度汇报节流。
	progressInterval = 200 * time.Millisecond
	// maxSegments 分片数硬上限，防止设置里填出几百个连接。
	maxSegments = 16
)

// state 是断点续传状态。写入目标文件同目录，与 .part 一一对应。
type state struct {
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Segments int    `json:"segments"`
	SegSize  int64  `json:"segSize"`
	Done     []bool `json:"done"`
}

// Native 是内建下载器：多连接分片 + 断点续传，行为对齐 aria2 的核心能力。
// 不依赖任何外部程序，也不需要用户配置。
type Native struct {
	// Client 允许注入（测试用）；nil 时用带 Cookie/Referer 的默认客户端。
	Client *http.Client
}

// Fetch 执行一次下载：dest 存在则续传，不存在则从头开始。
func (n *Native) Fetch(ctx context.Context, r Request) error {
	if err := os.MkdirAll(filepath.Dir(r.Dest), 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}
	client := n.Client
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: maxSegments,
		}}
	}

	size, ranged, err := probe(ctx, client, r)
	if err != nil {
		return err
	}
	part := r.Dest + partSuffix
	if !ranged || size <= 0 {
		return n.single(ctx, client, part, r)
	}
	segments := r.Segments
	if segments < 1 {
		segments = 4
	}
	if segments > maxSegments {
		segments = maxSegments
	}
	if max := int(size / minSegmentSize); max >= 1 && segments > max {
		segments = max
	}
	if segments <= 1 {
		return n.single(ctx, client, part, r)
	}
	return n.split(ctx, client, part, r, size, segments)
}

// ---------------------------------------------------------------------------
// 单连接下载：服务端不支持 Range，或文件小到不值得分片
// ---------------------------------------------------------------------------

func (n *Native) single(ctx context.Context, client *http.Client, part string, r Request) error {
	out, from, err := openPart(part)
	if err != nil {
		return err
	}
	defer out.Close()

	req, err := r.newRequest(ctx, http.MethodGet, "")
	if err != nil {
		return err
	}
	if from > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	if from > 0 && resp.StatusCode != http.StatusPartialContent {
		// 服务端忽略了 Range，从头重下，避免把两段数据拼在一起。
		if err := out.Truncate(0); err != nil {
			return err
		}
		from = 0
	}

	rep := newReporter(r)
	buf := make([]byte, chunkSize)
	for {
		if err := r.Gate(); err != nil {
			return err
		}
		nRead, rerr := resp.Body.Read(buf)
		if nRead > 0 {
			if _, werr := out.Write(buf[:nRead]); werr != nil {
				return werr
			}
			from += int64(nRead)
			rep.add(int64(nRead))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	return commit(part, r.Dest)
}

// ---------------------------------------------------------------------------
// 分片下载：每片一个 HTTP Range 请求，共享同一个目标文件的独立区间
// ---------------------------------------------------------------------------

func (n *Native) split(ctx context.Context, client *http.Client, part string, r Request, size int64, segments int) error {
	out, _, err := openPart(part)
	if err != nil {
		return err
	}
	defer out.Close()
	// 预分配到完整长度：各分片用 WriteAt 写入自己的区间，稀疏文件不会互相覆盖。
	if fi, serr := out.Stat(); serr != nil || fi.Size() != size {
		if err := out.Truncate(size); err != nil {
			return err
		}
	}

	st := loadState(part, r, size, segments)
	segSize := (size + int64(segments) - 1) / int64(segments)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		firstEr error
	)
	done := make([]bool, segments)
	copy(done, st.Done)
	rep := newReporter(r)

	for i := 0; i < segments; i++ {
		if st.Done[i] {
			continue
		}
		start := int64(i) * segSize
		end := start + segSize - 1
		if end > size-1 {
			end = size - 1
		}
		seg := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := n.pull(ctx, client, out, r, start, end)
			mu.Lock()
			if err != nil && firstEr == nil {
				firstEr = err
			}
			if err == nil {
				done[seg] = true
				saveState(part, state{URL: r.URL, Size: size, Segments: segments, SegSize: segSize, Done: done})
			}
			mu.Unlock()
			rep.add(0)
		}()
	}
	wg.Wait()
	rep.flush()
	if firstEr != nil {
		return firstEr
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(part + stateSuffix)
	return commit(part, r.Dest)
}

// pull 下载一个分片，返回实际写入字节数。
func (n *Native) pull(ctx context.Context, client *http.Client, out *os.File, r Request, start, end int64) (int64, error) {
	req, err := r.newRequest(ctx, http.MethodGet, "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("服务端未按分片返回内容: HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, chunkSize)
	var off = start
	for {
		if err := r.Gate(); err != nil {
			return off - start, err
		}
		nRead, rerr := resp.Body.Read(buf)
		if nRead > 0 {
			if _, werr := out.WriteAt(buf[:nRead], off); werr != nil {
				return off - start, werr
			}
			off += int64(nRead)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return off - start, rerr
		}
	}
	if off > end+1 {
		return off - start, fmt.Errorf("分片越界: 期望不超过 %d，实际 %d", end+1, off)
	}
	return off - start, nil
}

// ---------------------------------------------------------------------------
// 公共零件
// ---------------------------------------------------------------------------

// probe 探测文件大小与 Range 支持。用 GET + Range: bytes=0-0，
// 比 HEAD 更可靠：不少网盘 CDN 的 HEAD 返回 403 或不带 Content-Length。
func probe(ctx context.Context, client *http.Client, r Request) (size int64, ranged bool, err error) {
	req, err := r.newRequest(ctx, http.MethodGet, "bytes=0-0")
	if err != nil {
		return 0, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	if resp.StatusCode >= 300 {
		return 0, false, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusPartialContent {
		ranged = true
		// Content-Range: bytes 0-0/12345
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				size, _ = strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64)
			}
		}
		return size, ranged, nil
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength, false, nil
	}
	return 0, false, nil
}

// newRequest 造一个带完整请求头的下载请求，与原 HTTP 下载循环保持一致，
// 避免被 OSS 边缘策略拒绝。
func (r Request) newRequest(ctx context.Context, method, rangeSpec string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.URL, nil)
	if err != nil {
		return nil, err
	}
	ua := r.UA
	if ua == "" {
		ua = UA
	}
	req.Header.Set("User-Agent", ua)
	if r.Referer != "" {
		req.Header.Set("Referer", r.Referer)
	}
	req.Header.Set("Accept", "*/*")
	if r.Cookie != "" {
		req.Header.Set("Cookie", r.Cookie)
	}
	if rangeSpec != "" {
		req.Header.Set("Range", rangeSpec)
	}
	return req, nil
}

// openPart 打开（必要时创建）.part 文件，返回文件与已存在的字节数。
func openPart(part string) (*os.File, int64, error) {
	out, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, 0, fmt.Errorf("创建本地文件失败: %w", err)
	}
	fi, err := out.Stat()
	if err != nil {
		out.Close()
		return nil, 0, err
	}
	return out, fi.Size(), nil
}

// commit 把 .part 原子改名为正式文件（先删同名旧文件，Windows 不允许覆盖改名）。
func commit(part, dest string) error {
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("写入最终文件失败: %w", err)
	}
	return nil
}

// loadState 读取断点续传状态。状态与当前参数不一致（换 URL、换分片数、
// 文件大小变了）就不能续传，否则会拼出损坏文件——此时返回一份全未完成的空状态。
func loadState(part string, r Request, size int64, segments int) state {
	fresh := state{Done: make([]bool, segments)}
	raw, err := os.ReadFile(part + stateSuffix)
	if err != nil {
		return fresh
	}
	var st state
	if json.Unmarshal(raw, &st) != nil {
		return fresh
	}
	if st.URL != r.URL || st.Size != size || st.Segments != segments || len(st.Done) != segments {
		return fresh
	}
	return st
}

func saveState(part string, st state) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(part+stateSuffix, raw, 0o644)
}

// reporter 汇总各分片的字节数并按节流汇报给上层。
type reporter struct {
	mu      sync.Mutex
	r       Request
	total   atomic.Int64
	last    time.Time
	lastVal int64
}

func newReporter(r Request) *reporter {
	return &reporter{r: r, last: time.Now()}
}

// add 累加字节。分片并行时 n 可能为 0（仅用于触发一次汇报尝试）。
func (p *reporter) add(n int64) {
	if n > 0 {
		p.total.Add(n)
	}
	p.maybeReport(false)
}

func (p *reporter) flush() { p.maybeReport(true) }

func (p *reporter) maybeReport(force bool) {
	if p.r.Progress == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	v := p.total.Load()
	if !force {
		if now.Sub(p.last) < progressInterval || v == p.lastVal {
			return
		}
	}
	p.last, p.lastVal = now, v
	_ = p.r.Progress(v)
}

// errNoArgs / errNotFound 是外部下载器不可用时的两种明确错误。
func errNoArgs(d Descriptor) error {
	return fmt.Errorf("下载器 %s 未配置命令行参数，请到设置里填写", d.Label)
}

func errNotFound(d Descriptor) error {
	return fmt.Errorf("未检测到下载器 %s，请先安装或在设置里指定可执行文件路径", d.Label)
}

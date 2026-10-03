// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zhangjingwei/kuake_cli/sdk"

	"kuake-desktop/internal/config"
	"kuake-desktop/internal/loginproxy"
	"kuake-desktop/internal/transfer"
)

// 前后端约定的事件名。前端 bridge/wails.js 中的常量必须与这里保持一致。
const (
	EventTransferUpdate = "transfer:update"
	EventTransferList   = "transfer:list"
	EventAuthChanged    = "auth:changed"
	EventToast          = "toast"
)

// App 是 Wails 绑定给前端的唯一对象。
type App struct {
	ctx context.Context

	mu     sync.Mutex
	client *sdk.QuarkClient

	store    *config.Store
	settings config.Settings
	source   string
	profile  Profile

	// 交互式登录：会话、阶段与提示文案
	loginSession *loginproxy.Session
	loginPhase   string
	loginHint    string

	// requireFreshLogin 为 true 时，下一轮交互式登录只接受新凭证：
	// 清除凭证后浏览器里那份旧 Cookie 仍在，不加这个标记会直接跳过登录。
	requireFreshLogin bool

	// watcher 是会话有效性后台探测器（见 session_watch.go）
	watcher *sessionWatcher

	tm *transfer.Manager
}

// New 构造 App：加载本地配置、启动传输内核。
func New() *App {
	st := config.Load()
	a := &App{
		store:      st,
		settings:   st.Settings(),
		loginPhase: PhaseIdle,
	}
	a.tm = transfer.NewManager(&sdkRunner{app: a}, a.emitTask)
	// 任务持久化：与配置同目录，退出前会做最后一次同步落盘。
	a.tm.EnablePersistence(transfer.DefaultStatePath(a.store.Dir()))
	a.tm.Start(16)
	a.tm.SetConcurrency(a.settings.Concurrency)
	// 读回上次残留的任务，统一置为「已中断」等待用户手动续传。
	// 必须在 Start 之后调用：恢复的任务会经 push 推给前端。
	a.tm.RestoreTasks()
	return a
}

// Startup 在窗口创建后调用：按 环境变量 → 本地已保存会话 的顺序尝试自动登录。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if s := sdk.ResolveEnvCookieString(); s != "" {
		if _, err := a.connect(s, "env"); err == nil {
			return
		}
	}
	if c := a.store.Credentials().Cookie; c != "" {
		_, _ = a.connect(c, "saved")
	}
}

// DomReady 在前端就绪后推送一次全量快照，避免 UI 首屏空白。
func (a *App) DomReady(ctx context.Context) {
	a.emitAuth()
	a.emitTaskList()
}

// Shutdown 退出前停掉调度并落盘配置。
func (a *App) Shutdown(ctx context.Context) {
	a.stopSessionWatch()
	a.tm.CancelAll()
	a.tm.Stop()
	// 登录流程可能还挂在等待中，退出前确保端口被释放
	a.mu.Lock()
	session := a.loginSession
	a.loginSession = nil
	a.mu.Unlock()
	if session != nil {
		session.Close()
	}
	_ = a.store.UpdateSettings(a.settings)
}

// ---------- 会话 ----------

// connect 用给定凭证建立客户端并拉取用户信息；成功后写入内存态并广播事件。
// source 说明凭证来源：env（环境变量）/ manual（界面输入）/ saved（本地会话）。
func (a *App) connect(raw, source string) (AuthState, error) {
	cookie := sdk.NormalizeQuarkCookieInput(raw)
	if cookie == "" {
		return AuthState{}, errors.New("凭证为空")
	}
	qc, err := newClient(cookie)
	if err != nil {
		return AuthState{}, err
	}
	resp, err := qc.GetUserInfo()
	if err != nil {
		return AuthState{}, err
	}
	if resp == nil || !resp.Success {
		msg := "登录失败"
		if resp != nil && resp.Message != "" {
			msg = resp.Message
		}
		return AuthState{}, errors.New(msg)
	}

	a.mu.Lock()
	a.client = qc
	a.source = source
	a.profile = parseProfile(resp.Data)
	a.mu.Unlock()

	// 界面输入的凭证才落盘；环境变量属于临时来源，不写入磁盘。
	// 界面粘贴与浏览器登录都落盘；环境变量属于临时来源，不写入磁盘。
	if source == "manual" || source == "interactive" {
		_ = a.store.SaveCredentials(config.Credentials{Cookie: cookie, Source: source})
	}
	a.startSessionWatch()
	a.emitAuth()
	return a.AuthStatus(), nil
}

// newClient 包装 sdk.NewQuarkClient：该函数在无凭证时 panic，这里转成 error。
func newClient(cookie string) (qc *sdk.QuarkClient, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("初始化客户端失败: %v", r)
		}
	}()
	return sdk.NewQuarkClient(cookie), nil
}

// requireClient 返回已登录客户端，未登录时返回明确错误。
func (a *App) requireClient() (*sdk.QuarkClient, error) {
	a.mu.Lock()
	qc := a.client
	a.mu.Unlock()
	if qc == nil {
		return nil, errors.New("尚未登录，请先在登录页填写 Cookie")
	}
	return qc, nil
}

// ---------- 事件推送 ----------

func (a *App) emitAuth() {
	a.emitAuthReason("")
}

// emitAuthReason 广播登录态变化；reason 非空时随事件下发，供前端识别
// 「主动退出」与「会话失效」并做出不同的本地清理动作。
func (a *App) emitAuthReason(reason string) {
	if a.ctx == nil {
		return
	}
	st := a.AuthStatus()
	st.Reason = reason
	runtime.EventsEmit(a.ctx, EventAuthChanged, st)
}

func (a *App) emitTask(t *transfer.Task) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventTransferUpdate, taskToDTO(t))
}

func (a *App) emitTaskList() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventTransferList, a.ListTasks())
}

// notify 向前端推一条轻提示。
func (a *App) notify(level, text string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventToast, map[string]string{"level": level, "text": text})
}

// ---------- 通用工具 ----------

func taskToDTO(t *transfer.Task) TaskDTO {
	done := t.Done()
	progress := 0.0
	if t.Size > 0 {
		progress = math.Min(100, float64(done)/float64(t.Size)*100)
	}
	var finished int64
	if ft := t.FinishedAt(); !ft.IsZero() {
		finished = ft.Unix()
	}
	return TaskDTO{
		ID:         t.ID,
		Kind:       t.Kind,
		Name:       t.Name,
		LocalPath:  t.LocalPath,
		Dest:       t.Dest,
		RemotePath: t.RemotePath,
		Size:       t.Size,
		Done:       done,
		Progress:   math.Round(progress*10) / 10,
		Status:     string(t.Status()),
		Speed:      t.Speed(),
		Error:      t.ErrMsg(),
		Engine:     t.Engine,
		CreatedAt:  t.CreatedAt.Unix(),
		FinishedAt: finished,
	}
}

// parseProfile 从 /account/info + /1/clouddrive/member 合并出的 map 中提取展示字段。
// 上游字段名在不同版本间有别名，这里按候选 key 依次尝试，取不到就留空，不报错。
func parseProfile(data map[string]interface{}) Profile {
	p := Profile{}
	if data == nil {
		return p
	}
	p.Nickname = mapStr(data, "nickname", "nick_name", "nickName", "name", "account_name")
	p.Avatar = mapStr(data, "avatar", "head_url", "headUrl", "avatar_url")
	p.MemberType = mapStr(data, "member_type", "memberType", "vip_type")
	p.UsedBytes = mapNum(data, "use_capacity", "used_capacity", "used", "use_size")
	p.TotalBytes = mapNum(data, "total_capacity", "totalCapacity", "total", "total_size")
	return p
}

func mapStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// mapNum 兼容 float64 / int64 / int / 数字字符串四种形态。
func mapNum(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			return int64(n)
		case float32:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i
			}
		case string:
			if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
				return i
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return int64(f)
			}
		}
	}
	return 0
}

// maskCookie 脱敏：只保留前 6 位与后 4 位，便于用户确认填的是哪一份凭证。
func maskCookie(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= 12 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:6]) + "***" + string(runes[len(runes)-4:])
}

// joinRemote 拼接远端路径，统一使用正斜杠且不留重复斜杠。
func joinRemote(dir, name string) string {
	dir = strings.TrimRight(strings.TrimSpace(dir), "/")
	name = strings.TrimLeft(strings.TrimSpace(name), "/")
	if dir == "" {
		dir = "/"
	}
	if name == "" {
		return dir
	}
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// dirOf 返回远端路径的父目录。
func dirOf(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return "/"
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return "/" + p[:i]
	}
	return "/"
}

// extOf 取小写扩展名（不含点），目录返回空。
func extOf(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

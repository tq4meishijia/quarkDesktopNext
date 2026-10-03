// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Package app 是桌面端暴露给前端的唯一边界（Wails Bind 的就是 *App）。
//
// 职责边界：
//   - 本包只做「SDK 返回值 ↔ 前端 DTO」的转换、会话生命周期与事件推送；
//   - 不重新实现任何网盘协议，所有网络与文件操作一律转发到 kuake_cli/sdk；
//   - 不修改 kuake_cli 的任何代码。
package app

// 交互式登录的阶段。前端据此决定提示文案与按钮可用性。
const (
	PhaseIdle      = "idle"
	PhaseWaiting   = "waiting"
	PhaseCapturing = "capturing"
	PhaseSuccess   = "success"
	PhaseError     = "error"
	PhaseCancelled = "cancelled"
)

// InteractiveLoginState 是一次交互式登录的进度快照，登录页轮询它。
// 代理地址只在 waiting 阶段给出，登录成功后立即失效。
type InteractiveLoginState struct {
	Active bool   `json:"active"`
	Phase  string `json:"phase"`
	Hint   string `json:"hint"`
	URL    string `json:"url"`
	// Collected 已捕获的 Cookie 条数，让用户看到确实在推进。
	Collected int `json:"collected"`
	// Remaining 距离超时的剩余秒数，前端据此显示倒计时。
	Remaining int `json:"remaining"`
	// StaleExisting 为 true 表示强制重新登录模式下检测到浏览器里已有可用凭证，
	// 代理不会自动收敛；前端必须给出明确指引，不能让用户干等。
	StaleExisting bool `json:"staleExisting"`
}

// AuthState 登录态快照，登录页与侧边栏都消费它。
type AuthState struct {
	LoggedIn bool    `json:"loggedIn"`
	Source   string  `json:"source"` // env | manual | saved
	Masked   string  `json:"masked"` // 脱敏后的凭证，仅用于回显
	Message  string  `json:"message"`
	Profile  Profile `json:"profile"`
	// Reason 是本次状态变化的来源：logout / expired / authFailed / credCleared。
	// 前端据此决定提示文案，并在非 logout 的场景下同样清掉本地存储。
	Reason string `json:"reason"`
}

// Profile 用户信息与容量。
type Profile struct {
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar"`
	MemberType string `json:"memberType"`
	UsedBytes  int64  `json:"usedBytes"`
	TotalBytes int64  `json:"totalBytes"`
}

// FileItem 目录条目。
type FileItem struct {
	Fid      string `json:"fid"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"isDir"`
	Mtime    int64  `json:"mtime"` // Unix 秒
	Ctime    int64  `json:"ctime"` // Unix 秒
	Ext      string `json:"ext"`   // 小写扩展名，不含点
	Selected bool   `json:"-"`
}

// DirListing 一次目录浏览/搜索的结果。
type DirListing struct {
	Path       string     `json:"path"`
	Items      []FileItem `json:"items"`
	ReachedCap bool       `json:"reachedCap"` // 递归搜索是否触及上限而被截断
}

// TaskDTO 传输任务的前端视图。
type TaskDTO struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // upload | download
	Name       string  `json:"name"`
	LocalPath  string  `json:"localPath"`
	Dest       string  `json:"dest"`
	RemotePath string  `json:"remotePath"`
	Size       int64   `json:"size"`
	Done       int64   `json:"done"`
	Progress   float64 `json:"progress"` // 0-100
	Status     string  `json:"status"`
	Speed      float64 `json:"speed"` // 字节/秒
	Error      string  `json:"error"`
	Engine     string  `json:"engine"`
	CreatedAt  int64   `json:"createdAt"`
	FinishedAt int64   `json:"finishedAt"`
}

// ShareNode 分享链接里的一个可转存条目。
type ShareNode struct {
	Fid   string `json:"fid"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
	Token string `json:"token"` // share_fid_token，转存时需要原样带回
}

// SharePreview 解析分享链接后的预览结果。
type SharePreview struct {
	PwdID    string      `json:"pwdId"`
	Passcode string      `json:"passcode"`
	Stoken   string      `json:"stoken"`
	Title    string      `json:"title"`
	Items    []ShareNode `json:"items"`
}

// SaveShareRequest 转存请求。Fids 为空表示整包转存。
type SaveShareRequest struct {
	PwdID     string   `json:"pwdId"`
	Stoken    string   `json:"stoken"`
	Fids      []string `json:"fids"`
	Tokens    []string `json:"tokens"`
	TargetFid string   `json:"targetFid"` // 目标目录 fid，"0" 为根目录
	SaveAll   bool     `json:"saveAll"`
}

// SaveShareResult 转存结果。
type SaveShareResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	TaskID  string `json:"taskId"`
}

// ShareLink 创建分享得到的链接。
type ShareLink struct {
	URL       string `json:"url"`
	Passcode  string `json:"passcode"`
	PwdID     string `json:"pwdId"`
	ExpiresAt int64  `json:"expiresAt"`
}

// MyShareItem 我的分享列表条目。
type MyShareItem struct {
	ShareID   string `json:"shareId"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Passcode  string `json:"passcode"`
	ViewCnt   int64  `json:"viewCnt"`
	SaveCnt   int64  `json:"saveCnt"`
	CreatedAt int64  `json:"createdAt"`
	Expired   bool   `json:"expired"`
}

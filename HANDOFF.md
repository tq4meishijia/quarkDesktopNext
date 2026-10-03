# HANDOFF · quarkDesktopNext 交接文档

> **文档用途**：面向接手本项目的新开发者，浓缩项目定位、架构约束、运行方式、当前进度与风险点。
> **基线提交**：`ca17c91` — *fix(login): 修复交互式登录白屏与扫码后不跳转，并完善退出登录凭证清理*
> **基线分支**：`main`　**远程**：`git@github.com:tq4meishijia/quarkDesktopNext.git`
> **核验环境**：Windows 11 / Go 1.25.5 / Wails CLI v2.10.2
> **核验结论**：`go build` · `go vet` · `go test` 在 `kuake-desktop` 与 `quark-cil` 两个模块上均通过

---

## 目录

1. [项目概述](#1-项目概述)
2. [技术栈与架构说明](#2-技术栈与架构说明)
3. [关键模块与目录说明](#3-关键模块与目录说明)
4. [本地环境搭建与运行步骤](#4-本地环境搭建与运行步骤)
5. [当前开发进度](#5-当前开发进度)
6. [待办事项](#6-待办事项)
7. [已知问题及注意事项](#7-已知问题及注意事项)

---

## 1. 项目概述

### 1.1 是什么

**夸克网盘桌面版（`kuake-desktop`）** —— 在上游 CLI 项目 [`kuake_cli`](https://github.com/zhangjingwei/kuake_cli) 的 SDK 之上加一层适配层与一套桌面 GUI，让用户用鼠标操作夸克网盘。

**核心设计原则：桌面端不实现任何网盘协议。** 所有网络与文件操作均转发到上游 SDK，与 CLI 共用同一套能力；GUI 只负责调用和呈现。

### 1.2 规模

| 组成 | 规模 |
| --- | --- |
| 桌面端 Go 代码（`main.go` + `app/` + `internal/`，不含测试） | 5,140 行 |
| 桌面端 Go 测试 | 10 个测试文件，合计 2,203 行 |
| 上游 SDK（`quark-cil/`，含测试） | 12,781 行 |
| 前端 JS（`frontend/src/`，24 个文件） | 4,758 行 |
| 外部运行时依赖 | **仅 2 个**（Wails + 上游 SDK） |

桌面端自有代码**零第三方依赖**，只用 Go 标准库。所有重活（分片下载、HTTP 代理、任务队列）均为手写标准库实现。

### 1.3 已实现的用户能力

- **交互式登录** —— 起本地临时反向代理 + 调起系统浏览器，自动捕获完整 Cookie，规避手工粘贴缺失 HttpOnly 字段（`_UP_*`、`tfstk`）的问题；Cookie 粘贴与环境变量登录降级为兜底
- **文件浏览** —— 目录导航、面包屑、列表/网格双视图、客户端 BFS 搜索（可递归）、多选（含 Shift 区间）、排序、新建/重命名/删除/移动/复制
- **传输** —— 上传/下载任务队列，暂停/继续/取消/重试，断点续传，批量操作
- **内建多线程下载器** —— 纯 Go 实现，行为对齐 aria2：HTTP Range 分片并发、`.part` 断点续传、零外部程序依赖
- **可切换下载器** —— 内建 + aria2/Wget/cURL（进度可跟踪）或 IDM/迅雷/Motrix/FDM/JDownloader/系统浏览器（唤起接管）
- **下载路径完全自定义** —— 单次改写、保留网盘目录层级、同名默认加 `(1)` 序号而非覆盖
- **分享** —�� 链接解析、转存到任意目录、创建分享、我的分享列表、取消分享
- **自动构建与发布** —— GitHub Actions 双工作流，推 tag 产出 6 平台可执行文件

### 1.4 许可与合规（重要）

本项目采用**分层许可**，改动代码前务必理解边界：

| 范围 | 许可证 |
| --- | --- |
| 仓库根（`main.go`/`app/`/`internal/`/`frontend/`/`scripts/`/`wails.json`） | **MIT** |
| `quark-cil/` 子目录（上游） | **AGPL-3.0** |

`go.mod` 中有 `replace github.com/zhangjingwei/kuake_cli => ./quark-cil`，即**编译期链接**了 AGPL 代码。结论：

- 单独使用/引用根目录桌面端源码 → 适用 MIT
- 根目录源码与 `quark-cil/` 一同编译并**对外分发** → **整体产物须遵循 AGPL-3.0**
- **反向不成立**：不能把含 AGPL 代码的衍生作品整体以 MIT 分发

对 `quark-cil/` 的任何修改，须按 AGPL 第 13 条在**文件顶部声明 + `NOTICE` 双重标注**。仓库中已有两处此类修改（`internal/guard/guard.go`、`guard_test.go`，均为补齐 Windows 平台防护，未放宽既有规则），详见 `NOTICE` 第一节。

---

## 2. 技术栈与架构说明

### 2.1 技术栈

| 层 | 选型 | 版本 |
| --- | --- | --- |
| GUI 框架 | Wails v2 | v2.10.2 |
| 语言 | Go | 1.25.5（`go.mod` 声明 `go 1.25.5`） |
| 前端 | **原生 ES Module JavaScript** | 无框架、无构建 |
| 状态管理 | 自研 `createStore()`（浅合并） | — |
| 路由 | 自研 hash 路由 | — |
| DOM 渲染 | 自研 hyperscript `h()` | — |
| 图标 | 自研内联 SVG | — |
| 上游 SDK | `github.com/zhangjingwei/kuake_cli` | 本地 `replace` → `./quark-cil` |

> **Wails 间接依赖**（由 Wails 拖入，非直接使用）：`go-webview2`（Windows WebView2）、`labstack/echo/v4`、`gorilla/websocket`、`jchv/go-winloader`、`golang.org/x/sys` 等。完整清单见 `NOTICE` 第三节。

### 2.2 分层架构

```
┌──────────────────────────────────────────────────────────────┐
│  frontend/src/            零构建原生 ESM（24 个 .js）         │
│  pages/ components/       5 个页面 + 6 个组件                │
│  core/                    h() / store / router / theme …      │
│  bridge/           ★ 前端唯一数据出入口                       │
│    ├─ index.js            门面：运行时探测 wails 或 mock      │
│    ├─ wails.js            真实后端适配器                      │
│    └─ mock.js             浏览器预览假数据适配器              │
└───────────────────────┬──────────────────────────────────────┘
                        │ window.go.app.App.*()   Wails IPC
                        │ runtime.EventsEmit / EventsOn
┌───────────────────────▼──────────────────────────────────────┐
│  app/               ★ 前后端唯一边界（Wails Bind 的就是 *App） │
│                    只做 DTO 转换 / 会话生命周期 / 事件推送     │
│  app.go auth.go files.go transfer.go share.go settings.go …   │
└──────┬────────────────────────────┬──────────────────────────┘
       │ Runner 接口注入             │ 直接调用
┌──────▼──────────────┐   ┌─────────▼──────────────────────────┐
│ internal/transfer/  │   │ internal/engine/                  │
│ 传输队列内核         │   │ 下载引擎（分片/续传/外部下载器）    │
│ ★不依赖网盘 API      │   │ ★不依赖网盘 API，只吃直链 + 落盘路径│
└──────┬──────────────┘   └─────────┬──────────────────────────┘
       │ sdkRunner.Upload/Download    │ HTTP Range / 外部进程
┌──────▼──────────────────────────────▼──────────────────────────┐
│  quark-cil/sdk/     上游 CLI SDK（AGPL-3.0，只读禁止修改）     │
│  QuarkClient  →  https://drive-pc.quark.cn 等                  │
└───────────────────────────────────────────────────────────────┘
       ▲
┌──────┴───────────────────────────────────────────────────────┐
│ internal/loginproxy/  交互式登录：本地临时反向代理捕获 Cookie   │
│ internal/config/       settings.json / session.json 持久化      │
└───────────────────────────────────────────────────────────────┘
```

### 2.3 三条不可违反的依赖纪律

这三条在代码注释与 `docs/FEATURE-IMPL-PROMPT.md` 中被反复强调，**改动代码前必须理解**：

1. **`app` 包只做转换，不做协议** —— 任何网盘 API 调用都必须下沉到 `internal/` 或交给 SDK
2. **`internal/transfer` 与 `internal/engine` 不依赖网盘 API** —— 字节搬运通过 `Runner` 接口注入，下载只接受"直链 URL + 落盘路径"。这使二者可独立测试
3. **不修改 `quark-cil/`，不引入新的第三方依赖** —— 除非是补安全能力并按 AGPL 第 13 条标注

### 2.4 关键设计模式

**① 会话失效单一收口**（`app/auth.go`）

```go
// invalidateSession 是所有「会话失效」路径的唯一收口：
// 断开内存客户端、清空用户信息缓存、删除落盘凭证、广播 auth:changed。
// 覆盖：主动退出 / 会话过期 / 鉴权失败 / 清除本机凭证
```

覆盖 4 条入口，避免将来新增入口漏步骤。`Logout`/`HandleSessionInvalid` 无法触碰 WebView 的 `localStorage`，因此**前端在收到 `auth:changed` 后自行清理本地存储**（`core/session.js`），Go 侧只负责内存态 + 磁盘凭证。

**② `sdkRunner` 刻意私有化**（`app/transfer.go`）

```go
// 刻意使用未导出类型：Wails 会把 *App 的所有导出方法生成前端绑定，
// 若让 *App 自己实现 Runner，Upload/Download 就会泄漏到前端 API 里
// （且参数含 *transfer.Task，前端根本无法构造）。
type sdkRunner struct{ app *App }
```

**③ 宽松字段解析**（`app/app.go` + `app/share.go`）—— 夸克上游不同接口版本间字段名有别名，`mapStr`/`mapNum`/`mapBool` 按候选键依次尝试（如昵称按 `nickname → nick_name → nickName → name → account_name`），**取不到就留空，不报错**。

**④ 前端双桥接** —— `bridge/index.js` 运行时探测 Wails 注入，失败则回落 `mock.js`。页面代码只认 `bridge()`，永远不知道自己跑在桌面壳还是浏览器预览模式。这使得**不装 Go、不登录就能演示全部界面**（见 §4.4）。

**⑤ 三处独立于应用树的挂载点** —— `index.html` 把 `#toast-root` 与 `#modal-root` 挂在 `#app` 之外，因此页面整体重渲染不会把提示和对话框一起清掉。

---

## 3. 关键模块与目录说明

### 3.1 目录总览

```
quarkDesktopNext/
├── main.go                    应用入口（48 行）：Wails 启动、生命周期注册
├── go.mod / go.sum            根模块（module kuake-desktop）
├── wails.json                 Wails 配置（三个前端构建命令**全为空**）
├── README.md                  主文档（33 KB，11 章）
├── NOTICE                     合规声明（AGPL 分层 + 上游修改披露）
├── app/                       ★ Wails 绑定层（2,137 行，10 文件 + 3 测试）
├── internal/
│   ├── config/                配置与凭证持久化（203 行）
│   ├── engine/                下载引擎（940 行 + 287 行测试）
│   ├── loginproxy/            交互式登录代理（1,094 行 + 1,617 行测试）
│   └── transfer/              传输队列内核（694 行）
├── frontend/                  前端（24 个 JS，4,758 行）
├── quark-cil/                 上游 CLI 源码（AGPL-3.0，禁止修改）
├── scripts/                   构建/校验脚本（10 个）
├── build/                     打包资源（appicon.png、windows/）— gitignore
├── .github/workflows/         ci.yml、desktop-release.yml
├── appicon.png                应用图标（构建脚本检查此文件）
└── dist/                      构建产物落地目录 — gitignore
```

### 3.2 后端：`app/` — Wails 绑定层

前后端**唯一**边界。`main.go` 的 `Bind: []interface{}{application}` 只绑定一个对象 `*app.App`。

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| `app.go` | 367 | `App` 结构体、三个生命周期钩子、`connect` 登录流程、事件推送、DTO→Task 转换、宽松 map 解析工具 |
| `auth.go` | 216 | 登录/登出/环境变量登录/凭证状态/凭证清除/会话失效统一收口 |
| `files.go` | 360 | 目录浏览、客户端 BFS 搜索、增删改移、fid 解析 |
| `transfer.go` | 356 | `sdkRunner`（Runner 实现）+ 全部任务队列 API + 系统文件对话框 |
| `share.go` | 231 | 分享解析、转存、创建分享、我的分享、取消分享 |
| `settings.go` | 169 | 设置读写、下载器探测、目录选择/校验、系统文件管理器 |
| `dto.go` | 155 | 全部前后端 DTO + 交互式登录阶段常量 |
| `interactive.go` | 169 | 交互式登录编排：起代理 → 开浏览器 → 等凭证 → 复用 `connect` |
| `dest.go` | 124 | 本地落盘路径安全清洗（Windows 保留字符、设备名、同名策略） |

**生命周期钩子**：

| 钩子 | 行为 |
| --- | --- |
| `Startup(ctx)` | 保存 ctx → **环境变量 Cookie 优先**（`KUAKE_COOKIE`）→ 失败回落本地 `session.json` |
| `DomReady(ctx)` | 推一次全量快照（`emitAuth` + `emitTaskList`），避免 UI 首屏空白 |
| `Shutdown(ctx)` | `CancelAll` → `Stop` → 关闭代理释放端口 → 落盘 settings |

> 注意：`New()` 里已 `config.Load()` 并 `tm.Start(16)`，即**传输内核在 `main()` 构造时就启动**，早于 `OnStartup`。

**事件契约**（前端 `bridge/index.js` 的 `EVENTS` 必须与此一致）：

| 常量 | 事件名 | Payload | 触发时机 |
| --- | --- | --- | --- |
| `EventTransferUpdate` | `transfer:update` | `TaskDTO` | 任务状态/进度变化（节流 250ms） |
| `EventTransferList` | `transfer:list` | `[]TaskDTO` | `DomReady` 全量 |
| `EventAuthChanged` | `auth:changed` | `AuthState` | 登录成功 / 会话失效 |
| `EventToast` | `toast` | `{level, text}` | `notify()` |

**对外暴露 47 个方法**（以 `frontend/wailsjs/go/app/App.js` 的导出函数为准），分 5 组：

- **auth（12）**：`AuthStatus` `Login` `LoginFromEnv` `Logout` `HandleSessionInvalid` `CredentialState` `ClearCredentials` `GetProfile` `StartInteractiveLogin` `OpenInteractiveLoginURL` `InteractiveLoginStatus` `CancelInteractiveLogin`
- **files（8）**：`ListDir` `Search` `CreateFolder` `Rename` `Delete` `Move` `Copy` `ResolveFid`
- **transfer（15）**：`PickUploadFiles` `PickDownloadDir` `EnqueueUploads` `EnqueueDownloads` `ListTasks` `PauseTask` `ResumeTask` `CancelTask` `RetryTask` `PauseAllTasks` `ResumeAllTasks` `CancelAllTasks` `ClearCompletedTasks` `RevealLocal` `OpenTaskDest`
- **share（5）**：`ParseShare` `SaveShare` `CreateShareLink` `ListMyShares` `DeleteShare`
- **settings（7）**：`GetSettings` `SaveSettings` `ConfigDir` `EnsureDownloadDir` `ListDownloaders` `PickDownloaderExec` `RevealLocalDir`

### 3.3 后端：`internal/` 四个包

#### `internal/config/` — 配置与凭证持久化

单文件 `store.go`（203 行）。三条设计约束：不写入 `quark-cil/` 任何文件；凭证与配置分离（`settings.json` 0644 可备份，`session.json` **0600** 收紧）；默认值集中在 `defaults()`。

```go
type Settings struct {
    DownloadDir    string   // 下载目录
    Concurrency    int      // 1-16，默认 3
    Theme          string   // light|dark|system
    UploadPolicy   string   // skip|overwrite|rsync
    StartMinimized bool     // ⚠️ 空开关，见 §7
    Downloader     string   // builtin | 外部 ID
    DownloaderExec string
    DownloaderArgs []string
    Segments       int      // 1-16，默认 4
    SameName       string   // rename|overwrite|skip
}
```

**关键行为**：`Load()` **永不返回 error**（文件缺失/字段非法一律回落默认，保证 GUI 必能启动）；`sanitize()` 负责越界夹回；`SaveCredentials` 在 Cookie 为空时 `os.Remove` 删整个 `session.json`。

配置目录解析顺序：`KUAKE_DESKTOP_HOME` > `os.UserConfigDir()/kuake-desktop` > `~/.kuake-desktop`。
`SessionFilePath()` 刻意暴露给设置页展示 —— "凭证存在哪"必须对用户可见。

#### `internal/engine/` — 下载引擎

**职责**：决定"用什么方式把字节搬到本地"。与网盘 API 完全无关。

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| `engine.go` | 348 | `Request`/`Descriptor`/`Options` 契约、下载器注册表（8 个外部 + 1 内建）、`Run` 分派、占位符替换 |
| `native.go` | 421 | **内建分片下载器**：probe 探测、single 单连接、split 多分片、`.part` 续传、状态文件、进度节流 |
| `process.go` | 102 | 外部命令行下载器拉起 + 轮询目标文件大小汇报进度 |
| `lookup.go` | 54 | `lookPath`（Windows 自动补 `.exe`）、`openURL` |
| `proc_windows.go` / `proc_other.go` | 15 / 15 | 平台差异化 `sysProcAttr`（Windows `HideWindow`） |
| `engine_test.go` | 287 | 8 个测试 |

**`Native.Fetch` 执行流程**：

```
probe()  →  GET + Range: bytes=0-0
   ├─ 206 → ranged=true，从 Content-Range 解析 size
   └─ 200 → ranged=false，用 ContentLength
   （用 GET+Range 而非 HEAD：不少网盘 CDN 的 HEAD 返回 403 或不带 Content-Length）

!ranged || size<=0        → single()
segments 夹取 [1,16] 且 ≤ size/1MB，≤1 → single()
否则                      → split()
```

**`split()` 关键点**：`out.Truncate(size)` 预分配，各分片用 `out.WriteAt(buf, off)` 写自己区间（稀疏文件不互相覆盖）；**每片成功即落盘状态**（原子性靠单次 `WriteFile`）；全部完成 → 删状态 → `commit()`。`commit()` 中 `os.Remove(dest)` 是必需的 —— **Windows 不允许覆盖改名**。

**状态文件一致性校验**（`loadState`，防止拼出损坏文件）—— URL 变 / 分片数变 / 文件大小变 → 全未完成的空状态。

**常量**：`partSuffix = ".kuake-part"`、`stateSuffix = ".kuake-state"`、`chunkSize = 256KB`、`minSegmentSize = 1MB`、`progressInterval = 200ms`、`maxSegments = 16`

**三种接管方式**：

| Kind | 行为 | 进度 | 任务终态 |
| --- | --- | --- | --- |
| `process` | 本进程启动命令行（aria2/wget/curl） | 轮询目标文件大小 | completed |
| `launch` | 唤起 GUI 下载器（IDM/迅雷/Motrix/FDM/JDownloader） | 不跟踪 | **handedoff** |
| `url` | 直链写入剪贴板 + 默认程序打开 | 不跟踪 | **handedoff** |

**参数占位符**：`{url}` `{dir}` `{file}` `{cookie}` `{referer}` `{ua}`。模板是**参数数组**而非一整条命令行，因此路径含空格也不需要引号技巧。

**下载器注册表**（8 个外部）：`aria2c`(process) `wget`(process) `curl`(process) `idm`(launch) `thunder`(launch) `motrix`(launch) `fdm`(launch) `jdownloader`(launch) + `browser`(url) + `builtin`。

**外部下载器进度靠轮询文件大小**的原因（代码注释原文）：aria2/wget/curl 的进度输出格式各不相同，且都可能被用户重定向，**静默观察产物是唯一在三者上都成立的做法**。stdout/stderr 落到 `%TEMP%/quark-desktop-<id>.log`。

#### `internal/transfer/` — 传输队列内核

**与网盘实现完全无关**的队列内核，字节搬运由 `Runner` 接口注入。

```go
type Runner interface {
    Upload(ctx context.Context, t *Task, prog ProgressFunc) error
    Download(ctx context.Context, t *Task, prog ProgressFunc) error
}
var ErrHandedOff = errors.New("已移交给外部下载器")

type Status string  // pending|running|paused|completed|failed|cancelled|handedoff
```

`StatusHandedOff` 单列且 `Succeeded() == true` —— 已移交外部下载器**仍算正常结束**，不是失败。

**`Gate` 暂停闸门**：

```go
func (g *Gate) Wait(ctx context.Context) error   // 轮询间隔 100ms
```

取消走 context，不另开信号。`Cancel` 时先 `Gate().Set(false)` 解除暂停，避免 Runner 卡在闸门里收不到取消信号。

**双层并发控制**：`Start(maxWorkers)` 是硬上限（`app.New()` 传 16），**实际并行数由 `SetConcurrency(n)` 的信号量决定**（默认 3）。`SetConcurrency` 运行时替换 sem channel，**设置改动即刻生效**。

**进度回调三段式**（闸门 → 采样 → 节流推送）：

1. `t.Gate().Wait(ctx)` —— 暂停时阻塞在此
2. `setDone(done)`
3. 速度采样：距上次 ≥400ms 才更新
4. 事件节流：距上次 ≥250ms 才 emit

**`run()` 终态判定优先级**（顺序不可调换）：

```go
switch {
case errors.Is(err, ErrHandedOff):            → StatusHandedOff
case err == nil:                              → Completed
case errors.Is(err, context.Canceled):        → Cancelled
default:
    // 用户取消但 Runner 无法中断时，优先记为取消
    if t.Context().Err() != nil { → Cancelled } else { t.fail(err) }
}
```

任务 ID 格式：`up-20261002-194500-1` / `dl-20261002-194500-2`（前缀 + 时间戳 + 秒内自增序号）。

#### `internal/loginproxy/` — 交互式登录代理

**存在理由**（包注释原文）：夸克没有面向第三方应用的 OAuth 授权入口，手抄 Cookie 往往只粘到 `__pus`/`__puus`，而部分下载链接的回调校验需要网页上那一整段（含 `_UP_*`、`tfstk` 等 HttpOnly 字段，**在页面里根本看不到**）。

做法：本机起一个**只代理 `*.quark.cn` 的临时反向代理**，用户照常在浏览器登录，代理从请求头 Cookie 里拿"整段"。

**三条安全边界（都不能松）**：

1. 只监听 `127.0.0.1` 随机空闲端口（`net.Listen("tcp", "127.0.0.1:0")`）
2. 只转发白名单域名后缀（`.quark.cn`、`.alicdn.com`），其余一律 **403**，避免沦为开放代理
3. 只在登录期间存活：完成、取消或超时后立即关闭

> 放行 `g.alicdn.com` 的原因：登录页的 JS bundle（1.6 MB，含取码/发码/校验全部接口地址）托管在该 CDN 上。bundle 不经代理就无法改写其内部硬编码的 `https://uop.quark.cn/...`，浏览器随后直连会被 CORS 拦掉 —— 表现就是二维码区域"网络异常"。

**`ServeHTTP` 四条分支**：

1. 已捕获 && 是文档导航 → 返回内联收尾页
2. `path == "/"` → 302 到 `/__proxy/pan.quark.cn/`
3. 无 `/__proxy/` 前缀（SPA 根绝对路径）→ 从 Referer 推断 host → 回注合并 Cookie → forward
4. 有前缀 → 校验白名单 → absorb → forward

**四处实测调优记录（改动时务必先读代码注释）**：

| 机制 | 原因 |
| --- | --- |
| `isDocumentNavigation` 用 `Sec-Fetch-Dest` 区分文档与子资源 | 若把子资源也换成收尾页 HTML，浏览器会把 HTML 当 JS 解析 → **页面白屏且用户看不到任何提示**。这正是"扫码成功后不跳转、卡在白屏"的直接原因 |
| `gracePeriod = 30s`（曾用 8s，太紧） | 凭证捕获后夸克页面会自行跳 `/list` 并继续加载 JS/CSS/接口。8 秒表现为"文档 HTML 加载到了、脚本没跑起来"的白屏 |
| 请求体必须先读进内存 | 直接透传 `r.Body` 会让 Go 客户端改用 `Transfer-Encoding: chunked` 并丢掉 `Content-Length` —— passport/短信接口按 Content-Length 读取，收到 chunked 会读成空 body，表现为"验证码无效" |
| `Origin` 按「页面源站」而非目标主机还原 | 登录页在 `pan.quark.cn`，扫码 CAS 接口在 `uop.quark.cn`。上游按页面源站做 Origin/CSRF 校验，按目标主机还原会被拒绝 |

另有 `fetchableKeys` 白名单 + **「占位保护 → 改写 → 还原」三步走**（Go RE2 不支持负向前瞻）。顺序很关键：无差别改写会把夸克当标题文案用的图片地址也换成 `127.0.0.1` 路径，页面中部渲染出一行乱码。

**`RequireFreshLogin` 的必要性**（`Options` 注释）：用户在客户端清了凭证，但浏览器里那份夸克 Cookie 还在。代理起来后第一个请求就带旧的 `__pus/__puus`，会被立刻判定为"登录完成"，界面直接跳进主界面 —— **用户以为要重登，实际什么都没发生**。

**关键常量**：`prefix = "/__proxy"`、`defaultTimeout = 5min`、`settleDelay = 1500ms`、`maxRewriteBody = 8MB`、`maxRequestBody = 4MB`、`gracePeriod = 30s`

### 3.4 前端：`frontend/src/`

**技术栈：无框架、无构建。** 没有 `package.json`（全仓库唯一的 `package.json` 在 `frontend/wailsjs/runtime/`，是 Wails 官方 runtime 的库元数据）。`index.html` 直接 `<script type="module" src="./src/main.js">`，`main.go` 用 `//go:embed all:frontend` 整目录嵌入，配合 `AssetServer` 按静态资源伺服 —— 目录结构即 URL 结构。

| 自研替代 | 文件 | 说明 |
| --- | --- | --- |
| React / Vue | `core/dom.js` | `h()` hyperscript + `mount()` 整块替换（**无虚拟 DOM、无 diff**） |
| Redux / Zustand | `core/store.js` | 39 行，只做浅合并 + Set 订阅 |
| React Router | `core/router.js` | hash 路由（`wails://` 下 pushState 无可用基址） |
| CSS-in-JS / Tailwind | `styles/theme.css` | CSS 变量设计令牌 + 亮暗双主题 |
| 图标库 | `core/icons.js` | 内联 SVG path，`stroke: currentColor` 自动跟随主题 |

**目录结构**：

| 目录 | 文件数 | 说明 |
| --- | --- | --- |
| `core/` | 7 | `dom.js` `store.js` `router.js` `theme.js` `session.js` `format.js` `icons.js` |
| `bridge/` | 3 | `index.js`（门面）`wails.js`（真实）`mock.js`（预览假数据，668 行） |
| `components/` | 6 | `sidebar.js` `breadcrumb.js` `empty.js` `fileview.js` `modal.js` `taskrow.js` `toast.js` |
| `pages/` | 5 | `files.js`(653) `settings.js`(576) `share.js`(374) `login.js`(324) `transfer.js`(230) |
| `styles/` | 3 | `theme.css` `base.css` `layout.css` |

**登录状态四层收口**：

```
① 启动      app.js startApp() → auth.status()
② 主动退出  sidebar → confirmDialog → auth.logout() → 后端删 session.json + 广播
③ 会话失效  页面调 sessionInvalid(reason) → HandleSessionInvalid
④ 清凭证    settings.js → auth.clearCredentials()
                              ↓ 全部汇入
              app.js 的 resetSessionState() = clearSession() + 清内存态 + destroy 当前视图
```

**`reportSessionInvalid()` 的防御式设计**：后端不可达时**仍执行本地清理**并只 `console.warn` —— 不能留下痕迹。

**局部重渲染优化**：高频任务事件会打爆 store，因此 `transfer:update`/`transfer:list` 只调 `renderSidebarOnly()`（只 `replaceWith` 侧边栏，不动 `.app-content`），避免打断用户输入。

**`core/session.js` 的诚实声明**：夸克是第三方 Cookie 模式，SDK 无单点登出接口，本模块只能做到"清除本机痕迹 + 断开内存会话"。`OWNED_COOKIES`（`ctoken` `b-user-id` `__pus` `__puus` `__kps` `__kp` `__ktd` `auth`）每个都尝试 4 种 path/domain 组合覆写。

### 3.5 `frontend/wailsjs/` — 生成产物（勿手改）

文件头双语警示：`This file is automatically generated. DO NOT EDIT`。

| 文件 | 内容 |
| --- | --- |
| `go/app/App.js` | 47 个绑定函数，每个一行转发到 `window.go.app.App.*` |
| `go/app/App.d.ts` | TS 类型声明 |
| `go/models.ts` | `app.AuthState` / `config.Settings` 等结构 |
| `runtime/` | Wails 官方运行时（`EventsOn/Off`、`Clipboard`、`Window`） |

**生成命令**：`wails generate module`。**已提交进 git**（6 个文件全部跟踪），因此改了 `app/` 下方法签名后**必须重新生成并提交**，否则 CI 通过但真机调不到新方法。

### 3.6 `quark-cil/` — 上游 SDK（禁止修改）

| 路径 | 说明 |
| --- | --- |
| `sdk/` | `QuarkClient` 主体（`quark_client.go` `file.go` `share.go` `user.go` `queue.go` `task_manager.go` `types.go` `constants.go`） |
| `internal/guard/` | 黑名单与沙箱。**⚠️ 桌面端完全未接入**（见 §7） |
| `mcp/` | MCP 服务器，**仅 CLI 用，桌面端零引用** |
| `cmd/` `openclaw/` `sdk/validation/` | CLI 命令行、Skill、校验 |

**SDK 关键域名**（`sdk/constants.go`）：

```go
PAN_DOMAIN     = "https://pan.quark.cn"      // 用户信息
DRIVE_DOMAIN   = "https://drive-pc.quark.cn" // 大部分 API
DRIVE_H_DOMAIN = "https://drive-h.quark.cn"  // save_share_file
```

所有相对路径请求自动附加 `pr=ucpro&fr=pc`，并伪造完整 Chrome 142 指纹（`Sec-Ch-Ua*` / `Sec-Fetch-*` / `Origin` / `Referer`）。

**`task_manager.go` 是 CLI 的任务管理器，桌面端不使用** —— 桌面端有自己的 `internal/transfer`。这是有意的架构选择：CLI 面向批处理，桌面端面向交互。

### 3.7 构建与脚本

**`wails.json` 的关键在于三个构建命令全为空串**：

```json
{
  "name": "kuake-desktop",
  "outputfilename": "kuake-desktop",
  "frontend:install": "",
  "frontend:build": "",
  "frontend:dev": ""
}
```

零 npm/bundler 步骤。**注意**：`wails.json` **没有 `info` 段**，因此 `build/windows/info.json` 里的 `{{.Info.*}}` 占位符当前会渲染为空（见 §7）。

**`scripts/` 目录**：

| 脚本 | 平台 | 用途 |
| --- | --- | --- |
| `build.ps1` | Windows | **主构建脚本**，PowerShell 5.1+ 兼容。5 步：探测工具链 → 设隔离 `GOPATH/GOMODCACHE/GOCACHE` → 检查 `quark-cil/` → `wails generate module` → `wails build` |
| `build.bat` | Windows | 双击入口，转发到 `build.ps1` |
| `build.sh` | macOS/Linux | bash 版，支持 `-p darwin/universal`、`-c` 清理、`-v` 版本注入、`-d` 开发模式 |
| `preview.ps1` | Windows | 起静态服务器跑 mock 模式，**不需要 Go、不需要登录**即可看全部界面 |
| `smoke.mjs` | 全平台 | 可选运行时冒烟验证，用 `puppeteer-core` 驱动**系统自带 Edge/Chrome**（不下载 Chromium） |
| `make-icon.py` | 全平台 | 重新生成 `appicon.png`（纯标准库手写 PNG，无需 Pillow） |
| `add-license-header.py` | 全平台 | 为自有 Go 源码补 MIT+AGPL 声明头（幂等，不动 `quark-cil/`） |
| `check-frontend-syntax.mjs` | 全平台 | 以 **ESM 严格模式**批量解析 `frontend/` 下所有 `.js`（`node --check` 对含 import 的 `.js` 不按 ESM 解析，会漏检） |
| `verify-readme-tree.py` | 全平台 | 校验 README「目录结构」一节的路径是否都真实存在，防文档失真 |

**CI 工作流**：

| 文件 | 触发 | 内容 |
| --- | --- | --- |
| `ci.yml` | push / PR 到 `[main, master, release]` + 手动 | 2 个 job × 3 平台：`desktop`（build/vet/test/gofmt）与 `quark-cil`；汇总 job 便于分支保护只配一个必需检查。gofmt 检查**刻意跳过 Windows**（CRLF 会误报） |
| `desktop-release.yml` | push `v*` tag + 手动 | 6 平台矩阵：linux/amd64、linux/arm64、darwin/arm64、darwin/amd64(实验)、windows/amd64、windows/arm64(实验)。`needs: [version, build]` → `softprops/action-gh-release@v2`，版本号含 `-` 即预发布 |

---

## 4. 本地环境搭建与运行步骤

### 4.1 前置条件

| 依赖 | 版本 | 说明 |
| --- | --- | --- |
| Go | **1.25+** | `go.mod` 声明 `go 1.25.5` |
| Wails CLI | **v2.10.2** | `go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2` |
| WebView2 | — | Windows 11 自带；Win10 需另装 |
| webkit2gtk-4.1 | — | 仅 Linux 构建需要 |
| Node.js | 18+ | **可选**，仅用 `smoke.mjs` / 前端语法检查时需要 |
| 夸克 Cookie | — | 一份可用凭证（`__pus` + `__puus`） |

### 4.2 克隆与工具链

```bash
git clone git@github.com:tq4meishijia/quarkDesktopNext.git
cd quarkDesktopNext
```

构建脚本按**三级回落**探测工具链：

1. `PATH` 中的 `go` 与 `wails`（优先，尊重用户安装）
2. 工作区 `toolchain/goroot/go/bin` + `toolchain/gopath/bin`（自举 Go 1.25.5 + Wails CLI v2.10.2）—— 命中时**自动设隔离环境变量**，不污染全局
3. 都没有 → `-InstallWails` 自动 `go install`；Go 需自行装 1.21+

### 4.3 一键构建（推荐）

**Windows：**

```powershell
# 完整构建（含 wails generate module），输出到 build\bin\
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1

# 清理 + 注入版本号
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1 -Clean -Version 1.0.0

# 或双击
scripts\build.bat
```

**macOS / Linux：**

```bash
./scripts/build.sh                 # 构建
./scripts/build.sh -c              # 清理后构建
./scripts/build.sh -v 1.0.0        # 注入版本号
./scripts/build.sh -d              # 开发模式（热重载）
./scripts/build.sh -p darwin/universal   # 指定平台
./scripts/build.sh --install-wails      # 自动安装 Wails CLI
```

**已验证基线**（`scripts/README.md`）：`build.ps1 -Clean -Version 1.0.0` 在 Windows 11 / Go 1.25.5 / Wails v2.10.2 下全流程通过，**67.8s 产出 10.4 MB 的 `build\bin\kuake-desktop.exe`**。

### 4.4 开发模式与界面预览

```bash
# 开发模式（热重载）
wails dev                        # 或 ./scripts/build.sh -d
```

**仅预览界面（不需要 Go、不需要登录、不需要夸克账号）**：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\preview.ps1
# 默认 http://127.0.0.1:8123，-Port 可改
```

`preview.ps1` 起静态服务器跑 `bridge/mock.js` 假数据适配器，能演示全部 5 个页面与全部交互。**新增后端能力时必须同步补 `mock.js`**，否则预览模式缺功能。

### 4.5 手动构建（不用脚本）

```bash
# 1. 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2

# 2. 拉取依赖
go mod download

# 3. 生成前端绑定（★ 首次必须做一次；改了 app/ 方法签名后需重做）
wails generate module

# 4. 构建
wails build -trimpath -ldflags "-s -w" -o kuake-desktop

# 5. （可选）开发模式
wails dev
```

> **Linux 注意**：Ubuntu 24.04 只提供 `webkit2gtk-4.1`，而 Wails v2 默认按 4.0 走 pkg-config，不加 tag 会报 `Package 'webkit2gtk-4.0' ... not found`。CI 传的是 `-tags webkit2_41`，但**本地 `build.sh` 没有这个 tag**（见 §7）。

### 4.6 验证

```bash
# Go 构建 / 静态检查 / 测试
go build ./...
go vet ./...
go test ./...

# 上游模块（CI 会单独跑）
cd quark-cil && go build ./... && go vet ./... && go test ./...

# gofmt（Windows 上跳过，CRLF 会误报）
gofmt -l main.go app internal

# 前端语法（ESM 严格模式）
node --experimental-vm-modules scripts/check-frontend-syntax.mjs

# README 目录树与实际文件一致性
python scripts/verify-readme-tree.py

# 运行时冒烟（需先起 preview.ps1；驱动系统自带 Edge/Chrome）
node scripts/smoke.mjs
```

**当前基线**（本次交接核验）：`go build` / `go vet` / `go test` 在两个模块上全部通过。

```
# kuake-desktop
?   	kuake-desktop	[no test files]
ok  	kuake-desktop/app
?   	kuake-desktop/internal/config	[no test files]
ok  	kuake-desktop/internal/engine
ok  	kuake-desktop/internal/loginproxy
?   	kuake-desktop/internal/transfer	[no test files]

# quark-cil
ok  	github.com/zhangjingwei/kuake_cli/cmd
ok  	github.com/zhangjingwei/kuake_cli/cmd/validation
ok  	github.com/zhangjingwei/kuake_cli/internal/guard
ok  	github.com/zhangjingwei/kuake_cli/sdk
ok  	github.com/zhangjingwei/kuake_cli/sdk/validation
```

**冒烟测试基线**（README §9 记录，本机 Windows 11）：4 轮共 **116 项断言全过**，console 0 错误。冒烟走的是 **mock 桥接**，验证的是"界面 + 状态管理 + mock 适配层"，真实 Wails 桥接差异只在数据来源。

### 4.7 获取凭证

三种方式，优先级从高到低：

1. **交互式登录（推荐）** —— 应用登录页点"交互式登录"，自动起代理并打开浏览器，凭证自动入库
2. **环境变量** —— `KUAKE_COOKIE`（整段）或 `KUAKE_PUS` + `KUAKE_PUUS`。**不落盘**，仅当次运行有效
3. **Cookie 粘贴** —— 从浏览器 F12 → Network → 任选一条 `drive-pc.quark.cn` 请求 → 复制请求头里 Cookie 整段

凭证落在 `<配置目录>/session.json`（权限 **0600**）。配置目录：`KUAKE_DESKTOP_HOME` > `os.UserConfigDir()/kuake-desktop` > `~/.kuake-desktop`。

---

## 5. 当前开发进度

### 5.1 提交历史

```
ca17c91  fix(login): 修复交互式登录白屏与扫码后不跳转，并完善退出登录凭证清理
960563a  feat: 内建多线程下载器、外部下载器选择、自定义下载路径与登录体验优化
795c7d8  fix(ci): Linux 构建补 webkit2_41 标签，并让编译失败不再被静默吞掉
03cf86b  fix(ci): 修正发布工作流的产物收集，适配 Wails 各平台产物形态
60c47b6  feat(ci): 补回桌面端发布工作流，推送 v* 标签产出各平台可执行文件并发布 Release
c741318  fix(guard): 补齐 Windows 系统路径防护，修复 2 个失败用例
d288149  ci: 修正 Action 标注与警告
e9adc97  feat: 初始化 quarkDesktopNext 仓库（目录重构 + 开源合规化）
```

**当前状态**：`main` 分支工作区**干净**，无未提交改动。已发布版本 **v1.6.2**（`dist/` 与 `build/bin/` 可见产物）。

### 5.2 功能完成度

| 能力域 | 状态 | 备注 |
| --- | --- | --- |
| 交互式登录 | ✅ 完成 | 含白屏修复、`RequireFreshLogin`、阶段进度提示 |
| Cookie 粘贴 / 环境变量登录 | ✅ 完成 | 降级兜底路径 |
| 会话失效四路收口 | ✅ 完成 | 退出/过期/鉴权失败/清凭证 |
| 文件浏览与 CRUD | ✅ 完成 | 8 个方法全部实现 |
| 客户端 BFS 搜索 | ✅ 完成 | 三重阈值保护 |
| 上传 | ✅ 完成（单文件） | **文件夹上传未实现** |
| 下载（内建分片） | ✅ 完成 | 含续传、状态校验、进度节流 |
| 外部下载器接管 | ✅ 完成 | 8 个外部 + 内建，共 3 种 Kind |
| 暂停/继续/取消/重试 | ✅ 完成 | 单个 + 批量 |
| 下载路径自定义 | ✅ 完成 | 含保留层级、同名策略 |
| 分享（解析/转存/创建/列表/取消） | ✅ 完成 | **列表写死单页 100 条** |
| 设置页 | ✅ 完成 | 无"保存"按钮，改动即生效 |
| 亮/暗/跟随系统主题 | ✅ 完成 | |
| 任务队列持久化 | ❌ 未实现 | 应用重启后任务丢失 |
| 系统托盘常驻 | ❌ 未实现 | `StartMinimized` 是空开关 |
| 传输限速 | ❌ 未实现 | |
| 开机自启 | ❌ 未实现 | |
| 检查更新 | ❌ 未实现 | |
| 右键菜单上传 | ❌ 未实现 | |
| 多账号切换 | ❌ 未实现 | |
| 上传真中断 | ❌ 未实现 | 当前是软中断 |
| 传后哈希校验 | ❌ 未实现 | SDK 无远端哈希接口 |

### 5.3 测试覆盖

| 模块 | 状态 |
| --- | --- |
| `internal/engine` | ✅ 8 个测试：分片下载、状态续传、陈旧状态忽略、**Range 不支持时降级单连接**、闸门中断、占位符替换、注册表查找、`%VAR%` 展开 |
| `internal/loginproxy` | ✅ 5 个测试文件（1,617 行）：捕获、freshlogin、localizehtml、redirect、proxy_login_fix |
| `app` | ⚠️ 3 个测试文件（299 行）：`session_test.go`(5) `dest_test.go`(5) `auth_test.go`(1) |
| `internal/config` | ❌ 无测试 |
| `internal/transfer` | ❌ 无测试 |
| `app/files.go` `share.go` `settings.go` `transfer.go` | ❌ 无测试 |
| 前端 | ⚠️ 冒烟 116 项断言（**未接入 CI**） |

---

## 6. 待办事项

优先级按"用户可感知程度 × 改动成本"排序。

### 6.1 高优先级（用户可直接感知的功能缺口）

| # | 事项 | 位置 | 说明 |
| --- | --- | --- | --- |
| 1 | **上传文件夹** | `app/transfer.go:182` | 当前选到目录直接 `notify("warn", "暂不支持上传整个文件夹")` 后 `continue`。需递归遍历 + 保持目录结构（对应 `docs/FEATURE-IMPL-PROMPT.md` B2） |
| 2 | **下载文件夹** | `app/files.go` | 当前提示"文件夹需要打包后才能下载，暂不支持"。需先打包（zip）再下载，或递归建目录 |
| 3 | ~~**接入 `internal/guard` 前置校验**~~ | ✅ 已完成（2026-10-03）：因 Go internal 规则无法 import 上游 guard，改为在 `app/guard.go` 自有实现等价规则，已接入 `Delete`/`Move`/`Copy`/`EnqueueUploads`/`EnqueueDownloads` + 远端文件名穿越拦截。**双实现需手工同步**，见 §6.5 M1 |
| 4 | **分享列表翻页** | `app/share.go:48` | `GetShareList(pwdID, stoken, "0", 1, 100, ...)` 写死单页 100 条，超出**静默丢弃且无提示**（B1） |
| 5 | ~~**任务队列持久化**~~ | ✅ 已完成（2026-10-03）：落盘 `<配置目录>/transfer-tasks.json`，原子写 + 版本号 + 损坏容错。**恢复后不自动续传**（统一置 paused 等待手动 Resume），见 §6.5 M8 |
| 6 | ~~**会话失效自动检测**~~ | ✅ 已完成（2026-10-03）：`app/session_watch.go` 双路径 —— 后台每 5 分钟探测 `GetUserInfo` + 业务失败经 `a.respErr` 即时判定。**误判会把用户踢下线**，见 §6.5 M9 |

### 6.2 中优先级（体验与可配置性）

| # | 事项 | 说明 |
| --- | --- | --- |
| 7 | **系统托盘 + 启动最小化** | `Settings.StartMinimized` **只有字段与默认值**（`app.js` / `mock.js` / `settings.js` 的默认值里各有一处，**无任何 UI 控件**），`main.go` 无 `SystemTray` / `OnBeforeClose`，Go 侧零消费点 —— 是**完全的空开关**。需实现 UI + 托盘（C2） |
| 8 | **自定义分享提取码** | SDK 有 `SetSharePassword`，但 `CreateShareLink` 只接受 `needPasscode bool`，无法设置具体密码（B3） |
| 9 | **搜索阈值可配** | `searchMaxDepth=5` / `searchMaxNodes=3000` / `searchMaxHits=300` 三常量硬编码（B7） |
| 10 | **任务显示上传策略与秒传标识** | `TaskDTO` 无 `Policy` 字段、无比传标识，任务列表无法区分（B8 / A3） |
| 11 | **上传完整性校验** | SDK 已算 MD5/SHA1 但只用于秒传判定，落盘后无存在性/大小核对（B4） |
| 12 | **传输限速** | 纯自研（C3） |
| 13 | **右键菜单上传** | 需平台相关实现（C5） |

### 6.3 低优先级（工程基建）

| # | 事项 | 说明 |
| --- | --- | --- |
| 14 | **`main.go` 补 `var version`** | `build.ps1 -Version` 的 `-ldflags "-X main.version=..."` **仅当 `main.go` 声明了 `var version` 才生效**，当前 48 行的 `main.go` 没有 → 本地构建目前**静默跳过版本注入**，exe 不带版本号 |
| 15 | **`wails.json` 补 `info` 段** | 当前无 `info`，`build/windows/info.json` 的 `{{.Info.ProductVersion}}` / `{{.Info.CompanyName}}` / `{{.Info.Copyright}}` **全部渲染为空**，exe 属性无产品名/版本/版权 |
| 16 | ~~**前端检查接入 CI**~~ | ✅ 已完成（2026-10-03）：`ci.yml` 新增 `frontend` job（ESM 语法 + README 树 + 运行时冒烟）。**注意** `check-frontend-syntax.mjs` 需 `--experimental-vm-modules`，已内置可用性探测 |
| 17 | **给 `build.sh` 加 `webkit2_41`** | 该 tag 目前**只在 `desktop-release.yml` 传**，本地在 Ubuntu 24.04 构建会失败 |
| 18 | ~~**补 `internal/transfer` 与 `app/files.go` 测试**~~ | ✅ 已完成（2026-10-03）：`internal/transfer` 24 个用例（并发/暂停/持久化）、`internal/config` 13 个、`app` 37 个，全项目 130 个 |

### 6.5 后续维护必读（本轮修复引入的约束）

以下几条**不是待办，而是已实现方案的约束**。改动相关代码前必读，否则容易改回旧问题或做出错误假设。

| # | 约束 | 原因与后果 |
| --- | --- | --- |
| **M1** | **`app/guard.go` 与 `quark-cil/internal/guard` 是两份独立实现** | Go **禁止**跨 module import `internal/` 包（实测报 `use of internal package ... not allowed`），所以桌面端无法复用上游 guard，只能自行实现等价规则。**上游 guard 新增规则时桌面端不会自动同步**，需手工比对两边清单 |
| **M2** | **不要用「channel 容量」表达并发上限** | 旧实现用 `m.sem = make(chan, n)`，acquire/release 跨窗口时必然死锁。现为 `slotGate`（锁保护整数 + broadcast channel）。改回 channel 方案会重新引入 P0 缺陷 |
| **M3** | **暂停必须让出并发槽位** | Runner 遇暂停返回 `transfer.ErrPaused`（不是失败），`run()` 落成 paused 态并让槽。若把 `ErrPaused` 当失败处理，任务会丢续传状态 |
| **M4** | **`Gate.Wait` 暂停时返回 `ErrPaused` 而非 nil** | 调用方靠这个区分「用户暂停」与「真失败」。注意 http 层会把它包成 `%v`，故 `app/transfer.go` 有 `normalizeEngineError` 做文本兜底 |
| **M5** | **下载直链的 SSRF 校验是 fail-closed** | `app/urlguard.go` 对 DNS 解析失败**直接拒绝**（重试一次后放弃）。这是安全策略，不要改成 fail-open。若确实影响离线环境，须先与用户确认 |
| **M6** | **内建与外部下载器的 Cookie 不同** | 内建走 `cookieHeader`（完整凭证），外部走 `downloadCookieHeader`（白名单，剔除 `__pus` 账号登录态）。**不可统一成白名单**，否则外部下载器大量失效 |
| **M7** | **Windows 上 `session.json` 有真实 DACL** | `0600` 在 NTFS 上不表达 ACL，故 `secure_windows.go` 额外设 `PROTECTED_DACL`。该文件用 `golang.org/x/sys/windows`（本就在 go.mod 依赖图中，版本 v0.30.0，**不得升级大版本**） |
| **M8** | **任务持久化文件** | `<配置目录>/transfer-tasks.json`。恢复后**不自动续传**，统一置 paused 等待用户手动 Resume（避免重启即发起大量网络请求）。上传任务若本地源文件已消失会被跳过 |
| **M9** | **会话失效现在有 Go 侧自动检测** | `app/session_watch.go` 每 5 分钟探测 + 业务失败即时判定（经 `a.respErr`）。**判定必须严格**：`TestAuthRejectedFalsePositives` 覆盖了「网络错误/业务错误不得误判为失效」，改动该逻辑务必跑这几个用例 |
| **M10** | **不要在 SDK 侧加导出** | `session_watch.go` 用公开 API `GetUserInfo` 实现，因 SDK 的 `checkAuth` 是未导出函数。若在 SDK 加导出会带来 AGPL 合并冲突 + §13 披露义务 |

### 6.4 明确不做（丁类，已在 `docs/FEATURE-IMPL-PROMPT.md` 中确认）

回收站与恢复、文件历史版本、缩略图、在线预览、**服务端搜索**（只能客户端 BFS，属既定设计）、离线下载/磁力、目录双向同步。

---

## 7. 已知问题及注意事项

### 7.1 安全注意事项（最高优先级）

| # | 事项 | 说明 |
| --- | --- | --- |
| 1 | **AGPL 许可边界不可越线** | 根目录 MIT，但 `go.mod` 的 `replace` 指向 `./quark-cil`（AGPL-3.0），**整体分发产物必须按 AGPL-3.0**。改 `quark-cil/` 任何文件都要按第 13 条在文件顶部 + `NOTICE` 双重标注 |
| 2 | **guard 已在桌面端侧实现** | ✅ 上游 `quark-cil/internal/guard` 因 Go internal 规则**无法**被桌面端 import，故在 `app/guard.go` 自有实现等价规则并接入 Delete/Move/Copy/Enqueue*。两份实现需手工同步，见 §6.5 M1 |
| 3 | **登录代理的三条边界不能松** | 仅监听 `127.0.0.1` 随机端口 / 仅白名单域名（否则 403）/ 仅登录期存活。任何放宽都会让用户机器变成开放代理 |
| 4 | **凭证文件权限** | `session.json` 在 POSIX 上为 0600；**Windows 上额外设 DACL**（`0600` 在 NTFS 不表达 ACL，单独用无效）。刻意暴露路径让用户可见可自行删除。**修改时勿改成 0644**，见 §6.5 M7 |
| 5 | **`.gitignore` 已覆盖凭证** | `.env` / `.mcp.json` / `*.key` / `*.pem` / `id_rsa*` 等，标注"严禁提交"。`quark-cil/` **必须**纳入版本控制（随仓库分发的上游源码），故未忽略 |
| 6 | **夸克 Cookie 属敏感凭证** | 交互式登录捕获的是**完整** Cookie（含 HttpOnly 字段），等价于账号登录态。`NOTICE` 第五节有免责声明 |

### 7.2 架构约束（改动前必读）

| # | 约束 | 后果 |
| --- | --- | --- |
| 7 | **前端零构建不可破** | 无 `package.json`、无 bundler，`wails.json` 三个构建命令全空。改前端只需编辑 `frontend/src/` 下 `.js`。**引入 npm 依赖或构建步骤会破坏整个零构建假设** |
| 8 | **不要动 `quark-cil/`** | 除 AGPL 标注义务外，改动会与上游产生合并冲突。若确需补安全能力，走 §7.1 第 1 条流程 |
| 9 | **不要引入新第三方依赖** | 桌面端自有代码目前零第三方依赖（纯标准库）。新增前先确认标准库能否实现 |
| 10 | **`transfer` 与 `engine` 不得依赖网盘 API** | 这是它们可独立测试的前提。一旦引入 SDK 依赖，解耦失效 |
| 11 | **不要把 `Upload`/`Download` 挪到导出的 `App` 上** | Wails 会生成前端绑定，参数含 `*transfer.Task`，前端根本无法构造。这是 `sdkRunner` 刻意未导出的原因 |
| 12 | **`frontend/wailsjs/` 已入库，改签名必须重新生成** | 否则 CI 通过但真机调不到新方法 |
| 13 | **新增后端能力要同步补 `mock.js`** | 否则预览模式与冒烟测试缺该功能 |
| 14 | **新增登录态出口要调 `resetSessionState()`** | 登录态有 4 个出口，全部收口到 `app.js`。不要另写清理逻辑 |

### 7.3 已知功能限制（README §10 + 代码确认）

| # | 限制 | 说明 |
| --- | --- | --- |
| 15 | **上传取消是"软中断"** | SDK `UploadFile` 的进度回调无中断钩子，取消在**下一个进度回调边界**才生效。内建下载器直接消费 context + 暂停闸头，**取消立即生效**。两者交互体验不一致 |
| 16 | **暂停是"背压式"** | 阻塞进度回调，不是断连重连。外部下载器由它自己实现 |
| 17 | **搜索是客户端 BFS** | SDK 无服务端搜索接口。受深度 5 / 节点 3000 / 命中 300 三重保护，超限会置 `ReachedCap` 提示"结果可能被截断"。大网盘下性能与覆盖面受限（**属既定设计**） |
| 18 | **不支持上传/下载文件夹** | 两条 `暂不支持` 提示，是最直观的用户可感知缺口 |
| 19 | **外部下载器进度是观察值** | 命令行下载器无统一进度接口，只能轮询目标文件增长（500ms）。GUI 下载器属"唤起接管"，**完全不跟踪**，直链同时写入剪贴板 |
| 20 | **IDM 一类 GUI 下载器可能失败** | 夸克直链需 Cookie 头，IDM 命令行无法携带自定义请求头。`Descriptor.Note` 已明确告知用户 |
| 21 | **分片下载依赖服务端支持 Range** | 先发 `Range: bytes=0-0` 探测，返回 200 则降级单连接（并丢弃已读数据避免拼接错误） |
| 22 | **断点续传仅在同一任务生命周期内** | 要求同 URL / 同分片数 / 同文件大小。改过下载器参数或重启**不恢复** |
| 23 | **上传任务不支持文件夹** | 见 §7.3 第 18 项 |
| 24 | **`Settings.StartMinimized` 是空开关** | 有字段、有前端 UI、无 Go 消费点。见 §6.2 第 7 项 |
| 25 | **夸克无单点登出接口** | `core/session.js` 只能做到"清除本机痕迹 + 断开内存会话"，无法让服务端 Cookie 失效 |
| 26 | **`app` 包全局单锁 `a.mu`** | 保护 4 类不同生命周期的状态（client/settings/profile/loginSession）。锁粒度较粗但临界区极短（只做指针交换），无锁内 I/O。**若引入多账号（C7）需重新设计状态隔离** |
| 27 | **`a.ctx` 读取未加锁** | `emitAuth` 等处无锁读 `a.ctx`，属轻微竞态。因 Wails 生命周期先于事件触发而实际安全 |
| 28 | **`UpdateSettings` 是全量覆盖语义** | 不是字段级 merge。前端若传缺字段会被 `sanitize()` 填默认值 |

### 7.4 代码结构风险（改动时重点回归）

| # | 风险点 | 说明 |
| --- | --- | --- |
| 29 | **`internal/loginproxy/proxy.go` 932 行单文件** | ⚠️ 承担了 HTTP 代理 + HTML 改写 + Cookie 捕获 + 会话管理**四重职责**，是最复杂也最容易出回归的文件。尤其 `localizeHTML` 的「占位保护 → 改写 → 还原」三步逻辑，顺序不可调换 |
| 30 | **零测试覆盖的高改动风险区** | `app/files.go`(360) `app/share.go`(231) `app/settings.go`(169) `internal/transfer/`(694) 均无单元测试 |
| 31 | **`Settings` 是全量覆盖** | 见 §7.3 第 28 项 |

### 7.5 环境相关坑

| # | 坑 | 说明 |
| --- | --- | --- |
| 32 | **网络代理导致 `go build` 失败** | 若 `GOPROXY` 指向 `proxy.golang.org` 且网络不通，会报 `Bad Gateway`。解决：使用工作区 `toolchain/` 的隔离环境（`build.ps1` 自动处理），或 `GOPROXY=off` + 暖模块缓存 |
| 33 | **Linux 缺 `webkit2_41` tag** | 见 §6.3 第 17 项 |
| 34 | **图标有两处** | 根目录 `appicon.png`（构建脚本检查、`make-icon.py` 生成）与 `build/appicon.png`（真正嵌入 exe）。**换图标要同时动**；且 `build/` 被 gitignore |
| 35 | **`build/` 整个被 gitignore** | 故 `build/appicon.png`、`build/windows/info.json` **均未入库**，CI 重新生成。本地首次构建需 Wails 自动生成 |
| 36 | **gofmt 检查跳过 Windows** | CRLF 会让 gofmt 误报。CI 中 `if: matrix.os != 'windows-latest'` |
| 37 | **`docs/FEATURE-IMPL-PROMPT.md` 未入库** | ⚠️ 被 `.gitignore` 第 36 行显式忽略，`git ls-files docs/` 返回空。**它是仓库里唯一的未来功能规划文档**（甲/乙/丙/丁四类分级 + 路径穿越防护 + 统一错误码 + 前端契约约定，约 46 KB）。接手前需另找途径拿到，否则会丢失全部待办规划 |

### 7.6 交接必读的 8 条速查

1. **前端零构建** —— 改前端只编辑 `.js`，不要引入 npm 或构建步骤
2. **`wailsjs/` 是生成产物但已入库** —— 改 Go 方法签名后必须 `wails generate module` 并提交
3. **登录态 4 个出口全部收口到 `resetSessionState()`** —— 新增入口调它，别另写
4. **许可边界不可越线** —— 根目录 MIT，但 `replace` 指向 AGPL 上游，整体分发产物按 AGPL-3.0
5. **`webkit2_41` tag 只在 CI 传** —— 本地 `build.sh` 没有，Ubuntu 24.04 构建需手工补
6. **CI 不做前端校验** —— 语法检查与冒烟都是本地工具
7. **图标有两处** —— 根目录与 `build/` 各一份
8. **`docs/FEATURE-IMPL-PROMPT.md` 未入库** —— 唯一的未来功能规划，需另找途径获取

---

## 附：核心数据流速查

**启动自动登录**：
```
Wails OnStartup → sdk.ResolveEnvCookieString()（KUAKE_COOKIE）
  → 成功? connect(s, "env") 并 return
  → 否则读 session.json → connect(cookie, "saved")
      → sdk.NormalizeQuarkCookieInput()（补 __pus= 前缀、补尾分号）
      → newClient(cookie)【recover 把 SDK panic 转 error】
      → qc.GetUserInfo() → https://drive-pc.quark.cn/account/info
      → source != "env" 才落盘 session.json（0600）
      → emitAuth() ──► EventsEmit("auth:changed", AuthState)
OnDomReady: emitAuth() + emitTaskList()（避免首屏空白）
```

**交互式登录**：
```
前端 → StartInteractiveLogin()
  → 收掉上一轮 session（防端口泄漏）
  → loginproxy.StartWith(5min, {RequireFreshLogin})
  → BrowserOpenURL("http://127.0.0.1:P/__proxy/pan.quark.cn")
  → go awaitInteractiveLogin()  【立即返回，不阻塞前端】
  ── 浏览器侧：用户照常登录，密码不经过本进程 ──
  → ServeHTTP → 域名白名单校验 → forward() → absorb() 捕获 Cookie
  → settleDelay 1.5s → finish(Result{Cookie})
  ── 回到 Go 侧 ──
  → session.Wait() 返回 → PhaseCapturing → connect(cookie, "interactive")
  → PhaseSuccess + notify() ──► EventsEmit("toast")
  → gracePeriod 30s 后代理自动退出
前端轮询 InteractiveLoginStatus() 获取阶段/进度/倒计时
```

**内建分片下载**：
```
EnqueueDownloads(items, dir, keepTree)
  → resolveDest() 路径清洗 + 同名策略
  → tm.Enqueue(Spec{Dest, Fid, Size, Engine})
  ── worker goroutine ──
  → sdkRunner.Download → qc.GetDownloadURL(Fid) → 直链
  → engine.Request{URL, Dest, Cookie, Referer, UA, Segments, Progress, Gate}
  → Native.Fetch: probe → single/split
      每个 256KB 读循环：r.Gate() 检查暂停 → out.WriteAt → 汇报
      → Manager.onProgress: Gate.Wait → setDone → 采样(400ms) → 节流 emit(250ms)
  → EventsEmit("transfer:update", TaskDTO)
```

---

*文档生成时间：2026-10-03　|　基线：`ca17c91`　|　核验：build / vet / test 双模块通过*

# 夸克网盘桌面版（kuake-desktop）

> **版权与许可**
> Copyright (c) 2026 tq4meishijia
> 本仓库根目录的桌面端源码采用 **MIT** 许可证（见 [LICENSE](LICENSE)）；
> 子目录 `quark-cil/` 为上游项目 [kuake_cli](https://github.com/zhangjingwei/kuake_cli)
> 的原样副本，采用 **AGPL-3.0**（见 [quark-cil/LICENSE](quark-cil/LICENSE)）。
> 因本工程编译期链接 AGPL-3.0 代码，**将二者一同编译并对外分发的整体产物须遵循
> AGPL-3.0**。完整的版权归属与第三方依赖许可证清单见 [NOTICE](NOTICE)。

基于 [kuake_cli](https://github.com/zhangjingwei/kuake_cli) 二次开发的桌面端 GUI 客户端：
**沿用仓库原有的 Go 技术栈，不改动 CLI 任何核心逻辑**，只在其 SDK 之上加一层适配层与一套现代桌面界面。

> 本客户端是非官方第三方工具，与夸克网盘官方无关。使用即表示你已了解凭证可能失效、接口变更可能不可用等风险。

---

## 0. 这次改了什么

在原有 CLI 的 SDK 之上新增了桌面 GUI 客户端，并补了发布流水线：

1. **交互式登录**：原来要手抄 Cookie（而且往往只粘到 `__pus`，缺了下载回调校验需要的
   `_UP_*` / `tfstk` 等 HttpOnly 字段）。现在点一次「在浏览器中登录」，
   客户端会起一个只代理 `*.quark.cn` 的临时本地服务并调起系统浏览器，
   用户照常登录即可，凭证自动入库。粘贴 Cookie 仍保留作为兜底路径。
2. **发布工作流**：`.github/workflows/desktop-release.yml`，tag / 手动触发，
   6 个平台组合矩阵产出带校验和的可执行文件并发布 Release。

上游 CLI 的代码一行未改，GUI 全部通过 `app/` 适配层调用其 SDK。

---

## 1. kuake_cli 已具备的能力

先梳理上游 `sdk` 包实际提供什么，GUI 才能只做「适配」而不是「重写」。

| 能力域 | SDK 入口 | GUI 的落地位置 |
| --- | --- | --- |
| 凭证维持 | `sdk.NewQuarkClient(cookie)`、`NormalizeQuarkCookieInput()`、`ResolveEnvCookieString()` | 登录页：粘贴 Cookie 或从 `KUAKE_COOKIE` / `KUAKE_PUS`+`KUAKE_PUUS` 读取 |
| 用户信息 | `GetUserInfo()`（含 `use_capacity` / `total_capacity` / `member_type`） | 侧边栏容量条与账号区 |
| 目录浏览 | `List(path)`、`GetFileInfo(path)`、`CreateFolder(name, pdirFid)` | 文件浏览页（列表 / 网格 / 面包屑） |
| 文件操作 | `Move` / `Copy` / `Rename` / `Delete` | 多选操作条与行内操作 |
| 上传 | `UploadFile(local, dest, progressCb, opts)`，`UploadProgress` 含速度 / 剩余时间，支持并行分片与断点续传 | 传输任务页（复用其进度回调） |
| 下载 | `GetDownloadURL(fid)`、`DownloadFile(...)` | 传输任务页（取直链后自建循环，见「已知限制」） |
| 分享 | `GetShareInfo` → `GetShareStoken` → `GetShareList` → `SaveShareFile`，以及 `CreateShare` / `GetMyShareList` / `DeleteShare` | 分享解析与转存页 |
| 任务队列 | `TaskQueue` / `TaskManager`（`AddTask` / `CancelTask` / 回调） | 未直接复用，GUI 侧需要暂停/继续语义，改由 `internal/transfer` 实现（见下） |

---

## 2. 分层结构

```
浏览器/WebView 里的 UI 层        frontend/src/pages + components
        │  只调用 bridge()，不知道后端在哪
        ▼
适配层门面                       frontend/src/bridge/{index,wails,mock}.js
        │  wails：调用生成的绑定   mock：假数据，供静态预览
        ▼
Go 适配层（Wails Bind）          app/*.go
        │  只做 DTO 转换、会话生命周期、事件推送
        ▼
传输内核                         internal/transfer  （纯逻辑，不依赖网盘 API）
        │  通过 Runner 接口回调
        ▼
kuake_cli/sdk                    ★ 上游能力，零改动
```

`internal/transfer` 为什么不直接用 SDK 的 `TaskManager`：SDK 的任务模型是「提交即执行」，
没有暂停概念；GUI 需要暂停/继续/取消，因此这里实现了一个带闸门（Gate）与 context 取消的队列内核，
具体搬运仍通过 `Runner` 接口交回 `app` 包调用 SDK——**协议细节依然只有一份**。

---

## 3. 目录结构

```
kuake-desktop/
├── main.go                       Wails 启动入口（窗口尺寸、主题底色、Bind）
├── go.mod                        replace 指向本地 kuake_cli 源码
├── wails.json                    Wails 项目配置（前端零构建，故构建脚本留空）
├── appicon.png                   应用图标（scripts/make-icon.py 生成，可替换）
├── scripts/                      ★ 构建与开发脚本（详见 scripts/README.md）
│   ├── build.ps1                 Windows 一键构建（PowerShell 5.1+）
│   ├── build.bat                 同上的双击 / CMD 入口
│   ├── build.sh                  macOS / Linux 一键构建
│   ├── preview.ps1               mock 模式界面预览（不需要 Go）
│   ├── make-icon.py              重新生成 appicon.png（纯标准库）
│   └── smoke.mjs                 可选：浏览器运行时冒烟验证
├── app/                          ★ Go 适配层：前端唯一可见的边界
│   ├── app.go                    生命周期、会话建立、事件推送、通用工具
│   ├── dto.go                    前后端传输对象（AuthState / FileItem / TaskDTO / Share*）
│   ├── auth.go                   登录、环境变量登录、登出、用户资料
│   ├── files.go                  目录浏览、搜索、新建、重命名、移动、复制、删除
│   ├── transfer.go               Runner 实现（上传/下载）+ 任务 API + 文件对话框
│   ├── share.go                  分享解析、转存、创建分享、我的分享
│   └── settings.go               设置读写、下载目录校验
├── internal/
│   ├── config/store.go           配置与凭证持久化（settings.json / session.json 0600）
│   └── transfer/
│       ├── task.go               任务状态机 + 暂停闸门 Gate + Runner 接口
│       └── manager.go            并发调度、速度采样、事件节流、暂停/继续/取消/重试
└── frontend/
    ├── index.html                唯一 HTML 入口
    ├── wailsjs/                  Wails 生成的绑定（wails generate module 产出，勿手改）
    └── src/
        ├── main.js               入口与启动失败兜底
        ├── app.js                外壳：登录态、hash 路由、全局事件、侧边栏
        ├── core/
        │   ├── dom.js            h() 极简 hyperscript + mount/clear
        │   ├── format.js         字节、速度、剩余时间、日期格式化
        │   ├── icons.js          内联 SVG 图标集（描边式，自动跟随主题）
        │   ├── router.js         hash 路由
        │   ├── store.js          极简状态容器（读 / 浅合并写 / 订阅）
        │   └── theme.js          亮暗主题应用与系统偏好跟随
        ├── bridge/
        │   ├── index.js          门面：自动探测 wails / mock，导出统一契约
        │   ├── wails.js          真实后端适配器
        │   └── mock.js           预览用假数据（含模拟进度推进）
        ├── components/
        │   ├── sidebar.js        导航 + 容量条 + 账号区
        │   ├── breadcrumb.js     路径面包屑
        │   ├── fileview.js       列表视图 / 网格视图 + 多选区间算法
        │   ├── taskrow.js        任务条目（进度、速度、剩余时间、暂停/取消/重试）
        │   ├── empty.js          空数据 / 加载中 / 加载失败 / 骨架屏
        │   ├── toast.js          轻提示（含重复消息合并）
        │   └── modal.js          确认框与输入框
        ├── pages/
        │   ├── login.js          登录授权页
        │   ├── files.js          文件浏览页
        │   ├── transfer.js       传输任务页
        │   ├── share.js          分享解析与转存页
        │   └── settings.js       设置页
        └── styles/
            ├── theme.css         设计令牌（圆角/间距/字号/动效）+ 亮暗双主题变量
            ├── base.css          原子组件（按钮/表单/徽标/进度条/Toast/Modal）
            └── layout.css        外壳与各页面布局 + 响应式断点
```

---

## 4. 安装与启动

### 4.1 前置条件

- **Go 1.25+**（与 `go.mod` 一致）
- **Wails CLI**：`go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2`
- Windows 需要 WebView2 运行时（Windows 11 自带）；macOS / Linux 分别需要对应 WebKitGTK
- 一份可用的夸克网盘 Cookie

### 4.2 放置位置

`go.mod` 里有一行 `replace`，指向随仓库一同分发的 kuake_cli 源码：

```
replace github.com/zhangjingwei/kuake_cli => ./quark-cil
```

上游 CLI 源码位于本仓库的 `quark-cil/` 子目录，开箱即用、无需额外 clone：

```
quarkDesktopNext/        ← 本仓库根
├── app/ internal/ frontend/ scripts/   ← 桌面端工程（主工程，位于仓库根）
├── go.mod                              ← replace 指向 ./quark-cil
└── quark-cil/           ← upstream kuake_cli 源码（未被修改）
```

也可以删掉这行，改用上游发布版本：

```bash
go get github.com/zhangjingwei/kuake_cli@latest
```

由于 `replace` 指向仓库内相对路径，本地构建、GitHub Actions CI 与 Release 流程
均使用同一份 `go.mod`，无需为不同环境切换依赖来源。

### 4.3 生成前端绑定（必须做一次）

前端调用的 `window.go.app.App.*` 由 Wails 从 Go 代码生成，改过 `app/` 下的方法签名后需要重新生成：

```bash
wails generate module      # 产物写入 frontend/wailsjs/
```

### 4.4 一键脚本（推荐）

`scripts/` 下提供了一组构建脚本，自动完成环境探测（PATH → 本地 toolchain）、
上游源码检查、绑定生成与编译打包，详见 `scripts/README.md`：

```powershell
# Windows（PowerShell 5.1+）
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1              # 构建
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Dev         # 开发热重载
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Clean -Nsis -Version 1.0.0
.\scripts\build.bat -Clean -Nsis                                          # .bat 双击/命令行入口
```

```bash
# macOS / Linux
./scripts/build.sh                # 当前平台
./scripts/build.sh -d             # 开发热重载
./scripts/build.sh -p darwin/universal -c
```

脚本行为要点：

- PATH 里没有 go/wails 时自动回落到 `toolchain/` 自举工具链，并使用隔离的
  `GOPATH/GOMODCACHE/GOCACHE`，不污染全局；
- 上游源码 `quark-cil/` 缺失时直接给出恢复命令并失败退出；
- `-Version` 通过 ldflags 注入版本号（需 main.go 声明 `var version`，未声明则跳过）；
- `-Nsis` 生成 Windows 安装包、`-Upx` 压缩产物、`-Platform` 支持交叉构建。

### 4.5 手动开发与构建

也可以不经过脚本直接使用 Wails CLI：

```bash
wails dev            # 开发模式
wails build          # 产物在 build/bin/
wails build -nsis    # 额外产出 Windows 安装包
```

### 4.6 仅预览界面（不需要 Go / 不需要登录）

前端零构建，用任意静态服务器打开 `frontend/` 即可。检测不到 Wails 绑定时会自动切到 mock 数据：

```bash
cd frontend
python -m http.server 8123
# 打开 http://127.0.0.1:8123/index.html
# Windows 也可：powershell -File .\scripts\preview.ps1
```

预览模式下右上角会显示「预览模式」角标；登录页任意填 4 个字符以上即可进入，
文件、传输、分享三页都有可用的假数据与模拟进度。

### 4.7 自动构建与发布（GitHub Actions）

工作流文件：`.github/workflows/desktop-release.yml`（在仓库根目录）

> 仓库另有一个 `.github/workflows/release.yml`，那是根目录 kuake CLI 自己的发布流程；
> 桌面客户端用独立的工作流文件，两者互不干扰，产物会上传进同一个 Release。

- **触发**：推送 `v*` 标签，或 Actions 页面手动触发（`workflow_dispatch`，可填 tag 如 `v1.2.3`，留空自动生成 `v<日期>-<短sha>`）
- **权限**：`contents: write`
- **版本号**：一律从 tag 推导（去掉 `v` 前缀），手动触发同样生效
- **矩阵**：Linux / macOS / Windows × amd64 / arm64（共 6 个组合，`fail-fast: false`；
  macOS amd64 与 Windows arm64 标记为 experimental，Runner 不支持时该组合跳过而非让工作流失败）
- **产物**：`<项目名>-<版本>-<系统>-<架构>[.exe]` + 同名 `.sha256` 校验和
- **发布**：自动创建或更新该 tag 对应的 Release 并上传全部产物；重复运行会覆盖同名产物

本地打 tag 触发发布：

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
# 之后在 Actions 页面观察进度，产物会出现在 Releases 里
```

### 4.8 获取凭证

浏览器登录 pan.quark.cn → F12 → Network → 任选一条 `drive-pc.quark.cn` 请求 → 复制请求头里的 Cookie。
也可以直接设置环境变量后用登录页的「从环境变量读取」按钮：

```bash
export KUAKE_COOKIE='__pus=xxx; __puus=yyy;'
# 或拆分形式
export KUAKE_PUS='xxx'
export KUAKE_PUUS='yyy'
```

---

## 5. 交互式登录是怎么工作的

夸克没有面向第三方应用的 OAuth 授权入口，凭证只能从已登录浏览器的 Cookie 里取。
手抄的代价不只是麻烦，更在于页面里根本看不到 HttpOnly 字段。

流程：

```
点击「在浏览器中登录」
   └─> 127.0.0.1:<随机端口>/__proxy/pan.quark.cn/      ← 临时本地代理
         └─> 转发到 https://pan.quark.cn/...                ← 只放行 *.quark.cn
               ├─ 回程改写 Set-Cookie：去掉 Domain、Path 圈到本主机前缀
               ├─ 回程改写 Location：把跳转变回本地路径
               └─ 采集 Cookie（请求头 + 上游下发，跨主机合并）
                     └─> 走既有 connect() 校验并落盘 -> 广播 auth:changed
```

几个实现上的取舍：

- **每台上游主机用独立路径前缀**：`/__proxy/<host>/...`。浏览器按 path 隔离地保存和回传
  Cookie，多个域的凭证才不会互相污染，也才能把它们合并成完整的一段。
- **同时从请求头和 Set-Cookie 采集**：浏览器第一次访问某台主机时请求头里还没有
  Cookie，只看请求头会漏掉刚下发的那批；只看 Set-Cookie 又会漏掉浏览器里已有的旧凭证。
- **删掉 CSP 与 HSTS**：CSP 会把页面资源重新指向真实域名，那样流量就不再经过代理了。
- **安全边界**：只绑 `127.0.0.1`、只有 `*.quark.cn` 在白名单（其余一律 403，
  避免变成开放代理）、5 分钟超时兜底、结束时立刻关服务。

核心逻辑有单元测试覆盖（`internal/loginproxy/proxy_test.go`，
`go test ./internal/loginproxy/`）：地址改写、Cookie 改写、跨主机合并、
延迟收敛、超时与取消。

---

## 6. 前后端契约

### 6.1 前端 → 后端（Wails 绑定方法，共 39 个）

| 分组 | 方法 |
| --- | --- |
| 登录 | `AuthStatus` `Login` `LoginFromEnv` `Logout` `GetProfile` |
| 文件 | `ListDir` `Search` `CreateFolder` `Rename` `Delete` `Move` `Copy` `ResolveFid` |
| 传输 | `PickUploadFiles` `PickDownloadDir` `EnqueueUploads` `EnqueueDownloads` `ListTasks` `PauseTask` `ResumeTask` `CancelTask` `RetryTask` `PauseAllTasks` `ResumeAllTasks` `ClearCompletedTasks` `RevealLocal` |
| 分享 | `ParseShare` `SaveShare` `CreateShareLink` `ListMyShares` `DeleteShare` |
| 设置 | `GetSettings` `SaveSettings` `ConfigDir` `EnsureDownloadDir` |

> `Upload` / `Download` 两个 Runner 方法刻意挂在未导出类型 `sdkRunner` 上，
> 否则会被 Wails 一起生成到前端（参数含 `*transfer.Task`，前端根本无法构造）。

### 6.2 后端 → 前端（事件）

| 事件 | 载荷 | 用途 |
| --- | --- | --- |
| `transfer:update` | 单个 `TaskDTO` | 进度增量更新（节流 250ms） |
| `transfer:list` | `TaskDTO[]` | 全量快照 |
| `auth:changed` | `AuthState` | 登录 / 登出 |
| `toast` | `{level, text}` | 后端主动提示（如上传单文件失败） |

---

## 7. 界面说明

| 页面 | 覆盖的要点 |
| --- | --- |
| 登录授权 | Cookie 粘贴、环境变量登录、四步取 Cookie 指引、凭证存储说明、内联报错 |
| 文件浏览 | 列表 / 网格双视图、面包屑、搜索（可切「含子目录」）、多选（点击 / Ctrl / Shift 区间）、批量下载/重命名/移动/复制/删除、行内操作、三列排序、骨架屏 / 空态 / 错误态 |
| 传输任务 | 进行中-已完成-总速度汇总、全部暂停 / 继续 / 清除已结束、筛选（全部/进行中/已完成/失败）、单任务暂停/继续/取消/重试、实时进度与速度与剩余时间 |
| 分享转存 | 链接解析 → 条目勾选 → 目标目录 → 转存所选 / 整包转存；另有「我的分享」列表 |
| 设置 | 下载路径（含系统目录选择）、并发数 1–16、亮/暗/跟随系统主题、上传同名策略、配置目录展示、恢复默认 |

设计约束：圆角只有 8/12/18 三档，间距 4px 基准，颜色全部走 CSS 变量；
断点 1080px 侧边栏收成图标栏，760px 隐藏侧边栏并简化列表列，480px 网格降列宽。

---

## 8. 验证记录

交付前已完成以下自动化验证（本机，Windows 11 / Go 1.25.5 / Wails CLI v2.10.2）：

| 验证项 | 命令 / 方式 | 结果 |
| --- | --- | --- |
| Go 编译 | `go build ./...` | 通过 |
| Go 静态检查 | `go vet ./...` | 通过 |
| 前端语法 | 23 个 JS 文件逐个 `node --check` | 0 失败 |
| 模块解析 | 自写脚本校验全部相对 import | `imports=65 missing=0` |
| 运行时冒烟① | 系统自带 Edge（无头）驱动完整链路：登录 → 5 个页面路由 → 列表/网格/搜索（当前目录与含子目录）/多选 → 传输页筛选与批量操作 → 分享解析 → 设置页主题切换（深色/浅色/跟随系统生效）→ 720/460px 窄窗口 → 退出登录 | 43 项断言全过，console 0 错误 |
| 运行时冒烟② | 任务行暂停/继续/取消/重试、任务筛选、全部暂停/继续/清除已结束；文件页新建文件夹、重命名、删除确认、无结果空态 | 34 项断言全过，console 0 错误 |
| 资源加载 | 静态预览服务器访问日志 | 全部 200（favicon 已内联，无 404） |
| **真实构建** | `.\scripts\build.ps1 -Clean -Version 1.0.0` | 通过，67.8s 产出 `build\bin\kuake-desktop.exe`（10.4 MB，含应用图标） |

> 冒烟走的是 mock 桥接（浏览器里没有 `window.go`，`initBridge` 自动回落 mock 模式），
> 因此它验证的是「界面 + 状态管理 + mock 适配层」整条链路；真实 Wails 桥接走同一套门面接口，
> 差异只在数据来源。运行 `wails dev` 即可在真机上用同一界面连真实后端。

---

## 9. 已知限制

1. **上传取消是软中断**：`sdk.UploadFile` 的进度回调没有中断钩子，取消进行中的上传会在下一个进度回调边界生效；
   等待中的任务可以立即移除。下载走自建 HTTP 循环，取消立即生效。
2. **暂停是背压式**：暂停通过阻塞进度回调实现（不再向下游要数据），不是断开连接重连。
3. **搜索是客户端遍历**：SDK 没有服务端搜索接口，`Search` 会按目录广度优先下钻，
   受深度 5 / 节点 3000 / 命中 300 三重保护，触及上限时界面会提示「结果可能被截断」。
4. **不支持上传整个文件夹**：选到目录会跳过并提示，与 CLI 的单文件上传语义一致。
5. **下载不续传**：断点续传目前只在上传路径上由 SDK 提供。

---

## 10. 许可证

本仓库采用**分层许可证声明**：

| 范围 | 许可证 | 说明 |
| --- | --- | --- |
| 仓库根目录（桌面端自有代码：`main.go`、`app/`、`internal/`、`frontend/`、`scripts/`） | **MIT** | 见 [LICENSE](LICENSE)，Copyright (c) 2026 tq4meishijia |
| `quark-cil/`（上游 [kuake_cli](https://github.com/zhangjingwei/kuake_cli) 源码，原样引入、未作修改） | **AGPL-3.0** | 见 [quark-cil/LICENSE](quark-cil/LICENSE)，版权归上游作者所有 |

**兼容性说明（重要）**：MIT 属宽松许可证，单向兼容于 AGPL-3.0——MIT 代码可被并入
AGPL 作品，但**不能**将包含 AGPL-3.0 代码的衍生作品整体以 MIT 授权分发。因此：

- 单独使用或引用本仓库根目录的桌面端源码时，适用 MIT；
- 将根目录源码与 `quark-cil/` 一同编译并对外分发（含以网络服务形式提供）时，
  该整体产物须遵循 **AGPL-3.0**，包括向接收者提供完整对应源码、保留许可证全文，
  并显著标注对上游代码的修改。

第三方依赖的许可证清单与兼容性分析见 [NOTICE](NOTICE)：除 `quark-cil/`（AGPL-3.0）
外，其余依赖均为 MIT / BSD-2-Clause / BSD-3-Clause / ISC / Apache-2.0，与本工程兼容。

上游项目另声明商业使用需另行取得授权，相关免责声明见
`quark-cil/docs/DISCLAIMER.md`。本项目为第三方非官方实现，与夸克网盘官方无关。

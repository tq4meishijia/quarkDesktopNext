# scripts —— 构建与开发脚本

| 脚本 | 平台 | 用途 |
| --- | --- | --- |
| `build.ps1` / `build.bat` | Windows | 一键构建（环境探测 → 绑定生成 → wails build → 产物清单） |
| `build.sh` | macOS / Linux | 同上，bash 版 |
| `preview.ps1` | Windows | 仅预览界面（mock 数据，不需要 Go 与登录） |
| `make-icon.py` | 全平台 | 重新生成 `appicon.png`（纯标准库） |
| `smoke.mjs` | 全平台 | 可选：浏览器运行时冒烟验证 |

## 常用命令

```powershell
# Windows：常规构建（windows/amd64）
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1

# 清理构建 + NSIS 安装包 + UPX 压缩
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Clean -Nsis -Upx

# 交叉构建其它平台
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Platform windows/arm64

# 开发模式（热重载）
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Dev

# 只看界面（mock 预览）
powershell -ExecutionPolicy Bypass -File .\scripts\preview.ps1
```

```bash
# macOS / Linux
./scripts/build.sh                        # 当前平台
./scripts/build.sh -p darwin/universal -c # 通用二进制 + 清理构建
./scripts/build.sh -d                     # 开发模式
```

双击 `build.bat` 等价于不带参数运行 `build.ps1`；也可以把参数直接跟在后面：
`build.bat -Clean -Version 1.0.0`。

> 已验证：`build.ps1 -Clean -Version 1.0.0` 在 Windows 11 / Go 1.25.5 / Wails CLI v2.10.2
> 下全流程通过，67.8s 产出 `build\bin\kuake-desktop.exe`（10.4 MB）。

## 环境要求与自动探测

构建脚本按以下顺序解析工具链，全部命中即开始构建：

1. `PATH` 中的 `go` 与 `wails`（优先，尊重用户自己的安装）；
2. 工作区根目录的 `toolchain/goroot/go/bin` 与 `toolchain/gopath/bin`（本项目自举的
   Go 1.25.5 与 Wails CLI v2.10.2）——命中时自动设置隔离的
   `GOPATH / GOMODCACHE / GOCACHE`，不污染全局；
3. 都没有：`build.ps1 -InstallWails` 可自动 `go install` Wails CLI；Go 需自行安装 1.21+。

同时会检查：

- 上游 CLI 源码 `quark-cil/`（go.mod 的 replace 指向它，随仓库分发，缺失时脚本会给出恢复命令）；
- 根目录 `appicon.png`（缺失仅警告，产物会用 Wails 默认图标；可用 `python scripts/make-icon.py` 生成）。

## 版本号注入

`-Version 1.0.0` 会尝试通过 `-ldflags "-X main.version=1.0.0"` 写进二进制。
仅当 `main.go` 中声明了 `var version` 变量时生效，否则脚本提示后跳过、不影响构建。

## 冒烟验证（可选）

```bash
# 1) 起预览服务器（mock 模式）
powershell -File scripts\preview.ps1

# 2) 任意目录装一次 puppeteer-core，然后：
NODE_PATH=<目录>\node_modules node scripts\smoke.mjs
# 浏览器自动探测系统 Edge/Chrome，也可 KUAKE_SMOKE_BROWSER=<路径> 指定
```

输出逐条 PASS/FAIL 与 console 错误汇总，退出码 0/1，可接入 CI。

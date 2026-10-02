<#
.SYNOPSIS
    kuake-desktop 一键构建脚本（Windows，兼容 PowerShell 5.1+）。

.DESCRIPTION
    负责构建前的全部环境工作，然后调用 Wails 产出可执行文件：
      1. 探测 Go / Wails CLI（PATH 里没有时自动回落到工作区 toolchain 自举工具链）；
      2. 为本地工具链设置隔离的 GOPATH / GOMODCACHE / GOCACHE（不污染全局）；
      3. 检查上游 CLI 源码 quark-cil（随仓库分发）是否就位；
      4. 生成前端绑定（frontend/wailsjs）；
      5. wails build 编译打包，输出到 build\bin\。

.EXAMPLE
    # 常规构建（windows/amd64）
    powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1

    # 清理后构建 + 生成 NSIS 安装包 + 注入版本号
    powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Clean -Nsis -Version 1.0.0

    # 开发模式（热重载，不产出安装包）
    powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Dev

.NOTES
    版本号注入：若 main.go 里声明了 `var version = "dev"`，-Version 会通过
    ldflags 的 -X main.version 写进二进制；未声明则自动跳过，不会构建失败。
#>
[CmdletBinding()]
param(
    # 目标平台，逗号分隔，如 windows/amd64 或 windows/amd64,windows/arm64
    [string]$Platform = "windows/amd64",
    # 版本号，写进二进制（需要 main.go 声明 version 变量）
    [string]$Version = "",
    # 覆盖输出文件名（不含扩展名），默认取 wails.json 的 outputfilename
    [string]$Out = "",
    # WebView2 运行时策略：download=带下载器 / embed=内嵌安装器 / browser=依赖系统已装 / error=强制报错
    [ValidateSet("download", "embed", "browser", "error")]
    [string]$WebView2 = "download",
    # 构建前清空 build\bin
    [switch]$Clean,
    # 额外生成 NSIS 安装包（需要已安装 NSIS）
    [switch]$Nsis,
    # 用 UPX 压缩产物（需要已安装 UPX）
    [switch]$Upx,
    # 构建 debug 版（保留 DevTools 与控制台；不能叫 -Debug，那是 PowerShell 公共参数）
    [switch]$DebugBuild,
    # 跳过 wails generate module（前端绑定没变动时可以省时间）
    [switch]$SkipModule,
    # PATH 里没有 wails 时自动 go install（v2.10.2，与项目依赖一致）
    [switch]$InstallWails,
    # 开发模式：wails dev 热重载，构建参数被忽略
    [switch]$Dev
)

$ErrorActionPreference = "Stop"
$stopwatch = [System.Diagnostics.Stopwatch]::StartNew()

function Write-Step([string]$msg) { Write-Host ""; Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-Info([string]$msg) { Write-Host "    $msg" -ForegroundColor DarkGray }
function Fail([string]$msg) {
    Write-Host ""
    Write-Host "[FAIL] $msg" -ForegroundColor Red
    exit 1
}

# ---------- 定位目录 ----------
$Root = Split-Path -Parent $PSScriptRoot
$Workspace = Split-Path -Parent $Root          # 仓库的上级目录（仅用于回落查找自举工具链）
# 上游 CLI 源码现随仓库一同分发，位于 <仓库根>\quark-cil（go.mod 的 replace 指向它）
$RepoKuake = Join-Path $Root "quark-cil"
# 自举工具链：优先仓库内 toolchain\，其次工作区同级 toolchain\
$Toolchain = Join-Path $Root "toolchain"
if (-not (Test-Path (Join-Path $Toolchain "goroot\go\bin"))) {
    $Toolchain = Join-Path $Workspace "toolchain"
}
$GorootBin = Join-Path $Toolchain "goroot\go\bin"
$GopathBin = Join-Path $Toolchain "gopath\bin"

Push-Location $Root
try {
    Write-Step "项目目录：$Root"

    # ---------- 1. 上游 CLI 源码检查 ----------
    if (-not (Test-Path (Join-Path $RepoKuake "go.mod"))) {
        Fail ("未找到上游 CLI 源码：{0}`n     该目录随本仓库一同分发（quark-cil\）。若缺失，请先恢复工作区：`n     git checkout -- quark-cil   或   git clone --depth 1 https://github.com/zhangjingwei/kuake_cli `"{1}`"" -f $RepoKuake, $RepoKuake)
    }
    Write-Info "上游 CLI 源码就绪：$RepoKuake"

    # ---------- 2. 探测 Go ----------
    $go = Get-Command go -ErrorAction SilentlyContinue
    if (-not $go) {
        if (Test-Path (Join-Path $GorootBin "go.exe")) {
            Write-Info "PATH 中无 Go，使用本地自举工具链：$GorootBin"
            $env:PATH = "$GorootBin;$GopathBin;$env:PATH"
            # 本地工具链配套隔离缓存，避免污染全局 GOPATH
            $env:GOPATH = Join-Path $Toolchain "gopath"
            $env:GOMODCACHE = Join-Path $Toolchain "gopath\pkg\mod"
            $env:GOCACHE = Join-Path $Toolchain "gocache"
        }
        else {
            Fail "未找到 Go。请安装 Go 1.21+，或把自举工具链放到 $GorootBin"
        }
    }
    $goVersion = (& go version) -join " "
    Write-Info $goVersion

    # ---------- 3. 探测 Wails CLI ----------
    $wails = Get-Command wails -ErrorAction SilentlyContinue
    if (-not $wails) {
        if (Test-Path (Join-Path $GopathBin "wails.exe")) {
            Write-Info "使用本地 Wails CLI：$GopathBin"
            $env:PATH = "$GopathBin;$env:PATH"
            $wails = Get-Command wails
        }
        elseif ($InstallWails) {
            Write-Step "安装 Wails CLI v2.10.2"
            & go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
            if ($LASTEXITCODE -ne 0) { Fail "wails 安装失败，请检查网络后重试" }
            $goBin = (& go env GOPATH)
            $env:PATH = "$goBin\bin;$env:PATH"
            $wails = Get-Command wails -ErrorAction SilentlyContinue
            if (-not $wails) { Fail "安装完成但 PATH 中找不到 wails" }
        }
        else {
            Fail ("未找到 Wails CLI。两种解决方式：`n     a) 重跑并加 -InstallWails 由脚本自动安装；`n     b) 手动执行 go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2")
        }
    }
    Write-Info (( & $wails version ) -join " ")

    # ---------- 4. 网络代理 ----------
    if (-not $env:GOPROXY) {
        $env:GOPROXY = "https://goproxy.cn,https://proxy.golang.org,direct"
        Write-Info "GOPROXY 未设置，默认走 goproxy.cn"
    }
    if (-not $env:GOFLAGS) { $env:GOFLAGS = "-mod=mod" }

    # ---------- 5. 图标检查 ----------
    if (-not (Test-Path (Join-Path $Root "appicon.png"))) {
        Write-Warning "缺少 appicon.png，构建产物将使用 Wails 默认图标。可执行：python scripts\make-icon.py"
    }

    # ---------- 6. 开发模式分流 ----------
    if ($Dev) {
        Write-Step "进入开发模式（wails dev，Ctrl+C 退出）"
        & $wails dev
        exit $LASTEXITCODE
    }

    # ---------- 7. 生成前端绑定 ----------
    if (-not $SkipModule) {
        Write-Step "生成前端绑定（wails generate module）"
        & $wails generate module
        if ($LASTEXITCODE -ne 0) { Fail "生成绑定失败" }
    }
    else {
        Write-Info "已按 -SkipModule 跳过绑定生成"
    }

    # ---------- 8. 组装构建参数 ----------
    $ldflags = "-s -w"
    if ($Version) {
        $mainGo = Join-Path $Root "main.go"
        if ((Get-Content $mainGo -Raw) -match "var\s+version\s+") {
            $ldflags = "$ldflags -X main.version=$Version"
            Write-Info "注入版本号：$Version"
        }
        else {
            Write-Warning "main.go 未声明 version 变量，-Version $Version 未注入"
        }
    }

    $buildArgs = @("build", "-platform", $Platform, "-trimpath", "-ldflags", $ldflags, "-webview2", $WebView2)
    if ($Out) { $buildArgs += @("-o", $Out) }
    if ($Clean) { $buildArgs += "-clean" }
    if ($Nsis) { $buildArgs += "-nsis" }
    if ($Upx) { $buildArgs += "-upx" }
    if ($DebugBuild) { $buildArgs += "-debug" }

    Write-Step "构建 $Platform（wails build $($buildArgs[1..($buildArgs.Count - 1)] -join ' ')）"
    & $wails @buildArgs
    if ($LASTEXITCODE -ne 0) { Fail "wails build 失败（exit $LASTEXITCODE）" }

    # ---------- 9. 产物清单 ----------
    $stopwatch.Stop()
    $binDir = Join-Path $Root "build\bin"
    Write-Step ("构建完成，耗时 {0:N1}s，产物：" -f $stopwatch.Elapsed.TotalSeconds)
    if (Test-Path $binDir) {
        Get-ChildItem -Path $binDir -File | ForEach-Object {
            Write-Host ("    {0}  ({1:N1} MB)" -f $_.FullName, ($_.Length / 1MB)) -ForegroundColor Green
        }
    }
}
finally {
    Pop-Location
}

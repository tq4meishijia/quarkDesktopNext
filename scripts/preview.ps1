<#
.SYNOPSIS
    仅预览界面（不需要 Go、不需要登录）：起静态服务器跑 mock 模式。

.DESCRIPTION
    前端在浏览器里检测不到 window.go 时会自动回落到 mock 数据源，
    因此无需编译后端就能查看全部 5 个页面与交互。
    服务器会一直运行，按 Ctrl+C 停止。

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\scripts\preview.ps1          # 默认 http://127.0.0.1:8123
    powershell -ExecutionPolicy Bypass -File .\scripts\preview.ps1 -Port 9000
#>
[CmdletBinding()]
param(
    [int]$Port = 8123
)

$ErrorActionPreference = "Stop"
$frontend = Join-Path (Split-Path -Parent $PSScriptRoot) "frontend"

if (-not (Test-Path (Join-Path $frontend "index.html"))) {
    Write-Host "[FAIL] 未找到 $frontend\index.html" -ForegroundColor Red
    exit 1
}

# 优先使用 WorkBuddy 托管的 Python，其次回落系统 python
$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) {
    Write-Host "[FAIL] 未找到 python，请先安装" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "==> 预览模式（mock 数据）" -ForegroundColor Cyan
Write-Host "    地址：http://127.0.0.1:$Port/index.html" -ForegroundColor Green
Write-Host "    登录页随便填 4 个字符以上即可进入界面；按 Ctrl+C 停止服务器" -ForegroundColor DarkGray
Write-Host ""

& $python.Source -m http.server $Port --bind 127.0.0.1 --directory $frontend

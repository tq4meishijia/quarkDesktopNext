#!/usr/bin/env bash
#
# kuake-desktop 一键构建脚本（macOS / Linux）。
#
# 用法：
#   ./scripts/build.sh                          # 当前平台默认构建
#   ./scripts/build.sh -p darwin/universal      # 指定平台（darwin/amd64|darwin/arm64|darwin/universal|linux/amd64|linux/arm64）
#   ./scripts/build.sh -c -v 1.0.0              # 清理构建 + 注入版本号
#   ./scripts/build.sh -d                       # 开发模式（wails dev 热重载）
#   ./scripts/build.sh --install-wails          # 自动安装 Wails CLI v2.10.2
#
# 说明：
#   - PATH 中没有 go / wails 时，自动回落到工作区 toolchain/ 自举工具链；
#   - Linux 构建需要 webkit2gtk 4.1 等系统依赖（wails doctor 可自检）；
#   - 版本号注入：仅当 main.go 声明了 `var version` 变量时生效。
set -euo pipefail

STEP() { printf '\n\033[36m==> %s\033[0m\n' "$1"; }
INFO() { printf '    \033[2m%s\033[0m\n' "$1"; }
FAIL() { printf '\n\033[31m[FAIL] %s\033[0m\n' "$1" >&2; exit 1; }

# ---------- 参数解析 ----------
PLATFORM="" ; VERSION="" ; OUT="" ; CLEAN=0 ; DEBUG=0 ; SKIP_MODULE=0 ; INSTALL_WAILS=0 ; DEV=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--platform)   PLATFORM="$2"; shift 2 ;;
    -v|--version)    VERSION="$2"; shift 2 ;;
    -o|--out)        OUT="$2"; shift 2 ;;
    -c|--clean)      CLEAN=1; shift ;;
    --debug)         DEBUG=1; shift ;;
    -s|--skip-module) SKIP_MODULE=1; shift ;;
    --install-wails) INSTALL_WAILS=1; shift ;;
    -d|--dev)        DEV=1; shift ;;
    -h|--help)       grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) FAIL "未知参数：$1（-h 查看用法）" ;;
  esac
done

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE="$(dirname "$ROOT")"
# 上游 CLI 源码现随仓库一同分发，位于 <仓库根>/quark-cil（go.mod 的 replace 指向它）
REPO_KUAKE="$ROOT/quark-cil"
# 自举工具链：优先仓库内 toolchain/，其次工作区同级 toolchain/（均可选，缺失时回落到 PATH 中的 go）
if [[ -d "$ROOT/toolchain/goroot/go/bin" ]]; then
  TOOLCHAIN="$ROOT/toolchain"
else
  TOOLCHAIN="$WORKSPACE/toolchain"
fi
GOROOT_BIN="$TOOLCHAIN/goroot/go/bin"
GOPATH_BIN="$TOOLCHAIN/gopath/bin"

cd "$ROOT"
START=$(date +%s)

STEP "项目目录：$ROOT"

# ---------- 1. 上游 CLI 源码检查 ----------
if [[ ! -f "$REPO_KUAKE/go.mod" ]]; then
  FAIL "未找到上游 CLI 源码：$REPO_KUAKE
     该目录随本仓库一同分发（quark-cil/）。若缺失，请先恢复工作区：
     git checkout -- quark-cil   或   git clone --depth 1 https://github.com/zhangjingwei/kuake_cli \"$REPO_KUAKE\""
fi
INFO "上游 CLI 源码就绪：$REPO_KUAKE"

# ---------- 2. 探测 Go ----------
if ! command -v go >/dev/null 2>&1; then
  if [[ -x "$GOROOT_BIN/go" ]]; then
    INFO "PATH 中无 Go，使用本地自举工具链：$GOROOT_BIN"
    export PATH="$GOROOT_BIN:$GOPATH_BIN:$PATH"
    export GOPATH="$TOOLCHAIN/gopath"
    export GOMODCACHE="$TOOLCHAIN/gopath/pkg/mod"
    export GOCACHE="$TOOLCHAIN/gocache"
  else
    FAIL "未找到 Go。请安装 Go 1.21+，或把自举工具链放到 $GOROOT_BIN"
  fi
fi
INFO "$(go version)"

# ---------- 3. 探测 Wails CLI ----------
if ! command -v wails >/dev/null 2>&1; then
  if [[ -x "$GOPATH_BIN/wails" ]]; then
    INFO "使用本地 Wails CLI：$GOPATH_BIN"
    export PATH="$GOPATH_BIN:$PATH"
  elif [[ "$INSTALL_WAILS" == "1" ]]; then
    STEP "安装 Wails CLI v2.10.2"
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
    export PATH="$(go env GOPATH)/bin:$PATH"
  else
    FAIL "未找到 Wails CLI。两种解决方式：
     a) 重跑并加 --install-wails 由脚本自动安装；
     b) 手动执行 go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2"
  fi
fi
INFO "$(wails version 2>/dev/null | head -n1 || echo 'wails CLI 就绪')"

# ---------- 4. 平台与网络 ----------
if [[ -z "$PLATFORM" ]]; then
  case "$(uname -s)" in
    Darwin)
      if [[ "$(uname -m)" == "arm64" ]]; then PLATFORM="darwin/arm64"; else PLATFORM="darwin/amd64"; fi ;;
    Linux) PLATFORM="linux/amd64" ;;
    *) FAIL "不支持的宿主系统（Windows 请使用 scripts\\build.ps1）" ;;
  esac
fi
if [[ -z "${GOPROXY:-}" ]]; then
  export GOPROXY="https://goproxy.cn,https://proxy.golang.org,direct"
  INFO "GOPROXY 未设置，默认走 goproxy.cn"
fi
export GOFLAGS="${GOFLAGS:--mod=mod}"

# ---------- 5. 图标检查 ----------
if [[ ! -f "$ROOT/appicon.png" ]]; then
  echo "    [warn] 缺少 appicon.png，产物将使用 Wails 默认图标。可执行：python3 scripts/make-icon.py"
fi

# ---------- 6. 开发模式分流 ----------
if [[ "$DEV" == "1" ]]; then
  STEP "进入开发模式（wails dev，Ctrl+C 退出）"
  exec wails dev
fi

# ---------- 7. 生成前端绑定 ----------
if [[ "$SKIP_MODULE" != "1" ]]; then
  STEP "生成前端绑定（wails generate module）"
  wails generate module
else
  INFO "已按 --skip-module 跳过绑定生成"
fi

# ---------- 8. 组装构建参数 ----------
LDFLAGS="-s -w"
if [[ -n "$VERSION" ]]; then
  if grep -qE 'var[[:space:]]+version[[:space:]]+' main.go; then
    LDFLAGS="$LDFLAGS -X main.version=$VERSION"
    INFO "注入版本号：$VERSION"
  else
    echo "    [warn] main.go 未声明 version 变量，--version 未注入"
  fi
fi

BUILD_ARGS=(build -platform "$PLATFORM" -trimpath -ldflags "$LDFLAGS")
[[ -n "$OUT" ]] && BUILD_ARGS+=(-o "$OUT")
[[ "$CLEAN" == "1" ]] && BUILD_ARGS+=(-clean)
[[ "$DEBUG" == "1" ]] && BUILD_ARGS+=(-debug)

STEP "构建 $PLATFORM（wails ${BUILD_ARGS[*]}）"
wails "${BUILD_ARGS[@]}"

# ---------- 9. 产物清单 ----------
END=$(date +%s)
STEP "构建完成，耗时 $((END - START))s，产物："
if [[ -d "$ROOT/build/bin" ]]; then
  find "$ROOT/build/bin" -type f -maxdepth 1 | while read -r f; do
    SIZE_MB=$(du -m "$f" | cut -f1)
    printf '    \033[32m%s  (%s MB)\033[0m\n' "$f" "$SIZE_MB"
  done
fi

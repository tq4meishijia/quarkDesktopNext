#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""为 quarkDesktopNext 桌面端自有 Go 源码补充版权声明头。

仅处理仓库根模块（main.go、app/、internal/），不动 quark-cil/ 下的上游代码。
"""
import os
import sys

# 本脚本位于 scripts/，仓库根为其上一级
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
HEADER = (
    "// Copyright (c) 2026 tq4meishijia\n"
    "// SPDX-License-Identifier: MIT\n"
    "//\n"
    "// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权"
    "（许可证全文见仓库根 LICENSE）。\n"
    "// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的\n"
    "// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。\n"
    "\n"
)

MARKER = "SPDX-License-Identifier"

targets = []
for base in ("app", "internal"):
    for dirpath, _dirnames, filenames in os.walk(os.path.join(ROOT, base)):
        for fn in filenames:
            if fn.endswith(".go"):
                targets.append(os.path.join(dirpath, fn))
main_go = os.path.join(ROOT, "main.go")
if os.path.isfile(main_go):
    targets.append(main_go)

changed, skipped = [], []
for path in sorted(targets):
    with open(path, "r", encoding="utf-8") as f:
        content = f.read()
    if MARKER in content.split("\npackage ")[0]:
        skipped.append(path)
        continue
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(HEADER + content)
    changed.append(path)

print("已补充版权声明头 (%d 个文件):" % len(changed))
for p in changed:
    print("  +", os.path.relpath(p, ROOT))
if skipped:
    print("已存在声明头，跳过 (%d 个):" % len(skipped))
    for p in skipped:
        print("  =", os.path.relpath(p, ROOT))
print("\nquark-cil/ 为上游 AGPL-3.0 源码，未作任何修改。")
sys.exit(0)

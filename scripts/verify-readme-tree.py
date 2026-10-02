#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""校验 README 目录树中出现的路径是否与实际文件系统一致（临时校验脚本）。"""
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
README = os.path.join(ROOT, "README.md")
TREE_CHARS = "│├└─ "

in_tree = False
checked = 0
missing = []
stack = []  # (indent_level, name)

with open(README, encoding="utf-8") as f:
    for line in f:
        if line.startswith("## 4. 目录结构"):
            in_tree = True
            continue
        if in_tree and line.startswith("---"):
            break
        if not in_tree:
            continue
        if not line.startswith((" ", "│", "├", "└")):
            continue
        if "```" in line:
            continue
        # 去掉行内说明（连续两个空格之后的说明文字）
        body = line.rstrip("\n")
        # 计算层级：每 4 个空格为一级
        stripped = body.lstrip(TREE_CHARS)
        indent = len(body) - len(stripped)
        # 顶层条目带 4 个字符的树形前缀，故层级从 0 起算
        level = indent // 4 - 1
        name = stripped.split("  ")[0].strip()
        if not name or name == "←":
            continue
        # 兼容 "LICENSE / README.md / README.en.md" 这种并列写法
        for part in name.split(" / "):
            part = part.strip()
            if not part:
                continue
            stack = stack[:level]
            rel = os.path.join(*(stack + [part])) if stack else part
            stack = stack + [part]
            path = os.path.join(ROOT, rel)
            checked += 1
            if not os.path.exists(path):
                missing.append(rel)

print(f"已校验路径数：{checked}")
if missing:
    print("以下路径在 README 中出现但实际不存在：")
    for m in missing:
        print("  ❌", m)
    sys.exit(1)
print("✅ README 目录树中的路径全部与实际一致")

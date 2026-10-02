# -*- coding: utf-8 -*-
"""生成桌面端应用图标 appicon.png（256x256，纯标准库，无需 Pillow）。

用法：
    python scripts/make-icon.py            # 生成到项目根目录 appicon.png
    python scripts/make-icon.py -o out.png # 指定输出路径

图形为「渐变蓝圆角底 + 白色云朵」，与界面 favicon 同一配色体系。
构建脚本会在缺少 appicon.png 时提示执行本文件；图标缺失不会导致构建失败，
只是会使用 Wails 自带的默认图标。
"""
import argparse
import os
import struct
import sys
import zlib

SIZE = 256
CORNER = 56                      # 背景圆角半径
BG_TOP = (0x1e, 0x74, 0xff)      # 渐变起始（左上）
BG_BOTTOM = (0x0a, 0x4b, 0xd6)   # 渐变结束（右下）
FG = (255, 255, 255)             # 前景（白色云朵）

# 云朵由三个圆 + 一个底部圆角条并集构成：(cx, cy, r)
CLOUD_CIRCLES = [(96, 140, 34), (132, 110, 46), (168, 142, 30)]
# 底部圆角条：(x0, y0, x1, y1, 圆角)
CLOUD_BAR = (66, 142, 196, 174, 16)


def in_rounded_rect(x, y, radius, size):
    """点 (x, y) 是否落在圆角矩形内（含边界）。"""
    size -= 1
    if x < 0 or y < 0 or x > size or y > size:
        return False
    cx = min(max(x, radius), size - radius)
    cy = min(max(y, radius), size - radius)
    return (x - cx) ** 2 + (y - cy) ** 2 <= radius ** 2


def in_circle(x, y, cx, cy, r):
    return (x - cx) ** 2 + (y - cy) ** 2 <= r * r


def draw_cloud(x, y):
    """像素是否属于白色云朵。"""
    for cx, cy, r in CLOUD_CIRCLES:
        if in_circle(x, y, cx, cy, r):
            return True
    x0, y0, x1, y1, r = CLOUD_BAR
    if x0 <= x <= x1 and y0 <= y <= y1:
        cx = min(max(x, x0 + r), x1 - r)
        cy = min(max(y, y0 + r), y1 - r)
        if (x - cx) ** 2 + (y - cy) ** 2 <= r * r:
            return True
    return False


def build_pixels():
    """返回带过滤字节的 RGBA 行数据（2x2 超采样抗锯齿）。"""
    rows = []
    t_max = float(SIZE - 1) + float(SIZE - 1)
    sample = 2
    for y in range(SIZE):
        row = bytearray([0])
        for x in range(SIZE):
            hit_bg = 0
            hit_fg = 0
            for sy in range(sample):
                for sx in range(sample):
                    px = x + (sx + 0.5) / sample
                    py = y + (sy + 0.5) / sample
                    if in_rounded_rect(px, py, CORNER, SIZE):
                        hit_bg += 1
                        if draw_cloud(px, py):
                            hit_fg += 1
            if hit_bg == 0:
                row += bytes((0, 0, 0, 0))
                continue
            total = float(sample * sample)
            cover_bg = hit_bg / total
            cover_fg = hit_fg / total
            t = (x + y) / t_max
            bg = tuple(int(BG_TOP[i] + (BG_BOTTOM[i] - BG_TOP[i]) * t) for i in range(3))
            r = int(bg[0] + (FG[0] - bg[0]) * cover_fg)
            g = int(bg[1] + (FG[1] - bg[1]) * cover_fg)
            b = int(bg[2] + (FG[2] - bg[2]) * cover_fg)
            row += bytes((r, g, b, int(round(255 * cover_bg))))
        rows.append(bytes(row))
    return b"".join(rows)


def png_bytes(raw):
    def chunk(tag, data):
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    header = struct.pack(">IIBBBBB", SIZE, SIZE, 8, 6, 0, 0, 0)  # 8bit RGBA
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    default = os.path.join(here, os.pardir, "appicon.png")
    parser = argparse.ArgumentParser(description="生成 kuake-desktop 图标")
    parser.add_argument("-o", "--output", default=os.path.normpath(default), help="输出 PNG 路径")
    args = parser.parse_args()

    data = png_bytes(build_pixels())
    with open(args.output, "wb") as fh:
        fh.write(data)
    print("已生成图标：%s（%d x %d，%.1f KB）" % (os.path.abspath(args.output), SIZE, SIZE, len(data) / 1024.0))
    return 0


if __name__ == "__main__":
    sys.exit(main())

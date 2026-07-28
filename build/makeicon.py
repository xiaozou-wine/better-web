#!/usr/bin/env python3
"""生成应用图标。

为什么用脚本而非直接提交二进制图标：图标要改配色或形状时，改这里比重新
找设计工具再导出一遍快，且改动可在 diff 中看清。

产物：
  build/appicon.png       1024x1024，Wails 用它生成各平台图标
  build/windows/icon.ico  多尺寸 ico，Windows 快捷方式与任务栏用

设计意图：三个错位的圆环表示彼此隔离的多重身份，同心而不重合——
对应本项目的核心概念（同一程序下多个互不关联的浏览器环境）。
不画浏览器窗口或地球，那类图形在小尺寸下会糊成一团。
"""

import math
import os
import struct
import zlib

SIZE = 1024
# 深色底 + 蓝紫渐变环。与界面配色一致（背景 #0f131b，主色 #4a7cff）。
BG = (15, 19, 27)
RINGS = [
    # (中心 x 比例, 中心 y 比例, 半径比例, 线宽比例, 颜色)
    (0.50, 0.38, 0.26, 0.075, (74, 124, 255)),   # 蓝
    (0.36, 0.60, 0.26, 0.075, (139, 108, 255)),  # 紫
    (0.64, 0.60, 0.26, 0.075, (64, 200, 190)),   # 青
]


def blend(dst, src, alpha):
    """按 alpha 混合两个 RGB 元组。"""
    return tuple(int(d + (s - d) * alpha) for d, s in zip(dst, src))


def render(size):
    """渲染一张 size×size 的 RGB 位图，返回逐行字节。

    手写抗锯齿而非引入 Pillow：图标只画圆环，几何简单，
    避免为一次性脚本添加依赖。用到中心距离与线宽的差值做边缘羽化。
    """
    px = [[BG for _ in range(size)] for _ in range(size)]
    # 超采样 2x2 抗锯齿：单点采样在小尺寸下环边会出锯齿。
    offsets = [(0.25, 0.25), (0.75, 0.25), (0.25, 0.75), (0.75, 0.75)]

    for cxr, cyr, rr, wr, color in RINGS:
        cx, cy = cxr * size, cyr * size
        radius, half = rr * size, wr * size / 2
        # 只遍历该环的外接矩形，避免整图扫描。
        lo_x = max(0, int(cx - radius - half - 2))
        hi_x = min(size, int(cx + radius + half + 2))
        lo_y = max(0, int(cy - radius - half - 2))
        hi_y = min(size, int(cy + radius + half + 2))

        for y in range(lo_y, hi_y):
            for x in range(lo_x, hi_x):
                hits = 0
                for ox, oy in offsets:
                    d = math.hypot(x + ox - cx, y + oy - cy)
                    if abs(d - radius) <= half:
                        hits += 1
                if hits:
                    # 环之间相交处叠色，体现"多个身份并存"。
                    px[y][x] = blend(px[y][x], color, hits / len(offsets))
    return px


def write_png(path, px):
    size = len(px)
    raw = b"".join(
        b"\x00" + b"".join(struct.pack("3B", *px[y][x]) for x in range(size))
        for y in range(size)
    )

    def chunk(tag, data):
        body = tag + data
        return struct.pack(">I", len(data)) + body + struct.pack(
            ">I", zlib.crc32(body) & 0xFFFFFFFF)

    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n")
        # 8 位深、真彩色、无隔行。
        f.write(chunk(b"IHDR", struct.pack(">2I5B", size, size, 8, 2, 0, 0, 0)))
        f.write(chunk(b"IDAT", zlib.compress(raw, 9)))
        f.write(chunk(b"IEND", b""))


def write_ico(path, sizes):
    """写多尺寸 ico。

    每一帧都独立渲染而非缩放大图：缩放会让 16px 的细环消失，
    而独立渲染时线宽按比例保持可见。
    """
    images = []
    for s in sizes:
        px = render(s)
        # ico 内嵌 PNG（Vista 起支持），比 BMP 省体积且带 alpha 语义。
        tmp = path + f".{s}.tmp"
        write_png(tmp, px)
        with open(tmp, "rb") as f:
            images.append(f.read())
        os.remove(tmp)

    header = struct.pack("<3H", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    entries, blobs = b"", b""
    for s, data in zip(sizes, images):
        # 256 在 ico 目录项里记为 0。
        dim = 0 if s >= 256 else s
        entries += struct.pack("<4B2H2I", dim, dim, 0, 0, 1, 32,
                               len(data), offset)
        offset += len(data)
        blobs += data

    with open(path, "wb") as f:
        f.write(header + entries + blobs)


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    png = os.path.join(here, "appicon.png")
    ico = os.path.join(here, "windows", "icon.ico")

    write_png(png, render(SIZE))
    print(f"已生成 {png} ({os.path.getsize(png)} 字节)")

    # 16 与 32 供任务栏与列表，48/64 供桌面，256 供大图标视图。
    write_ico(ico, [16, 32, 48, 64, 128, 256])
    print(f"已生成 {ico} ({os.path.getsize(ico)} 字节)")


if __name__ == "__main__":
    main()

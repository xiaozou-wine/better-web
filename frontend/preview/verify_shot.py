"""从截图里采样关键区域的颜色，确认主题真的落到了渲染结果上。

只靠对比度计算能验证 token 取值，但验证不了"这套 token 有没有真的
应用到页面"。这里读 PNG 像素，检查页面底色、卡片底色与两者的层次差。
不依赖第三方库：PNG 解码用 zlib + 手写 unfilter。
"""
import struct
import sys
import zlib
from pathlib import Path


def read_png(path: Path) -> tuple[int, int, list[list[tuple[int, int, int]]]]:
    """解出 RGB 像素矩阵。只支持 8bit truecolor（Chrome 截图就是这种）。"""
    data = path.read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise ValueError(f"不是 PNG: {path}")

    pos, idat, meta = 8, bytearray(), None
    while pos < len(data):
        (length,) = struct.unpack(">I", data[pos : pos + 4])
        ctype = data[pos + 4 : pos + 8]
        body = data[pos + 8 : pos + 8 + length]
        if ctype == b"IHDR":
            w, h, depth, color = struct.unpack(">IIBB", body[:10])
            if depth != 8 or color not in (2, 6):
                raise ValueError(f"不支持的 PNG 格式 depth={depth} color={color}")
            meta = (w, h, 3 if color == 2 else 4)
        elif ctype == b"IDAT":
            idat += body
        elif ctype == b"IEND":
            break
        pos += 12 + length

    assert meta, "缺 IHDR"
    w, h, nch = meta
    raw = zlib.decompress(bytes(idat))
    stride = w * nch

    out: list[list[tuple[int, int, int]]] = []
    prev = bytearray(stride)
    p = 0
    for _ in range(h):
        ft = raw[p]
        line = bytearray(raw[p + 1 : p + 1 + stride])
        p += 1 + stride
        # PNG 逐行 filter 反解。
        for i in range(stride):
            a = line[i - nch] if i >= nch else 0
            b = prev[i]
            c = prev[i - nch] if i >= nch else 0
            if ft == 1:
                line[i] = (line[i] + a) & 0xFF
            elif ft == 2:
                line[i] = (line[i] + b) & 0xFF
            elif ft == 3:
                line[i] = (line[i] + (a + b) // 2) & 0xFF
            elif ft == 4:
                pa, pb, pc = abs(b - c), abs(a - c), abs(a + b - 2 * c)
                pred = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pred) & 0xFF
        out.append([tuple(line[i : i + 3]) for i in range(0, stride, nch)])  # type: ignore
        prev = line
    return w, h, out


def luminance(rgb: tuple[int, int, int]) -> float:
    def ch(c: int) -> float:
        s = c / 255
        return s / 12.92 if s <= 0.04045 else ((s + 0.055) / 1.055) ** 2.4

    r, g, b = (ch(c) for c in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def hexs(rgb: tuple[int, int, int]) -> str:
    return "#%02x%02x%02x" % rgb


def main() -> int:
    shots = Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/bwshots")
    failed = 0

    for name, expect_dark in (("dark", True), ("light", False)):
        path = shots / f"{name}.png"
        if not path.exists():
            print(f"[{name}] 缺截图 {path}")
            failed += 1
            continue

        w, h, px = read_png(path)
        # 页面底：右下角空白区。卡片：列表区左侧偏内的一点。
        canvas = px[h - 12][w - 12]
        card = px[int(h * 0.42)][int(w * 0.52)]
        lc, lk = luminance(canvas), luminance(card)
        is_dark = lc < 0.2

        print(f"[{name}] {w}x{h}")
        print(f"  页面底 {hexs(canvas)} 亮度 {lc:.3f}")
        print(f"  卡片   {hexs(card)} 亮度 {lk:.3f}")

        if is_dark != expect_dark:
            print(f"  FAIL 期望{'暗' if expect_dark else '亮'}色，实测相反")
            failed += 1
        else:
            print(f"  OK   主题为{'暗' if is_dark else '亮'}色")

        # 卡片必须与页面底分得开，否则列表看起来是一片平的。
        #
        # 用对比度比值而非线性亮度差：相对亮度经过 gamma 曲线，同样的
        # "看起来差一档"在亮端的线性差值远大于暗端，用绝对差会把暗色
        # 主题误判成没有层次。
        hi, lo = max(lc, lk), min(lc, lk)
        sep = (hi + 0.05) / (lo + 0.05)
        if sep < 1.05:
            print(f"  FAIL 卡片与页面底几乎同色（对比度 {sep:.3f}）")
            failed += 1
        else:
            print(f"  OK   卡片与页面底有层次（对比度 {sep:.3f}）")

    # 亮暗两图必须真的不同，防止截到同一张。
    d, l = shots / "dark.png", shots / "light.png"
    if d.exists() and l.exists():
        if d.read_bytes() == l.read_bytes():
            print("\nFAIL 亮暗截图内容完全相同，主题未生效或截图失败")
            failed += 1
        else:
            print("\nOK   亮暗截图内容不同")

    print()
    print(f"{failed} 项未通过" if failed else "截图验证通过")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

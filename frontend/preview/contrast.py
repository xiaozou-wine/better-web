"""检查 tokens.css 里各语义色对的 WCAG 对比度。

对比度是确定性计算，不该靠目测判断。这里直接解析 tokens.css，
按主题算出正文/次要文字/状态色与其背景的比值，低于阈值就报出来。
"""
import re
import sys
from pathlib import Path

TOKENS = Path(__file__).resolve().parent.parent / "src" / "styles" / "tokens.css"

# (前景 token, 背景 token, 最低要求, 说明)
# 正文与图标按 WCAG AA：普通文字 4.5，大号文字/图形 3.0。
PAIRS = [
    ("--c-text", "--c-canvas", 4.5, "正文 / 页面底"),
    ("--c-text", "--c-raised", 4.5, "正文 / 卡片"),
    ("--c-text-muted", "--c-raised", 4.5, "次要文字 / 卡片"),
    ("--c-text-faint", "--c-raised", 3.0, "弱化文字 / 卡片"),
    ("--c-text-faint", "--c-canvas", 3.0, "弱化文字 / 页面底"),
    ("--c-accent-fg", "--c-accent", 4.5, "主按钮文字 / 主按钮底"),
    ("--c-accent-text", "--c-accent-soft", 4.5, "强调文字 / 强调底"),
    ("--c-ok-text", "--c-ok-soft", 4.5, "成功文字 / 成功底"),
    ("--c-warn-text", "--c-warn-soft", 4.5, "警告文字 / 警告底"),
    ("--c-err-text", "--c-err-soft", 4.5, "错误文字 / 错误底"),
    ("--c-border-strong", "--c-raised", 1.6, "控件边框 / 卡片"),
]


def parse_blocks(css: str) -> dict[str, dict[str, str]]:
    """按选择器块收集自定义属性，返回 {选择器: {token: 值}}。"""
    # 先剥注释：否则块前的注释会被并进选择器文本，导致选择器匹配不上。
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    out: dict[str, dict[str, str]] = {}
    for sel, body in re.findall(r"([^{}]+)\{([^{}]*)\}", css):
        sel = " ".join(sel.split())
        vars_ = dict(re.findall(r"(--[\w-]+)\s*:\s*([^;]+);", body))
        if vars_:
            out.setdefault(sel, {}).update({k: v.strip() for k, v in vars_.items()})
    return out


def resolve(blocks: dict[str, dict[str, str]], selectors: list[str]) -> dict[str, str]:
    """按选择器顺序层叠出一套 token（后者覆盖前者）。"""
    merged: dict[str, str] = {}
    for sel in selectors:
        for key, val in blocks.items():
            if sel in [s.strip() for s in key.split(",")]:
                merged.update(val)
    return merged


def to_rgb(v: str) -> tuple[int, int, int]:
    v = v.strip()
    m = re.fullmatch(r"#([0-9a-fA-F]{6})", v)
    if m:
        h = m.group(1)
        return tuple(int(h[i : i + 2], 16) for i in (0, 2, 4))  # type: ignore
    m = re.fullmatch(r"#([0-9a-fA-F]{3})", v)
    if m:
        h = m.group(1)
        return tuple(int(c * 2, 16) for c in h)  # type: ignore
    raise ValueError(f"无法解析颜色: {v}")


def luminance(rgb: tuple[int, int, int]) -> float:
    def ch(c: int) -> float:
        s = c / 255
        return s / 12.92 if s <= 0.04045 else ((s + 0.055) / 1.055) ** 2.4

    r, g, b = (ch(c) for c in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def ratio(fg: str, bg: str) -> float:
    a, b = luminance(to_rgb(fg)), luminance(to_rgb(bg))
    hi, lo = max(a, b), min(a, b)
    return (hi + 0.05) / (lo + 0.05)


def main() -> int:
    css = TOKENS.read_text(encoding="utf-8")
    blocks = parse_blocks(css)
    themes = {
        "dark": [":root", "[data-theme='dark']"],
        "light": ["[data-theme='light']"],
        # 未设 data-theme 时的亮色分支，取值必须与显式 light 一致。
        "light(system)": [":root:not([data-theme])"],
    }

    failed = 0

    # 显式 light 与 system 亮色分支是两份手抄的取值，必须逐项一致，
    # 否则用户切到"跟随系统"会看到一套与"亮色"略有差异的配色。
    explicit = resolve(blocks, [":root", "[data-theme='light']"])
    system = resolve(blocks, [":root", ":root:not([data-theme])"])
    drift = [
        k
        for k in explicit
        if k.startswith(("--c-", "--shadow-")) and explicit[k] != system.get(k)
    ]
    print("[亮色两分支一致性]")
    if drift:
        failed += len(drift)
        for k in drift:
            print(f"  DIFF {k}: light={explicit[k]!r} system={system.get(k)!r}")
    else:
        print("  OK  显式 light 与 system 亮色分支取值一致")

    for theme, selectors in themes.items():
        tokens = resolve(blocks, [":root"] + selectors)
        print(f"\n[{theme}]")
        for fg, bg, need, label in PAIRS:
            if fg not in tokens or bg not in tokens:
                print(f"  ?  {label}: 缺 token ({fg} / {bg})")
                failed += 1
                continue
            r = ratio(tokens[fg], tokens[bg])
            ok = r >= need
            if not ok:
                failed += 1
            print(f"  {'OK' if ok else 'LOW'}  {label}: {r:.2f} (需 ≥{need})")

    print()
    if failed:
        print(f"{failed} 项未达标")
        return 1
    print("全部达标")
    return 0


if __name__ == "__main__":
    sys.exit(main())

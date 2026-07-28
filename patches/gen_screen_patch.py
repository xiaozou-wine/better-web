#!/usr/bin/env python3
"""机械生成 019-screen-fingerprint.patch。

手写 unified diff 的 hunk 头与行数极易出错，改为：构造修改后的文件内容，
再用 difflib 生成 diff。这样补丁与源码版本严格对应。

用法：
    python gen_screen_patch.py <原始 screen.cc 路径> <输出 patch 路径>
"""
import difflib
import sys
from pathlib import Path

# 需要新增的 include。
#
# <algorithm> 与 <optional> 是必需的：原文件没有包含它们，而补丁用到
# std::max 与 std::optional。Chromium 的 include-what-you-use 规则要求
# 显式包含，依赖传递包含会在上游调整头文件时突然编译失败。
#
# Chromium 的 include 顺序要求：关联头文件、C 系统头、C++ 标准库头，
# 再是项目头。因此标准库头与项目头必须分别插入到不同位置，
# 不能一股脑追加到末尾。
STD_INCLUDES = [
    '#include <algorithm>\n',
    '#include <optional>\n',
    '\n',
]

# ui/gfx/geometry/size.h 必须显式包含：screen.h 只引入了 rect.h，
# 而本补丁用到 gfx::Size。rect.h 目前恰好间接带入 size.h，但依赖这种
# 传递关系很脆弱，上游一次头文件整理就会让构建失败。
PROJECT_INCLUDES = [
    '#include "base/command_line.h"\n',
    '#include "base/strings/string_number_conversions.h"\n',
    '#include "components/ungoogled/ungoogled_switches.h"\n',
    '#include "ui/gfx/geometry/size.h"\n',
]

# 匿名命名空间中的辅助实现，插在 `namespace blink {` 之后。
HELPERS = '''
namespace {

// Height in physical pixels reserved for the OS taskbar / dock when
// synthesizing the available screen rect.
//
// A desktop reporting screen.height == screen.availHeight has no taskbar,
// which is a headless signal. The value itself is not identifying; what
// matters is that some space is reserved.
constexpr int kSyntheticTaskbarHeight = 40;

// Plausible bounds for a screen dimension. Out-of-range values are rejected
// rather than clamped: a bogus switch value must not silently produce an
// absurd screen size that is more identifying than the real one.
constexpr int kMinScreenDimension = 320;
constexpr int kMaxScreenDimension = 8192;

// Returns the spoofed screen size, or nullopt when the switches are absent or
// invalid.
//
// Both width and height must be valid. Accepting one without the other would
// mix a spoofed dimension with a real one, yielding an aspect ratio that
// matches no real device.
std::optional<gfx::Size> SpoofedScreenSize() {
  const base::CommandLine* cmd = base::CommandLine::ForCurrentProcess();
  if (!cmd->HasSwitch(switches::kFingerprintScreenWidth) ||
      !cmd->HasSwitch(switches::kFingerprintScreenHeight)) {
    return std::nullopt;
  }

  int width = 0;
  int height = 0;
  if (!base::StringToInt(
          cmd->GetSwitchValueASCII(switches::kFingerprintScreenWidth),
          &width) ||
      !base::StringToInt(
          cmd->GetSwitchValueASCII(switches::kFingerprintScreenHeight),
          &height)) {
    return std::nullopt;
  }
  if (width < kMinScreenDimension || width > kMaxScreenDimension ||
      height < kMinScreenDimension || height > kMaxScreenDimension) {
    return std::nullopt;
  }
  return gfx::Size(width, height);
}

// Builds the rect reported to script for the given screen size.
//
// |available| selects the work area, which must be strictly smaller than the
// full screen rect so that a taskbar is implied. The origin stays at (0,0)
// because a bottom-docked taskbar leaves the top-left corner unchanged.
gfx::Rect SyntheticScreenRect(const gfx::Size& size, bool available) {
  if (!available) {
    return gfx::Rect(size);
  }
  const int usable_height =
      std::max(kMinScreenDimension, size.height() - kSyntheticTaskbarHeight);
  return gfx::Rect(0, 0, size.width(), usable_height);
}

}  // namespace
'''

# 插在 GetRect 取真实 ScreenInfo 之前的覆盖逻辑。
OVERRIDE = '''
  // Report the spoofed geometry before any host-derived scaling: the switch
  // values are already what script should observe, so applying the host's
  // device scale factor on top would distort them.
  if (std::optional<gfx::Size> spoofed = SpoofedScreenSize()) {
    return SyntheticScreenRect(*spoofed, available);
  }
'''


def patch_lines(lines):
    """返回修改后的行列表。找不到锚点时报错，不静默产出错误补丁。"""
    out = list(lines)

    # 1) 项目头：插到最后一个 #include 之后。
    last_include = max(
        i for i, ln in enumerate(out) if ln.startswith('#include ')
    )
    out[last_include + 1:last_include + 1] = PROJECT_INCLUDES

    # 2) 标准库头：插到关联头文件（screen.h）之后、其余项目头之前，
    #    以符合 Chromium 的 include 顺序要求。
    own_header = next(
        (
            i
            for i, ln in enumerate(out)
            if ln.startswith('#include "third_party/blink/renderer/core/frame/screen.h"')
        ),
        None,
    )
    if own_header is None:
        raise SystemExit('找不到关联头文件 screen.h 的 include')
    out[own_header + 1:own_header + 1] = ['\n'] + STD_INCLUDES[:-1]

    # 3) 辅助实现：插到 `namespace blink {` 之后。
    ns = next(
        (i for i, ln in enumerate(out) if ln.rstrip() == 'namespace blink {'),
        None,
    )
    if ns is None:
        raise SystemExit('找不到 `namespace blink {`，源码结构可能已变更')
    # 必须按行切分再插入：difflib 以列表元素为「行」，把多行字符串作为
    # 单个元素会让内部换行拿不到 `+` 前缀，产出 malformed patch。
    out[ns + 1:ns + 1] = HELPERS.splitlines(keepends=True)

    # 4) 覆盖逻辑：插到 GetRect 中取 ScreenInfo 之前。
    sig = 'gfx::Rect Screen::GetRect(bool available) const {'
    start = next((i for i, ln in enumerate(out) if sig in ln), None)
    if start is None:
        raise SystemExit('找不到 Screen::GetRect 定义，源码结构可能已变更')
    anchor = next(
        (
            i
            for i in range(start, min(start + 20, len(out)))
            if 'const display::ScreenInfo& screen_info = GetScreenInfo();' in out[i]
        ),
        None,
    )
    if anchor is None:
        raise SystemExit('找不到 GetRect 内的 ScreenInfo 取值行')
    out[anchor:anchor] = OVERRIDE.splitlines(keepends=True)
    return out


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    src, dst = Path(sys.argv[1]), Path(sys.argv[2])
    original = src.read_text(encoding='utf-8').splitlines(keepends=True)
    modified = patch_lines(original)

    rel = 'third_party/blink/renderer/core/frame/screen.cc'
    diff = difflib.unified_diff(
        original, modified, fromfile='a/' + rel, tofile='b/' + rel, n=3
    )
    # 必须写 LF：patch 对 CRLF 的 hunk 分隔符处理不一致，会报 malformed。
    # newline='' 关闭 Python 的行尾转换，内容里的 \n 原样落盘。
    dst.write_text(HEADER + ''.join(diff), encoding='utf-8', newline='')
    print(f'已写入 {dst}')


HEADER = '''Implement screen resolution fingerprinting.

The --fingerprint-screen-width and --fingerprint-screen-height switches are
already declared in components/ungoogled/ungoogled_switches.cc and propagated
to renderers by render_process_host_impl.cc, but nothing consumes them, so
passing values has no effect. This patch implements the consumer.

Screen::GetRect() is the single choke point: width(), height(), availWidth(),
availHeight(), availLeft() and availTop() all read through it. Overriding here
keeps every accessor consistent; patching them individually risks a
width/availWidth mismatch, which is itself a detectable inconsistency.

The available rect is kept strictly smaller than the screen rect by reserving
space for a taskbar. A desktop where screen.height == screen.availHeight
implies no taskbar, which CreepJS flags via its noTaskbar heuristic.

Generated by patches/gen_screen_patch.py against Chromium 148.0.7778.215.

'''

if __name__ == '__main__':
    main()

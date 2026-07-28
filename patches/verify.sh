#!/usr/bin/env bash
# 验证内核补丁：能否干净应用 + 逻辑是否正确。
#
# 不需要完整的 Chromium 构建树。做两件事：
#   1. 拉取补丁目标文件的对应版本，dry-run 应用，确认上下文仍匹配
#   2. 用桩件编译并运行补丁中的辅助逻辑，确认取值与一致性约束正确
#
# 上游每四周一个大版本，升级后先跑这个脚本，再决定是否开始数小时的构建。
set -euo pipefail

CHROMIUM_VERSION="${CHROMIUM_VERSION:-148.0.7778.215}"
PATCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# 走代理拉源码；BW_PROXY 为空则直连。
CURL_OPTS=(-sS -L --max-time 180)
if [[ -n "${BW_PROXY:-}" ]]; then
  CURL_OPTS+=(--socks5-hostname "${BW_PROXY#socks5://}")
fi

SRC_REL="third_party/blink/renderer/core/frame/screen.cc"
SRC_URL="https://raw.githubusercontent.com/chromium/chromium/${CHROMIUM_VERSION}/${SRC_REL}"

echo "== 拉取 Chromium ${CHROMIUM_VERSION} 的 ${SRC_REL}"
mkdir -p "$WORK/$(dirname "$SRC_REL")"
curl "${CURL_OPTS[@]}" "$SRC_URL" -o "$WORK/$SRC_REL"
if [[ ! -s "$WORK/$SRC_REL" ]]; then
  echo "拉取失败：文件为空。检查网络或 BW_PROXY 设置。" >&2
  exit 1
fi

echo "== dry-run 应用补丁"
(cd "$WORK" && patch -p1 --dry-run < "$PATCH_DIR/019-screen-fingerprint.patch")

echo "== 实际应用并检查结果"
(cd "$WORK" && patch -p1 < "$PATCH_DIR/019-screen-fingerprint.patch" >/dev/null)
for needle in 'SpoofedScreenSize' 'SyntheticScreenRect' 'kSyntheticTaskbarHeight'; do
  if ! grep -q "$needle" "$WORK/$SRC_REL"; then
    echo "应用后找不到 $needle，补丁未生效" >&2
    exit 1
  fi
done

# 覆盖逻辑必须在读取真实 ScreenInfo 之前，否则伪造值会被宿主机缩放污染。
override_line="$(grep -n 'SpoofedScreenSize()' "$WORK/$SRC_REL" | tail -1 | cut -d: -f1)"
screeninfo_line="$(grep -n 'const display::ScreenInfo& screen_info' "$WORK/$SRC_REL" | head -1 | cut -d: -f1)"
if (( override_line >= screeninfo_line )); then
  echo "覆盖逻辑位置错误：应在读取 ScreenInfo 之前" >&2
  exit 1
fi

echo "== 编译并运行逻辑验证"
g++ -std=c++20 -Wall -Wextra -Werror -o "$WORK/verify_logic" "$PATCH_DIR/verify_logic.cc"
"$WORK/verify_logic"

echo
echo "补丁验证通过（Chromium ${CHROMIUM_VERSION}）"

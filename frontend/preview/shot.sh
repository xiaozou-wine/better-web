#!/usr/bin/env bash
# 视觉检查用：对预览页截图。参数为输出目录。
set -euo pipefail

CHROME="/c/Program Files/Google/Chrome/Application/chrome.exe"
URL="http://localhost:5199/preview/index.html"
OUT="${1:?用法: shot.sh <输出目录>}"
mkdir -p "$OUT"

shoot() {
  local name="$1" theme="$2" size="$3"
  local profile
  profile=$(mktemp -d)
  # 用 data URL 无法注入 localStorage，改为在 profile 目录预置 Local Storage 太脆弱，
  # 因此走 URL hash 由页面自行读取（见 mock.ts 未实现时退回系统偏好）。
  "$CHROME" \
    --headless=new \
    --disable-gpu \
    --hide-scrollbars \
    --force-color-profile=srgb \
    --user-data-dir="$profile" \
    --window-size="$size" \
    --screenshot="$(cygpath -w "$OUT/$name.png")" \
    --virtual-time-budget=4000 \
    "$URL?theme=$theme" >/dev/null 2>&1
  rm -rf "$profile"
  echo "  $name.png"
}

echo "截图输出到 $OUT"
shoot dark  dark  1280,900
shoot light light 1280,900
shoot narrow-dark dark 760,900

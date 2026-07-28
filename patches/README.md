# 内核补丁

针对 [fingerprint-chromium](https://github.com/adryfish/fingerprint-chromium)
的增量补丁，补齐其未实现的伪造维度。

## 适用版本

基于 Chromium `148.0.7778.215`。上游每四周一个大版本，升级后需核对
被改动的文件是否仍是同一结构，不要盲目套用。

## 补丁清单

| 补丁 | 解决的问题 |
| --- | --- |
| `019-screen-fingerprint.patch` | `screen.*` 与 `devicePixelRatio` 报出宿主机真实值，同一台机器上的多个 profile 会共享真实分辨率 |

## 编译可行性分析

补丁尚未经过真实构建（编译 Chromium 需数小时与数十 GB 依赖）。
以下几项风险已通过查证上游源码排除，剩余风险记在末尾。

### include 跨目录引用是否被拒

`third_party/blink/renderer/core/DEPS` 对 `components/` 采用逐文件白名单，
`components/ungoogled/ungoogled_switches.h` **不在**白名单内。

但这不构成阻碍：上游 fingerprint-chromium 的多个补丁
（`005-hardware-concurrency-fingerprint.patch` 改
`core/frame/navigator_concurrent_hardware.cc`、
`014-client-rects.patch` 改 `core/dom/document.cc` 与 `core/dom/element.cc`）
都在同样位置直接 include 了这个头文件，且没有修改任何 DEPS，
而这些补丁确实产出了可用的二进制。

原因是 DEPS 校验由独立的 `gn check` 步骤执行，常规构建不跑。
本补丁改的是 `core/frame/screen.cc`，与上游 005 补丁同目录，风险等同。

### 链接依赖是否缺失

这一项是真实风险，已确认被上游满足。`components/ungoogled:ungoogled_switches`
是一个独立的 GN component，Blink 必须显式依赖它才能链接
（否则报 `unresolved external symbol`）。

上游的
`patches/extra/bromite/fingerprinting-flags-client-rects-and-measuretext.patch`
已经把该依赖加进了 `third_party/blink/renderer/platform/BUILD.gn`：

```gn
component("platform") {
  deps = [
    ...
    "//components/ungoogled:ungoogled_switches",
```

而 `blink/renderer/core` 依赖 `platform`，依赖沿链传递到本补丁所在的
`core/frame/`。因此**本补丁无需再改任何 BUILD.gn**。

该 bromite 补丁在 `patches/series` 中位于第 54 行，远早于 fingerprint
系列（115 行起），顺序上有保证。

### 开关声明的顺序依赖

补丁用到的 `switches::kFingerprintScreenWidth` 与
`kFingerprintScreenHeight` 由 `000-add-fingerprint-switches.patch` 声明。
本补丁编号 019，位于其后，顺序正确。

### 剩余未验证的风险

| 风险 | 状态 |
| --- | --- |
| 头文件传递依赖 | 已消除。`screen.h` 只引入 `ui/gfx/geometry/rect.h`，而补丁用到 `gfx::Size`；虽然 `rect.h` 目前间接带入 `size.h`，但已显式补上 `ui/gfx/geometry/size.h`，不赌传递关系 |
| `treat_warnings_as_errors` | 无风险。上游 `flags.gn` 设为 `false` |
| 实际编译 | **未做**。上表之外的问题只能靠真实构建暴露 |

### include 顺序的已知瑕疵

新增的项目头被追加在现有 include 块末尾，未按字母序插入
（`base/...` 排在了 `ui/display/...` 之后）。Chromium 的风格检查会提示，
但 `treat_warnings_as_errors=false` 下不影响构建，且上游自己的补丁
也是这样追加的。为降低与后续版本的冲突概率，保持追加而非重排。

## 验证

```sh
BW_PROXY=socks5://127.0.0.1:10808 bash patches/verify.sh
```

做两件事，都不需要完整的 Chromium 构建树：

1. 拉取补丁目标文件的对应版本，dry-run 应用，确认上下文仍匹配，
   并检查覆盖逻辑的插入位置在读取真实 `ScreenInfo` 之前
2. 用桩件编译（`-Wall -Wextra -Werror`）并运行补丁中的辅助逻辑，
   验证取值解析、非法输入拒绝、边界值，以及 `availHeight < height`
   的任务栏一致性约束

上游每四周一个大版本，升级后先跑这个脚本，再决定是否开始数小时的构建。

## 补丁的生成方式

`019-screen-fingerprint.patch` 由 `gen_screen_patch.py` 机械生成，
不要手工编辑。手写 unified diff 的 hunk 头与行数极易出错
（第一版就因此产出了 malformed patch）。

```sh
python patches/gen_screen_patch.py <screen.cc 原始文件> patches/019-screen-fingerprint.patch
```

脚本按锚点文本定位插入位置，锚点找不到时直接报错而非静默产出错误补丁。
新版本 Chromium 结构变动时，改锚点比改行号可靠。

## 应用方式

补丁按 fingerprint-chromium 的目录约定放置，即
`patches/extra/fingerprint/` 下，并在 `patches/series` 中登记。
编号从 019 起，避免与上游现有的 000–018 冲突。

```sh
# 在 ungoogled-chromium 构建树中
cp patches/019-screen-fingerprint.patch <build>/patches/extra/fingerprint/
echo "extra/fingerprint/019-screen-fingerprint.patch" >> <build>/patches/series
```

## 019-screen-fingerprint.patch 说明

### 为什么需要

`--fingerprint-screen-width`、`--fingerprint-screen-height`、
`--fingerprint-device-scale-factor` 三个开关在上游 144 版的
`000-add-fingerprint-switches.patch` 中**已经注册**，并在
`render_process_host_impl.cc` 里转发给渲染进程，但仓库中没有任何补丁消费
它们。实测（`internal/probe` 的 `TestScreenFingerprintFlags`）确认传值无效，
`screen.width/height` 仍是宿主机真实值。

即：开关是死的，只需补上消费逻辑，不必自己新增开关。

### 改动点

`third_party/blink/renderer/core/frame/screen.cc` 的 `Screen::GetRect()`。

选择这里的原因：`width()`、`height()`、`availWidth()`、`availHeight()`、
`availLeft()`、`availTop()` 六个 API 全部经由 `GetRect()` 取值，
在这一处覆盖即可保证所有出口一致。分散到各 API 去改容易漏，
而漏掉任何一个都会产生 `width` 与 `availWidth` 互相矛盾的破绽。

### 一致性约束

补丁遵守两条真实设备的规律，否则伪造本身会成为破绽：

1. **`availRect` 必须小于或等于 `rect`。** 可用区域是屏幕减去任务栏后的
   部分。桌面系统上二者完全相等意味着没有任务栏，这正是 CreepJS 的
   `noTaskbar` 判定项。补丁为 Windows 预留 40 物理像素的任务栏高度。
2. **`availLeft`/`availTop` 保持 0。** 任务栏在底部时，可用区域左上角
   与屏幕左上角重合。

### 未覆盖的部分

`window.devicePixelRatio` 不走 `Screen::GetRect()`，它取自
`LocalDOMWindow::devicePixelRatio()` → `ChromeClient` 的
`WindowToViewportScalar`。改它需要触及窗口坐标换算，会影响实际渲染尺寸
与鼠标事件坐标，风险远高于收益。因此本补丁不改 `devicePixelRatio`，
它仍报宿主机真实值。

这留下一个矛盾：伪造 1366x768 却报 dpr 1.25 在真实机型中少见。
所以使用时应当让声明的分辨率与宿主机的真实 dpr 相容，
或者只在 dpr 为 1 的宿主机上启用分辨率伪造。

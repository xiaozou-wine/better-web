---
name: seed-screening-platform-gap
description: matchseed 筛选环境与真实启动环境不一致（缺 --fingerprint-platform）导致筛出的同族种子在实际启动时仍是跨厂商；launchdebug 漏传 DeviceLabel 使锁定机型静默失效；以及机房出口才是 Turnstile 的真阻碍
metadata:
  type: reference
  node_type: memory
---

# 种子筛选的平台缺口，与两个静默失效

2026-07-28，内核 148.0.7778.215，宿主机为某 NVIDIA 独显笔记本。起因是把本项目的
指纹环境接给外部 CDP 工具做表单自动化，Cloudflare Turnstile 过不去。

## 1. `matchseed` 筛的环境和真实启动的环境不一样

`probe.SeedGPUFamily` 只传 `--fingerprint=<seed>`，而
`launcher.BuildArgs` 实际启动时还会按机型档案传 `--fingerprint-platform`。
**平台参数会改变内核派生出的 GPU。**

同一个种子 `279042489` 的实测对照：

| 参数 | 派生 GPU |
|---|---|
| `--fingerprint=279042489`（matchseed 的环境） | NVIDIA RTX 4060 Laptop |
| 加上 `--fingerprint-platform=macos`（真实启动） | **Apple M2 / Metal** |

后果：`matchseed` 报"找到同族种子"（宿主机 NVIDIA，派生 NVIDIA），
建成 profile 启动后是 Apple M2 —— 跨厂商，正是
[gpu-family-cloudflare.md](gpu-family-cloudflare.md) 判定被拦的条件。
筛选的结论对真实启动**不成立**，而且没有任何提示。

那次种子随机抽到 macOS 机型档案（档案库 15 条里 5 条是 macOS），
所以这不是罕见路径。声称 macOS 却在 Windows 上跑 D3D11 渲染，
比 Intel/NVIDIA 跨厂商更容易被识别。

**做法：筛选时把平台参数一起传。** 新增
[cmd/matchseedplat](../cmd/matchseedplat/main.go)：
`go run ./cmd/matchseedplat windows 24`。实测在 windows 平台下第 2 个候选
命中（种子 `1123774884` → RTX 4060 Laptop），启动后实测确认就是 RTX 4060。

原 `matchseed` 没改 —— 它作为"这台机器能不能筛出同族种子"的可行性探测仍然有效，
但**不能用它的结果直接建 profile**。

## 2. `cmd/launchdebug` 漏传 `DeviceLabel`，锁定机型静默失效

```go
// 错的（原样）
fp := fingerprint.DeriveForKernel(p.Seed, resolvedGeo, k.Version)
// 对的（session.go 的 GUI 路径一直是这个）
fp := fingerprint.DeriveWithDeviceLabel(p.Seed, resolvedGeo, p.DeviceLabel, k.Version)
```

`DeriveForKernel` 不看 `p.DeviceLabel`，直接按种子抽机型。实测锁定
`Windows 11 / RTX 4060 Laptop`，`launchdebug` 启动后报
`Windows 11 / GTX 1660 入门桌面` —— **界面里锁了机型，命令行启动却是另一台设备**，
无任何警告。已修，并补上 GUI 路径同款的"档案已不在库中"警告。

这是本项目 memory 里反复出现的那类错误模式（见
[system-chrome-daily-mode.md](system-chrome-daily-mode.md) 的"同类状态残留只修一半"）：
**GUI 路径和 CLI 路径各自实现同一件事，其中一条漏了字段。**
`cmd/` 下其余工具值得照这条查一遍。

## 3. 机型档案里的 GPU 字段确实不生效（README 说法证实）

锁定 `Windows 11 / RTX 4060 Laptop`，实测派生出
`ANGLE (Intel, Intel(R) Iris(R) Xe Graphics ...)`。所以**锁机型控制不了 GPU 厂商**，
只有筛种子能。README 里"catalog.go 对应字段保留但不生效"这条是准确的。

推论：要同时控制平台和 GPU 厂商，得先用 `-device` 锁平台、再在该平台下筛种子。
两件事都要做，只做一件不够。

## 4. 真阻碍是机房出口，不是指纹

绕了一大圈才定位到。同一个 profile（种子 `1123774884` / RTX 4060 / Windows /
Europe-Berlin），唯一变量是出口：

| 出口 | 目标站 Turnstile |
|---|---|
| 直连 | **8 秒过**，token 816 字符 |
| 德国某机房代理（ASN 归属 `hosting`） | 18 秒不过，token 恒 0 |

`checkproxy` 早就打了"出口是机房 IP，多账号场景极易被识别"的警告，
它是对的。**先做单变量对照，再动指纹配置** —— 为验"指纹跨厂商"这个错假设
花了约 40 分钟筛种子建 profile。

## 5. `scrapingcourse.com/cloudflare-challenge` 已失去判别力

[gpu-family-cloudflare.md](gpu-family-cloudflare.md) 推荐它作为端点，
但 2026-07-28 实测**普通系统 Chrome 直连也被拦**（"Just a moment…"）。
所有配置都拦 = 不能用来区分好坏。用它做基线会得出"什么配置都过不了"的
错误结论，掩盖真正的变量。

那条 memory 里"端点选择"一节需要更新。**判别力要先用裸跑基线验一次**
再拿它下结论 —— 端点的有效性会随 CF 规则变化而失效。

## 6. 顺带：命令行建 profile

面板原本是唯一的 profile 创建入口，无 GUI 环境（自动化流程）没法备环境。
新增 [cmd/mkprofile](../cmd/mkprofile/main.go)，复用
`store` / `fingerprint` / `model` 三个包，所以校验规则、DPAPI 加密、
种子派生与面板一致，不是绕过它们直写 SQLite：

```bash
go run ./cmd/mkprofile -name cf-nv2 -seed 1123774884 \
  -device "Windows 11 / RTX 4060 Laptop" -proxy <代理行> -kernel 148.0.7778.215
```

沿用面板的两条 fail-closed：机型档案不存在直接拒绝（不静默回退），
重名直接拒绝（`launchdebug` 按名字查，重名会静默取第一个匹配）。

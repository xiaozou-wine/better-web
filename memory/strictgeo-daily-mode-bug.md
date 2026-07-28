# StrictGeo 对日常模式的误拦

2026-07-27 排查。现象：日常模式的 `USA` profile 点启动，报「查询代理出口地失败，中止启动以避免时区与出口地矛盾」，三个查询服务全部 `context deadline exceeded`。而同一个代理用「测试代理」按钮是通的。

## 根因：fail-closed 检查没分模式

`internal/session/session.go` 的 `resolveGeo` 里，出口探测失败后的 `StrictGeo` 分支对所有 profile 一律生效。但这个检查的理由只对指纹模式成立：

- `internal/launcher/args.go` 的 `BuildArgs` 只在 `p.Kind == model.KindFingerprint` 分支里注入 `--timezone` / `--lang` / `--accept-lang`
- 日常模式（`KindDaily`）不注入任何地理参数，时区取自本机，探测结果只写进 `Status.Exit` 供界面展示

所以日常模式下探测失败实际损失的只是一条「出口是否机房 IP」的提示，代价却是拒绝启动。

同一个函数里 `GeoOverride` 路径已经做对了这个判断，注释写着「地理信息已经有了，启动的前提已满足，缺的只是附加的风险提示」。**漏的是日常模式这条同类情形**——两者的共同特征是「时区不来自探测结果」，判断依据应该是这一点，而不是笼统的「探测失败」。

## 为什么是间歇性的

`internal/geo/lookup.go` 的 `LookupTimeout = 10 * time.Second`。用 `cmd/checkproxy` 连测该 socks5 代理 4 次：2.7s / 3.2s / 5.0s 成功，1 次 12s 超时。延迟正好卡在超时边缘，于是"有时能启动，有时报错"。

**排障线索：** 反复启动失败但「测试代理」偶尔能通 = 代理延迟卡在 10 秒边缘，换代理比重试有用。这条已写进 `docs/usage.md`。

## 修复

日常模式探测失败降级为警告照常启动，指纹模式的 fail-closed 一字未动。回归测试在 `internal/session/dailygeo_test.go`，两条：

1. 日常模式探测失败仍能启动且留下警告
2. 锁住「日常模式不注入地理参数」这个前提——降级逻辑的成立依赖于它，将来若有人给日常模式加时区注入，这条会失败以提醒同步改 `resolveGeo`

第 2 条是刻意加的：单靠第 1 条的话，前提被推翻时不会有任何信号，降级就悄悄变成了"用本机时区配外国出口"的矛盾。

## 附带发现

`USA` 的出口属于某 IDC 机房 ASN，机房 IP。日常浏览无妨，养账号风险高。

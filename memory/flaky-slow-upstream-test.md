# 全量测试里 internal/proxy 会间歇性变红

2026-07-28 发现。`go test ./...` 报 `TestForwarderSurvivesSlowUpstreamHandshake` 失败：

```
上游握手耗时 12s（< dialTimeout 20s）本应成功，实际失败:
socks connect tcp ...: EOF
```

**不是代码问题。** 单独 `go test -run TestForwarderSurvivesSlowUpstreamHandshake -count=3 ./internal/proxy/` 连跑三次全过。

## 原因是 CPU 争抢

这个测试用 `startSlowSOCKS5` 真等 12 秒——刻意选在 `clientHandshakeTimeout`(10s) 与 `dialTimeout`(20s) 之间，验证两段超时没有被同一个 deadline 罩住。预算只剩 8 秒余量。

而全量跑时同一台机器上并行着两个启真实浏览器的包：`internal/session` 约 412s、`internal/probe` 约 25s、`internal/app` 约 57s。争抢把这 8 秒余量吃掉了。

## 怎么办

改动没碰 `internal/proxy` 时看到这条失败，先单独重跑该包确认，别去改超时值——把 12s 往下调会削弱这条回归测试的意义（它测的就是"落在两个超时之间"这个区间）。

真要修，方向是让全量测试不并行跑重量级包（`-p 1` 或给这类测试加 build tag 移出默认集），而不是动被测代码的超时常量。目前没修。

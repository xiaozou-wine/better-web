# 贡献指南

## 提 issue 之前

带上内核版本（界面「关于」或 `go run ./cmd/listkernels`）、操作系统版本、
以及复现步骤。指纹类问题请附 `go run ./cmd/scoreprofile <名称>` 的输出。

**不要在 issue 里贴真实代理地址、账号密码或未脱敏的指纹采集结果。** 示例 IP
请用 [RFC 5737](https://www.rfc-editor.org/rfc/rfc5737) 保留段
（`198.51.100.0/24`、`203.0.113.0/24`）。

## 提 PR 之前

```bash
cd frontend && npm install && npm run build && cd ..   # 首次，生成 embed 需要的产物
go build ./...
go test ./...
go vet ./...
cd frontend && npx svelte-check
```

四项都要过。需要真实内核的测试在未安装内核时自动跳过，这是预期行为。

前端产物那步不能省：`main.go` 用 `//go:embed all:frontend/dist`，而该目录不入库，
没构建过的话 `go build ./...` 会直接失败。只改命令行工具时可以用
`go build ./cmd/...` 绕过。

改动 Go 侧导出类型后跑 `wails generate module` 重新生成前端绑定，并把生成结果
一起提交。

## 代码约定

- 逻辑改动带测试，bug 修复带回归测试。
- 匹配现有风格，不顺手重构无关代码。
- 注释说明意图和边界条件，不复述代码在做什么。
- 日志和错误信息里的代理密码必须脱敏。现有的
  `TestProfileViewNeverExposesProxyPassword`、`TestExportBundleOmitsPassword`
  一类测试是防线，不要绕过。
- `internal/probe/testdata/` 下的采集结果如需更新，先确认里面没有你本机的
  精确 GPU 设备 ID 和 canvas/WebGL 哈希——那些是机器级标识。

## 内核升级

见 [README 的开发一节](README.md#开发)。必须跑 `TestBaselineHasNotDrifted`
确认已有 profile 的指纹没有漂移。

## 范围之外

平台风控规避、批量注册、刷量类功能请求不予受理，见
[README 的用途声明](README.md#用途声明)。

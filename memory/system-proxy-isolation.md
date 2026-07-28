# 系统代理与本项目的隔离关系

2026-07-27 排查。起因是 Edge 首页显示海外异地出口，怀疑 better-web 的代理泄漏到了系统层。

## 结论：无关，是本机的全局代理

本机注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`：

```
ProxyEnable   = 0x1
ProxyServer   = 127.0.0.1:10808
ProxyOverride = <local>;localhost;127.*;10.*;172.16.*~172.31.*;192.168.*
```

10808 是常见本地代理客户端的默认 SOCKS 端口。本机某个代理客户端设置了系统级全局代理，Edge 走它，所以显示的是该客户端当前节点的出口（海外某城市）。

## 本项目不影响系统出口，三条证据

1. 全库搜不到任何修改系统代理的代码——`ProxyEnable`、`ProxyServer`、`InternetSetOption`、`WinHttpSetDefault`、`netsh winhttp` 均无命中。
2. 唯一涉及代理的地方是 `internal/launcher/args.go` 的 `--proxy-server=`，那是给 Chromium 进程的命令行参数，只作用于它自己。
3. 转发器只绑 `127.0.0.1` 的随机端口（`internal/proxy/forwarder.go`），且注释里写了理由：它不带认证，绑 `0.0.0.0` 会让同网段任何人白用。

所以 Edge 的出口和 better-web 某个 profile 的出口是两条独立链路。

## 一个隐含依赖，改系统代理时要注意

`ProxyOverride` 里含 `127.*`，因此本机回环流量不走系统代理。这恰好让两处不受干扰：

- `internal/proxy` 的转发器（浏览器连本地端口）
- `internal/probe` 的本地采集与跑分服务（页面 POST 回 `127.0.0.1:随机端口`）

**如果哪天把 `127.*` 从 ProxyOverride 里去掉，这两处会被系统代理拦。** 表现会是采集超时或转发器连不上，而不是明确报错，排查起来不直观。

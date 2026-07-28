# 系统链接接管

2026-07-28 实现并实测。记录的是无法从代码看出的平台约束与已验证的行为。

## 不能编程把自己设成默认浏览器

只能注册成**候选**。`HKCU\...\UrlAssociations\https\UserChoice` 的 `Hash` 值算法未
公开，Win11 起还有 UCPD（User Choice Protection Driver）拦截对该键的直接写入。
这是微软有意的设计，不是能绕的技术问题——绕过去的后果是系统判定关联已损坏、
回退到 Edge。

来源：[MS Default Programs](https://learn.microsoft.com/windows/win32/shell/default-programs)、
[UCPD 分析](https://binary.ninja/2025/03/25/default-browser-upcd.html)

因此界面必须把「已注册为候选」与「已是系统默认」分成两个状态显示。合成一个的话，
「已注册但没生效」这个最常见的中间态无法表达，而那正是需要提示用户去手动选的时候。

## 注册表写 HKCU 而非 HKLM

按 MS 文档，安装完成后再改机器级关联会失败。per-user 才是正确层级，且不需要提权。
`ApplicationDescription` 是必填项——缺它的应用不会出现在默认应用候选列表里，
而 Register 本身不报错，表现是「注册成功但设置里找不到」。

`http` 与 `https` 必须都声明。只声明其一时系统不把它当浏览器候选，
「新浏览器已注册」的系统通知也不会出现（触发条件是同时接管两个协议）。

注册后要调 `SHChangeNotify(SHCNE_ASSOCCHANGED)`，否则新注册的应用要等很久
（或到下次登录）才出现在设置页里，用户会以为注册失败。

## 递送给已运行实例：实测确认

内核 148.0.7778.215 上用 CDP 枚举 target 验证：

| 命令（对已运行的同一 user-data-dir） | 结果 |
| --- | --- |
| `--new-window <URL>` | 打印 `Opening in existing browser session.`，新窗口打开该 URL |
| `--incognito <URL>` | 同上，开无痕窗口 |

递送进程约 1 秒退出（端到端实测 0 秒）。**这条路径绕过本项目的跨进程锁**，
且不会损坏 user-data-dir——递送方根本不打开那个目录，只是把命令行交给 singleton。

**递送时不能带代理与指纹参数。** 已运行实例的代理和指纹在它自己启动时就定好了，
中途改不了，传过去会被忽略。传了只会留下「传了但没生效」的错误排障线索，
所以 `launcher.HandoffArgs` 干脆不传。有测试钉住这一点。

`--incognito` 与 `--new-window` 不能同时传：`--incognito` 本身就开一个新的无痕窗口，
两个都传时 Chromium 会开一个普通新窗口再开一个无痕窗口，用户看到两个。

## 判断「是否已在运行」用锁文件而非会话表

`--open-url` 是个刚起来的短命进程，它的 `session.Manager` 会话表必然是空的，
只有跨进程锁文件能反映真相。为此导出了 `session.HeldByOtherProcess`。

这个判断有竞态（返回后持有者可能立刻退出），但在本用途下无害：递送失败时
Chromium 会自己起一个新实例，而那正是本来想要的结果。

## 指纹模式 + 无痕是要拒绝的组合

无痕只是不落盘，指纹伪造与代理照旧生效——出口 IP 与指纹一个字不变，
反而丢掉了养号需要的登录态。用户以为「无痕更干净」，实际得到的和以为的不是一回事。
这与 fail-closed 拦的是同一类问题（以为得到了实际没得到），因此拒绝而非警告。

在三处拦：`app.SetURLHandler` 保存前拒绝、`launcher.BuildArgs` 与
`launcher.HandoffArgs` 只在 `KindDaily` 时追加该开关。

## 跨包的隐式契约

`urlhandler` 往注册表写的命令行是 `"<exe>" --open-url=%1`，解析它的是 `cli.go`。
internal 包不能被 main 包之外引用，main 包也不能被 internal 引用，因此两处各自
定义常量。漂移的表现是点链接后进程起来又立刻退出、且不报任何错。

为此把命令行拼接抽成 `urlhandler.CommandLineFor`，`cli_test.go` 断言它含
`cli.go` 定义的开关名。同一个测试还断言两个开关不互为前缀——`runCLI` 用
`strings.HasPrefix` 分派，互为前缀会串。

## 一个测试上的选择

`urlhandler_windows_test.go` 真写 HKCU 而非打桩。这个包的全部价值就在于写对
注册表位置，打桩测的是「我以为的位置」。测试先 `Query` 记下原状态，
`t.Cleanup` 里按原状态还原——测试机上用户可能已经开了这个功能，直接
`Unregister` 收尾会抹掉他的配置。

不写 `UserChoice`，所以跑测试不会真的抢走默认浏览器。

## 已验证

```
--help                          含 --open-url 说明
--open-url=file:///...          拒绝，退出码 1
--open-url=javascript:...       拒绝，退出码 1
--open-url=https://...（未配置） 报错并指引去设置，退出码 1
--open-url=https://...（冷启动） 起浏览器，注入 --proxy-server=socks5://127.0.0.1:<转发端口>
                                与 --disable-non-proxied-udp，URL 在参数末尾，写入锁文件
--open-url=https://...（已运行） 0 秒返回，锁文件 PID 不变，新窗口打开
注册表往返                       Register/Query/Unregister 全通，注销后无残留
```

## 踩过的坑

守护进程用 `timeout 75` 包起来测递送时，`timeout` 到点杀掉守护进程会把浏览器一起
带走，于是第二次调用实际走的是冷启动（30 秒）而非递送。看到「递送耗时 30 秒」
先核对浏览器进程是否还活着。

`taskkill` 不带 `/F` 只发 WM_CLOSE，GUI 子系统程序（better-web 是）收不到，
进程不会退。

## 待办：Edge / Firefox 支持

2026-07-28 用户明确表示「之后打算加 edge 或者 firefox 支持，先不加」。当前
`internal/kernel` 只认 fingerprint-chromium 与官方 Chrome。

动手前先想清楚这件事：**代理注入与目录隔离靠的是 `--proxy-server` 与
`--user-data-dir`，这是 Chromium 系专有参数。**

- Edge 是 Chromium 系，两个参数都认，加个路径探测（`App Paths\msedge.exe`）基本够用。
- **Firefox 不认这两个参数**，会当未知参数忽略——浏览器照常打开，但代理没走、
  目录没隔离，且没有任何可见痕迹。这正是本项目 fail-closed 原则要拦的那类故障。
  Firefox 要走 profile 参数（`-profile <dir>`）与 prefs.js 里的代理配置，
  是另一套机制，不是加个路径探测就行。

所以加 Firefox 时必须同时决定：非 Chromium 内核走哪条隔离路径，以及探测不到时
是 fail-closed 拒绝还是回退。这个决策当时没定，留给实现时再问。

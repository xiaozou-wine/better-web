# 日常模式走系统 Chrome，与那道 fail-closed

## 为什么加

日常模式（`KindDaily`）本就不做任何指纹伪造、只要目录隔离，所以没有理由用指纹内核。
而指纹内核基于 ungoogled-chromium，代价是登不了 Google 账号、没有同步、可能缺 DRM。

用户的真实诉求是「管理多个 Google 浏览器账号」，不是反检测。官方 Chrome 加独立
`user-data-dir` 就能满足：cookie、登录态、扩展完全隔离，可同时开多个。

**代理与目录隔离照旧生效**——转发器、出口探测、时区对齐都在 Go 侧完成，与用哪个
浏览器无关。这是这个改动成本低的根本原因。

## 实测修正了一个判断

我最初说"换系统 Chrome 能解决版本落后"，探测后发现**宿主机的系统 Chrome 也停在
148.0.7778.179**（官方稳定版当时已到 150），可能关了自动更新。

所以实际收益只有 Google 账号 + 同步 + DRM，**不含版本更新**。这条值得记：
别再把"用系统 Chrome"当成解决内核滞后的手段。

## ungoogled 的边界改不了

浏览器级的 Google 登录与同步不可用，是因为 ungoogled-chromium 编译时不带 Google 的
OAuth client ID 与 API key。这是上游存在的目的，不是缺陷。

网页里登 Google（打开 accounts.google.com 用 Gmail、YouTube）**是可以的**，走的是
普通 cookie。不能用的只是"浏览器设置里登录账号并同步"。

塞 `GOOGLE_API_KEY` / `GOOGLE_DEFAULT_CLIENT_ID` 环境变量理论上能绕，但官方 key
不允许第三方构建使用，自己申请的过不了 Google 对浏览器登录的白名单，实际大概率
仍登不上。而且即便能登，同步会把书签密码历史往 Google 传并带上设备标识，
等于在一个做多账号隔离的工具里主动建立跨 profile 关联通道——方向本身是错的。

## fail-closed 为什么要三道闸

`--fingerprint` 是打进 Chromium C++ 源码的补丁，官方二进制不认识它，会当成未知参数
**静默忽略**——页面照常打开、脚本照常执行、不报任何错，但报出的是宿主机真实指纹、
真实 GPU、真实时区。

这比代理失效更隐蔽：代理挂了至少能从出口 IP 看出来，浏览器用错什么痕迹都没有，
等发现时账号可能已被关联。所以按三层拦：

| 位置 | 拦什么 |
| --- | --- |
| `CreateProfile` / `UpdateProfile` | 落库前拒绝，不留下启动时才报错的记录 |
| `session.start` 开头 | **先于内核解析**——否则报的是「未找到内核」，把用户引向错误方向 |
| `session.start` 解析后 | 核对内核确实支持伪造，拦住内核目录里混入的非 fingerprint-chromium 构建 |

编辑路径同样能造出非法组合：把日常 profile 改成指纹模式时，原有的
`UseSystemBrowser` 会留下来，所以 Update 也要校验。

系统 Chrome 找不到时**报错而非回退到指纹内核**：用户选它是为了 Google 账号与同步，
静默换回去后这些能力全没有，而界面显示的仍是「系统 Chrome」。

## 探测走注册表而非硬编码路径

`App Paths\chrome.exe`，HKLM 与 HKCU 都查——Chrome 支持用户级安装（装到
`%LOCALAPPDATA%`）和自定义位置，写死 Program Files 会漏掉。注册表查不到时兜底
三个默认位置（便携部署、企业分发有时不写注册表）。

不用 `StartMenuInternet`：它的 command 值带引号和参数，要额外解析。

版本号从安装目录下的版本子目录名读，不执行 `chrome.exe --version`——后者在 Windows
上不往 stdout 输出版本，这是 Chrome 的已知行为。

`--custom-ntp` 是 ungoogled 专有补丁，用系统 Chrome 时不能传，否则 Chrome 会弹
"使用了不受支持的标记"提示条，而那个提示本身就是异常信号。

## 我犯过的错误模式：同类状态残留只修了一半

前端有两个"勾选后切换 profile 类型会残留"的字段：

- `useSystemBrowser`（仅日常模式可用）
- `matchHostGPU`（仅指纹模式可用）

第一次我只给 `useSystemBrowser` 加了 `$derived` 归零，`matchHostGPU` 漏了。
结果用户在指纹模式勾了 GPU 匹配、切成日常模式后无法保存，Go 侧报
「日常模式不伪造指纹，无法按宿主机 GPU 筛选种子」。

教训：**发现一处"界面隐藏不等于状态清零"的 bug 时，要同时检查有没有对称的另一处。**
光隐藏控件不够，勾选状态还在变量里，用户切换类型后就会提交非法组合。

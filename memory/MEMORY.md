# better-web Memory Index

基于 fingerprint-chromium 的指纹浏览器管理器（Wails + Go + Svelte-TS）的长期知识。

只记录无法从代码或 Git 历史直接推导的内容：设计决策的理由、非显然的踩坑、以及实测得出的能力边界。功能说明与实测数据在项目 `README.md` 里，那份是随代码演进的文档；本目录记的是「为什么这么做」和「踩过什么坑」。

## 条目

- [CLI 直启、快捷方式与图标](cli-shortcut-and-icon.md) — 直启为何必须阻塞、PowerShell 注入与路径逃逸防护、图标逐尺寸独立渲染、IconLocation 是冗余项、wails build 失败导致的误判
- [系统代理与本项目的隔离](system-proxy-isolation.md) — Edge 显示异地出口的真实原因、本项目不影响系统出口的三条证据、ProxyOverride 含 `127.*` 这个隐含依赖
- [StrictGeo 对日常模式的误拦](strictgeo-daily-mode-bug.md) — fail-closed 检查未分模式导致日常模式被误拦、判断依据应是「时区是否来自探测」、10 秒超时边缘造成的间歇性失败
- [前端设计系统与亮暗双主题](frontend-design-system.md) — 主题偏好为何存三态而非解析结果、accent 默认态比 hover 更深的原因、层次判断不能用线性亮度差、`--h-topbar` 与侧栏 sticky 的耦合、mock Wails 绑定的两个坑
- [回程丢包与转发器 deadline 缺陷](socks5-return-path-loss.md) — 「裸 TCP 通但代理不通」的判读、换端口为何无效、hy2 与 SOCKS5 同出口只差传输层、`handle` 用一个 deadline 罩住两段不同预算、转发错误静默丢弃导致不可观测、单侧观察三次误判的教训
- [系统链接接管](url-handler.md) — 为何只能注册成候选而不能设为默认（UserChoice 的 Hash 与 UCPD）、递送给已运行实例的实测结论、递送路径为何不能带代理参数、指纹模式拒绝无痕的理由、跨包 flag 契约的钉法、Edge/Firefox 支持的待办与前置决策
- [代理的一行粘贴，与被拒的标签块格式](proxy-line-input.md) — 解析为何只留后端一份、`ParseProxyLine` 为何不走 `svc()`、带字段名的多行块为何否掉（缺用户名时歧义无解）
- [全量测试里 internal/proxy 会间歇性变红](flaky-slow-upstream-test.md) — CPU 争抢吃掉 8 秒余量、不要下调那个 12s
- [GPU 厂商族决定能否过 Cloudflare](gpu-family-cloudflare.md) — 判据是跨厂商而非存在伪造、A–O 十五组对照、伪造只改两个字符串而 pixelHash/扩展/WebGPU 全是真值、`lies=0` 不代表能过商业风控。**「端点选择」一节已过期，见下一条**
- [种子筛选的平台缺口与两个静默失效](seed-screening-platform-gap.md) — `matchseed` 不传 `--fingerprint-platform`，筛出的同族种子在真实启动时可能变跨厂商（同一种子 macos 平台下派生 Apple M2 而非 NVIDIA）；`cmd/launchdebug` 漏传 `DeviceLabel` 导致锁定机型静默失效（已修）；锁机型控制不了 GPU 厂商，两件事都要做；**真阻碍是机房出口不是指纹**（同 profile 只改出口：直连 8 秒过 / 机房代理 18 秒不过）；`scrapingcourse.com` 对裸 Chrome 也拦，已无判别力
- [日常模式走系统 Chrome](system-chrome-daily-mode.md) — 收益是 Google 账号而非版本更新（系统 Chrome 也停在 148）、fail-closed 为何要三道闸、注册表 App Paths 探测、同类状态残留只修一半的错误模式
- [GUI 程序调控制台命令会弹窗](console-window-flash.md) — 四处调用点而用户只报了一处、GPU 采集为何不能用 `--headless`、跑分路径要求相反

# 使用指南

面向使用者。技术设计、实测数据与代码约束见根目录 [README.md](../README.md)。

## 先认清这是什么

better-web 是一个**启动器面板**，不是浏览器本身。

```
双击 better-web.exe
  └─ 面板窗口：profile 列表、新建、编辑、启动、停止
       └─ 点「启动」后弹出的是一个独立的 Chromium 窗口 ← 浏览器在这里
```

面板和浏览器是两个进程、两个窗口。面板里没有地址栏和标签页，它管的是配置；
真正的浏览体验来自 Chromium 本身，标签页、扩展、书签、开发者工具一应俱全。

关掉面板不会关掉已启动的浏览器窗口，反之亦然。但面板退出时会主动停止它启动的
全部会话，所以别在跑着 profile 时关面板。

## 首次使用

### 1. 装内核

指纹能力来自打过补丁的 Chromium，不随程序分发（约 190MB 压缩包 / 425MB 解压后）。

界面在检测不到内核时会显示下载入口，点「查看可用版本 → 下载并安装」即可。
下载走 GitHub，国内网络通常需要代理。

也可以手动装：从 [fingerprint-chromium releases](https://github.com/adryfish/fingerprint-chromium/releases)
下载对应平台的包，解压后把**内容**（不是外层目录）放到

```
%APPDATA%\better-web\kernels\<版本号>\chrome.exe
```

版本号目录名必须与实际版本一致，例如 `148.0.7778.215`。

### 2. 建第一个 profile

点「新建 Profile」，两种类型选一个：

| 类型 | 用途 | 行为 |
| --- | --- | --- |
| **指纹模式** | 多账号、需要隔离身份 | 生成随机种子，推导出一整套自洽的伪造环境 |
| **日常模式** | 当普通浏览器用 | **不注入任何指纹参数**，就是原生 Chromium，只做 profile 目录隔离 |

类型创建后不可更改。种子也不可更改——它是这个 profile 的设备身份，
改了等于换了一台机器，已登录的账号会看到设备变更。要新指纹就新建 profile。

### 3. 配代理（指纹模式几乎必须）

勾选「使用代理」，填协议、主机、端口、用户名、密码。

保存前**先点「测试代理」**。它会经该代理查出口 IP，返回三样东西：

- 出口国家与推导出的时区、语言（启动时会自动应用，不用手填）
- 出口 IP 的类型判定：住宅 / 机房 / 未知
- 判定为机房 IP 时给出警告

机房 IP 在多账号场景下极易被识别，警告要当真。判定基于组织名关键词，会有漏判，
所以「未知」不等于安全。

带密码的代理会经本地转发接入——Chromium 的 `--proxy-server` 不支持密码认证，
这一层是必需的，你不需要做任何配置。

## 日常操作

| 操作 | 说明 |
| --- | --- |
| 启动 | 起转发器 → 查出口地 → 推导指纹 → 启动浏览器。慢速代理下可能要等几秒 |
| 停止 | 投递关闭消息让浏览器正常退出并落盘，不是强杀 |
| 编辑 | 可改名称、代理、地理覆盖、内核版本、备注。种子与类型不可改 |
| 删除 | **只删配置记录，保留浏览数据**（Cookie 与登录态在磁盘上） |

列表按最近使用排序。每张卡片显示该 profile 推导出的机型、时区、语言、种子，
以及代理地址和是否已配认证。

### 启动失败时

最常见的两种：

**「查询代理出口地失败，中止启动」** —— 代理不通。这是故意设计的：
指纹模式要把出口地写进 `--timezone` / `--lang`，查不到就无法对齐，
时区与真实出口不符比不启动更糟，所以宁可失败也不静默回退到默认值。
先用「测试代理」确认代理本身可用。

只有**指纹模式**这样拦。日常模式不注入时区语言（用的就是本机真实环境），
出口地只用来提示是否机房 IP，查不到便降级成一条警告照常启动。
指定了地理覆盖的 profile 同理——启动前提已满足，缺的只是附加提示。

代理时快时慢（探测超时是 10 秒）时会间歇性触发这条。反复失败但「测试代理」
偶尔能通，说明代理延迟卡在超时边缘，换个稳定的代理比重试有用。

**「该 profile 已在运行」** —— 同一个 `user-data-dir` 被两个进程打开会损坏数据，
所以拦住了。先停止再启动。

注意这个保护**只在同一进程内有效**：同时从界面和命令行启动同一 profile 时，
第二个实例会因目录被占用而直接退出，不会有友好报错。

## 什么会变、什么不会变

两条独立的线，混淆它们会导致配置出错：

| 跟着**种子**变 | 跟着**代理出口**变 |
| --- | --- |
| 机型、CPU 核数、UA、品牌 | 时区 |
| canvas / audio 指纹 | 语言、Accept-Language |
| GPU 型号、内存 | |

所以换代理不会改变设备身份，只会改变地理位置——这符合"同一台设备换了个地方上网"，
是合理的。而换种子等于换设备，会破坏账号的设备连续性。

## 已知局限

用之前应当知道的：

**同一台机器上的多个 profile 共享真实的屏幕分辨率与 GPU 型号。**
这两项内核不支持伪造（`screen.*` 有开关但未实现，GPU 参数在内核 144 被移除）。
如果多账号被平台关联，这是最可能的原因。`patches/019-screen-fingerprint.patch`
写了分辨率的补丁，但需要自行构建内核才生效，且未经实际编译验证。

**TLS 指纹（JA4）所有 profile 完全相同。**
转发器是纯 TCP 隧道、不解密 TLS，这是有意的——任何 TLS 中间人都会把 ClientHello
改写成 Go 标准库的形状，等于主动标记自己不是 Chrome。代价是链路层无法按 profile 变化。
这是整个品类的边界而非本项目的缺陷。降低这类关联靠使用方式（不同网段的出口、
错开活动时间、避免批量同时操作），不靠改指纹。

**Audio 指纹只有 5 个取值，不能作为 profile 的区分依据。** Canvas 是 100% 唯一的，
判断两个 profile 会不会被关联看 canvas。

**Cookie 与登录态在所有平台都是明文存储**，那是 Chromium 自己的格式。
代理密码在 Windows 上经 DPAPI 加密，但这不构成放松对数据目录警惕的理由。
`%APPDATA%\better-web` 整个目录不要同步到网盘或共享位置。

**界面上还没有的入口**：选择机型（只能由种子随机决定）、清除浏览数据、
批量启停。前两项后端已实现，第三项有命令行工具 `cmd/multilaunch`。

## 能同时开几个

取决于内存而非本项目。每个实例主进程约 100–116MB，但 Chromium 会派生渲染、GPU、
网络子进程，实际总占用通常 300–500MB。16GB 内存大约 20–30 个。

另一个限制是代理带宽：每个实例跑自己的请求，共享上游会互相挤。

## 排障

界面之外有几个只读的终端工具，都不回显代理密码：

```bash
go run ./cmd/dumpprofiles          # 列出全部 profile 及其推导出的环境
go run ./cmd/checkproxy            # 逐个检测代理连通性与出口质量
go run ./cmd/scoreprofile <名称>   # 用该 profile 的真实配置在检测站跑分
go run ./cmd/identifykernel [端口] # 确认某端口后面是指纹内核还是普通 Chrome
```

**怀疑指纹没生效** —— 跑 `scoreprofile`。关键看 CreepJS 的 `lies` 是否为 0
（衡量伪造项之间有无矛盾）以及 `headless` / `stealth` 是否为 0%。
`likeHeadless=25%` 是正常的，真实桌面 Chrome 同样如此。

**怀疑某个站点识别出了自动化** —— 编辑 profile，用「关闭指定的伪造」逐项排除
（font / audio / canvas / clientrects / gpu），二分定位是哪一项触发的。
生产环境不要保持关闭状态，每关一项就少一层伪装。

**怀疑走的是真实 IP** —— 在浏览器里访问任意查 IP 的站点。代理不可用时启动会失败
而不会退化成直连，所以能打开页面就说明代理在工作。

## 自动化接入

先选对入口，两个工具面向不同场景：

| | `cmd/ephemeral` | `cmd/launchdebug` |
| --- | --- | --- |
| 身份 | 每次现造，不落库，用完即删 | 库里已有的 profile |
| 适合 | 脚本驱动、并发、一次一套新指纹 | 手工排障 |
| 跨进程锁 | 有（走 `session.Manager`） | **无**，两个实例可同开一个 user-data-dir |
| CDP 端口 | 默认由内核分配，不打架 | 写死 9222，不检测占用 |
| 输出 | 一行 JSON，供脚本解析 | 人类可读文本 |

**自动化用 `ephemeral`。** 下面先讲它。

### cmd/ephemeral：给脚本驱动的一次性实例

```bash
go run ./cmd/ephemeral --proxy=<代理行> [--cdp-port=N] [--kernel=版本] [--device=<标签>]
go run ./cmd/ephemeral --allow-direct
```

出口策略是 fail-closed 的：不给 `--proxy` 就必须显式 `--allow-direct`，否则拒绝启动。
静默用本机真实 IP 会把账号和本人关联起来，而那种失败没有任何可见痕迹。

就绪后往 stdout 打一行 JSON，**不必猜端口也不必解析人类可读输出**：

```json
{"event":"ready","cdpPort":51234,"cdpUrl":"http://127.0.0.1:51234",
 "pid":12345,"profileDir":"...","seed":1123774884,
 "exitIp":"198.51.100.10","exitKind":"hosting",
 "timezone":"Europe/Berlin","locale":"de-DE","device":"Windows 11 / RTX 4060 Laptop",
 "warnings":["出口是机房 IP（...），多账号场景下极易被识别"]}
```

`cdpUrl` 可直接交给 `connect_over_cdp`。字段是接口，只增不改。

**stdout 上只有这一行**，诊断信息全部走 stderr。所以脚本读一行 stdout 解析 JSON
就够了，不用过滤日志噪声。

两个字段值得脚本主动检查：`exitKind` 为 `hosting` 说明出口是机房 IP，多账号场景极易被
识别；`warnings` 非空时不阻断启动，但应当记进日志。

**生命周期：转发器跑在 `ephemeral` 进程里。** 用完给这个进程发终止信号，
不要直接杀浏览器——杀错了会留下孤儿转发器，或让浏览器悄悄失去代理。
`--timeout` 可以设运行时长上限，到点自动退出，适合防脚本挂死。

`--keep-dir` 保留临时目录便于排查，注意里面含 Cookie 与登录态。

### cmd/launchdebug：跑已有 profile

```bash
go run ./cmd/launchdebug <profile 名称> [端口]   # 默认 9222
```

用该 profile 的完整配置启动并开放 CDP 调试端口。实测开启 CDP 不影响 CreepJS 与
BrowserScan 的成绩，但商业风控（Cloudflare、DataDome）是否有更强的 CDP 检测未经验证。

启动后连它，**用 connect 而不是 launch**——launch 会起一个全新的浏览器，
指纹和代理都不是这个 profile 的：

```js
// Playwright
const browser = await chromium.connectOverCDP('http://127.0.0.1:9222')

// Puppeteer
const browser = await puppeteer.connect({ browserURL: 'http://127.0.0.1:9222' })
```

**接管已有的 context 和 page，别新建。** `browser.newContext()` 拿到的是干净环境，
这个 profile 的 cookie 和登录态都不在里面：

```js
const ctx = browser.contexts()[0]
const page = ctx.pages()[0] ?? await ctx.newPage()
```

脚本跑完只 `disconnect`，不要 `browser.close()`——那会连带关掉浏览器，
`launchdebug` 的转发器清理逻辑就走不到了。正常退出方式是给 `launchdebug` 发 Ctrl+C。

**别用端口号判断实例身份。** 把自动化接到错误的实例上不会报错：页面正常打开、
脚本正常执行，但指纹和出口 IP 都是本机的，等发现时账号可能已经出问题。
用 `identifykernel` 确认——它比对目标实例报告的 CPU 核数与本机真实核数。

### 无界面环境批量备 profile

面板原本是唯一的创建入口。没有 GUI 的场景用 `cmd/mkprofile`：

```bash
go run ./cmd/mkprofile -name acct-01 -proxy <代理行> -kernel 148.0.7778.215
go run ./cmd/mkprofile -name acct-02 -match-gpu           # 筛与宿主机 GPU 同厂商的种子
go run ./cmd/mkprofile -name daily-01 -daily              # 日常模式，不做指纹伪造
```

它复用 `store` / `fingerprint` / `model` 三个包，所以校验规则、DPAPI 加密、种子派生
与面板完全一致，不是绕过它们直写 SQLite。

几条互斥与 fail-closed，都是直接退出而非静默处理：

| 情况 | 行为 | 退出码 |
| --- | --- | --- |
| 缺 `-name` | 拒绝 | 2 |
| `-match-gpu` 与 `-seed` 同用 | 拒绝（前者要自己搜种子） | 2 |
| `-daily` 与 `-seed`/`-match-gpu` 同用 | 拒绝（日常模式不用种子） | 2 |
| `-device` 的机型档案不存在 | 拒绝 | 2 |
| profile 重名 | 拒绝 | 1 |

重名之所以是硬错误：`launchdebug` 按名字查 profile，重名它会取第一个匹配，
于是用错身份且无任何提示。批量脚本里请把退出码当真，别 `|| true` 忽略掉。

### 多实例并发

```bash
go run ./cmd/multilaunch 5 --cdp=2         # 起 5 个，前 2 个开 9222、9223
go run ./cmd/multilaunch 5 --cdp=2:9400    # 起始端口改 9400
go run ./cmd/multilaunch 3 美国 --cdp=1    # 只用名称以「美国」开头的 profile
```

CDP 按实例而非全局开关，需要自动化的才开端口。瓶颈不在本项目——转发器每实例只占
一个监听 socket 加一个 goroutine（20 个启动共 517µs）——而在 Chromium 的内存占用
和每个 profile 各自的代理带宽。

数量超过可用 profile 数时，它会克隆出临时 profile（各自独立的种子与目录），
因为同一 profile 不能重复启动。这一点在自动化里要留意：**别对同一个 profile
并发起两个实例**，user-data-dir 是独占的。

它还会逐个实测并汇总各实例的出口 IP。多个实例共用一个出口，指纹再自洽也会被直接关联，
这个汇总就是为了不让共用 IP 静默发生。

## 合规提醒

工具本身用途正当（隐私保护、广告验证、跨账号运营、爬虫测试），但多账号操作
通常违反具体平台的服务条款。风险在使用场景，不在工具。

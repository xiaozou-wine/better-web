# better-web

基于 [fingerprint-chromium](https://github.com/adryfish/fingerprint-chromium) 的指纹浏览器管理器。
每个 profile 一份独立的 user-data-dir、一套由种子确定性推导的浏览器环境、一条独占的代理链路。

Wails v2 + Go + Svelte-TS。

本文件面向维护者（设计取舍、实测数据、代码约束）。
**要上手使用请看 [docs/usage.md](docs/usage.md)。**

## 用途声明

这个项目面向浏览器隐私保护、指纹检测机制研究，以及你**自己拥有或已获授权**的账号与
系统的多环境管理和自动化测试。

指纹隔离技术同样可以被用于规避平台风控、批量注册、刷量和欺诈。这类用途不在本项目
的目标范围内，作者不提供相关支持，也不接受该方向的功能请求。使用者需自行确保其行为
符合目标平台的服务条款与所在地法律。

本软件按 MIT 许可证「按原样」提供，不含任何担保。

## 已实现的能力

对照 AdsPower 一类成熟产品逐项补齐的结果。每项都有测试覆盖。

| 能力 | 实现位置 | 备注 |
| --- | --- | --- |
| Profile 隔离与指纹推导 | `fingerprint`、`launcher` | 种子 → 自洽环境，机型从档案库整体抽取 |
| 代理链路与地理对齐 | `proxy`、`geo` | 本地转发补认证，时区语言按出口 IP 自动对齐 |
| 分组、标签、搜索 | `store.Query` | 单层分组；多标签取交集；关键词搜名称与备注 |
| 批量启停/删除/分组/标签 | `app.batch` | 启动有并发上限（默认 4），单个失败不中断其余 |
| 代理批量导入 | `app.ImportProxies` | 三种格式，逐个建 profile 并各自生成新种子 |
| 代理一行粘贴 | `model.ParseProxy` | 表单里粘一行拆进各字段，格式与批量导入一致 |
| 配置导出与导入 | `transfer` | 只搬配置不搬浏览数据；导入可选保留或重生成种子 |
| 凭据加密 | `secret` | Windows 走 DPAPI，其他平台如实报告为明文 |
| 机型锁定 | `model.Profile.DeviceLabel` | 按 Label 存储；换机型需显式确认 |
| 启动页与新标签页 | `launcher`、`model.Startup` | 命令行实现，Preferences 方案实测无效 |
| 桌面快捷方式 | `shortcut` | 直接启动指定 profile |
| GPU 厂商族匹配 | `app.gpumatch` | 筛种子使伪造 GPU 与宿主机同族，应对 Cloudflare |
| 内核下载与多版本管理 | `kernel` | 原子安装，含 Zip Slip 防御 |
| 指纹采集与检测站跑分 | `probe` | 真实内核实测，含回归门禁 |
| 一次性实例 + CDP 接入 | `cmd/ephemeral` | 不落库、用完即删，端口默认由内核分配 |

**没做也不打算做**：窗口排列（涉及 Windows 窗口 API，需单独评估）、
RPA 流程编辑器（有 CDP 接入给 Playwright，不重复造）、云端同步
（本地工具，同步凭据与浏览数据的风险大于收益）。

### cmd/ephemeral：给外部脚本驱动的一次性实例

`launchdebug` 跑库里已有的 profile，适合手工排障。自动化场景要的是"一个账号
一套全新指纹、用完就丢"，于是有了这个：每次现造一个不落库的临时身份，
启动后往 stdout 打一行 JSON（CDP 地址、出口 IP、时区、机型），调用方据此
`connect_over_cdp`。

三处与 `launchdebug` 的差异都是自动化场景下必须的：

- 走 `session.Manager` 而非自己 `exec`，因此**有跨进程锁**。`launchdebug` 绕过了
  `session` 包，两个实例可以同时打开一个 `user-data-dir` 而不报错。
- CDP 端口默认 `--cdp-port=0` 由内核分配，从 `DevToolsActivePort` 回读，
  并且确认 `/json/version` 真能应答才算就绪。`launchdebug` 写死 9222 且不检测占用。
- 与 `multilaunch` 不同，**每个实例必须显式指定自己的代理**。那个工具为测量启动
  能力而让克隆实例沿用同一代理，多账号场景下会被平台直接关联。

出口策略是 fail-closed 的：不给 `--proxy` 就必须显式 `--allow-direct`。

进程生命周期要点：转发器跑在 `ephemeral` 进程里，所以调用方应当终止**它**而不是
直接杀浏览器 —— 杀错了会留下孤儿转发器，或让浏览器悄悄失去代理。

## 设计前提

指纹的目标是**看起来像一台普通真机**，不是看起来独一无二。逐字段独立随机会产出
真实世界不存在的组合（例如声称 Windows 却报 Apple GPU），那本身就是最强的自动化信号。
因此机型只能从预置的自洽档案库中整体抽取，见 `internal/fingerprint/catalog.go`。

同理，真正的难点不是改指纹，是**一致性**：同一 profile 每次启动必须看起来是同一台机器，
且各维度之间不能互相矛盾。平台封号的常见依据不是"这个指纹是假的"，而是
"这个账号上次是 A 设备现在是 B 设备"。

## 快速开始

```bash
cd frontend && npm install && cd ..   # 首次
wails dev     # 开发模式
wails build   # 产出 build/bin/better-web.exe
```

`main.go` 用 `//go:embed all:frontend/dist` 嵌入前端产物，而该目录不入库。所以
**首次必须先走一次 `wails build` 或 `npm run build`**，否则直接 `go build ./...`
会报 `pattern all:frontend/dist: no matching files found`。命令行工具不受影响：

```bash
go build ./cmd/...   # 只编译 cmd 下的工具，不需要前端产物
```

产出的是**启动器面板**，不是浏览器：它管理 profile 配置，点「启动」后弹出的
独立 Chromium 窗口才是浏览器。两者是两个进程。

首次运行需要装内核：界面会显示内核目录并提供下载入口，也可手动解压到

```
<用户配置目录>/better-web/kernels/<版本号>/chrome.exe
```

## 终端工具

界面之外的命令行工具。只读的那些都不回显代理密码。

核对与排障（只读，不改任何状态）：

```bash
go run ./cmd/dumpprofiles          # 列出全部 profile 及其推导出的环境
go run ./cmd/checkproxy            # 逐个检测代理连通性与出口质量（ASN 判定）
go run ./cmd/listkernels           # 列出已安装的内核版本
go run ./cmd/identifykernel [端口] # 确认某端口后面是指纹内核还是普通 Chrome
go run ./cmd/scoreprofile <名称>   # 用该 profile 的真实配置在检测站跑分
go run ./cmd/scoreprofile <名称> --antibot  # 换成 Cloudflare / DataDome 实测
go run ./cmd/cdpexperiment <名称>  # 三组对照：测量启用 CDP 对跑分的影响
```

筛种子（只读，输出候选供你决定）：

```bash
go run ./cmd/matchseed [上限]              # 探测本机 GPU 并筛一个同族种子
go run ./cmd/matchseedplat <平台> [上限]   # 同上，但在指定平台下筛
```

`matchseed` 不传 `--fingerprint-platform`，而真实启动会按机型档案传。同一个种子在
不同平台下派生的 GPU 可能跨厂商，所以**别拿 `matchseed` 的结果直接建 profile**，
要建请用 `matchseedplat`。

会改状态的：

```bash
go run ./cmd/mkprofile -name <名称> [-proxy <代理行>]  # 命令行建 profile，落库
go run ./cmd/launchdebug <名称>          # 跑已有 profile，带 CDP 端口，手工排障用
go run ./cmd/ephemeral --proxy=<代理行>  # 用完即弃的临时实例 + CDP，供脚本驱动
go run ./cmd/multilaunch <数量>          # 并发启动多个实例，报告耗时/内存/出口 IP
go run ./cmd/seturlhandler               # 查看链接接管配置（带 --profile=<名称> 则设置）
```

自动化请用 `ephemeral` 而非 `launchdebug`，理由见 [docs/usage.md 的自动化接入](docs/usage.md#自动化接入)。

## 系统链接接管

其他应用（编辑器、聊天软件、邮件）点开的链接交给指定的 Profile 打开，而不是裸的
默认浏览器窗口。行为对齐「没有 better-web 时点链接」的体验：

| 目标 Profile 状态 | 行为 |
| --- | --- |
| 未运行 | 完整启动它（代理转发器、出口探测、指纹全部照旧），打开该链接 |
| 已运行 | 在已有实例里新开一个窗口，接管进程立即退出（实测 0 秒） |

配置在「导入导出 → 系统链接接管」里。只列日常模式的 Profile：链接接管是「随手点开
一个链接」的场景，用指纹 Profile 承接等于把随机链接混进养号环境。

### 注册只能做到一半

应用可以把自己注册成**候选**默认浏览器（写 `HKCU\SOFTWARE\RegisteredApplications`
与 `Capabilities\UrlAssociations`），但**不能编程把自己设成默认**：`UserChoice` 键带
一个未公开算法的 Hash 校验值，Win11 起还有 UCPD 驱动拦截对它的直接写入。这是微软
有意的设计，防的就是应用私自抢默认浏览器。

因此界面上分两个状态显示——「已注册为候选」与「已是系统默认」。最后一步必须用户
在「设置 → 默认应用」里手动选，界面提供按钮直接跳到那一页。

### 安全边界

注册成默认浏览器后，机器上任何应用都能往 `--open-url=` 传字符串，因此按白名单
放行：只接受 `http` 与 `https`。被拒绝的几类及原因见 `urlhandler.ValidateURL`——
`file://` 能读本机文件，`javascript:` 与 `data:` 能执行代码，`chrome://` 能改浏览器
设置。另外拒绝含控制字符的输入与以 `-` 开头的主机名（会被内核当成命令行开关）。

### 无痕窗口

可选用无痕窗口打开接管的链接，**仅日常模式**。指纹模式 + 无痕这个组合会被拒绝：
无痕只是不落盘，指纹伪造与代理照旧生效，出口 IP 与指纹一个字不变，反而丢掉了养号
需要的登录态。用户以为「无痕更干净」，实际得到的和以为的不是一回事。

## 日常模式可以用系统 Chrome

日常模式（`KindDaily`）本就不做任何指纹伪造、只要目录隔离，所以没必要用指纹内核。
勾选「使用系统已装的 Google Chrome」后该 profile 走官方 Chrome，拿回指纹内核缺少的
能力：可登录 Google 账号、书签密码同步、自动更新、流媒体 DRM。

指纹内核基于 ungoogled-chromium，而 ungoogled 的目的就是去掉 Google 集成——编译时
不带 OAuth 凭据，所以浏览器级的 Google 登录和同步都不可用。这是上游的取舍，改不了。

**代理与目录隔离照旧生效**：转发器、出口探测、时区对齐都在 Go 侧完成，与用哪个浏览器
无关。所以日常 profile 换成官方 Chrome 后代理链路一个字都不用改。

### 指纹模式绝不允许用系统 Chrome

`--fingerprint` 是打进 Chromium C++ 源码的补丁，官方二进制不认识它，会当成未知参数
**静默忽略**——页面照常打开、脚本照常执行、不报任何错，但报出的是宿主机真实指纹、
真实 GPU、真实时区。

这类故障比代理失效更隐蔽：代理挂了至少能从出口 IP 看出来，浏览器用错什么痕迹都没有。
因此按 fail-closed 处理，三道闸：

| 位置 | 拦什么 |
| --- | --- |
| `app.CreateProfile` / `UpdateProfile` | 落库前拒绝非法组合，不留下启动时才报错的记录 |
| `session.start` 开头 | 启动前校验，且**先于内核解析**——否则报的会是「未找到内核」，把用户引向错误方向 |
| `session.start` 解析后 | 核对解析出的内核确实支持伪造，拦住内核目录里混入的非 fingerprint-chromium 构建 |

系统 Chrome 找不到时**报错而非回退到指纹内核**：用户选它是为了 Google 账号与同步，
静默换回指纹内核后这些能力全没有，而界面显示的仍是「系统 Chrome」。

探测走注册表 `App Paths\chrome.exe`（HKLM 与 HKCU 都查），而非硬编码 Program Files——
Chrome 支持用户级安装和自定义位置。注册表查不到时兜底几个默认路径。

## 数据目录

`%APPDATA%\better-web`（macOS/Linux 为对应的用户配置目录）：

| 路径 | 内容 |
| --- | --- |
| `profiles.db` | profile 配置。含代理凭据，密码字段在 Windows 上经 DPAPI 加密 |
| `profiles/<id>/` | 各 profile 的 user-data-dir，含 Cookie 与登录态 |
| `kernels/<版本>/` | 已安装内核 |

整个目录按凭据目录对待：权限 0700，不要同步到网盘、共享目录或代码仓库。

代理密码在 Windows 上经 DPAPI 加密，密钥绑定当前用户账户——它能防「数据库被
单独拷到别处或别的账户下」，防不了已能在当前用户下执行代码的攻击者（程序自己
能解密，攻击者也能）。非 Windows 平台尚未接入系统密钥库，仍是明文。

而 `profiles/` 下的 Cookie 与登录态在**所有平台都是明文**——那是 Chromium 自己的
存储格式。所以「密码加密了」不构成放松对数据目录警惕的理由。

## 实测结论（内核 148.0.7778.215）

以下都是 `internal/probe` 用真实内核跑出来的，不是从文档推断的。
完整记录（含各类实现踩坑与回归测试位置）见
[internal/probe/findings.md](internal/probe/findings.md)。

**跑分成绩**（CreepJS + BrowserScan，归档于 `internal/probe/testdata/scores-*.json`）

| 指标 | 值 |
| --- | --- |
| CreepJS lies | 0 |
| headless / stealth | 0% / 0% |
| like headless | 25% |
| low entropy | timezone, intl |
| BrowserScan | Normal（三次复跑一致） |

`lies=0` 衡量的是各伪造项**之间**有没有互相矛盾。两项低熵都指向时区，含义是该时区在
用户群里太常见所以区分度低——这不是缺陷，反而说明落在了大匿名集里。

但要认清这项指标的边界：**它满分不等于能过商业风控**。CreepJS 只能横向比较各伪造值，
拿不到"真实值"作参照；Cloudflare 能把伪造的 WebGL vendor 跟实际渲染出的像素比对，
于是同一个 profile 在 CreepJS 判 lies=0、在 Cloudflare 被拦。详见下面的商业风控实测。

`likeHeadless=25%` 对应 `prefersLightColor`、`noContentIndex`、`noContactsManager`、
`noDownlinkMax` 四项，来自 Chromium 本身不支持这些实验性 API（Content Index 与
Contacts Manager 是移动端 API），真实桌面 Chrome 同样命中，不是伪造痕迹。关键在于
`headless` 与 `stealth` 两个数组都为空。

上表是直连环境下的成绩。用真实 profile（含其代理）跑分：

```bash
go run ./cmd/scoreprofile <profile 名称>
```

实测经一个德国机房代理的完整链路，结果同样是 lies=0 / headless 0% / stealth 0% /
BrowserScan Normal——代理链路本身不影响指纹自洽性，两者是独立的问题。

### 一个容易误判的现象

UA 里的 `Windows NT 10.0` 不区分 Windows 10 与 11，这是 Chrome 的 UA 精简
（UA reduction）行为，真实的 Windows 11 用户同样如此。要区分只能读 UA-CH 的
`platformVersion`。因此像 Whoer 这类只解析 UA 字符串的站点会把 Windows 11 的
profile 显示成 `Win10.0`，那不是伪造泄露。

判断有没有泄露真实环境，看 `hardwareConcurrency` 更可靠——它来自档案库的设定值，
与宿主机的真实核数无关。

**生效的参数**：`--fingerprint`、`--fingerprint-platform`、`--fingerprint-platform-version`、
`--fingerprint-brand`、`--fingerprint-brand-version`、`--fingerprint-hardware-concurrency`、
`--timezone`、`--lang`、`--accept-lang`、`--disable-spoofing`。

**不可控的维度**：GPU 型号、`deviceMemory`、屏幕尺寸。内核 144 移除了
`--fingerprint-gpu-vendor/renderer`，这些值现在全部由种子自行派生。`catalog.go` 里
对应字段保留但**不生效**，仅供人工核对档案自洽性，字段说明见 `model.DeviceProfile`。

GPU 不可控这一条有实际后果：种子派生出的 GPU 厂商与宿主机不同族时，Cloudflare 会
拦截，见下面的商业风控实测。要控制它只能通过筛种子间接实现。

**跨作用域一致**：主线程与 Worker（含 OffscreenCanvas）的 UA、时区、核数、内存完全相同，
关键 API 的 `toString()` 均为 `[native code]`。这证实伪造做在 C++ 层而非 JS 注入——
后者会被原型链比对和 `toString` 检查戳穿。

### 已知短板

三项都在内核层面，改不了：

1. **声称 Linux 时 WebGL 报 Direct3D**（Windows 宿主机上）。Linux 不存在 Direct3D，
   单一信号即可检出。该档案已标记 `KnownIssue`、排除在随机抽取之外，只能手动选择。
   声称 macOS 时内核处理正确（报 Apple/Metal）。
2. **Audio 指纹只有 5 个取值**。24 个种子唯一率 21%，最大一组碰撞覆盖 7 个种子。
   Canvas 是 100% 唯一，所以 profile 之间仍可区分，但 audio 不能作为区分依据。
3. **TLS/HTTP2 指纹所有 profile 相同**（看 JA4，不是 JA3）。转发器是纯 TCP 隧道，
   不解密 TLS——这是有意的：任何 TLS 中间人都会把 ClientHello 改写成 Go 标准库的
   形状，等于主动标记自己不是 Chrome。代价是链路层无法按 profile 变化。

   实测（`TestReportTLSFingerprintStability`）：

   | 指纹 | 同一 profile 三次 | 跨声称系统 |
   | --- | --- | --- |
   | JA3 | 三次**全不同** | 不同 |
   | JA4 | 三次**完全相同** | 相同 |
   | Akamai HTTP/2 | 三次完全相同 | 相同 |

   JA3 每次连接都变，因为 Chrome 按 RFC 8701 在密码套件与扩展列表里插入 GREASE
   随机值。所以**跨平台看到的 JA3 差异是假象**，不能据此认为 TLS 指纹随声称的系统
   变化；判断多账号是否会被 TLS 层关联要看 JA4。反过来说，任何拿 JA3 做设备标识的
   系统本身就不可靠。

   这不是本项目的短板而是整个品类的边界：只要基于真实 Chromium 内核，JA4 就必然是
   该内核版本的 JA4。真正做 TLS 层伪装的是 curl-impersonate、utls 一类不带浏览器的
   HTTP 客户端库，属于另一种产品形态。

   JA4 相同也不等于可疑——全世界同版本 Chrome 用户的 JA4 都一样，是个极大的匿名集。
   它只在与其他信号组合时才有关联价值（同一 JA4 + 同一网段 + 相近活动时间）。
   降低这类关联度靠使用方式（不同网段的出口、错开活动时间、避免批量同时操作），
   不靠改指纹。

## 代理的三种填法

解析都走 `model.ParseProxy` 这一份实现，支持 `host:port`、`host:port:user:pass`、
`scheme://user:pass@host:port`，省略协议按 SOCKS5 处理。密码含 `@` 或 `:` 时用四段
冒号形式——URL 形式下这些字符会破坏解析，而要求用户做百分号编码不现实。

| 入口 | 用途 |
| --- | --- |
| 表单里的五个字段 | 手工逐项填，或核对粘贴后的结果 |
| 表单里的「粘贴一行代理」 | 代理商给的一行文本直接拆进上面五个字段，不新建 profile |
| 头部的「批量导入」 | 一行一个，每行建一个 profile 并各自生成新种子 |

一行粘贴经 Wails 绑定 `ParseProxyLine` 调后端解析，没有在前端重写一份 TS 版本：
两份实现迟早漂移，而漂移的后果是表单显示的凭据与实际存的不是一回事。该绑定不依赖
`app.Service`，解析是纯函数，数据目录不可用时也能用。

不支持带字段名的多行块（`协议 SOCKS5` / `主机 ...` 这种）。缺了「用户名」标签时
剩下那行是用户名还是密码无法判断，猜错会把密码填进用户名，而这类错误在界面上看不出来。

## 配置的导出与导入

界面头部的「导出配置」「导入配置」，与「批量导入」是两件不同的事：后者从粘贴的代理
文本从零建号，前者把现有 profile 配置搬到别处（备份、换机器）。实现在 `internal/transfer`。

**导入时种子语义必须由用户明确选择**，没有安全的默认值：

| 用途 | 种子 | 后果 |
| --- | --- | --- |
| 恢复备份 / 迁移机器 | 保留原值 | 还原为同一批设备，账号看到的设备没变 |
| 以某份配置为模板批量建号 | 生成新值 | 每个 profile 是不同设备 |

选错的代价不对称：批量建号时误留原种子，所有 profile 共用同一套 canvas 指纹，
平台侧可直接关联；恢复备份时误换种子，等于所有账号同时换了设备。

**默认不导出密码。** 导出文件会被复制、转发、甚至提交到仓库，凭据进入这类文件的风险
远高于导入后重填一次的成本。但会留 `hadPassword` 标记，导入后提示需补填几个——
否则代理会静默认证失败。文件权限 0600，即使不含密码里面也有代理地址与账号名。

四类问题会主动警告：档案库条目数不同（同一种子在两台机器上抽到不同机型，仅在保留种子
时提示）、文件内种子重复、缺密码、逐条失败带文件内行号。

**部分失败不回滚。** 导入 100 条第 37 条重名就跳过继续，已成功的保留——整批回滚会让
用户丢掉已导入好的几十条。校验与落库分两阶段，校验失败不留半写入状态。

只搬配置，不搬 `user-data-dir` 里的 Cookie 与登录态：那是 Chromium 的私有 SQLite 格式，
几十到几百 MB，且两台机器同时使用会冲突。

## 多实例并发

```bash
go run ./cmd/multilaunch <数量> [profile 名称前缀] [--cdp=<个数>[:<起始端口>]]
```

实测 5 个实例并发启动 1139ms 全部就绪，**并发效率 1.00**（被串行化会是 5 倍时间）。
`session.Manager` 的 `start()` 全程无锁，只在读写会话表时短暂持锁；每个 profile
独占一个转发器（20 个转发器启动共 517µs）。

三层隔离都有测试保证（`internal/session/multi_test.go`）：不同 profile 并发启动
全部成功且 PID 与种子两两不同；并发停止全部收敛；同一 profile 并发启动恰好只有
一个成功——两个进程同时打开一个 `user-data-dir` 会损坏数据。

**能开几个取决于 Chromium 而非本项目。** 实测每个主进程约 100-116MB，但 Chromium
还会派生渲染/GPU/网络子进程，单实例实际总占用通常 300-500MB。16GB 内存大约 20-30 个。
另一个限制是代理带宽——每个实例都跑自己的请求，共享上游会互相挤。

### 克隆实例是一次性的

`multilaunch` 在数量超过库里 profile 数时会克隆：换独立的 ID、**新种子**与临时目录，
但**沿用源 profile 的代理**。

换种子是必须的——相同种子等于同一台设备开多个窗口，canvas 哈希完全一致，一眼被关联。
但这也意味着克隆实例**不能用来养号**：账号需要稳定的设备身份，而克隆每次都是新设备，
种子不落库、目录进程退出即删。

沿用代理则是该工具的局限：多个实例走同一出口 IP，在真实多账号场景下是致命的。
启动后会显式警告并列出共用出口的实例。真要多账号运营，必须在界面上一个个建
profile，各自有持久种子、独立目录和**独立代理**。

注意区分两条独立的线：机型、核数、UA、canvas、audio、GPU 跟着**种子**变；
时区、语言、Accept-Language 跟着**代理出口**变。

### 重复启动保护是两层的

`session.Manager` 的会话表只覆盖自己那一张表。界面与命令行工具同时运行时两边各有
一个 Manager，甚至同一进程内也可能有多个，它们互不可见——只靠会话表会放行重复启动，
而两个 Chromium 打开同一个 `user-data-dir` 会损坏 profile 数据（Chromium 自己的处理是
让后来者静默退出，用户看不到原因）。

因此第二层是 profile 目录内的 `.better-web-lock` 文件，记录持有者 PID。启动时若发现
锁被**活着的**进程持有，返回 `ErrLockedByOtherProcess` 并带上该 PID，便于判断该关掉哪个。

持有者已退出的陈旧锁会直接接管，内容损坏的锁也一样——崩溃后留下的锁不该需要人工清理，
那是比不加锁更糟的故障模式。释放时只删自己写的那份，避免删掉已被接管的锁。

## 关于自动化与爬虫

默认不开 CDP，但**实测表明开了也不影响跑分**——这与常见说法相反，所以用数据说话。

`cmd/cdpexperiment` 做三组对照，同一 profile、同一出口，唯一变量是 CDP 状态：

| 指标 | 不开 CDP | 只开端口 | 开端口 + 客户端持续发命令 |
| --- | --- | --- | --- |
| liesCount | 0 | 0 | 0 |
| headless / stealth | 0% / 0% | 0% / 0% | 0% / 0% |
| likeHeadless | 25% | 25% | 25% |
| BrowserScan | Normal | Normal | Normal |

第三组是在跑分全程每 1.5 秒发一轮 `Runtime.enable` / `Page.enable` / `Network.enable` /
`Runtime.evaluate`，制造真实的 CDP 使用痕迹。BrowserScan 有一列专门叫 CDP，仍判 Normal。

**这个结论的边界**：只覆盖 CreepJS 与 BrowserScan。Cloudflare、DataDome 一类商业风控
是否有更强的 CDP 检测，未经验证。默认仍不开，是因为不开没有任何代价。

## 商业风控实测（内核 148.0.7778.215，2026-07-27）

```bash
go run ./cmd/scoreprofile <profile 名称> --antibot   # 用真实配置测
BW_RUN_ANTIBOT_CONTROL=1 go test -run TestAntibotControl -v ./internal/probe/
```

与上面的自洽性跑分是两类判据：CreepJS/BrowserScan 输出公开的环境评分，
Cloudflare/DataDome 输出不公开的放行或拦截结果，且随出口 IP 信誉与访问频次浮动。
所以商业风控站不进 `DefaultSites`，也不进门禁——否则内核回归检查会随 IP 抖动。

### Cloudflare：判据是 GPU 跨厂商，不是"存在 GPU 伪造"

累加式对照，直连住宅出口，宿主机 RTX 2070，唯一变量是命令行参数：

| 组 | 参数 | 派生 GPU | 结果 |
| --- | --- | --- | --- |
| A | 裸跑，无任何伪造参数 | 真实 RTX 2070 | 通过 |
| B | 只加 `--fingerprint=770828460` | Intel 集显 | **被拦** |
| F | 种子 + `--disable-spoofing=canvas` | Intel 集显 | 被拦 |
| G | 种子 + `--disable-spoofing=canvas,gpu` | 真实 | 通过 |
| I | 种子 + `--disable-spoofing=gpu` | 真实 | 通过 |
| J | 种子 + 关除 gpu 外全部 | Intel 集显 | 被拦 |
| K | 种子 + `--disable-features=WebGPU` | Intel 集显 | 被拦 |
| L | 种子 + 关 WebGPU + 屏蔽 `debug_renderer_info` | Intel 集显 | 被拦 |
| M | `--fingerprint=470000000` | **RTX 3060** | **通过** |
| N | `--fingerprint=544000000` | **RTX 4060** | **通过** |
| O | `--fingerprint=174000000` | AMD Radeon | 被拦 |

三层验证：I 与 J 双向确认责任在 gpu 这一项；M/N 与 B/O 确认判据是**跨厂商**而非存在
伪造——M/N 的 GPU 伪造照样开着却能过，而跨厂商在 Intel 和 AMD 两个方向都被拦；
K/L 排除了 WebGPU 与 `debug_renderer_info` 两条通路。B/M/N 各复跑两轮，同批次一致。

矛盾的具体来源（`BW_RUN_GPU=1 go test -run TestGPUSpoofContradiction`）：伪造只改了
WebGL 的 `vendor` 与 `renderer` 两个字符串，其余全是真实 NVIDIA 的值——

| 项 | spoofed vs honest |
| --- | --- |
| `vendor` / `renderer` | 已改写为 Intel |
| `pixelHash`（实际渲染结果） | **完全相同**（同一个 8 位十六进制值） |
| 扩展列表 | **完全相同**（WebGL1 35 项 / WebGL2 32 项） |
| 着色器精度 | **完全相同** |
| 能力上限 | 16 项里 15 项相同，只有 `MAX_VERTEX_UNIFORM_VECTORS` 改了 4095→4096 |
| WebGPU `adapterInfo` | **完全相同**：`vendor: nvidia, architecture: turing` |

同族之所以能过，是因为同厂商不同型号之间这些值本就接近。声称 Intel 集显却渲染出
NVIDIA 的像素、报 NVIDIA 的扩展集合，则是单一信号即可判定的矛盾。

这也解释了 CreepJS 判 `lies=0` 而 Cloudflare 仍拦：前者查各伪造项**之间**是否自洽，
后者能拿伪造值跟**实际渲染结果**比。**自洽性跑分满分不等于能过商业风控。**

### 解决办法：按宿主机 GPU 族筛种子

新建 profile 时勾选「筛选种子以匹配本机 GPU 厂商」，或命令行先试：

```bash
go run ./cmd/matchseed        # 探测本机 GPU 并筛一个同族种子，用默认上限 24
go run ./cmd/matchseed 40     # 指定上限
```

实现思路：GPU 由内核从种子派生，**没有命令行参数可指定，Go 侧也算不出来**
（派生算法在内核的 C++ 代码里）。所以只能反复生成随机种子、逐个启动内核实测，
直到派生出与宿主机同厂商的型号。每个候选一次冷启动约 2 秒，上限 24 次。

筛选目标是运行时探测出来的，代码里没有硬编码的 GPU 型号或种子值。换台机器跑，
探测到的就是那台机器的显卡——**这套逻辑本身是宿主机相对的，不需要为不同机器改代码。**
不可移植的只是筛出来的那个种子值。

端到端实测（`BW_RUN_SEED_MATCH=1 go test -run TestSeedMatchEndToEnd -v ./internal/probe/`）
连跑三轮，三个不同种子全部通过 Cloudflare，**且 GPU 伪造全程开启**：

| 轮次 | 试了几个 | 选中种子 | 派生 GPU | Cloudflare |
| --- | --- | --- | --- | --- |
| 1 | 2 | 2121403139 | RTX 4060 | 通过 |
| 2 | 6 | 1267368815 | RTX 4060 | 通过 |
| 3 | 2 | 1163651504 | RTX 3060 | 通过 |

对照组裸跑同样通过，排除了 IP 信誉侥幸。命中率约 1/7（宿主机为 NVIDIA 时），
所以 24 次上限找不到的概率约 2.4%。

几条设计约束：

- **只对新建生效。** 种子是 profile 身份的根，改已有 profile 的种子等于换设备。
  编辑表单里不提供这个选项。
- **筛选失败一律报错，绝不静默退回随机种子。** 用户开这个选项就是为了过 Cloudflare，
  静默给一个跨厂商的种子会让他以为已经能过，直到账号出问题才发现。
- **批量导入不支持。** 筛一个种子最多 50 秒，导入 20 条就是十几分钟；而「筛一次给
  所有 profile 复用」不能接受——相同种子等于同一台设备，canvas 哈希一致会让这批账号
  被一眼关联，恰好违背导入功能给每个 profile 独立种子的初衷。
- **筛选结果写进 profile 备注**，日后能判断某个 profile 是否筛过。
- **种子只对当前宿主机有效。** 换机器后 GPU 族可能不同，同一个种子又变成跨厂商了。
  它匹配的是宿主机，不是绝对安全的值。

  这一条对**配置迁移**有实际后果：在 NVIDIA 机器上筛好的种子，导出后在 Intel 机器上
  以「保留原种子」导入，会静默退回跨厂商——profile 配置一个字没改，却从能过变成被拦，
  而且没有任何界面痕迹。因此导出文件记录 `hostGPUFamily`，导入时两侧不同则警告
  （只在保留原种子时警告，生成新种子时原机器的 GPU 族已无关）。探测失败时字段留空，
  退化为不警告，不阻断导出导入。

另一条路是 `profile.DisableSpoofing` 加 `gpu`，代价是所有 profile 都上报宿主机真实
GPU，多一个跨 profile 共享的关联信号。筛种子失败时才考虑它。

扫描种子分布：`BW_RUN_GPU_SEEDS=1 BW_GPU_SEEDS=14 go test -run TestScanSeedGPU -v ./internal/probe/`

### DataDome：本轮未能得出指纹层结论

裸跑组前三轮通过，此后同一组转为持续被拦到 `geo.captcha-delivery.com`，
期间浏览器参数完全没变，变化的只有该 IP 的累计访问次数。这是 IP 维度的频次限流，
不是指纹判定。要测它需换未使用过的出口并隔开时间。

由此得出实测的解读规则：**必须保留裸跑对照组**。只有同批次内「裸跑通过、伪造被拦」
才能把责任归到指纹；两组都被拦只能说明该 IP 已被限流。

### 判据方向的一个教训

第一版 Cloudflare 判据是「没命中挑战页特征就算通过」，实测误判：Accept-Language
按代理出口设成 de-DE 后返回的是德语挑战页（`Nur einen Moment…`），英文文案全部落空，
而该页也不再用 `#challenge-running` 那套 id，元素判据一并落空，于是被判成通过。

挑战页的形态由对方控制、会本地化也会改版，穷举必然漏。改为认目标页自己的正向成功
标记（`You bypassed the Cloudflare challenge`）。宁可漏报通过，不可误报通过。

### 接给外部 CDP 工具

```bash
go run ./cmd/launchdebug <profile 名称> [端口]     # 默认 9222
go run ./cmd/identifykernel [端口]                # 确认端口后面是不是指纹内核
```

用该 profile 的完整配置（指纹 + 代理 + 时区对齐）启动浏览器并开放调试端口，
Ctrl+C 优雅关闭并清理转发器。之后 Playwright、Puppeteer 或任意 CDP 客户端都能接入。

实测经该通道读到的全部是伪造值：`hardwareConcurrency` 报档案里的 16 而非宿主机真实的
24，时区语言按代理出口对齐为 `Europe/Berlin` / `de-DE`，`navigator.webdriver` 为
false，出口 IP 是代理地址。指纹链路与代理链路都不受 CDP 影响。

必须带 `--remote-allow-origins=*`（`launchdebug` 已处理）：Chrome 111 起会拒绝
Origin 头不在白名单内的 WebSocket 升级请求，而 Node 的 WebSocket 客户端默认带 Origin，
不放行会直接握手失败。

### 别用端口号判断实例身份

端口是启动参数，普通 Chrome 也能开在任何端口上。而把自动化接到错误的实例上**不会报错**：
页面正常打开、脚本正常执行，但指纹和出口 IP 都是本机的，等发现时账号已经出问题了。

`identifykernel` 用行为层面的判据——比对目标实例报告的 `hardwareConcurrency` 与
`runtime.NumCPU()` 拿到的本机真实核数。不看版本号，因为不同构建可能撞号。
两者相同时它不下结论，只提示「无法确认」并建议核对时区与出口 IP。

实测对比（本机真实核数 / Asia/Shanghai / RTX 2070）：

| 项 | 普通 Chrome | 指纹内核 |
| --- | --- | --- |
| cores | 24 | 16 |
| deviceMemory | 16 | 8 |
| 时区 / 语言 | Asia/Shanghai / en-US | Europe/Berlin / de-DE |
| WebGL | RTX 2070 | Intel UHD Graphics |
| screen | 2048x1152 | 2048x1152（未伪造，见已知短板） |

在 Git Bash 里手动 `taskkill` 需要加 `MSYS_NO_PATHCONV=1`，否则 `/PID` 会被当成路径
转换成 `C:/Program Files/Git/PID`。

另一条不依赖 CDP 的路是 **MV3 扩展的内容脚本**，即 `internal/probe` 跑分所用的方式。
能力受限（可读写 DOM、可点击，无法拦截网络请求或控制导航），但不需要调试端口。

## 包结构

| 包 | 职责 |
| --- | --- |
| `model` | 领域模型，字段的生效性说明都在这里 |
| `fingerprint` | 种子 → 完整环境的确定性推导，机型档案库，按 GPU 族筛种子 |
| `geo` | 出口地反查、ASN 类型判定（住宅 / 机房 / 未知） |
| `proxy` | 本地转发器。内核的 `--proxy-server` 不支持密码认证，故需转发补凭据 |
| `kernel` | 内核定位、版本解析、下载安装 |
| `launcher` | Profile + 指纹 → Chromium 命令行 |
| `session` | 启动编排：转发器 → 出口探测 → 推导指纹 → 启动内核 |
| `secret` | 凭据加密。Windows 走 DPAPI，其他平台退化为明文直通 |
| `store` | SQLite 持久化，含列迁移、密码加解密、分组标签查询 |
| `transfer` | 配置文件的导出与导入，纯逻辑不依赖 store |
| `app` | 给前端绑定的服务层 |
| `probe` | 用真实内核采集指纹、跑分、比对基线 |

## 几条不可调换的约束

**启动顺序**（`session.start`）：起转发器 → 经代理查出口地 → 由种子与地理推导指纹 → 启动内核。
第二步失败时，指纹模式默认中止启动而非静默回退——时区与真实出口不符比不启动更糟。
日常模式与已指定地理覆盖的 profile 则降级为警告：它们的时区不取自探测结果，
探测只为附加"出口是否机房 IP"的提示，为一条提示拒绝启动不成比例。

**fail-closed**：上游代理不可用时必须让连接失败，绝不能退化成直连。静默直连不报错、
页面照常打开，但出口已是真实 IP，伪装全部作废且用户毫无察觉。四种失效场景都有测试锁住。

**DNS 走代理**：域名原样交给上游解析。本地解析会让 DNS 请求绕过代理，泄露访问记录。

**WebRTC**：配了代理即传 `--disable-non-proxied-udp`，否则 STUN 会直接暴露真实 IP。

**优雅关闭**：Windows 上必须用 `taskkill /PID <pid>`（不带 `/T`、不带 `/F`）投递 WM_CLOSE。
`os.Interrupt` 对 GUI 进程无效，直接 Kill 会丢失未落盘的会话数据并让下次启动弹出
"未正确关闭"提示——那本身就是异常信号。加 `/T` 反而会失败：渲染进程没有窗口关不掉，
taskkill 随即以"子进程仍在运行"为由拒绝关闭主进程。见 `terminate_windows.go`。

## 开发

```bash
go test ./...                          # 全部测试
go test -race ./internal/proxy/        # 并发相关
go vet ./...
cd frontend && npx svelte-check        # 前端类型检查
```

需要真实内核的测试在未安装时自动跳过。几个环境变量开关：

```bash
BW_WRITE_BASELINE=1     # 重新采集指纹基线
BW_COLLISION_SEEDS=24   # 统计各维度的种子碰撞率
BW_KERNEL_DIR=<path>    # 指定内核目录
BW_RUN_SCORE=1          # 跑自洽性评分（需 BW_TEST_PROXY）
BW_RUN_ANTIBOT=1        # 跑商业风控实测（需 BW_TEST_PROXY）
BW_RUN_ANTIBOT_CONTROL=1  # 跑商业风控累加对照，直连不需要代理
BW_RUN_GPU=1            # GPU 深度采集，对比伪造前后哪些项没被覆盖
BW_RUN_GPU_SEEDS=1      # 扫描种子派生出的 GPU 型号，配 BW_GPU_SEEDS=<个数>
BW_RUN_SEED_MATCH=1     # 端到端验收：筛种子后实测 Cloudflare
BW_RUN_GPU_MATCH=1      # 端到端验收：建带 GPU 匹配的 profile（在 internal/app）
```

**内核升级后必须做两件事**：跑 `TestBaselineHasNotDrifted` 确认已有 profile 的指纹
没有漂移；同步 `fingerprint.fallbackBrandVersion`（品牌版本正常由内核版本推导，
该常量只是兜底）。基线检测把差异分成"身份漂移"和"环境差异"两类——
canvas/audio/UA/核数变化算漂移，屏幕尺寸和 WebGL 型号变化不算（换台机器跑就会不同）。

修改 Go 侧导出类型后跑 `wails generate module` 重新生成前端绑定。注意别用匿名结构体
作为导出字段的类型：Wails 的 TS 生成器会把它降级成 `any`，前端就此失去类型检查。

## 许可与致谢

本项目以 [MIT 许可证](LICENSE) 发布。

浏览器内核来自 [fingerprint-chromium](https://github.com/adryfish/fingerprint-chromium)，
本仓库不分发内核二进制，只在运行时下载安装。`patches/` 下的增量补丁基于 Chromium
源码，Chromium 为 BSD-3-Clause，各补丁文件头已标注上游归属。

`memory/` 是开发过程中的排障记录，已移除真实代理端点与宿主机标识，
文档中出现的 IP 均为 [RFC 5737](https://www.rfc-editor.org/rfc/rfc5737) 保留段的示例地址。

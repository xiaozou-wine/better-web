# 检测站实测结论（内核 148.0.7778.215）

数据来自 `internal/probe` 的真实内核跑分，非推测。复现方式见各测试的注释。

## 成绩

| 站点 | 指标 | 值 |
| --- | --- | --- |
| CreepJS | lies | 0 |
| CreepJS | headless | 0% |
| CreepJS | stealth | 0% |
| CreepJS | like headless | 25% |
| CreepJS | low entropy | timezone、intl |
| BrowserScan | verdict | Normal |

`lies = 0` 是最有意义的一项：它衡量伪造项之间有无互相矛盾。

## like headless 25% 的构成

16 项判定中命中 4 项：

| 项 | 成因 | 处置 |
| --- | --- | --- |
| `prefersLightColor` | 系统主题为亮色 | 可用 `--force-dark-mode` 消除（实测降到 19%），但**不建议默认开启**，理由见下 |
| `noContentIndex` | 上游 Chromium 桌面端未启用该 API | 不处理，见下 |
| `noContactsManager` | 同上 | 不处理 |
| `noDownlinkMax` | 同上 | 不处理 |

### 后三项不是缺陷，不要去"修"

一度以为这三项是 ungoogled-chromium 移除 Google 组件时连带砍掉的，
需要打内核补丁补回。查 Chromium 源码后确认这个归因是错的——
`third_party/blink/renderer/platform/runtime_enabled_features.json5` 中：

| 特性 | status |
| --- | --- |
| `ContentIndex` | `{"Android": "stable", "default": "experimental"}` |
| `ContactsManager` | `{"Android": "stable", "default": "test"}` |
| `NetInfoDownlinkMax` | `{"Android": "stable", "ChromeOS": "stable", "default": "experimental"}` |

也就是说**上游 Chromium 只在 Android/ChromeOS 上启用这三个 API，
真实的 Windows 版 Chrome 同样没有**。

CreepJS 侧的实现印证了这一点：这三项出现在它的 Linux 平台特征清单里
（`["Linux"]` 分支下缺这些 API 才算异常），但 `headlessEstimate` 是无条件
计算的，不看声明的平台。我们声明 Windows，而 Windows Chrome 本来就没有
这些 API，于是必然命中。

结论：这是 CreepJS 的误判，不是本项目的伪装漏洞。**强行打补丁启用它们
反而会偏离真实 Windows Chrome 的行为，制造新的矛盾项**（而 lies 才是
真正要紧的指标）。保持现状。

### 对照实验：真实 Chrome 同样是 25%

已由人工在同一台宿主机上用正常安装的 Chrome 访问 CreepJS 核对，
结果同样是 **like headless 25%**。

这是决定性证据：未经任何改造的桌面 Chrome 得分与本项目一致，
说明这 25% 全部来自桌面 Chrome 的固有特征，不含本项目引入的破绽。
因此 like headless 这一项**不需要也不应该去优化**——把它降到 0
反而会偏离真实 Chrome，制造出真实浏览器不具备的特征组合。

自动化的对照测试留在 `baseline_chrome_manual_test.go`，但在宿主机已有
Chrome/Edge 实例运行时无法执行：新启动的进程会把命令行交给已有实例，
`--user-data-dir` 与 `--load-extension` 全部失效，表现为跑分超时。
测试会检测到这种情况并明确跳过。要跑需先完全退出所有 Chromium 系进程。

### 为何不默认开启深色模式

亮色主题在真实用户中占多数。为降低 6 个百分点而选少数派配置会缩小匿名集，
与"看起来像一台普通真机"的目标相反——独特性本身就是识别信号。
这一项保持默认。

## low entropy: timezone 与 intl

含义是该维度在 CreepJS 的用户样本中区分度低，而非配置有误：
`America/New_York` 是最常见的时区之一。落在大匿名集里是好事，不需要处理。

## 已知短板（内核限制，非本项目缺陷）

| 维度 | 状况 |
| --- | --- |
| `screen.*` | 开关已注册但内核未实现，实测传值无效（见 `TestScreenFingerprintFlags`）。已写补丁补上消费逻辑，见 `patches/019-screen-fingerprint.patch`，需自行构建内核才生效 |
| `devicePixelRatio` | 不走 `Screen::GetRect()`，补丁未覆盖，仍报宿主机真实值 |
| WebGL metadata | 伪造仅在 Linux 生效，Windows 上 GPU vendor/renderer 改不动 |
| `deviceMemory` | 无对应参数，由内核从种子在 8/16/32 中选取 |
| Audio 噪声 | 离散度有限，14 个种子仅产出 5 个不同取值（唯一率 36%），**不可作为区分 profile 的依据**；Canvas 唯一率 100%，用它 |

## 采集方法上的坑

1. **不能用 CDP。** CreepJS 检测 CDP 痕迹，用它采集等于测量前先污染样本。
   本项目用 MV3 未打包扩展的内容脚本，共享 DOM 且无需 CDP。
2. **MV3 禁用 `unsafe-eval`。** 提取逻辑必须作为真实代码内联，
   `new Function` 求值字符串会被 CSP 拦掉。
3. **必须关掉后台节流。** 窗口被遮挡时 Chromium 会节流渲染与定时器，
   评分算不完，实测约 40% 的运行会卡住。需要
   `--disable-backgrounding-occluded-windows`、`--disable-renderer-backgrounding`、
   `--disable-background-timer-throttling`。

   这三个开关**只加在采集器里，不进生产启动路径**：禁用后台节流本身就是
   偏离 Chrome 默认行为的可检测差异，为了跑分稳定而改变日常运行环境
   是本末倒置。
4. **不能用固定等待。** 评分耗时波动，22 秒处于临界，会读到
   `FP ID: Computing...` 中间态。改为轮询就绪信号后耗时降到约 6 秒。
5. **`#creep-fingerprint` 是占位元素。** 它显示 "Computing..."，算完后
   CreepJS 用一个不带该 id 的 div 整体替换它，盯它会永远判为未就绪。
6. **BrowserScan 不能对正文做关键词匹配。** 其说明文字本身含 "robot"，
   且 `Test Results:` 后紧跟的是分区标签（Webdriver / User-Agent / CDP），
   不是结论。须读结论容器本身。
7. **明细 modal 内无换行。** 判定项明细在 `.modal-content` 里以
   "键: true/false" 连续排布，按行解析会得到空结果。

8. **必须按进程树回收内核。** Chromium 是多进程架构，只调
   `cmd.Process.Kill()` 会让渲染、GPU、网络等子进程成为孤儿，
   每个都占着已建立的 socket。实测约 40 次采集后残留 15 个进程、
   近 2000 个 TIME_WAIT，动态端口池（默认 16384 个）被耗尽，
   后续连接报 `Only one usage of each socket address is normally permitted`，
   连带把 `internal/proxy` 的测试也弄失败。

   现由 `reapKernel` 统一回收：Windows 用 `taskkill /F /T`，
   其他平台设置 `Setpgid` 后向进程组发 SIGKILL。回收失败会打日志，
   不静默吞掉——否则端口耗尽根本查不到原因。
   回归测试见 `TestRepeatedCollectLeavesNoOrphans`。

### 端口耗尽为何不能靠改监听端口解决

曾考虑把 better-web 的本地转发器挪到注册端口段（1024–49151）来规避。
实测该方案无效：耗尽的是**出站连接**占用的动态端口，不是监听端口。

按远端端口统计 TIME_WAIT 的分布：1471 条远端为 80（浏览器发出的 HTTP
请求）、257 条远端为代理端口。这些连接的本地端口由系统从动态池自动分配，
应用层无法指定。转发器自身的监听端口只有个位数，挪走省不下任何东西。

真正的消耗源是采集本身：每条浏览器连接经转发器出网会占用两个本地端口
（一条到转发器、一条到上游），而 SOCKS5 隧道的每条 TCP 流必须独立，
无法复用。因此对策是控制采集频率与可靠回收进程，而非调整端口分配。

### 统计类测试必须显式启用

`internal/proxy/bench_test.go` 中的三个 `TestReport*` 为量化吞吐会建立
大量短连接（并发 32 档一轮就有 640 条）。实测它们在常规
`go test ./...` 中运行会让 TIME_WAIT 累积到两万以上，打满 16384 个动态
端口，连带把同包内的 fail-closed 等**真实断言测试**变成随机失败
（实测 6 次运行 6 次都有失败，且每次失败的测试都不同——这是资源竞争
而非逻辑缺陷的典型特征）。

现已改为默认跳过，需显式启用：

```sh
BW_RUN_BENCH=1 go test -run TestReport ./internal/proxy/
```

三个统计测试之间也会互抢端口（前一个跑掉几百个端口后，后一个只能报
资源不足），因此用互斥锁串行执行并在其间留 3 秒回收窗口。

实测数据（本机，仅供跨版本对比）：

| 指标 | 值 |
| --- | --- |
| 建连 + 1KB 往返，直连 | 253µs |
| 同上，经转发器 | 845µs（额外 591µs，3.3x） |
| 并发 32 时均摊 | 110µs/次（优于单并发的 522µs，说明并发扩展性良好） |
| 20 个转发器启动 | 均摊 25µs/个 |

## 启动页：官方源码查对了，实现方式仍然是错的

这一条是本项目最值得记住的教训——**查证到官方源码也可能得出错误方案，
只有实测能定论**。

需求是"浏览器打开时显示什么页面"。查 Chromium 源码得到了看起来完备的答案：

- `chrome/common/pref_names.h`：键名 `session.restore_on_startup`、`session.startup_urls`
- `chrome/browser/prefs/session_startup_pref.h`：枚举值，且注意到官方注释
  "For historical reasons the enum and value registered in the prefs don't
  line up"——写入 prefs 的是 PrefValue（1/4/5/6）而非 Type（0/2/3/4）
- `chrome/installer/util/initial_preferences.h`：确认 initial_preferences
  与 `Default/Preferences` 结构相同

据此写了完整的 Preferences 读改写实现，11 个单测全过。**用真实内核一测，
不生效。** 浏览器照旧打开新标签页，日志报 `Requested load of chrome://newtab/`。

进一步查证确认了一个反直觉的事实：**内核确实读入并保留了这些键**
（`restore_on_startup = 4` 与 `startup_urls` 完好，文件从 1 个键长到 38 个），
但启动时就是不按它打开页面。

对照实测三种方式：

| 方式 | 实测 |
| --- | --- |
| 命令行末尾传 URL | **生效** |
| `--custom-ntp=<URL>` | **生效** |
| Preferences 的 `session.startup_urls` | **不生效** |

`--custom-ntp` 来自 ungoogled-chromium 的
`patches/extra/ungoogled-chromium/add-flag-for-custom-ntp.patch`，
已确认存在于内核 148 的二进制中。最终改为纯命令行实现，
删掉了 Preferences 方案。回归测试见 `TestRealKernelStartupOptions`。

### 启动 URL 必须放在参数列表最末

Chromium 把不以 `-` 开头的尾部参数当作要打开的 URL。夹在开关中间时，
它会被解析成前一个开关的值——表现为"启动页没生效，且某个开关行为异常"，
极难定位。`launcher.BuildArgs` 中有注释与测试锁住这个顺序。

### 两项设置容易混淆

| 设置 | 作用 | 实现 |
| --- | --- | --- |
| 新标签页 | 每次打开新标签都到该地址 | `--custom-ntp=<URL>` |
| 启动页 | 只在启动那一次打开，可多个 | 命令行末尾传 URL |

`--homepage` 与两者都无关，它只设置主页按钮的目标。

新标签页在实际使用中比启动页更重要：ungoogled-chromium 移除了 Google 的
新标签页组件，默认是完全空白的页面。

## 其他实现层面的坑

以下都是实际踩到并已修复的，附回归测试位置。

### marshalOptional 漏掉一个 nil 指针类型会静默出错

`store.marshalOptional` 用 switch 判断"空值"并映射成 SQL NULL。
新增可选字段时若忘了加 case，nil 指针会落到默认分支被
`json.Marshal` 成字符串 `"null"`，入库后读取时按 JSON 解析——
得到的不是 nil 而是一个零值结构体，**且不报错**。
表现为"配置明明没设置，读出来却有个空配置"。

守卫测试 `TestMarshalOptionalMapsEmptyValuesToNull` 覆盖全部可选类型，
下次漏加 case 会被拦住。

### 建索引必须放在 migrations 而非 schema

`store.Open` 先执行 `schema` 再执行 `migrate`。旧库的 `profiles` 表已存在
（`CREATE TABLE IF NOT EXISTS` 不生效）且还没有新列，此时在 schema 阶段
`CREATE INDEX ON profiles(新列)` 会因列不存在而失败，
**整个 Open 报错、旧库再也打不开**。

回归测试 `TestGroupIndexExistsOnFreshAndMigratedDatabases` 同时覆盖新库与
迁移后的旧库。另注意列名不能用 `group`——它是 SQL 保留字，本项目用 `grp`。

### SQLite 的 LIKE 需要显式声明 ESCAPE

转义了 `%` 与 `_` 但不加 `ESCAPE '\'` 子句时，SQLite 把反斜杠当普通字符
参与匹配，本应命中的记录一条也查不到。反过来，完全不转义则搜索 `%`
会匹配全部记录——两种都是静默的错误结果，比报错更难发现。
见 `store.Query` 与 `TestQueryEscapesLikeWildcards`。

### 必须按进程树回收内核

Chromium 是多进程架构，只调 `cmd.Process.Kill()` 会让渲染、GPU、网络等
子进程成为孤儿，每个都占着已建立的 socket。实测约 40 次采集后残留
15 个进程、近 2000 个 TIME_WAIT，打满 16384 个动态端口，后续连接报
`Only one usage of each socket address is normally permitted`，
**连带把 internal/proxy 的真实断言测试变成随机失败**。

现由 `probe.reapKernel` 统一回收：Windows 用 `taskkill /F /T`，
其他平台设 `Setpgid` 后向进程组发 SIGKILL。回收失败会打日志不静默吞。
回归测试 `TestRepeatedCollectLeavesNoOrphans`。

同一个坑在测试收尾时还有一个变体：`Stop` 只投递关闭消息就返回，
测试随即结束仍会留下残留。需要 `WaitSession` 等进程真正退出。

### 统计进程数不能按 wmic 的表格输出计数

`wmic process ... get ProcessId,ExecutablePath` 的默认表格格式会因列宽把
长路径折行，`grep -c` 会把同一进程重复计数（实测把 0 个残留误报成 30 个）。
须用 `/format:csv`。

## 分数的可比性

只适合跨内核版本比趋势，不宜与他人的绝对分数对照：
评分受出口 IP、系统字体、真实 GPU 等宿主机因素影响。

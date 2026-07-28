# GPU 厂商族决定能否过 Cloudflare

内核 148.0.7778.215，宿主机为某 NVIDIA 独显笔记本，直连住宅出口，2026-07-27 实测。

## 结论

Cloudflare 的判据是**伪造 GPU 与宿主机 GPU 跨厂商**，不是"存在 GPU 伪造"。

同族伪造照样开着 `--fingerprint`，照样通过；跨厂商在 Intel 与 AMD 两个方向都被拦。
这个区分很关键：它意味着不必关掉 GPU 伪造，只需让派生出的型号与宿主机同厂商。

## 十五组对照的关键结果

| 组 | 参数 | 派生 GPU | 结果 |
| --- | --- | --- | --- |
| A | 裸跑 | 真实宿主机 NVIDIA 独显 | 通过 |
| B | `--fingerprint=770828460` | Intel 集显 | 被拦 |
| I | 种子 + `--disable-spoofing=gpu` | 真实 | 通过 |
| J | 种子 + 关除 gpu 外全部 | Intel 集显 | 被拦 |
| K | 种子 + `--disable-features=WebGPU` | Intel 集显 | 被拦 |
| L | K + 屏蔽 `debug_renderer_info` | Intel 集显 | 被拦 |
| M | `--fingerprint=470000000` | **RTX 3060** | **通过** |
| N | `--fingerprint=544000000` | **RTX 4060** | **通过** |
| O | `--fingerprint=174000000` | AMD Radeon | 被拦 |

三层验证互相独立：I/J 双向确认责任在 gpu 这一项；M/N 与 B/O 确认判据是跨厂商；
K/L 排除 WebGPU 与 `debug_renderer_info` 这两条我最初怀疑的通路。
B、M、N 各复跑两轮，同批次内一致。

canvas、font、audio、clientrects 四项都不触发拦截。

## 矛盾的来源

`TestGPUSpoofContradiction` 逐项 diff 出来的：伪造**只改了 WebGL 的 vendor 与 renderer
两个字符串**，其余全是真实 NVIDIA 的值。

| 项 | spoofed vs honest |
| --- | --- |
| `vendor` / `renderer` | 已改写为 Intel |
| `pixelHash`（实际渲染结果） | **完全相同**（同一个 8 位十六进制值） |
| 扩展列表 | **完全相同**（WebGL1 35 项 / WebGL2 32 项） |
| 着色器精度 | **完全相同** |
| 能力上限 | 16 项里 15 项相同 |
| WebGPU `adapterInfo` | **完全相同**：`vendor: nvidia, architecture: <宿主机架构代号>` |

所以声称 Intel 集显却渲染出 NVIDIA 的像素、报 NVIDIA 的扩展集合、WebGPU 直接把
宿主机的真实架构代号说出来——单一信号即可判定。同族之所以能过，是因为同厂商不同型号之间
这些值本就接近。

唯一被改的能力参数是 `MAX_VERTEX_UNIFORM_VECTORS: 4095→4096`。方向其实是对的
（Intel 真机报 4096，NVIDIA D3D11 报 4095），不是问题所在。

## 为什么只能靠筛种子间接控制

内核 144 起 GPU 伪造从独立参数并入种子派生，`--fingerprint-gpu-vendor/renderer`
被移除。派生算法在内核的 C++ 代码里，**Go 侧算不出来，也没有参数可指定**。

因此唯一办法是反复生成随机种子、逐个启动内核实测，直到派生出同族型号。
每个候选一次冷启动约 2 秒，上限取 24（实测同族概率约 1/7，24 次找不到的概率约 2.4%）。

实测 14 个种子的分布：Intel 11、NVIDIA 2、AMD 1。所以宿主机是 NVIDIA 时同族种子
找得到但不密集；宿主机是 Intel 反而容易。

## 两条容易踩的推论

**`lies=0` 不代表能过商业风控。** CreepJS 查的是各伪造项**之间**是否自洽，
而 Cloudflare 能拿伪造值跟**实际渲染结果**比——后者是前者结构上查不到的维度。
README 里原先隐含"跑分满分即安全"，这次被证伪。

**筛出的种子只对当前宿主机有效。** 换机器后 GPU 族可能不同，同一个种子又变成跨厂商。
对配置迁移的后果：在 NVIDIA 机器上筛好的种子，导出后在 Intel 机器上以「保留原种子」
导入，会静默退回跨厂商，profile 配置一个字没改却从能过变成被拦。因此导出文件记录
`hostGPUFamily`，导入时两侧不同则警告。

## 端点选择

`antoinevastel.com/bots/datadome` 已不可用——作者 2024 年底离开 DataDome，
那页现在是个人主页，无任何判定输出。`nowsecure.nl` 裸 curl 直接返回 200，
没有判别力。

可用的是 `scrapingcourse.com/cloudflare-challenge`（403 + `Cf-Mitigated: challenge`）。
DataDome 侧 `datadome.co` 与 `www.leboncoin.fr` 都带 `x-datadome: protected`，
但测试期间该出口 IP 被持续限流，所有组都被拦，无法用于判断。

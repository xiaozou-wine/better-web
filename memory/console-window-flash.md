# GUI 程序调控制台命令会弹窗，四处都要抑制

## 成因

better-web 是 Windows GUI 子系统程序（PE 头 `Subsystem = 2`，无控制台）。
它 `exec.Command` 启动的 `powershell`、`taskkill`、`tasklist` 都是控制台程序，
Windows 只能为它们各新建一个控制台窗口——表现就是黑框闪一下。

修法是给 `SysProcAttr.CreationFlags` 加 `CREATE_NO_WINDOW`（`0x08000000`，
`syscall` 包没导出这个常量，按 Microsoft 文档定义写在各包内）。

不用 `HideWindow`：后者依赖 STARTUPINFO 的 `wShowWindow`，对已有控制台的场景才生效；
这里需要的是从一开始就不分配控制台。

## 四处调用点，用户只报了一处

用户只说"创建快捷方式时闪"，但顺着查发现同类调用有四处，其余几处平时也在闪，
只是没往这上面联想：

| 位置 | 命令 | 触发频率 |
| --- | --- | --- |
| `shortcut/shortcut_windows.go` | `powershell`（生成 .lnk） | 创建快捷方式 |
| `session/terminate_windows.go` | `taskkill` ×2 | **每次停止浏览器** |
| 同上 | `tasklist` | **轮询进程状态** |
| `probe/killtree_windows.go` | `taskkill /F /T` | 每次 GPU 采集收尾 |

最后一处在筛选种子时会连调最多 24 次。

教训：**用户报告的症状位置往往只是同类问题里最显眼的那一处。**
修之前先 grep 全部 `exec.Command`，确认还有哪些走的是控制台程序。

已加源码级测试锁住：遍历 `terminate_windows.go` 里所有 `exec.Command` 调用，
逐个确认被 `noWindow()` 包住。这类问题不会让任何测试失败、只有用户能看见，
所以值得用测试兜住。

## GPU 采集不能用 --headless 消除闪窗

`probe.HostGPU` / `CollectGPU` 会启动真实浏览器采集再 kill，窗口可见就会闪。
但**不能改用 `--headless`**：headless 会让 WebGL 退化为 SwiftShader，采到的 renderer
与真实驱动无关，而 GPU 探针存在的意义就是读真实驱动行为。

正确做法是 `--window-position=-32000,-32000` 把窗口移出可视区域。
实测确认无副作用：honest 组仍报宿主机真实独显，`pixelHash` 与直接启动一致，
渲染行为未变。

**跑分路径（`score.go`）方向相反，必须保持窗口可见。** 窗口被遮挡或最小化时
Chromium 会节流后台渲染与定时器，实测约 40% 的运行会卡住算不完评分。
所以那里反而有 `--disable-backgrounding-occluded-windows`。

这两条要求相反，已加测试分别锁住，防止以后被"顺手统一"。

## 顺带减少触发次数

宿主机显卡不会在运行期间变，所以给 `DetectHostGPU` 加了缓存，按内核版本分键
（不同内核对同一显卡报出的 renderer 可能不同——ANGLE 版本、驱动适配层都会变）。
导出 bundle、筛种子、界面探测三处共用一次结果。

失败不缓存：可能是内核临时不可用或超时，缓存失败会让一次偶发故障持续整个会话。

## 一个排查时的误判

最初怀疑闪的是控制台，我直接读 PE 头确认 `Subsystem = 2`（GUI，无控制台），
于是排除了"exe 自己带控制台"这个方向，转而查它启动的子进程。
读 PE 头比猜快，也比试探性加参数可靠。

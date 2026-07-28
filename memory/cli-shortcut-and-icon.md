# CLI 直启、桌面快捷方式与应用图标

2026-07-27 实现并实测。记录的是无法从代码直接看出的约束与已验证的结论。

## CLI 直启必须阻塞到浏览器退出

`better-web --profile=<名称>` 绕过管理面板直启，实现在 `cli.go`。

**不能启动后立即返回。** 代理转发器跑在本进程内（`internal/proxy` 的 Forwarder 是进程内的监听器），进程一退出转发器就消失，浏览器随即失去代理——这正是 fail-closed 要防的情形，只是原因换成了自己人。所以 `launchProfile` 用 `svc.WaitSession(id)` 阻塞，并挂 `signal.NotifyContext` 让 Ctrl+C 走正常停止流程。

参数用 profile **名称**而非 ID：ID 是 UUID，写进快捷方式属性里无法阅读也无法核对；名称在库中有唯一索引。找不到时会列出现有名称——快捷方式参数是手写的，拼错很常见。

分派必须放在 `wails.Run` 之前，否则窗口会闪一下再关。

## 快捷方式的两个安全点

`internal/shortcut` 包。Windows 生成 `.lnk`，Linux 生成 `.desktop`，macOS 明确报不支持（`.app` 需要 Info.plist 与签名配合，生成一个不能用的文件比报错更糟）。

**PowerShell 注入。** `.lnk` 是 COM 的 IShellLink 二进制格式，手写不现实，借 `WScript.Shell` 生成。但 profile 名称是用户输入，直接拼进脚本文本会让 `$(whoami)` 这类内容变成可执行代码。做法是整段脚本 base64 编码后用 `-EncodedCommand` 传入（UTF-16LE 再 base64），**值全部通过环境变量传递，脚本里只引用变量名**。测试覆盖了 `$(...)`、反引号、分号、与号、引号五类输入。

**路径逃逸。** 名称含 `..\..\Windows` 会让文件写到目标目录外。`sanitizeFileName` 替换所有路径分隔符与 Windows 保留字符，另外处理两个容易漏的点：控制字符（< 0x20），以及结尾的点与空格——Windows 会静默去掉它们，导致实际文件名与预期不符。测试断言拼接后路径必须仍在目标目录内。

**不要设 IconLocation。** 曾显式设 `$lnk.IconLocation = exe + ',0'`，实测 `WScript.Shell` 根本不把它写进 lnk（用 UTF-16 与 latin-1 两种视图都搜不到 `,0`）。但快捷方式图标是正确的——因为目标本身就是 exe，Windows 默认取它的第一个图标。该设置属于冗余。

## 图标用脚本生成

`build/makeicon.py` 生成 `build/appicon.png`（1024²）与 `build/windows/icon.ico`（6 尺寸）。原来的是 Wails 模板默认图标。

用脚本而非提交二进制：改配色或形状时改脚本比重开设计工具快，且改动在 diff 里看得见。

**每个尺寸独立渲染，不从大图缩放。** 缩放会让 16px 的细环消失；独立渲染时线宽按比例（`wr * size`）保持可见。手写 2×2 超采样抗锯齿，避免为一次性脚本引入 Pillow。

ico 内嵌 PNG 而非 BMP（Vista 起支持），省体积且带 alpha 语义。目录项里 256 记为 0。

验证方式：解析 ico 目录确认 6 帧都是有效内嵌 PNG，再确认 6 段数据都出现在编译后的 exe 内。

## 已验证

```
--help                  正常输出，退出码 0
--profile=错误名字       列出现有 profile，退出码 1
--profile=test          启动成功，走代理，报出 ASN 警告
test.lnk                目标路径 + --profile=test 正确写入
icon                    6 帧全部嵌入 exe
```

## 一个反复出现的坑

`wails build` 失败时 `build/bin/better-web.exe` 保持旧版本，界面上看不到新功能，容易误判成「功能没做」。本轮就发生过：`transfer.Export` 加了 `model.GPUFamily` 参数后 `bundle.go` 未同步，构建连续失败，而 `go build ./...` 单独跑是过的（Wails 的绑定生成阶段才报错）。**看到界面缺功能时先核对 `build/bin` 的时间戳。**

在 Git Bash 里手动 `taskkill /PID` 需要 `MSYS_NO_PATHCONV=1`，否则 `/PID` 被转换成 `C:/Program Files/Git/PID`。

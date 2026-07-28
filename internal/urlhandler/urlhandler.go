// Package urlhandler 把 better-web 注册成系统的候选默认浏览器，
// 使其他应用点开的 http/https 链接能交给指定的 profile 打开。
//
// 只能注册成"候选"，不能直接设为默认。Windows 的 UserChoice 键带一个
// 未公开算法的 Hash 校验值，且 Win11 起有 UCPD 驱动拦截对它的直接写入——
// 这是有意的设计，防的就是应用私自抢走默认浏览器。
// 见 https://learn.microsoft.com/windows/win32/shell/default-programs
//
// 因此最后一步必须由用户在"设置 → 默认应用"里手动选。调用方的提示文案
// 必须说清这点：注册成功不等于已经生效，否则用户点完按钮就以为好了。
package urlhandler

import "errors"

// ErrUnsupported 表示当前平台没有实现默认浏览器注册。
//
// 非 Windows 平台返回它而非静默成功：静默成功会让界面显示"已注册"，
// 而用户点链接时什么都不会发生，且没有任何线索指向原因。
var ErrUnsupported = errors.New("当前平台不支持注册为默认浏览器候选")

// ProgID 是 better-web 在系统里的程序标识。
//
// 用自有前缀而非复用 ChromeHTML 之类：ProgID 决定了 shell\open\command，
// 复用别人的会改掉那个浏览器的启动命令。
const ProgID = "BetterWebURL"

// appName 是写进 RegisteredApplications 的键名，也是设置界面里显示的名称。
const appName = "better-web"

// appDescription 是设置界面里的说明文字。
//
// 必填项：按 MS 文档，缺 ApplicationDescription 的应用不会出现在
// 默认应用的候选列表里——注册看似成功，但用户在设置里找不到它。
const appDescription = "指纹浏览器管理器，把链接交给指定的 profile 打开"

// openURLFlag 是传给自身的参数名，与 main 包 cli.go 的 openURLFlag 必须一致。
//
// 两处各自定义而非共用一个常量：internal 包不能被 main 包之外引用，
// 而 main 包也不能被 internal 引用。有测试钉住一致性，见 cli_test.go。
const openURLFlag = "--open-url="

// CommandLineFor 返回注册表里 shell\open\command 应写入的命令行。
//
// 抽成导出函数是为了让 main 包的测试能核对开关名一致——那是个跨包的隐式
// 契约，漂移的表现是点链接后进程起来又立刻退出，且不报任何错。
//
// 可执行文件路径必须加引号：路径里有空格时（Program Files 是默认安装位置）
// ShellExecute 会把 "C:\Program" 当成程序名。
func CommandLineFor(exePath string) string {
	return `"` + exePath + `" ` + openURLFlag + `%1`
}

// settingsURI 是 Windows 默认应用设置页的地址。
//
// 需要打开它是因为最后一步只能由用户手动完成，而"设置 → 应用 → 默认应用"
// 藏得深。直接跳到那一页能省掉一段说不清的路径描述。
const settingsURI = "ms-settings:defaultapps"

// Status 是注册状态的快照。
type Status struct {
	// Registered 表示已写入注册表、会出现在系统的默认应用候选列表里。
	Registered bool `json:"registered"`
	// IsDefault 表示用户已在系统设置里把 better-web 选为 https 的默认处理程序。
	//
	// 与 Registered 分开呈现：注册是程序能做到的，设为默认只有用户能做。
	// 合成一个状态会让"已注册但没生效"这个最常见的中间态无法表达。
	IsDefault bool `json:"isDefault"`
}

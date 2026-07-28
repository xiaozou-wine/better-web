//go:build windows

package probe

import (
	"os/exec"
	"syscall"
)

// hideWindow 让子进程不创建自己的控制台窗口。
//
// 只影响 Chromium 启动时可能一并弹出的控制台，不影响浏览器窗口本身——
// 那个由 --window-position 移出可视区域处理。两者一起才能做到采集全程
// 不在用户面前闪任何东西。
//
// CREATE_NO_WINDOW 而非 HideWindow：后者只对控制台程序生效，而 chrome.exe
// 是 GUI 子系统程序，需要前者才能阻止系统为它分配控制台。
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// createNoWindow 对应 Windows 的 CREATE_NO_WINDOW 进程创建标志。
//
// syscall 包没有导出这个常量，因此按 Microsoft 文档的定义写在此处：
// https://learn.microsoft.com/windows/win32/procthread/process-creation-flags
const createNoWindow = 0x08000000

//go:build windows

package session

import (
	"os/exec"
	"syscall"
)

// createNoWindow 对应 Windows 的 CREATE_NO_WINDOW 进程创建标志。
//
// syscall 包没有导出这个常量，因此按 Microsoft 文档的定义写在此处：
// https://learn.microsoft.com/windows/win32/procthread/process-creation-flags
const createNoWindow = 0x08000000

// noWindow 让子进程不创建控制台窗口，返回原 cmd 便于链式使用。
//
// 必须给每个 taskkill / tasklist 调用都加上：better-web 是 GUI 子系统程序
// （无控制台），而这些都是控制台程序，Windows 会为它们各新建一个控制台——
// 表现就是启动、停止浏览器时黑框闪一下。
//
// 这类命令在正常使用中调用频繁（每次停止会话至少一次 taskkill，
// 还有轮询状态用的 tasklist），漏掉一处就会持续闪。
func noWindow(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}

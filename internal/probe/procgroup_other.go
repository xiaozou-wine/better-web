//go:build !windows

package probe

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 让子进程自成一个进程组，使 killProcessTree 能一次终止
// 整棵树。不设置的话 Chromium 的子进程会脱离控制，成为占着 socket 的孤儿。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

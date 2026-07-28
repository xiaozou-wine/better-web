//go:build !windows

package probe

import (
	"os"
	"syscall"
)

// killProcessTree 终止指定进程及其全部子进程。
//
// 必须按进程树杀：Chromium 是多进程架构，只终止主进程会留下渲染、GPU、
// 网络等子进程，它们各自占着已建立的 socket，连续采集会耗尽端口池。
//
// 这里向进程组发信号。要让它生效，启动时必须设置 Setpgid 使子进程
// 归入同一进程组，见 startProcessGroup。
func killProcessTree(pid int) error {
	// 负 pid 表示整个进程组。
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		// 进程组不存在时退回单进程终止，例如未设置 Setpgid 的情况。
		p, findErr := os.FindProcess(pid)
		if findErr != nil {
			return err
		}
		return p.Kill()
	}
	return nil
}

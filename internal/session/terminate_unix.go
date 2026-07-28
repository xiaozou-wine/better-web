//go:build !windows

package session

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// gracefulTimeout 是等待浏览器自行退出的上限。
// Chromium 关闭时要落盘会话数据，profile 越大越慢。
const gracefulTimeout = 15 * time.Second

// terminate 在类 Unix 系统上优雅地结束浏览器进程。
//
// SIGTERM 是 Chromium 的正常退出信号，会走完落盘流程；直接 SIGKILL 会丢失
// 未落盘的 Cookie 与登录态，并让下次启动弹出"未正确关闭"的恢复提示。
// 只有在宽限期内没退出时才升级为强杀。
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		// 进程已经不在了，无需再做处理。
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		// 信号发不出去时直接尝试强杀，不然进程会一直挂着。
		return killNow(p)
	}
	if waitExit(p, gracefulTimeout) {
		return nil
	}
	return killNow(p)
}

func killNow(p *os.Process) error {
	if err := p.Kill(); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		if waitExit(p, 2*time.Second) {
			return nil
		}
		return fmt.Errorf("强制结束进程 %d 失败: %w", p.Pid, err)
	}
	return nil
}

// waitExit 轮询等待进程退出，返回是否已退出。
//
// 用轮询而非 Process.Wait：cmd.Wait 已由 start 中的 goroutine 独占调用，
// 重复调用会返回错误。
func waitExit(p *os.Process, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(p) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return !alive(p)
}

// alive 报告进程是否仍在运行。
func alive(p *os.Process) bool {
	if p == nil {
		return false
	}
	err := p.Signal(syscall.Signal(0))
	return !errors.Is(err, os.ErrProcessDone)
}

// processAlive 按 PID 判断进程是否仍在运行。
//
// 信号 0 不投递任何内容，只做权限与存在性检查，是判断进程存活的惯用法。
// 判断失败时保守地认为仍在运行：用于终止流程时，误判为已退出会跳过强杀而
// 留下孤儿进程；用于锁检查时，误判为已退出会让两个实例同时打开一个
// user-data-dir 而损坏数据。
//
// 注意 FindProcess 在 Unix 上从不失败（它只是包装 PID），所以必须靠
// Signal 的返回值判断。EPERM 表示进程存在但不属于当前用户，仍算存活。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	// EPERM 一类错误说明进程存在但无权操作，属于存活。
	return !errors.Is(err, syscall.ESRCH)
}

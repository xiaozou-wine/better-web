//go:build windows

package session

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// gracefulTimeout 是等待浏览器自行退出的上限。
//
// Chromium 关闭时要落盘 Cookie、登录态与 Session Storage，profile 越大越慢。
// 超时后只能强杀，因此这个值宁可给宽些——用户点了停止最多等这么久，
// 而数据丢失是不可逆的。
const gracefulTimeout = 15 * time.Second

// terminate 在 Windows 上优雅地结束浏览器进程。
//
// 为什么不用 Process.Signal(os.Interrupt)：Windows 不支持向 GUI 进程投递
// 该信号，调用必定失败，于是代码退回 Kill——等于每次停止都在硬杀浏览器。
// Chromium 被 Kill 时未落盘的会话数据会丢失，且下次启动会弹出"未正确关闭"
// 的恢复提示，那个提示本身对多账号场景就是额外的异常信号。
//
// taskkill 不带 /F 时会向窗口投递 WM_CLOSE，等价于用户点关闭按钮，
// Chromium 由此走正常退出流程，并自行带走全部子进程。
//
// 关键细节：不能加 /T。实测（TestProbeTaskkillOnRealChromium）表明 /T 会先
// 尝试关闭子进程，而渲染进程没有窗口、无法用 WM_CLOSE 关闭，taskkill 随即
// 以"子进程仍在运行"为由拒绝关闭主进程，整个调用失败。只针对主进程投递
// WM_CLOSE 才有效。
//
// 只有在宽限期内没退出时才升级为强杀。
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// taskkill 自身可能因进程已退出而报错，此处不视为失败：
	// 后面统一以进程是否真的结束为判断依据。
	cmd := noWindow(exec.CommandContext(ctx, "taskkill", "/PID", fmt.Sprint(p.Pid)))
	_ = cmd.Run()

	if waitExit(p, gracefulTimeout) {
		return nil
	}

	// 宽限期内没退出，只能强杀。数据可能不完整，但不能让进程永远挂着。
	//
	// 这里要带 /T /F：优雅关闭失败说明主进程已失去响应，不会再自行回收
	// 渲染进程，只杀主进程会留下一批孤儿进程占住 user-data-dir，
	// 导致该 profile 再也启动不了。
	kctx, kcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer kcancel()
	_ = noWindow(exec.CommandContext(kctx, "taskkill",
		"/PID", fmt.Sprint(p.Pid), "/T", "/F")).Run()
	if waitExit(p, 5*time.Second) {
		return nil
	}

	// taskkill 也没能结束它，退回 Go 自带的 Kill 作为最后手段。
	if err := p.Kill(); err != nil {
		// 进程可能刚好在此刻退出，再确认一次避免误报。
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
// 重复调用会返回错误。这里只需知道进程是否还在。
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
	return processAlive(p.Pid)
}

// processAlive 按 PID 判断进程是否仍在运行。
//
// Windows 上 Signal(syscall.Signal(0)) 这类 Unix 惯用法不可用，改查 tasklist。
// 查询失败时保守地认为仍在运行：用于终止流程时，误判为已退出会跳过强杀而
// 留下孤儿进程；用于锁检查时，误判为已退出会让两个实例同时打开一个
// user-data-dir 而损坏数据。两处的代价都指向同一个保守方向。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := noWindow(exec.CommandContext(ctx, "tasklist",
		"/FI", fmt.Sprintf("PID eq %d", pid), "/NH", "/FO", "CSV")).Output()
	if err != nil {
		return true
	}
	// 无匹配时 tasklist 输出提示文字而非 CSV 行，据此判断进程已退出。
	return bytes.Contains(out, []byte(fmt.Sprintf("%d", pid)))
}

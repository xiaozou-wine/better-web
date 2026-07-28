//go:build windows

package probe

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// killProcessTree 终止指定进程及其全部子进程。
//
// 必须按进程树杀：Chromium 是多进程架构，主进程之下还有渲染、GPU、
// 网络等子进程。只调 Process.Kill() 会留下一堆孤儿进程，每个都占着
// 已建立的 socket，连续多次采集会把动态端口池耗尽
// （实测约 40 次采集后残留 15 个进程、近 2000 个 TIME_WAIT，
// 表现为后续连接报 "Only one usage of each socket address"）。
//
// 用 taskkill /T 而非枚举进程树自行终止：/T 由系统按父子关系处理，
// 不会因枚举与终止之间的竞态漏掉刚创建的子进程。
func killProcessTree(pid int) error {
	// /F 强制终止，/T 连带子进程。
	// 带上输出便于诊断：静默失败会让残留进程问题无从排查。
	//
	// 同样要抑制控制台窗口：筛选种子时每个候选采集完都会调一次，
	// 最多 24 次，漏掉这里就会连闪二十几下。
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill 终止进程树 %d 失败: %w (%s)",
			pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

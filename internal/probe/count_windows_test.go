//go:build windows

package probe

import (
	"os/exec"
	"strings"
	"testing"
)

// countKernelProcesses 统计指定可执行文件对应的运行中进程数。
//
// 按完整路径匹配而非仅按进程名：宿主机上通常还装着正常的 Chrome，
// 按 chrome.exe 计数会把用户自己的浏览器算进来，导致断言毫无意义。
func countKernelProcesses(t *testing.T, execPath string) int {
	t.Helper()
	// WMIC 的 WHERE 子句需要把反斜杠转义成双反斜杠。
	escaped := strings.ReplaceAll(execPath, `\`, `\\`)
	// 用 /format:csv 而非默认的表格格式：表格输出会因列宽把长路径折行，
	// 按行计数会把同一个进程重复算多次（实测把 0 个残留误报成 30 个）。
	out, err := exec.Command("wmic", "process", "where",
		"ExecutablePath='"+escaped+"'", "get", "ProcessId", "/format:csv").Output()
	if err != nil {
		// 查不到进程时 wmic 以非零退出并输出 "No Instance(s) Available."
		return 0
	}

	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// CSV 首行是表头 "Node,ProcessId"，其余每行一个进程。
		if line == "" || strings.HasPrefix(line, "Node") {
			continue
		}
		count++
	}
	return count
}

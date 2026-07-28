//go:build !windows

package probe

import (
	"os/exec"
	"strings"
	"testing"
)

// countKernelProcesses 统计指定可执行文件对应的运行中进程数。
//
// 按完整路径匹配而非仅按进程名：宿主机上通常还装着正常的 Chrome，
// 按名字计数会把用户自己的浏览器算进来。
func countKernelProcesses(t *testing.T, execPath string) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", execPath).Output()
	if err != nil {
		// pgrep 无匹配时以非零退出，属正常情况。
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

//go:build !windows

package probe

import (
	"os/exec"
	"strings"
)

// chromiumRivals 是会抢占命令行的 Chromium 系浏览器进程名。
var chromiumRivals = []string{"chrome", "chromium", "msedge"}

// chromiumAlreadyRunning 报告是否有 Chromium 系浏览器正在运行，
// 并返回首个命中的进程名。
func chromiumAlreadyRunning() (string, bool) {
	for _, name := range chromiumRivals {
		out, err := exec.Command("pgrep", "-x", name).Output()
		if err != nil {
			// pgrep 无匹配时以非零退出，这属于正常情况。
			continue
		}
		if strings.TrimSpace(string(out)) != "" {
			return name, true
		}
	}
	return "", false
}

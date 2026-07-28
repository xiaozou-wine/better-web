//go:build windows

package probe

import (
	"os/exec"
	"strings"
)

// chromiumRivals 是会抢占命令行的 Chromium 系浏览器进程名。
// 这些浏览器同一 profile 只允许一个实例，新进程会把参数转交给已有实例。
var chromiumRivals = []string{"chrome.exe", "msedge.exe"}

// chromiumAlreadyRunning 报告是否有 Chromium 系浏览器正在运行，
// 并返回首个命中的进程名。
//
// 用 tasklist 而非枚举进程 API：只需一个布尔结论，避免为此引入依赖。
func chromiumAlreadyRunning() (string, bool) {
	for _, name := range chromiumRivals {
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH").Output()
		if err != nil {
			// 查不到就不阻塞测试，让后续步骤自行失败并给出真实原因。
			continue
		}
		if strings.Contains(string(out), name) {
			return name, true
		}
	}
	return "", false
}

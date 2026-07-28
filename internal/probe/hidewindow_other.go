//go:build !windows

package probe

import "os/exec"

// hideWindow 在非 Windows 平台无需处理：控制台窗口是 Windows 特有的概念，
// 而浏览器窗口本身由 --window-position 移出可视区域。
func hideWindow(_ *exec.Cmd) {}

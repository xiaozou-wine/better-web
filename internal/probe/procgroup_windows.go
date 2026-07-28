//go:build windows

package probe

import "os/exec"

// setProcessGroup 在 Windows 上无需额外设置：killProcessTree 用
// taskkill /T 按父子关系终止整棵树，不依赖进程组。
func setProcessGroup(cmd *exec.Cmd) {}

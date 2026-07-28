//go:build !windows

package kernel

import (
	"fmt"
	"os"
	"path/filepath"
)

// chromeCandidates 是各平台官方 Chrome 的常见安装位置。
//
// 非 Windows 平台没有注册表，只能按已知路径查找。这几条覆盖了 macOS 的
// 应用包与 Linux 各发行版包管理器的默认位置。
func chromeCandidates() []string {
	home, _ := os.UserHomeDir()
	out := []string{
		// macOS
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		// Linux 常见位置
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/opt/google/chrome/chrome",
		"/snap/bin/google-chrome",
	}
	if home != "" {
		out = append(out,
			filepath.Join(home, "Applications", "Google Chrome.app",
				"Contents", "MacOS", "Google Chrome"))
	}
	return out
}

// SystemChrome 返回系统已装的官方 Chrome。
//
// 未安装时返回 ErrNotFound。不读版本号：非 Windows 平台没有以版本命名的
// 安装子目录，而版本号只用于界面显示，缺它不影响启动。
func SystemChrome() (Kernel, error) {
	for _, p := range chromeCandidates() {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return Kernel{
				ExecPath: p,
				Source:   SourceSystemChrome,
				Name:     "系统 Chrome",
			}, nil
		}
	}
	return Kernel{}, fmt.Errorf("%w: 未检测到系统安装的 Google Chrome", ErrNotFound)
}

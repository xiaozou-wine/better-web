//go:build windows

package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// chromeRegKeys 是官方 Chrome 登记安装路径的注册表位置，按优先级排列。
//
// 用注册表而非硬编码 Program Files 路径：Chrome 支持用户级安装
// （装到 %LOCALAPPDATA%）和自定义位置，写死路径会漏掉这些情况。
//
// App Paths 是 Windows 记录可执行文件位置的标准机制，比
// StartMenuInternet 更可靠——后者的 command 值带引号和参数，要额外解析。
var chromeRegKeys = []struct {
	root registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`},
	{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`},
}

// findSystemChromePath 定位系统已装的官方 Chrome。
//
// 找不到时返回空串而非错误：未装 Chrome 是正常状态，调用方据此回退。
func findSystemChromePath() string {
	for _, k := range chromeRegKeys {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		// App Paths 的默认值就是可执行文件全路径。
		val, _, err := key.GetStringValue("")
		_ = key.Close()
		if err != nil {
			continue
		}
		p := strings.Trim(strings.TrimSpace(val), `"`)
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	// 注册表查不到时兜底几个默认位置。Chrome 偶有安装未写注册表的情况
	// （便携部署、企业分发），而这几个路径覆盖了官方安装器的全部默认值。
	for _, base := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// systemChromeVersion 读取 Chrome 的版本号。
//
// 从安装目录下的版本子目录名取，而不是执行 chrome.exe --version：
// 后者在 Windows 上不往 stdout 输出版本（这是 Chrome 的已知行为），
// 而安装目录里必然有一个以版本号命名的子目录。
func systemChromeVersion(execPath string) string {
	dir := filepath.Dir(execPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 版本目录形如 150.0.7871.186，首段必须是数字。
		name := e.Name()
		major, _, ok := strings.Cut(name, ".")
		if !ok || major == "" {
			continue
		}
		if strings.IndexFunc(major, func(r rune) bool {
			return r < '0' || r > '9'
		}) >= 0 {
			continue
		}
		if best == "" || compareVersion(name, best) > 0 {
			best = name
		}
	}
	return best
}

// SystemChrome 返回系统已装的官方 Chrome。
//
// 未安装时返回 ErrNotFound。版本号读不到时仍返回该内核，只是 Version 为空：
// 版本号只用于界面显示，缺它不影响启动。
func SystemChrome() (Kernel, error) {
	p := findSystemChromePath()
	if p == "" {
		return Kernel{}, fmt.Errorf("%w: 未检测到系统安装的 Google Chrome", ErrNotFound)
	}
	return Kernel{
		Version:  systemChromeVersion(p),
		ExecPath: p,
		Source:   SourceSystemChrome,
		Name:     "系统 Chrome",
	}, nil
}

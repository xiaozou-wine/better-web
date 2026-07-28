//go:build !windows

package shortcut

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func desktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位用户目录失败: %w", err)
	}
	return filepath.Join(home, "Desktop"), nil
}

// create 在类 Unix 系统上生成启动器。
//
// Linux 用 freedesktop 的 .desktop 格式；macOS 的等价物是 .app 目录包，
// 结构复杂且需要 Info.plist 与代码签名配合，不在本包范围内——
// macOS 上返回明确的错误而非生成一个不能用的文件。
func create(dir, profileName, exePath string) (string, error) {
	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("macOS 暂不支持自动创建快捷方式；" +
			"可手动用「自动操作」或 shell 脚本调用 better-web --profile=<名称>")
	}

	path := filepath.Join(dir, sanitizeFileName(profileName)+".desktop")

	// Exec 里的参数必须转义：profile 名称可能含空格或引号，
	// 未转义会让 Exec 行被切成多个参数，或提前闭合引号。
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=better-web profile
Exec=%s --profile=%s
Terminal=false
Categories=Network;WebBrowser;
`, escapeDesktopValue(profileName), quoteExec(exePath), quoteExec(profileName))

	// 0700 而非 0755：文件里含 profile 名称，属于本用户的配置。
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return "", fmt.Errorf("写入 .desktop 文件失败: %w", err)
	}
	return path, nil
}

// escapeDesktopValue 转义 .desktop 值中的特殊字符。
// 规范要求反斜杠与换行必须转义，否则解析会出错或截断。
func escapeDesktopValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// quoteExec 把值包成 Exec 行可安全使用的带引号字符串。
// 规范中双引号内需转义反斜杠与双引号本身。
func quoteExec(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

//go:build windows

package shortcut

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
)

// createNoWindow 对应 Windows 的 CREATE_NO_WINDOW 进程创建标志。
//
// syscall 包没有导出这个常量，因此按 Microsoft 文档的定义写在此处：
// https://learn.microsoft.com/windows/win32/procthread/process-creation-flags
const createNoWindow = 0x08000000

// desktopDir 返回当前用户的桌面目录。
//
// 优先读 USERPROFILE 而非调 SHGetKnownFolderPath：后者要 syscall 且返回的
// 已重定向路径（OneDrive 接管桌面时）反而更难处理。桌面被重定向时
// 用户自己会发现快捷方式没出现，可用 Dir 显式指定。
func desktopDir() (string, error) {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		return "", fmt.Errorf("未设置 USERPROFILE，无法定位桌面")
	}
	desktop := filepath.Join(home, "Desktop")
	if _, err := os.Stat(desktop); err == nil {
		return desktop, nil
	}
	// OneDrive 接管桌面时的常见位置。
	if od := os.Getenv("OneDrive"); od != "" {
		if alt := filepath.Join(od, "Desktop"); dirExists(alt) {
			return alt, nil
		}
	}
	return desktop, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// create 生成 .lnk 快捷方式。
//
// .lnk 是 COM 的 IShellLink 二进制格式，手写不现实，因此借 PowerShell 的
// WScript.Shell 生成。安全要点：参数不能拼进脚本文本——profile 名称由用户
// 输入，其中的引号或 $( ) 会变成可执行代码。改为把整段脚本 base64 编码后
// 用 -EncodedCommand 传入，值本身通过环境变量传递，脚本里只引用变量名。
//
// 不设 IconLocation：目标本身就是 exe，Windows 默认取它的第一个图标，
// 即 build/windows/icon.ico 嵌进二进制的那个。实测显式设置也不会被
// WScript.Shell 写进 lnk，属于冗余。
func create(dir, profileName, exePath string) (string, error) {
	linkPath := filepath.Join(dir, sanitizeFileName(profileName)+".lnk")

	// 脚本里不出现任何用户输入，全部走环境变量。
	const script = `$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut($env:BW_LNK_PATH)
$lnk.TargetPath = $env:BW_EXE_PATH
$lnk.Arguments = '--profile=' + $env:BW_PROFILE_NAME
$lnk.WorkingDirectory = Split-Path -Parent $env:BW_EXE_PATH
$lnk.Description = 'better-web: ' + $env:BW_PROFILE_NAME
$lnk.Save()`

	ctxTimeout := 20 * time.Second
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", encodeUTF16LE(script))
	cmd.Env = append(os.Environ(),
		"BW_LNK_PATH="+linkPath,
		"BW_EXE_PATH="+exePath,
		"BW_PROFILE_NAME="+profileName,
	)
	// 不让 PowerShell 弹出控制台窗口。
	//
	// better-web 是 GUI 子系统程序（无控制台），而 powershell.exe 是控制台
	// 程序，Windows 会为它新建一个控制台——表现就是创建快捷方式时黑框闪一下。
	//
	// CREATE_NO_WINDOW 而非 HideWindow：后者依赖 STARTUPINFO 的 wShowWindow，
	// 对已有控制台的场景才生效；这里需要的是从一开始就不分配控制台。
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("调用 PowerShell 失败: %w", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("创建快捷方式失败: %w", err)
		}
	case <-time.After(ctxTimeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("创建快捷方式超时")
	}

	// 以文件是否真的出现为准，而非只看退出码：
	// COM 调用失败时 PowerShell 有时仍返回 0。
	if _, err := os.Stat(linkPath); err != nil {
		return "", fmt.Errorf("快捷方式未生成: %w", err)
	}
	return linkPath, nil
}

// encodeUTF16LE 把脚本编码成 PowerShell -EncodedCommand 要求的格式：
// UTF-16LE 再 base64。
func encodeUTF16LE(s string) string {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

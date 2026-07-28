// Package shortcut 在桌面创建直接启动某个 profile 的快捷方式。
//
// 用途：绕过管理面板双击直达。目标是当前可执行文件加 --profile=<名称> 参数。
package shortcut

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options 描述要创建的快捷方式。
type Options struct {
	// ProfileName 是要启动的 profile 名称，会作为 --profile 参数写入。
	ProfileName string
	// Dir 是快捷方式的存放目录，留空时用桌面。
	Dir string
	// ExePath 是目标可执行文件，留空时用当前进程的路径。
	ExePath string
}

// Create 创建快捷方式，返回其完整路径。
func Create(opt Options) (string, error) {
	name := strings.TrimSpace(opt.ProfileName)
	if name == "" {
		return "", fmt.Errorf("profile 名称为空")
	}

	exe := opt.ExePath
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("定位当前程序路径失败: %w", err)
		}
	}
	// 解析符号链接：从 go run 启动时 os.Executable 指向临时目录，
	// 那个路径在进程退出后就失效了，写进快捷方式会得到一个死链接。
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	dir := opt.Dir
	if dir == "" {
		var err error
		dir, err = desktopDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}

	return create(dir, name, exe)
}

// sanitizeFileName 把 profile 名称转成合法文件名。
//
// 用户可以给 profile 起任何名字，其中的路径分隔符与保留字符必须替换，
// 否则创建快捷方式会失败，或更糟——写到意料之外的目录去。
func sanitizeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		// Windows 的保留字符，其中 / 与 \ 是路径分隔符，必须处理。
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b.WriteByte('_')
		default:
			if r < 0x20 {
				b.WriteByte('_')
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	// 结尾的点与空格在 Windows 上会被静默去掉，导致实际文件名与预期不符。
	out = strings.TrimRight(out, ". ")
	if out == "" {
		out = "profile"
	}
	// 留出扩展名与路径长度余量。
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

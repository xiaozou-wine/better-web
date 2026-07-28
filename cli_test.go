package main

import (
	"strings"
	"testing"

	"better-web/internal/urlhandler"
)

// TestOpenURLFlagMatchesRegistryCommand 钉住 CLI 与注册表写入的开关名一致。
//
// urlhandler 往注册表写的命令行是 "<exe>" --open-url=%1，而解析它的是 cli.go。
// 两处是跨包的隐式契约，internal 包不能引用 main 包，因此只能各自定义常量。
// 漂移的表现是点链接后 better-web 起来又立刻退出（参数没被识别），
// 而且不报任何错——正是那种最难查的故障。
func TestOpenURLFlagMatchesRegistryCommand(t *testing.T) {
	cmd := urlhandler.CommandLineFor(`C:\bw\better-web.exe`)
	if !strings.Contains(cmd, openURLFlag) {
		t.Fatalf("注册表命令行 %q 不含 cli.go 定义的开关 %q", cmd, openURLFlag)
	}
	// %1 必须紧跟在开关后面，否则 shell 传进来的 URL 会成为独立参数。
	if !strings.HasSuffix(cmd, openURLFlag+"%1") {
		t.Errorf("注册表命令行应以 %s%%1 结尾, 实际为 %q", openURLFlag, cmd)
	}
}

// TestFlagsAreDistinct 钉住两个开关不会互相前缀匹配。
//
// runCLI 用 strings.HasPrefix 分派，若一个开关是另一个的前缀，
// 传前者时会同时命中两个分支。
func TestFlagsAreDistinct(t *testing.T) {
	if strings.HasPrefix(profileFlag, openURLFlag) ||
		strings.HasPrefix(openURLFlag, profileFlag) {
		t.Errorf("开关 %q 与 %q 互为前缀，分派会串", profileFlag, openURLFlag)
	}
}

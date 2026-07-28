//go:build windows

package session

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

// 每个 taskkill / tasklist 调用都必须抑制控制台窗口。
//
// better-web 是 GUI 子系统程序（无控制台），而这些都是控制台程序，Windows
// 会为它们各新建一个控制台——表现就是启动、停止浏览器时黑框闪一下。
//
// 这类调用在正常使用中很频繁：每次停止会话至少一次 taskkill，还有轮询状态
// 用的 tasklist。漏掉一处就会持续闪，而这种问题不会让任何测试失败，
// 只有用户能看见。因此用源码断言把它锁住。
func TestConsoleCommandsSuppressWindow(t *testing.T) {
	src, err := os.ReadFile("terminate_windows.go")
	if err != nil {
		t.Fatalf("读取源码失败: %v", err)
	}
	text := string(src)

	// 找出全部 exec.Command / exec.CommandContext 调用，逐个确认被 noWindow 包住。
	call := regexp.MustCompile(`exec\.Command(Context)?\(`)
	locs := call.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		t.Fatal("未找到任何 exec.Command 调用，测试的前提已不成立")
	}

	for _, loc := range locs {
		// noWindow(...) 必须紧邻在调用之前。取前 12 个字符足够覆盖
		// "noWindow(" 加少量空白。
		start := loc[0] - 12
		if start < 0 {
			start = 0
		}
		prefix := text[start:loc[0]]
		if !strings.Contains(prefix, "noWindow(") {
			line := strings.Count(text[:loc[0]], "\n") + 1
			t.Errorf("terminate_windows.go:%d 的 exec.Command 未用 noWindow 包裹，"+
				"会弹出控制台窗口", line)
		}
	}
}

// noWindow 必须设置 CREATE_NO_WINDOW。
func TestNoWindowSetsFlag(t *testing.T) {
	cmd := noWindow(exec.Command("cmd", "/c", "echo"))
	if cmd.SysProcAttr == nil {
		t.Fatal("未设置 SysProcAttr")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Error("未设置 CREATE_NO_WINDOW 标志")
	}
}

// 已有 SysProcAttr 时应按位追加而非整体替换，
// 否则会抹掉调用方设置的其他进程创建标志。
func TestNoWindowPreservesExistingFlags(t *testing.T) {
	const other = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	cmd := exec.Command("cmd", "/c", "echo")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: other}
	noWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&other == 0 {
		t.Error("覆盖了已有的进程创建标志")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Error("未追加 CREATE_NO_WINDOW")
	}
}

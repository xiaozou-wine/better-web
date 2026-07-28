package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"better-web/internal/kernel"
)

// fakeKernelSrc 是假内核源码：把收到的命令行参数写入 BW_ARGS_FILE 指定的
// 文件，然后驻留等待被终止，以此模拟浏览器进程。
const fakeKernelSrc = `package main

import (
	"os"
	"strings"
	"time"
)

func main() {
	if out := os.Getenv("BW_ARGS_FILE"); out != "" {
		_ = os.WriteFile(out, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	// 驻留，等待 Stop 终止。设上限避免测试异常时留下孤儿进程。
	time.Sleep(60 * time.Second)
}
`

// buildFakeKernel 编译假内核并安装到内核目录，返回 Store。
// 整个包只编译一次。
func buildFakeKernel(t *testing.T, version string) *kernel.Store {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建内核目录失败: %v", err)
	}

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(fakeKernelSrc), 0o600); err != nil {
		t.Fatalf("写入假内核源码失败: %v", err)
	}

	name := "chrome"
	if runtime.GOOS == "windows" {
		name = "chrome.exe"
	}
	out := filepath.Join(dir, name)

	cmd := exec.Command("go", "build", "-o", out, "main.go")
	cmd.Dir = srcDir
	// 独立的模块缓存外的最小环境：沿用当前环境即可，只需禁用模块以免联网。
	cmd.Env = append(os.Environ(), "GO111MODULE=off", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译假内核失败: %v\n%s", err, b)
	}
	return kernel.NewStore(root)
}

// waitForFile 等待文件出现并返回其内容按行切分的结果。
func waitForFile(t *testing.T, path string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			return strings.Split(string(b), "\n")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时，假内核未写出参数", path)
	return nil
}

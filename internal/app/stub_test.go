package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// argsDumpSrc 是充当内核的桩程序：把收到的命令行参数写入 BW_ARGS_FILE
// 指定的文件，然后驻留等待被停止，以此模拟浏览器进程。
const argsDumpSrc = `package main

import (
	"os"
	"strings"
	"time"
)

func main() {
	if out := os.Getenv("BW_ARGS_FILE"); out != "" {
		_ = os.WriteFile(out, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	// 驻留等待停止。设上限避免测试异常时留下孤儿进程。
	time.Sleep(60 * time.Second)
}
`

// kernelExecName 返回当前平台的内核可执行文件名，与 kernel 包保持一致。
func kernelExecName() string {
	if runtime.GOOS == "windows" {
		return "chrome.exe"
	}
	return "chrome"
}

// buildArgsDumpingBinary 编译桩程序并返回其字节内容，供打进测试用压缩包。
func buildArgsDumpingBinary(t *testing.T) []byte {
	t.Helper()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(argsDumpSrc), 0o600); err != nil {
		t.Fatalf("写入桩程序源码失败: %v", err)
	}
	out := filepath.Join(t.TempDir(), kernelExecName())

	cmd := exec.Command("go", "build", "-o", out, "main.go")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GO111MODULE=off", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译桩程序失败: %v\n%s", err, b)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取桩程序失败: %v", err)
	}
	return data
}

// readArgsFile 等待桩程序写出参数并返回按行切分的结果。
func readArgsFile(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			return strings.Split(string(b), "\n")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时：内核桩程序未写出参数", path)
	return nil
}

// assertArg 断言参数列表中 flag 的取值等于 want。
func assertArg(t *testing.T, args []string, flag, want string) {
	t.Helper()
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			if v != want {
				t.Errorf("%s = %q, 期望 %q", flag, v, want)
			}
			return
		}
	}
	t.Errorf("参数列表中找不到 %s（期望 %q）: %v", flag, want, args)
}

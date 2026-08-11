package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadPortFileAcceptsTwoLineFormat(t *testing.T) {
	// Chromium 实际写的是两行：端口 + WebSocket 路径。
	dir := t.TempDir()
	path := filepath.Join(dir, devToolsPortFile)
	write(t, path, "54321\n/devtools/browser/abc-def\n")

	got, err := readPortFile(path)
	if err != nil {
		t.Fatalf("readPortFile 出错: %v", err)
	}
	if got != 54321 {
		t.Errorf("端口 = %d, want 54321", got)
	}
}

func TestReadPortFileRejectsPartialWrite(t *testing.T) {
	// 文件可能被读到只写了一半。必须报错让调用方重试，
	// 不能返回一个截断后仍能解析成数字的错误端口。
	dir := t.TempDir()
	path := filepath.Join(dir, devToolsPortFile)
	write(t, path, "")

	if _, err := readPortFile(path); err == nil {
		t.Error("空文件应当报错，否则会被当成有效端口")
	}
}

func TestReadPortFileRejectsOutOfRange(t *testing.T) {
	for _, content := range []string{"0\n", "70000\n", "-1\n"} {
		dir := t.TempDir()
		path := filepath.Join(dir, devToolsPortFile)
		write(t, path, content)
		if _, err := readPortFile(path); err == nil {
			t.Errorf("内容 %q 应当被拒绝", content)
		}
	}
}

func TestReadPortFileMissingFileErrors(t *testing.T) {
	_, err := readPortFile(filepath.Join(t.TempDir(), devToolsPortFile))
	if !os.IsNotExist(err) {
		t.Errorf("缺文件应返回 NotExist，得到 %v", err)
	}
}

func TestProbeCDPAcceptsOKEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/json/version" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"Browser":"Chrome/148.0.0.0"}`))
		}))
	defer srv.Close()

	if err := probeCDP(context.Background(), portOf(t, srv.URL)); err != nil {
		t.Errorf("probeCDP 应当通过: %v", err)
	}
}

func TestProbeCDPRejectsNonCDPListener(t *testing.T) {
	// 端口被别的服务占用时必须失败，否则会 connect 到错误的目标。
	// 这正是 launchdebug 写死 9222 又不检测占用所埋的坑。
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
	defer srv.Close()

	if err := probeCDP(context.Background(), portOf(t, srv.URL)); err == nil {
		t.Error("非 CDP 监听者应当被拒绝")
	}
}

func TestWaitDevToolsPortReturnsOnceFileAndPortReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/json/version" {
				_, _ = w.Write([]byte(`{"Browser":"Chrome"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
	defer srv.Close()
	port := portOf(t, srv.URL)

	dir := t.TempDir()
	// 文件延迟出现，模拟内核启动耗时。
	go func() {
		time.Sleep(300 * time.Millisecond)
		write(t, filepath.Join(dir, devToolsPortFile),
			strconv.Itoa(port)+"\n/devtools/browser/x\n")
	}()

	got, err := waitDevToolsPort(context.Background(), dir, 10*time.Second)
	if err != nil {
		t.Fatalf("waitDevToolsPort 出错: %v", err)
	}
	if got != port {
		t.Errorf("端口 = %d, want %d", got, port)
	}
}

func TestWaitDevToolsPortTimesOutWhenFileNeverAppears(t *testing.T) {
	start := time.Now()
	_, err := waitDevToolsPort(context.Background(), t.TempDir(), 600*time.Millisecond)
	if err == nil {
		t.Fatal("文件始终不出现时必须超时报错")
	}
	if !strings.Contains(err.Error(), devToolsPortFile) {
		t.Errorf("错误信息应提到 %s，得到: %v", devToolsPortFile, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("超时后应尽快返回，实际耗时 %s", elapsed)
	}
}

func TestWaitDevToolsPortHonorsCanceledContext(t *testing.T) {
	// ctx 取消后必须立刻返回，不能耗尽整个 timeout：
	// 调用方按 Ctrl+C 时不应再等几十秒。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := waitDevToolsPort(ctx, t.TempDir(), 30*time.Second); err == nil {
		t.Fatal("ctx 已取消时应当报错")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("ctx 取消后应立即返回，实际耗时 %s", elapsed)
	}
}

func TestWaitCDPReadyFailsWhenNothingListens(t *testing.T) {
	// 取一个确定没人监听的端口：先占住再释放。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	if err := waitCDPReady(context.Background(), port, 500*time.Millisecond); err == nil {
		t.Error("无人监听时必须报错")
	}
}

func TestSweepStaleDirsOnlyRemovesOwnPrefix(t *testing.T) {
	// 清理必须只碰自己的前缀。误删 multilaunch 的目录会打断正在跑的实例，
	// 误删系统临时目录里的其他内容更是不可接受。
	tmp := os.TempDir()
	mine, err := os.MkdirTemp(tmp, tmpDirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.MkdirTemp(tmp, "bw-multi-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(other) }()

	sweepStaleDirs()

	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Errorf("自己前缀的目录应被删除: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("其他工具的目录不该被碰: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(rawURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestBuildExtraArgsMinimize(t *testing.T) {
	// --minimize 应追加 off-screen 窗口位置,把窗口移出可视区(不是 headless)
	args := buildExtraArgs(0, true)
	found := false
	for _, a := range args {
		if a == "--window-position=-32000,-32000" {
			found = true
		}
	}
	if !found {
		t.Errorf("minimize 未追加 window-position,args=%v", args)
	}
}

func TestBuildExtraArgsDefaultNoWindowPosition(t *testing.T) {
	// 默认(不 minimize)不该带 window-position —— 否则所有用户都被移出可视区
	args := buildExtraArgs(0, false)
	for _, a := range args {
		if strings.HasPrefix(a, "--window-position") {
			t.Errorf("非 minimize 不应带 window-position,args=%v", args)
		}
	}
}

func TestBuildExtraArgsAlwaysHasCDPAndOrigin(t *testing.T) {
	// 两条基础参数任何模式都不能丢
	args := buildExtraArgs(9222, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--remote-debugging-port=9222", "--remote-allow-origins=*",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少 %s,args=%v", want, args)
		}
	}
}

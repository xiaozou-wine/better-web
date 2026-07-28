package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"better-web/internal/kernel"
)

// terminate 必须让真实浏览器走优雅关闭路径，以便落盘会话数据。
//
// 这是本项目在 Windows 上曾长期存在的缺陷：原实现先发 os.Interrupt，
// 而 Windows 不支持向 GUI 进程投递该信号，于是每次都退回 Kill——
// 等于每次点"停止"都在硬杀浏览器，丢失未落盘的 Cookie 与登录态，
// 并让下次启动弹出"未正确关闭"提示，那本身就是额外的异常信号。
//
// 必须用真实内核验证，不能用假内核替身：优雅关闭在 Windows 上依赖向窗口
// 投递 WM_CLOSE，而控制台程序没有窗口，taskkill 会直接拒绝，测出来的
// 失败属于替身不具代表性而非实现有问题。
//
// 判定依据是"关闭耗时远小于宽限期"：走优雅路径几秒内就结束，
// 退化成强杀则要等满 gracefulTimeout。
func TestTerminateShutsDownRealBrowserGracefully(t *testing.T) {
	k := realKernelForTerminate(t)

	cmd := exec.Command(k,
		"--user-data-dir="+t.TempDir(),
		"--no-first-run", "--no-default-browser-check",
		// 窗口开到屏幕外，避免打断用户；仍是有头模式，窗口真实存在。
		"--window-position=-32000,-32000",
		"about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动内核失败: %v", err)
	}
	pid := cmd.Process.Pid
	// 由本测试独占 Wait，terminate 内部只做轮询不调用 Wait。
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	defer func() {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/T", "/F").Run()
	}()

	// 等窗口创建完成，否则 WM_CLOSE 无处可投。
	time.Sleep(5 * time.Second)

	started := time.Now()
	if err := terminate(cmd.Process); err != nil {
		t.Fatalf("terminate 失败: %v", err)
	}
	select {
	case <-done:
	case <-time.After(gracefulTimeout + 15*time.Second):
		t.Fatal("terminate 后进程未退出")
	}

	elapsed := time.Since(started)
	t.Logf("关闭耗时 %v（宽限期 %v）", elapsed.Round(time.Millisecond), gracefulTimeout)
	if elapsed >= gracefulTimeout {
		t.Errorf("关闭耗时 %v 已达宽限期，说明优雅关闭未生效、退化成了强杀；"+
			"浏览器会因此丢失未落盘的会话数据", elapsed.Round(time.Millisecond))
	}
}

// realKernelForTerminate 定位已安装的真实内核，未安装时跳过。
func realKernelForTerminate(t *testing.T) string {
	t.Helper()
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Skip("未设置 APPDATA，跳过真实内核测试")
	}
	list, err := kernel.NewStore(filepath.Join(appData, "better-web", "kernels")).List()
	if err != nil {
		t.Fatalf("枚举内核失败: %v", err)
	}
	if len(list) == 0 {
		t.Skip("没有已安装的内核，跳过真实内核测试")
	}
	return list[0].ExecPath
}

// 不响应关闭请求的进程必须在宽限期后被强杀，不能永远挂着。
func TestTerminateEscalatesToKill(t *testing.T) {
	if testing.Short() {
		t.Skip("需要等待完整宽限期，-short 模式下跳过")
	}
	// 复用不处理信号的假内核：它只是 Sleep，不会响应关闭请求。
	store := buildFakeKernel(t, "148.0.0.0")
	k, err := store.Resolve("")
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}

	cmd := exec.Command(k.ExecPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	started := time.Now()
	if err := terminate(cmd.Process); err != nil {
		t.Fatalf("terminate 失败: %v", err)
	}
	select {
	case <-done:
	case <-time.After(gracefulTimeout + 15*time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("宽限期过后进程仍未被强杀")
	}
	t.Logf("耗时 %v（宽限期 %v）", time.Since(started).Round(time.Millisecond), gracefulTimeout)
}

// 对已退出的进程调用 terminate 不应报错。
// Stop 可能与进程自行退出竞争，报错会让界面显示无意义的失败。
func TestTerminateOnExitedProcessIsNoop(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestTerminateOnExitedProcessIsNoop$", "-test.count=0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	_ = cmd.Wait() // 确保已退出

	if err := terminate(cmd.Process); err != nil {
		t.Errorf("对已退出进程调用 terminate 报错: %v", err)
	}
}

// nil 进程不应导致 panic：Stop 在会话刚建立尚无进程时可能传入 nil。
func TestTerminateNilProcess(t *testing.T) {
	if err := terminate(nil); err != nil {
		t.Errorf("terminate(nil) 报错: %v", err)
	}
}

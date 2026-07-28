// Package probe 用真实内核采集浏览器指纹，用于验证指纹是否按预期生效
// 以及内核升级后指纹是否漂移。
//
// 采集方式：起一个本地页面，页面内读取各项指纹并 POST 回来。不依赖 CDP，
// 因为启用 CDP 本身就是可被检测的信号，会污染采集结果。
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Result 是采集到的一组指纹。字段对应各项常被用于识别的浏览器特征。
type Result struct {
	UserAgent           string   `json:"userAgent"`
	Platform            string   `json:"platform"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	DeviceMemory        float64  `json:"deviceMemory"`
	Languages           []string `json:"languages"`
	Language            string   `json:"language"`
	Timezone            string   `json:"timezone"`
	TimezoneOffset      int      `json:"timezoneOffset"`
	ScreenWidth         int      `json:"screenWidth"`
	ScreenHeight        int      `json:"screenHeight"`
	DevicePixelRatio    float64  `json:"devicePixelRatio"`
	// WebGL 相关。Vendor/Renderer 取自 WEBGL_debug_renderer_info。
	WebGLVendor   string `json:"webglVendor"`
	WebGLRenderer string `json:"webglRenderer"`
	// CanvasHash 与 AudioHash 是渲染结果的指纹摘要，用于判断噪声是否生效。
	CanvasHash string `json:"canvasHash"`
	AudioHash  string `json:"audioHash"`
	// Webdriver 为 true 说明暴露了自动化特征。
	Webdriver bool `json:"webdriver"`
	// UAData 是 navigator.userAgentData 的 Client Hints 内容。
	UAData map[string]any `json:"uaData"`
	// Plugins 是插件名列表。
	Plugins []string `json:"plugins"`
}

// Probe 用指定内核采集指纹。
type Probe struct {
	// ExecPath 是内核可执行文件路径。
	ExecPath string
	// Timeout 是单次采集的超时。留空时用 60 秒。
	Timeout time.Duration
}

// defaultTimeout 是单次采集的默认超时。冷启动 Chromium 需要数秒。
const defaultTimeout = 60 * time.Second

// Collect 用给定的额外命令行参数启动内核，采集并返回指纹。
//
// extraArgs 是除必需参数外的附加参数，通常来自 launcher.BuildArgs 的输出。
// 采集结束后内核会被终止。
func (p *Probe) Collect(ctx context.Context, extraArgs []string) (Result, error) {
	if p.ExecPath == "" {
		return Result{}, errors.New("未指定内核路径")
	}
	if _, err := os.Stat(p.ExecPath); err != nil {
		return Result{}, fmt.Errorf("内核不可用: %w", err)
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results := make(chan Result, 1)
	srvErrs := make(chan error, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("启动采集服务失败: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(probePage))
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var res Result
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&res); err != nil {
			srvErrs <- fmt.Errorf("解析采集结果失败: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case results <- res:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrs <- err
		}
	}()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	url := "http://" + ln.Addr().String() + "/"

	// 每次采集用独立的临时 profile 目录，避免残留状态影响结果。
	dataDir, err := os.MkdirTemp("", "bw-probe-*")
	if err != nil {
		return Result{}, fmt.Errorf("创建临时 profile 目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	args := append([]string{}, extraArgs...)
	if !hasUserDataDir(args) {
		args = append(args, "--user-data-dir="+dataDir)
	}
	// 采集期间不需要窗口交互，但必须保留 GPU：禁用 GPU 会让 WebGL 退化成
	// SwiftShader，采到的 renderer 就不是真实值了。
	args = append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--window-size=1280,800",
		// 移出可视区域，避免采集时在用户面前闪一下。
		// 不用 --headless：那会让 WebGL 退化为 SwiftShader（见上方注释），
		// 采到的 renderer 就不是真实值了。
		"--window-position=-32000,-32000",
		url,
	)

	cmd := exec.CommandContext(ctx, p.ExecPath, args...)
	setProcessGroup(cmd)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("启动内核失败: %w", err)
	}
	defer func() { reapKernel(cmd) }()

	select {
	case res := <-results:
		return res, nil
	case err := <-srvErrs:
		return Result{}, err
	case <-ctx.Done():
		return Result{}, fmt.Errorf("采集超时: %w", ctx.Err())
	}
}

// reapKernel 回收内核进程及其全部子进程。
//
// 必须按进程树回收：Chromium 是多进程架构，只终止主进程会留下渲染、
// GPU、网络等子进程，每个都占着已建立的 socket。连续采集时这些孤儿
// 进程累积会耗尽动态端口池，后续连接报
// "Only one usage of each socket address is normally permitted"。
func reapKernel(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := killProcessTree(cmd.Process.Pid); err != nil {
		// 不静默吞掉：按树终止失败会留下占着 socket 的孤儿进程，
		// 连续采集时表现为端口耗尽，没有这行日志根本查不到原因。
		log.Printf("回收内核进程树失败，可能残留孤儿进程: %v", err)
		// 退回单进程终止，至少不让主进程留着。
		_ = cmd.Process.Kill()
	}
	// 等待回收僵尸进程。子进程已被系统终止，这里不会阻塞。
	_ = cmd.Wait()
}

func hasUserDataDir(args []string) bool {
	for _, a := range args {
		if len(a) >= 16 && a[:16] == "--user-data-dir=" {
			return true
		}
	}
	return false
}

// SaveBaseline 把采集结果写成缩进的 JSON，便于纳入版本管理做差异比对。
func SaveBaseline(path string, res Result) error {
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

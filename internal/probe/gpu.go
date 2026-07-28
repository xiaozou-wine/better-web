package probe

// GPU 深度探针：定位 GPU 伪造在哪一项上与真实渲染行为矛盾。
//
// 由来：实测 --fingerprint 一旦启用（GPU 伪造随之生效），Cloudflare 即拦截，
// 而 --disable-spoofing=gpu 后通过（见 TestAntibotControl）。基线采集只读
// UNMASKED_VENDOR/RENDERER 两个字符串，看不出矛盾在哪——伪造把这两个字符串
// 改了，但 WebGL 的能力上限、扩展列表、着色器精度、实际渲染结果都由真实驱动
// 产生。任何一项与声称的型号对不上，就是可判定的矛盾。
//
// 所以这里采集的是"能拿来跟型号对照的一切"，用于回答两个问题：
//   - 伪造只改了字符串，还是也改了能力参数？
//   - 有没有第二条泄露真实 GPU 的通路（WebGPU 独立于 WebGL）？

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// GPUReport 是一次 GPU 深度采集的结果。
//
// 字段分三类：声称的身份（WebGL/WebGPU 报出的型号字符串）、真实能力
// （驱动决定的上限与扩展）、渲染产物（像素哈希）。伪造若只覆盖第一类，
// 第二三类就会与之矛盾。
type GPUReport struct {
	// WebGL1 与 WebGL2 分别采集：两个上下文各自初始化，伪造可能只覆盖其一。
	WebGL1 GLContext `json:"webgl1"`
	WebGL2 GLContext `json:"webgl2"`
	// WebGPU 是 navigator.gpu 报出的适配器信息。它与 WebGL 是两套独立实现，
	// 是最可能被漏掉的泄露通路。
	WebGPU WebGPUInfo `json:"webgpu"`
	// UAArch 是 UA-CH 高熵值里的架构与位数，与 GPU 无关但用于交叉核对平台。
	UAArch map[string]any `json:"uaArch"`
	Err    string         `json:"err,omitempty"`
}

// GLContext 是一个 WebGL 上下文的完整画像。
type GLContext struct {
	Available bool   `json:"available"`
	Vendor    string `json:"vendor"`   // UNMASKED_VENDOR_WEBGL
	Renderer  string `json:"renderer"` // UNMASKED_RENDERER_WEBGL
	// GLVersion/ShadingLang 是 VERSION 与 SHADING_LANGUAGE_VERSION，
	// 它们带 ANGLE 与驱动版本号，伪造往往漏改。
	GLVersion   string `json:"glVersion"`
	ShadingLang string `json:"shadingLang"`
	// Limits 是驱动决定的能力上限（MAX_TEXTURE_SIZE 等）。
	// 不同档次的 GPU 上限不同，这是最直接的型号交叉验证依据。
	Limits map[string]any `json:"limits"`
	// Extensions 是支持的扩展列表。集成显卡与独立显卡的扩展集合有差异。
	Extensions []string `json:"extensions"`
	// Precision 是各着色器阶段的浮点精度描述。
	Precision map[string]any `json:"precision"`
	// PixelHash 是固定场景的渲染结果哈希，反映实际光栅化行为。
	PixelHash string `json:"pixelHash"`
}

// WebGPUInfo 是 navigator.gpu 适配器信息。
type WebGPUInfo struct {
	Available bool `json:"available"`
	// AdapterInfo 是 GPUAdapterInfo 的 vendor/architecture/device/description。
	// 真实 GPU 型号常出现在 description 或 architecture 里。
	AdapterInfo map[string]any `json:"adapterInfo"`
	// Limits 是 WebGPU 上报的设备上限，同样由真实驱动决定。
	Limits   map[string]any `json:"limits"`
	Features []string       `json:"features"`
	Err      string         `json:"err,omitempty"`
}

// GPUProbe 用指定内核采集 GPU 深度画像。
type GPUProbe struct {
	ExecPath string
	Timeout  time.Duration
}

// CollectGPU 启动内核并采集 GPU 深度画像。
//
// 与 Probe.Collect 同样不用 CDP：启用 CDP 是可检测信号，会污染采集。
// 必须保留 GPU（不能加 --disable-gpu），否则 WebGL 退化为 SwiftShader，
// 采到的就不是真实驱动行为了。
func (p *GPUProbe) CollectGPU(ctx context.Context, extraArgs []string) (GPUReport, error) {
	if p.ExecPath == "" {
		return GPUReport{}, errors.New("未指定内核路径")
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results := make(chan GPUReport, 1)
	srvErrs := make(chan error, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return GPUReport{}, fmt.Errorf("启动采集服务失败: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(gpuPage))
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var res GPUReport
		// 上限放宽到 4MB：扩展列表与 limits 表比基线采集大得多。
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&res); err != nil {
			srvErrs <- fmt.Errorf("解析 GPU 采集结果失败: %w", err)
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

	dataDir, err := os.MkdirTemp("", "bw-gpu-*")
	if err != nil {
		return GPUReport{}, fmt.Errorf("创建临时 profile 目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	args := append([]string{}, extraArgs...)
	if !hasUserDataDir(args) {
		args = append(args, "--user-data-dir="+dataDir)
	}
	args = append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--window-size=1280,800",
		// 把窗口移到屏幕外，避免采集时在用户面前闪一下。
		//
		// 不用 --headless：headless 会让 WebGL 退化为 SwiftShader，采到的
		// GPU 就不是真实驱动的值了，而这正是本探针要测的东西。
		// 移出可视区域是唯一既保留真实渲染又不打扰用户的做法。
		"--window-position=-32000,-32000",
		"http://"+ln.Addr().String()+"/",
	)

	cmd := exec.CommandContext(ctx, p.ExecPath, args...)
	setProcessGroup(cmd)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return GPUReport{}, fmt.Errorf("启动内核失败: %w", err)
	}
	defer func() { reapKernel(cmd) }()

	select {
	case res := <-results:
		return res, nil
	case err := <-srvErrs:
		return GPUReport{}, err
	case <-ctx.Done():
		return GPUReport{}, fmt.Errorf("GPU 采集超时: %w", ctx.Err())
	}
}

// SaveGPUReport 把 GPU 画像写成缩进 JSON，便于跨组比对。
func SaveGPUReport(path string, r GPUReport) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

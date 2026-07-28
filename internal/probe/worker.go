package probe

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

// ScopeValues 是某一执行作用域（主线程或 Worker）中读到的指纹。
type ScopeValues struct {
	UserAgent           string  `json:"userAgent"`
	Platform            string  `json:"platform"`
	HardwareConcurrency int     `json:"hardwareConcurrency"`
	DeviceMemory        float64 `json:"deviceMemory"`
	Language            string  `json:"language"`
	Timezone            string  `json:"timezone"`
	TimezoneOffset      int     `json:"timezoneOffset"`
	UADataPlatform      string  `json:"uaDataPlatform"`
	WebGLRenderer       string  `json:"webglRenderer"`
}

// ScopeResult 是主线程与 Worker 的对照采集结果。
type ScopeResult struct {
	Main   ScopeValues  `json:"main"`
	Worker *ScopeValues `json:"worker"`
	// WorkerAvailable 为 false 表示 Worker 不可用，无法比对（而非比对不一致）。
	WorkerAvailable bool `json:"workerAvailable"`
	// PrototypeLies 是关键 API 被改写的痕迹。源码层伪造不应留痕，
	// 非空说明存在 JS 层注入。
	PrototypeLies []string `json:"prototypeLies"`
	CanvasHash    string   `json:"canvasHash"`
}

// CollectScopes 采集主线程与 Worker 的指纹对照。
//
// 与 Collect 的区别：Collect 只看主线程的值对不对，本函数看同一个值在两个
// 作用域里是否一致。检测器（如 CreepJS）把跨作用域不一致当作说谎的直接证据，
// 因为真实浏览器的这些值在两处必然相同。
func (p *Probe) CollectScopes(ctx context.Context, extraArgs []string) (ScopeResult, error) {
	if p.ExecPath == "" {
		return ScopeResult{}, errors.New("未指定内核路径")
	}
	if _, err := os.Stat(p.ExecPath); err != nil {
		return ScopeResult{}, fmt.Errorf("内核不可用: %w", err)
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results := make(chan ScopeResult, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ScopeResult{}, fmt.Errorf("启动采集服务失败: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(workerProbePage))
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var res ScopeResult
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&res); err != nil {
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
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	dataDir, err := os.MkdirTemp("", "bw-scope-*")
	if err != nil {
		return ScopeResult{}, fmt.Errorf("创建临时 profile 目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	args := append([]string{}, extraArgs...)
	if !hasUserDataDir(args) {
		args = append(args, "--user-data-dir="+dataDir)
	}
	args = append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--window-size=1280,800",
		"http://"+ln.Addr().String()+"/",
	)

	cmd := exec.CommandContext(ctx, p.ExecPath, args...)
	if err := cmd.Start(); err != nil {
		return ScopeResult{}, fmt.Errorf("启动内核失败: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	select {
	case res := <-results:
		return res, nil
	case <-ctx.Done():
		return ScopeResult{}, fmt.Errorf("采集超时: %w", ctx.Err())
	}
}

// CheckScopeConsistency 比对两个作用域的值，返回不一致项。
//
// WebGL 不参与比对：主线程走 canvas、Worker 走 OffscreenCanvas，
// 两条渲染路径在真实浏览器上也可能给出不同字符串，据此判定会误报。
func CheckScopeConsistency(r ScopeResult) []Issue {
	if !r.WorkerAvailable || r.Worker == nil {
		return nil
	}
	var out []Issue
	add := func(field, main, worker string) {
		if main != worker {
			out = append(out, Issue{
				Check:  "跨作用域不一致",
				Detail: fmt.Sprintf("%s: 主线程 %q 而 Worker %q", field, main, worker),
				Severe: true,
			})
		}
	}
	m, w := r.Main, *r.Worker
	add("userAgent", m.UserAgent, w.UserAgent)
	add("platform", m.Platform, w.Platform)
	add("hardwareConcurrency", fmt.Sprint(m.HardwareConcurrency), fmt.Sprint(w.HardwareConcurrency))
	add("deviceMemory", fmt.Sprint(m.DeviceMemory), fmt.Sprint(w.DeviceMemory))
	add("language", m.Language, w.Language)
	add("timezone", m.Timezone, w.Timezone)
	add("timezoneOffset", fmt.Sprint(m.TimezoneOffset), fmt.Sprint(w.TimezoneOffset))
	add("userAgentData.platform", m.UADataPlatform, w.UADataPlatform)

	for _, lie := range r.PrototypeLies {
		out = append(out, Issue{
			Check:  "原型被改写",
			Detail: lie,
			Severe: true,
		})
	}
	return out
}

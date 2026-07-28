package probe

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// 扫描若干种子，列出各自派生出的 GPU 型号。
//
// 启用方式：
//
//	BW_RUN_GPU_SEEDS=1 BW_GPU_SEEDS=12 \
//	  go test -run TestScanSeedGPU -timeout 600s -v ./internal/probe/
//
// 用途：GPU 由种子派生，不受 catalog.go 控制，因此想知道"能否伪造成与宿主机
// 同族的 GPU"只能实测扫描。若某些种子恰好派生出 NVIDIA（与本机同族），
// 就能用它验证 Cloudflare 的判据到底是不是"伪造 GPU 与真实 GPU 不同族"。
func TestScanSeedGPU(t *testing.T) {
	if os.Getenv("BW_RUN_GPU_SEEDS") != "1" {
		t.Skip("未设置 BW_RUN_GPU_SEEDS=1，跳过种子 GPU 扫描")
	}
	k := realKernel(t)
	p := &GPUProbe{ExecPath: k.ExecPath}

	count := 12
	if v := os.Getenv("BW_GPU_SEEDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}

	// 先取真实 GPU 作为参照。
	honest, err := p.CollectGPU(context.Background(),
		[]string{"--fingerprint=1", "--disable-spoofing=gpu"})
	if err != nil {
		t.Fatalf("采集真实 GPU 失败: %v", err)
	}
	realRenderer := honest.WebGL1.Renderer
	t.Logf("宿主机真实 GPU: %s", realRenderer)
	realFamily := gpuFamily(realRenderer)
	t.Logf("真实 GPU 厂商族: %s\n", realFamily)

	// 用间隔较大的种子，避免相邻种子派生出同一型号。
	families := map[string]int{}
	for i := 0; i < count; i++ {
		seed := strconv.Itoa(100000000 + i*37000000)
		rep, err := p.CollectGPU(context.Background(),
			[]string{"--fingerprint=" + seed})
		if err != nil {
			t.Logf("种子 %s 采集失败: %v", seed, err)
			continue
		}
		fam := gpuFamily(rep.WebGL1.Renderer)
		families[fam]++
		mark := ""
		if fam == realFamily {
			mark = "  ← 与宿主机同族"
		}
		t.Logf("种子 %-12s → %-6s %s%s",
			seed, fam, rep.WebGL1.Renderer, mark)
	}
	t.Logf("\n厂商族分布: %v", families)
	if families[realFamily] == 0 {
		t.Logf("扫描范围内没有种子派生出 %s，无法用同族对照验证判据", realFamily)
	}
}

// gpuFamily 从 renderer 字符串提取厂商族。
// 判据是矛盾检测的核心：伪造与真实 GPU 是否同族决定了渲染行为能否对上。
func gpuFamily(renderer string) string {
	s := strings.ToLower(renderer)
	switch {
	case strings.Contains(s, "nvidia"), strings.Contains(s, "geforce"):
		return "NVIDIA"
	case strings.Contains(s, "intel"):
		return "Intel"
	case strings.Contains(s, "amd"), strings.Contains(s, "radeon"):
		return "AMD"
	case strings.Contains(s, "apple"):
		return "Apple"
	case renderer == "":
		return "空"
	}
	return "其他"
}

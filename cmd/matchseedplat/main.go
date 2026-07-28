// Command matchseedplat 在指定平台下筛选与宿主机 GPU 同厂商的种子。
//
// 与 cmd/matchseed 的区别，也是它存在的理由：matchseed 探测时只传
// --fingerprint=<seed>，而实际启动还会按机型档案传 --fingerprint-platform。
// 平台不同，内核派生出的 GPU 也不同 —— 实测种子 279042459 在无平台参数下
// 派生 NVIDIA RTX 4060，加上 --fingerprint-platform=macos 后变成 Apple M2。
// 于是 matchseed 筛出的"同族种子"在真实启动时可能仍是跨厂商，而这正是
// Cloudflare 的判据（见 memory/gpu-family-cloudflare.md）。
//
// 用法：
//
//	go run ./cmd/matchseedplat [平台] [上限]
//
//	平台默认 windows，可选 windows / macos / linux。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/probe"
)

func main() {
	platform := "windows"
	if len(os.Args) > 1 {
		platform = os.Args[1]
	}
	switch platform {
	case "windows", "macos", "linux":
	default:
		fmt.Fprintf(os.Stderr, "平台 %q 无效，可选 windows / macos / linux\n", platform)
		os.Exit(2)
	}
	attempts := fingerprint.DefaultSeedAttempts
	if len(os.Args) > 2 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "尝试上限 %q 无效，需为正整数\n", os.Args[2])
			os.Exit(2)
		}
		attempts = n
	}

	base, err := os.UserConfigDir()
	must(err, "定位用户配置目录")
	k, err := kernel.NewStore(filepath.Join(base, "better-web", "kernels")).Resolve("")
	must(err, "定位内核")
	fmt.Printf("内核: %s   目标平台: %s\n", k.Version, platform)

	ctx := context.Background()
	host, renderer, err := probe.HostGPU(ctx, k.ExecPath)
	must(err, "探测宿主机 GPU")
	fmt.Printf("宿主机 GPU: %s\n  %s\n\n", host, renderer)

	fmt.Printf("在 --fingerprint-platform=%s 下筛选同族种子（上限 %d 次）…\n",
		platform, attempts)

	p := &probe.GPUProbe{ExecPath: k.ExecPath, Timeout: 45 * time.Second}
	var hit int32
	for i := 0; i < attempts; i++ {
		seed, err := fingerprint.NewSeed()
		must(err, "生成种子")
		// 关键：把平台参数一起传进去，与 launcher.BuildArgs 的实际行为一致
		rep, err := p.CollectGPU(ctx, []string{
			fmt.Sprintf("--fingerprint=%d", seed),
			"--fingerprint-platform=" + platform,
		})
		if err != nil {
			fmt.Printf("  种子 %-12d 探测失败: %v\n", seed, err)
			continue
		}
		r := rep.WebGL1.Renderer
		if r == "" {
			r = rep.WebGL2.Renderer
		}
		fam := model.ParseGPUFamily(r)
		mark := ""
		if fam == host {
			mark = "  ← 命中"
			hit = seed
		}
		fmt.Printf("  种子 %-12d → %-8s%s\n", seed, fam, mark)
		if hit != 0 {
			fmt.Printf("\n找到同族种子: %d\n  %s\n", hit, r)
			fmt.Printf("\n建这个 profile:\n")
			fmt.Printf("  go run ./cmd/mkprofile -name <名字> -seed %d -device \"<%s 机型标签>\" -proxy <代理>\n",
				hit, platform)
			fmt.Println("\n注意: 种子只对当前宿主机 + 当前平台参数组合有效。")
			return
		}
	}
	fmt.Fprintf(os.Stderr, "\n试了 %d 个候选都没命中 %s。加大上限，或改用 -device 选同平台机型后重试\n",
		attempts, host)
	os.Exit(1)
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

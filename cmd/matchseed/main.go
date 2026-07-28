// Command matchseed 探测宿主机 GPU 并筛选一个同族的种子。
//
// 用途：实测表明 Cloudflare 的判据是伪造 GPU 与宿主机跨厂商（见 README），
// 而 GPU 由内核从种子派生、无参数可控，只能靠反复试来间接选择。
// 界面新建 profile 时勾选「匹配宿主机 GPU」走的是同一套逻辑，这个工具
// 用于在建 profile 之前先确认这台机器能否筛出同族种子、大概要试几次。
//
// 用法：
//
//	go run ./cmd/matchseed              # 用默认上限
//	go run ./cmd/matchseed 40           # 指定尝试上限
//
// 每个候选要冷启动一次内核（约 2 秒），因此上限别设太大。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"better-web/internal/fingerprint"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/probe"
)

// prober 把 probe 的探测能力适配成 fingerprint.GPUProber。
type prober struct{ execPath string }

func (p prober) SeedGPUFamily(ctx context.Context, seed int32) (model.GPUFamily, string, error) {
	return probe.SeedGPUFamily(ctx, p.execPath, seed)
}

func main() {
	attempts := fingerprint.DefaultSeedAttempts
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "尝试上限 %q 无效，需为正整数\n", os.Args[1])
			os.Exit(2)
		}
		attempts = n
	}

	base, err := os.UserConfigDir()
	must(err, "定位用户配置目录")
	k, err := kernel.NewStore(filepath.Join(base, "better-web", "kernels")).Resolve("")
	must(err, "定位内核")
	fmt.Printf("内核: %s\n", k.Version)

	ctx := context.Background()
	host, renderer, err := probe.HostGPU(ctx, k.ExecPath)
	must(err, "探测宿主机 GPU")
	fmt.Printf("宿主机 GPU: %s\n  %s\n\n", host, renderer)

	fmt.Printf("开始筛选同族种子（上限 %d 次，每次约 2 秒）…\n", attempts)
	match, err := fingerprint.FindSeedForGPUFamily(
		ctx, prober{execPath: k.ExecPath}, host, attempts)

	for _, c := range match.Tried {
		if c.Err != "" {
			fmt.Printf("  种子 %-12d 探测失败: %s\n", c.Seed, c.Err)
			continue
		}
		mark := ""
		if c.Family == host {
			mark = "  ← 命中"
		}
		fmt.Printf("  种子 %-12d → %-8s%s\n", c.Seed, c.Family, mark)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n筛选失败: %v\n", err)
		fmt.Fprintln(os.Stderr,
			"\n可选做法：加大尝试上限，或改用 DisableSpoofing 关闭 gpu 伪造")
		fmt.Fprintln(os.Stderr,
			"（后者代价是所有 profile 共享宿主机真实 GPU，多一个关联信号）")
		os.Exit(1)
	}

	fmt.Printf("\n找到同族种子: %d\n  %s\n", match.Seed, match.Renderer)
	fmt.Printf("试了 %d 个候选。\n", len(match.Tried))
	fmt.Println("\n注意：这个种子只对当前这台宿主机有效。换机器后 GPU 族可能不同，")
	fmt.Println("同一个种子就又变成跨厂商了——它匹配的是宿主机，不是绝对安全的值。")
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

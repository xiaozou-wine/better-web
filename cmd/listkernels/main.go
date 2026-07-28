// Command listkernels 列出已安装与可下载的内核版本。
//
// 用于核对多内核能力：每个 profile 可用 KernelVersion 锁定版本，
// 而锁定的前提是知道有哪些版本可选。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"better-web/internal/kernel"
)

func main() {
	base, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位用户配置目录失败:", err)
		os.Exit(1)
	}
	root := filepath.Join(base, "better-web", "kernels")

	store := kernel.NewStore(root)
	installed, err := store.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "枚举已安装内核失败:", err)
		os.Exit(1)
	}
	fmt.Printf("内核目录: %s\n", root)
	fmt.Printf("已安装 %d 个:\n", len(installed))
	for _, k := range installed {
		fmt.Printf("  %-18s 主版本 %d\n", k.Version, k.Major())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	releases, err := (&kernel.Fetcher{}).ListReleases(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n查询可下载版本失败: %v\n", err)
		os.Exit(1)
	}

	// 标出哪些已装，避免重复下载 400MB。
	have := make(map[string]bool, len(installed))
	for _, k := range installed {
		have[k.Version] = true
	}

	fmt.Printf("\n可下载 %d 个（上游 release，新版在前）:\n", len(releases))
	for _, r := range releases {
		mark := " "
		if have[r.Version] {
			mark = "✓"
		}
		fmt.Printf("  %s %-18s %6.1f MB  %s\n",
			mark, r.Version, float64(r.Size)/1024/1024, r.AssetName)
	}
	fmt.Println("\n✓ = 已安装。每个 profile 可用 KernelVersion 锁定版本，")
	fmt.Println("  锁定后内核升级不会改变该 profile 的指纹。")
}

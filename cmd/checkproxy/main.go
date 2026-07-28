// Command checkproxy 对已配置的 profile 逐个检测代理连通性与出口质量。
//
// 与界面上的"测试代理"按钮等价，但可在终端批量执行，便于一次核对多个 profile。
// 只读，不修改任何数据，也不回显代理密码。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"better-web/internal/app"
	"better-web/internal/store"
)

func main() {
	base, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位用户配置目录失败:", err)
		os.Exit(1)
	}
	root := filepath.Join(base, "better-web")

	svc, err := app.New(app.NewPaths(root))
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化服务失败:", err)
		os.Exit(1)
	}
	defer func() { _ = svc.Close() }()

	// 需要密码原文来发起检测，因此直接读库而不用 ProfileView。
	db, err := store.Open(filepath.Join(root, "profiles.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败:", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	list, err := db.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 profile 失败:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	for _, p := range list {
		fmt.Printf("\n=== %s ===\n", p.Name)
		if p.Proxy == nil {
			fmt.Println("未配置代理，跳过（该 profile 会用真实 IP 出网）")
			continue
		}
		fmt.Printf("代理: %s://%s:%d\n", p.Proxy.Scheme, p.Proxy.Host, p.Proxy.Port)

		r := svc.CheckProxy(ctx, p.Proxy)
		if !r.OK {
			fmt.Printf("结果: 失败（%d ms）\n原因: %s\n", r.ElapsedMs, r.Err)
			continue
		}
		fmt.Printf("结果: 连通（%d ms）\n", r.ElapsedMs)
		if r.Exit != nil {
			fmt.Printf("出口 IP  : %s\n", r.Exit.IP)
			if r.Exit.Org != "" {
				fmt.Printf("归属     : %s", r.Exit.Org)
				if r.Exit.ASN != 0 {
					fmt.Printf(" (AS%d)", r.Exit.ASN)
				}
				fmt.Println()
			}
			fmt.Printf("网络类型 : %s\n", r.Exit.Kind)
			fmt.Printf("出口位置 : %s / %s\n", r.Exit.Geo.CountryCode, r.Exit.Geo.Region)
		}
		if r.Aligned != nil {
			fmt.Printf("将生效的时区/语言: %s / %s\n", r.Aligned.Timezone, r.Aligned.Locale)
		}
		for _, w := range r.Warnings {
			fmt.Printf("⚠ %s\n", w)
		}
	}
}

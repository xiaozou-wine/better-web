// Command launchdebug 用指定 profile 的完整配置启动一个带 CDP 调试端口的浏览器。
//
// 用途：把本项目的指纹环境与代理链路，接给外部的 CDP 工具（Playwright、
// Puppeteer 或任何 CDP 客户端）使用。启动后进程保持前台运行，Ctrl+C 退出并清理转发器。
//
// 实测（cmd/cdpexperiment）表明开调试端口不影响 CreepJS/BrowserScan 的评分，
// 因此这条路可用。但该结论只覆盖这两个站点，商业风控未验证。
//
// 用法：
//
//	go run ./cmd/launchdebug <profile 名称> [端口]
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/kernel"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/proxy"
	"better-web/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: go run ./cmd/launchdebug <profile 名称> [端口]")
		os.Exit(2)
	}
	name := os.Args[1]
	port := "9222"
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	base, err := os.UserConfigDir()
	must(err, "定位用户配置目录")
	root := filepath.Join(base, "better-web")

	db, err := store.Open(filepath.Join(root, "profiles.db"))
	must(err, "打开数据库")
	defer func() { _ = db.Close() }()

	list, err := db.List()
	must(err, "读取 profile")
	var p *model.Profile
	for _, item := range list {
		if item.Name == name {
			p = item
			break
		}
	}
	if p == nil {
		fmt.Fprintf(os.Stderr, "找不到名为 %q 的 profile\n", name)
		os.Exit(1)
	}

	k, err := kernel.NewStore(filepath.Join(root, "kernels")).Resolve(p.KernelVersion)
	must(err, "定位内核")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 代理转发与出口探测，与正常启动流程一致。
	var proxyAddr string
	var resolvedGeo *model.Geo
	if p.Proxy != nil {
		f, err := proxy.New(p.Proxy)
		must(err, "创建转发器")
		proxyAddr, err = f.Start()
		must(err, "启动转发器")
		defer func() { _ = f.Close() }()

		client, err := f.HTTPClient()
		must(err, "构造探测客户端")
		info, err := geo.NewResolver(client).LookupExit(ctx)
		if err != nil {
			fmt.Printf("出口探测失败: %v（仍继续启动）\n", err)
		} else {
			fmt.Printf("出口: %s / %s / %s (%s)\n",
				info.IP, info.Geo.CountryCode, info.Org, info.Kind)
			g := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
			resolvedGeo = &g
			for _, w := range warningsFor(info.Kind, info.Org) {
				fmt.Printf("⚠ %s\n", w)
			}
		}
	}
	if resolvedGeo == nil {
		resolvedGeo = p.GeoOverride
	}

	// 必须走 DeriveWithDeviceLabel 而不是 DeriveForKernel —— 后者不看
	// p.DeviceLabel，锁定的机型会被静默忽略，改成按种子抽取。表现是界面里
	// 锁了机型、这里启动却是另一台设备，且没有任何提示（实测锁
	// "Windows 11 / RTX 4060 Laptop" 启动后报 "Windows 11 / GTX 1660 入门桌面"）。
	// 与 session.Manager 的 GUI 启动路径保持一致。
	if p.Kind == model.KindFingerprint && p.DeviceLabel != "" {
		if _, ok := fingerprint.FindDevice(p.DeviceLabel); !ok {
			fmt.Fprintf(os.Stderr,
				"警告: 锁定的机型「%s」已不在档案库中，本次改用按种子抽取的机型，"+
					"该 profile 的设备特征已发生变化\n", p.DeviceLabel)
		}
	}
	fp := fingerprint.DeriveWithDeviceLabel(p.Seed, resolvedGeo, p.DeviceLabel, k.Version)
	args, err := launcher.BuildArgs(p, &fp, proxyAddr, nil)
	must(err, "组装命令行")

	// 调试端口之外还要显式指定 --user-data-dir 已由 BuildArgs 处理。
	// 加 --remote-allow-origins 是必需的：Chrome 111 起会拒绝 Origin 头
	// 不在白名单内的 WebSocket 升级请求，Node 的 WebSocket 客户端默认带 Origin，
	// 不放行会直接 403 而握手失败。
	args = append(args,
		"--remote-debugging-port="+port,
		"--remote-allow-origins=*",
	)

	fmt.Printf("\nprofile : %s（种子 %d）\n", p.Name, p.Seed)
	fmt.Printf("内核    : %s\n", k.Version)
	fmt.Printf("机型    : %s\n", fp.Device.Label)
	fmt.Printf("声称    : %s / %s / %s\n", fp.Device.Platform, fp.Timezone, fp.Locale)
	fmt.Printf("数据目录: %s\n", p.ProfileDir)
	fmt.Printf("调试端口: %s\n", port)

	cmd := exec.Command(k.ExecPath, args...)
	must(cmd.Start(), "启动内核")
	fmt.Printf("\n浏览器已启动 (PID %d)。CDP 工具现在可以连接：\n", cmd.Process.Pid)
	fmt.Printf("  端点  http://127.0.0.1:%s\n", port)
	fmt.Printf("  Playwright   browser = await chromium.connectOverCDP('http://127.0.0.1:%s')\n", port)
	fmt.Printf("  Puppeteer    browser = await puppeteer.connect({browserURL: 'http://127.0.0.1:%s'})\n", port)
	fmt.Println("\n务必接管已有的 context 与 page，新建会丢掉本 profile 的会话状态：")
	fmt.Println("  const ctx = browser.contexts()[0]")
	fmt.Println("  const page = ctx.pages()[0] ?? await ctx.newPage()")
	fmt.Printf("\n接之前建议先跑 go run ./cmd/identifykernel %s 确认连的是指纹内核。\n", port)
	fmt.Println("Ctrl+C 关闭浏览器并清理转发器。")

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	select {
	case <-ctx.Done():
		fmt.Println("\n收到退出信号，正在关闭浏览器…")
		// 与 session 包一致：先优雅关闭，给浏览器机会落盘会话数据。
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
		}
	case <-exited:
		fmt.Println("\n浏览器已退出。")
	}
}

// warningsFor 复用与界面一致的出口风险提示。
func warningsFor(kind geo.IPKind, org string) []string {
	switch kind {
	case geo.IPKindHosting:
		return []string{fmt.Sprintf("出口是机房 IP（%s），多账号场景下极易被识别", org)}
	case geo.IPKindUnknown:
		return []string{"无法判定出口网络类型，请自行确认是否为住宅 IP"}
	}
	return nil
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"better-web/internal/app"
	"better-web/internal/model"
	"better-web/internal/session"
)

// profileFlag 是直接启动指定 profile 的命令行开关。
//
// 存在原因：桌面快捷方式需要绕过管理面板直达某个 profile。
// 用长选项而非位置参数，便于将来扩展且不与 Wails 自身的参数冲突。
const profileFlag = "--profile="

// openURLFlag 是系统把链接交给 better-web 时使用的开关。
//
// 注册表里写的命令行是 "<exe>" --open-url=%1，见 internal/urlhandler。
// 两处的字面量必须一致，有测试钉住，见 cli_test.go。
const openURLFlag = "--open-url="

// runCLI 处理命令行直启模式，返回是否已由 CLI 处理完毕。
//
// 返回 false 时 main 继续启动 GUI。这个分派放在 wails.Run 之前：
// 快捷方式与链接接管的意义都是跳过面板，起了窗口再关掉会闪一下。
func runCLI() (handled bool, exitCode int) {
	var target, openURL string
	for _, a := range os.Args[1:] {
		switch {
		case a == "--help" || a == "-h":
			printUsage()
			return true, 0
		case strings.HasPrefix(a, profileFlag):
			target = strings.TrimPrefix(a, profileFlag)
		case strings.HasPrefix(a, openURLFlag):
			openURL = strings.TrimPrefix(a, openURLFlag)
		}
	}
	// --open-url 先判：它由系统触发，一次只会带这一个参数。
	// 两个都给时以它为准而非报错——真出现了也说明是链接接管路径。
	if openURL != "" {
		return true, openURLFromSystem(openURL)
	}
	if target == "" {
		return false, 0
	}
	return true, launchProfile(target)
}

func printUsage() {
	fmt.Println(`better-web —— 指纹浏览器管理器

  better-web                       打开管理面板
  better-web --profile=<名称>      直接启动指定 profile 并退出
  better-web --open-url=<链接>     用配置好的 profile 打开链接
  better-web --help                显示本帮助

--profile 供桌面快捷方式使用：启动浏览器后本进程即退出，
但会保留代理转发器所需的常驻进程直到浏览器关闭。

--open-url 供系统链接接管使用，由注册表里的命令行触发，通常不手动调用。
目标 profile 在 better-web 的设置里指定。`)
}

// openURLFromSystem 处理系统交来的链接。
//
// 行为对齐"没有 better-web 时点链接"的体验：目标 profile 没在跑就完整启动它，
// 已经在跑就在已有实例里新开一个窗口。
//
// 只有新起会话时才阻塞。递送路径必须尽快退出——点链接的应用往往在等这个
// 进程返回，挂住会让它看起来卡死。
func openURLFromSystem(rawURL string) int {
	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位数据目录失败:", err)
		return 1
	}
	svc, err := app.New(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		return 1
	}
	defer func() { _ = svc.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	started, id, err := svc.OpenURL(ctx, rawURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开链接失败:", err)
		return 1
	}
	if !started {
		// 已递送给运行中的实例，本进程没有需要守护的资源，尽快退出。
		return 0
	}

	// 新起了会话，必须阻塞到浏览器退出：代理转发器跑在本进程内，
	// 进程一退出转发器就没了，浏览器随即失去代理。
	done := make(chan struct{})
	go func() {
		svc.WaitSession(id)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = svc.Stop(id)
		svc.WaitSession(id)
	}
	return 0
}

// launchProfile 启动指定名称的 profile，阻塞直到浏览器退出。
//
// 必须阻塞而非启动后立即返回：代理转发器跑在本进程内，进程一退出转发器就没了，
// 浏览器随即失去代理——那正是 fail-closed 要防的情形，只是原因换成了自己人。
func launchProfile(name string) int {
	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位数据目录失败:", err)
		return 1
	}
	svc, err := app.New(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		return 1
	}
	defer func() { _ = svc.Close() }()

	id, err := resolveProfileID(svc, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Ctrl+C 或系统关机时走正常的停止流程，让浏览器有机会落盘会话数据。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := svc.Start(ctx, id)
	if err != nil {
		if errors.Is(err, session.ErrAlreadyRunning) ||
			errors.Is(err, session.ErrLockedByOtherProcess) {
			// 重复启动同一 profile 会损坏 user-data-dir，这不是故障而是保护。
			fmt.Fprintf(os.Stderr, "%s\n", err)
			return 2
		}
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		return 1
	}
	fmt.Printf("已启动 %s (PID %d)\n", name, st.PID)
	if st.Exit != nil && st.Exit.IP != "" {
		fmt.Printf("出口: %s (%s)\n", st.Exit.IP, st.Exit.Kind)
	}
	for _, w := range st.Warnings {
		fmt.Fprintf(os.Stderr, "警告: %s\n", w)
	}

	// 等浏览器退出或收到中断信号。
	done := make(chan struct{})
	go func() {
		svc.WaitSession(id)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		fmt.Println("收到退出信号，正在关闭浏览器…")
		_ = svc.Stop(id)
		svc.WaitSession(id)
	}
	return 0
}

// resolveProfileID 按名称查找 profile，返回其 ID。
//
// 按名称而非 ID：ID 是 UUID，写进快捷方式参数里无法阅读也无法核对，
// 而名称在库中有唯一索引。
func resolveProfileID(svc *app.Service, name string) (string, error) {
	list, err := svc.ListProfiles()
	if err != nil {
		return "", fmt.Errorf("读取 profile 列表失败: %w", err)
	}
	for _, p := range list {
		if p.Name == name {
			return p.ID, nil
		}
	}

	// 找不到时列出可选名称——快捷方式的参数是手写的，拼错很常见。
	names := make([]string, 0, len(list))
	for _, p := range list {
		if p.Kind == model.KindFingerprint || p.Kind == model.KindDaily {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("找不到名为 %q 的 profile，且库中没有任何 profile", name)
	}
	return "", fmt.Errorf("找不到名为 %q 的 profile。现有: %s",
		name, strings.Join(names, "、"))
}

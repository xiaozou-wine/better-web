// Command ephemeral 启动一个用完即弃的指纹浏览器实例，并开放 CDP 端口。
//
// 与 cmd/launchdebug 的分工：那个跑库里已有的 profile，适合手工排障；这个
// 每次现造一个不落库的临时身份，适合"一个账号一套全新指纹、用完就丢"的
// 自动化场景（注册机、批量采集）。
//
// 与 cmd/multilaunch 的分工：那个为测量并发启动能力而克隆现有 profile，
// 因此多个实例**沿用同一个代理**——在真实多账号场景下会被平台直接关联。
// 这个工具要求每个实例显式指定自己的代理，不提供共用出口的可能。
//
// 三件事与 launchdebug 不同，都是自动化场景下必须的：
//
//  1. 走 session.Manager 而非自己 exec，因此拿到跨进程锁。launchdebug 绕过了
//     session 包，两个实例可以同时打开一个 user-data-dir 而不报错。
//  2. CDP 端口默认由内核分配（--cdp-port=0），避免并发时端口打架。
//     launchdebug 写死 9222 且不检测占用。
//  3. 就绪后往 stdout 打一行 JSON，把 CDP 地址、出口 IP、指纹交给调用方。
//     调用方据此 connect_over_cdp，不必猜端口也不必解析人类可读的输出。
//
// 用法：
//
//	ephemeral --proxy=<代理行> [--cdp-port=N] [--kernel=版本] [--keep-dir]
//	ephemeral --allow-direct   [--cdp-port=N]
//
// 代理行的格式与界面批量导入一致，见 model.ParseProxy：
//
//	host:port
//	host:port:user:pass          ← 密码含 @ 或 : 时只能用这种
//	scheme://host:port
//	scheme://user:pass@host:port
//
// 出口策略是 fail-closed 的：不给 --proxy 就必须显式 --allow-direct，否则
// 拒绝启动。静默用本机真实 IP 会把账号和本人关联起来，那种失败没有任何
// 可见痕迹，等发现时账号已经废了。
//
// 进程生命周期：启动后保持前台运行，直到浏览器退出或收到 Ctrl+C/SIGTERM。
// 退出时删除临时目录、关闭代理转发器、释放锁。调用方应当在用完后给本进程
// 发终止信号，而不是直接杀浏览器——转发器跑在本进程里，杀错了会留下孤儿。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/session"
)

// tmpDirPrefix 是临时 profile 目录的前缀。与 multilaunch 的 bw-multi- 区分，
// 这样两个工具的残留清理互不干扰。
const tmpDirPrefix = "bw-ephemeral-"

// readyLine 是就绪后打给调用方的一行 JSON。
//
// 用 JSON 而非人类可读文本：本工具的调用方是脚本。字段名一旦定下就是接口，
// 改动会破坏调用方，因此只增不改。
type readyLine struct {
	Event string `json:"event"`
	// CDPPort 是实际生效的调试端口。传 0 让内核分配时，这里是解析出的真实端口。
	CDPPort int `json:"cdpPort"`
	// CDPURL 可直接交给 Playwright 的 connect_over_cdp。
	CDPURL string `json:"cdpUrl"`
	PID    int    `json:"pid"`
	// ProfileDir 是本次的临时目录，退出时会删除。
	ProfileDir string `json:"profileDir"`
	Seed       int32  `json:"seed"`
	// ExitIP 是经代理实测到的出口 IP。直连模式为空。
	ExitIP string `json:"exitIp,omitempty"`
	// ExitKind 是出口网络类型判定：residential / hosting / unknown。
	ExitKind string `json:"exitKind,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Locale   string `json:"locale,omitempty"`
	Device   string `json:"device,omitempty"`
	// Warnings 是不阻断启动但调用方应当记录的问题，如出口是机房 IP。
	Warnings []string `json:"warnings,omitempty"`
}

func main() {
	fs := flag.NewFlagSet("ephemeral", flag.ContinueOnError)
	proxyLine := fs.String("proxy", "",
		"代理行，格式见 model.ParseProxy。与 --allow-direct 二选一")
	allowDirect := fs.Bool("allow-direct", false,
		"显式允许不走代理、用本机真实 IP 出网")
	cdpPort := fs.Int("cdp-port", 0,
		"CDP 调试端口。0 表示由内核分配一个空闲端口（并发时推荐）")
	kernelVer := fs.String("kernel", "",
		"锁定内核版本，留空用默认内核")
	keepDir := fs.Bool("keep-dir", false,
		"退出时保留临时目录，便于排查。注意目录含 Cookie 与登录态")
	deviceLabel := fs.String("device", "",
		"锁定机型档案 Label，留空按种子抽取")
	timeout := fs.Duration("timeout", 0,
		"运行时长上限，到点自动退出。0 表示不限")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	// fail-closed：出口必须是明确选择的结果，不存在"忘了给代理就静默直连"。
	// 见 automation/CLAUDE.md 的硬规则 3，理由同样适用于这里。
	if *proxyLine == "" && !*allowDirect {
		fatal(errors.New(
			"未指定 --proxy。要用本机真实 IP 出网必须显式加 --allow-direct，" +
				"因为那会把账号与本机关联起来"))
	}
	if *proxyLine != "" && *allowDirect {
		fatal(errors.New("--proxy 与 --allow-direct 不能同时使用"))
	}
	if *cdpPort != 0 && (*cdpPort < 1024 || *cdpPort > 65535) {
		fatal(fmt.Errorf("--cdp-port 需为 0 或 1024-65535，收到 %d", *cdpPort))
	}

	var upstream *model.Proxy
	if *proxyLine != "" {
		p, err := model.ParseProxy(*proxyLine)
		if err != nil {
			// ParseProxy 的错误信息已对密码脱敏，可以直接透出。
			fatal(fmt.Errorf("解析 --proxy 失败: %w", err))
		}
		upstream = p
	}

	base, err := os.UserConfigDir()
	if err != nil {
		fatal(fmt.Errorf("定位用户配置目录失败: %w", err))
	}
	root := filepath.Join(base, "better-web")

	kernels := kernel.NewStore(filepath.Join(root, "kernels"))
	if _, err := kernels.Resolve(*kernelVer); err != nil {
		fatal(fmt.Errorf("定位内核失败: %w", err))
	}

	// 清理上次被强杀留下的残留。放在启动之前而非退出时的唯一依靠：
	// defer 在 SIGKILL、任务管理器结束进程时不会执行，而残留目录含登录态。
	if n := sweepStaleDirs(); n > 0 {
		logf("已清理 %d 个上次运行残留的临时目录", n)
	}

	profileDir, err := os.MkdirTemp("", tmpDirPrefix)
	if err != nil {
		fatal(fmt.Errorf("创建临时目录失败: %w", err))
	}
	cleanupDir := func() {
		if *keepDir {
			logf("按 --keep-dir 保留临时目录: %s", profileDir)
			return
		}
		if err := os.RemoveAll(profileDir); err != nil {
			// 不静默吞：残留目录含 Cookie，用户需要知道它还在。
			logf("⚠ 删除临时目录失败，其中含登录态,请手动清理 %s: %v", profileDir, err)
		}
	}

	seed, err := fingerprint.NewSeed()
	if err != nil {
		cleanupDir()
		fatal(fmt.Errorf("生成指纹种子失败: %w", err))
	}

	// 临时 profile 不落库，只在内存里存在。ID 用目录名保证同一时刻唯一：
	// session.Manager 用 ID 做会话表的键，重复会误判为"已在运行"。
	p := &model.Profile{
		ID:            filepath.Base(profileDir),
		Name:          "ephemeral-" + filepath.Base(profileDir),
		Kind:          model.KindFingerprint,
		Seed:          seed,
		ProfileDir:    profileDir,
		Proxy:         upstream,
		KernelVersion: *kernelVer,
		DeviceLabel:   *deviceLabel,
		ExtraArgs: []string{
			fmt.Sprintf("--remote-debugging-port=%d", *cdpPort),
			// Chrome 111 起拒绝 Origin 不在白名单内的 WebSocket 升级请求，
			// 而多数 CDP 客户端默认带 Origin，不放行会直接 403 握手失败。
			"--remote-allow-origins=*",
		},
	}

	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	mgr := session.NewManager(kernels)
	// StrictGeo 保持默认的 true：时区与真实出口不符比启动失败更糟——
	// 声称在德国却报上海时区，本身就是最强的自动化信号。
	st, err := mgr.Start(ctx, p)
	if err != nil {
		cleanupDir()
		fatal(fmt.Errorf("启动失败: %w", err))
	}
	defer func() {
		_ = mgr.Stop(p.ID)
		mgr.Wait(p.ID)
		cleanupDir()
	}()

	// 端口由内核分配时，真实端口只能从 DevToolsActivePort 文件读。
	port := *cdpPort
	if port == 0 {
		port, err = waitDevToolsPort(ctx, profileDir, 40*time.Second)
		if err != nil {
			fatal(fmt.Errorf("读取内核分配的调试端口失败: %w", err))
		}
	} else if err := waitCDPReady(ctx, port, 40*time.Second); err != nil {
		fatal(fmt.Errorf("调试端口 %d 未就绪: %w", port, err))
	}

	line := readyLine{
		Event:      "ready",
		CDPPort:    port,
		CDPURL:     fmt.Sprintf("http://127.0.0.1:%d", port),
		PID:        st.PID,
		ProfileDir: profileDir,
		Seed:       seed,
		Warnings:   st.Warnings,
	}
	if st.Exit != nil {
		line.ExitIP = st.Exit.IP
		line.ExitKind = string(st.Exit.Kind)
	}
	if st.Fingerprint != nil {
		line.Timezone = st.Fingerprint.Timezone
		line.Locale = st.Fingerprint.Locale
		line.Device = st.Fingerprint.Device.Label
	}
	// 就绪行走 stdout 且只有这一行；诊断信息全部走 stderr，
	// 这样调用方可以直接 json.loads(stdout 的第一行) 而不必过滤。
	if err := json.NewEncoder(os.Stdout).Encode(line); err != nil {
		fatal(fmt.Errorf("输出就绪信息失败: %w", err))
	}

	for _, w := range st.Warnings {
		logf("⚠ %s", w)
	}
	logf("已就绪。CDP: http://127.0.0.1:%d  出口: %s  按 Ctrl+C 退出",
		port, exitDesc(line))

	// 浏览器被用户手动关闭时也应当退出，否则调用方会一直等一个死实例。
	browserGone := make(chan struct{})
	go func() {
		mgr.Wait(p.ID)
		close(browserGone)
	}()
	select {
	case <-ctx.Done():
		logf("收到退出信号，正在清理…")
	case <-browserGone:
		logf("浏览器已退出，正在清理…")
	}
}

func exitDesc(l readyLine) string {
	if l.ExitIP == "" {
		return "本机真实 IP（--allow-direct）"
	}
	return fmt.Sprintf("%s (%s)", l.ExitIP, l.ExitKind)
}

// sweepStaleDirs 删除上次运行残留的临时目录，返回清理个数。
//
// 只删本工具自己的前缀，不碰系统临时目录里的其他内容。正被运行中实例占用的
// 目录删不掉，跳过即可——下次运行会再试。
func sweepStaleDirs() int {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return 0
	}
	var n int
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tmpDirPrefix) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(os.TempDir(), e.Name())); err == nil {
			n++
		}
	}
	return n
}

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

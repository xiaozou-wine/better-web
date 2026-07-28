// Command multilaunch 并发启动多个 profile，测量实际耗时与资源占用。
//
// 回答"能同时开几个"这个问题。瓶颈不在本项目的代码——转发器每个实例只占
// 一个监听 socket 加一个 goroutine（实测 20 个启动共 517µs）——而在 Chromium
// 本身的内存占用，以及每个 profile 各自的代理带宽。
//
// 用法：
//
//	go run ./cmd/multilaunch <数量> [profile 名称前缀] [--cdp=<个数>[:<起始端口>]]
//
// 例：
//
//	multilaunch 5                    起 5 个，都不开调试端口
//	multilaunch 5 --cdp=2            前 2 个开 9222、9223，其余 3 个不开
//	multilaunch 5 --cdp=2:9400       起始端口改为 9400
//	multilaunch 3 美国 --cdp=1       只用名称以"美国"开头的 profile
//
// CDP 按实例而非全局开关：需要自动化的开端口，其余保持不开。实测
// （cmd/cdpexperiment）显示开端口不影响跑分，但不开也没有代价，故默认不开。
//
// 未指定前缀时使用库里已有的全部 profile。数量超过可用 profile 数时，
// 会用同一份配置克隆出临时 profile（各自独立的种子与目录），
// 因为同一 profile 不能重复启动。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/proxy"
	"better-web/internal/session"
	"better-web/internal/store"
)

func main() {
	// 清理放在参数校验之前：残留目录含 Cookie 与登录态，无论本次要不要启动
	// 实例都该清掉。放在校验之后会导致误用命令时清理被跳过。
	if n := sweepStaleDirs(); n > 0 {
		fmt.Printf("已清理 %d 个上次运行残留的临时目录\n", n)
	}

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr,
			"用法: go run ./cmd/multilaunch <数量> [profile 名称前缀] [--cdp=<个数>[:<起始端口>]]")
		os.Exit(2)
	}
	n, err := strconv.Atoi(os.Args[1])
	if err != nil || n < 1 {
		fmt.Fprintln(os.Stderr, "数量必须是正整数")
		os.Exit(2)
	}

	// --cdp=<个数>[:<起始端口>]：给前若干个实例分配调试端口。
	//
	// 按实例而非全局开关：CDP 只对需要自动化的实例开，其余保持不开端口。
	// 实测（cmd/cdpexperiment）显示开端口不影响跑分，但不开也没有任何代价，
	// 所以默认全部不开，按需指定。
	cdpCount, cdpBase := 0, 9222
	prefix := ""
	for _, a := range os.Args[2:] {
		if v, ok := strings.CutPrefix(a, "--cdp="); ok {
			cntStr, portStr, hasPort := strings.Cut(v, ":")
			c, err := strconv.Atoi(cntStr)
			if err != nil || c < 0 {
				fmt.Fprintln(os.Stderr, "--cdp 的个数必须是非负整数")
				os.Exit(2)
			}
			cdpCount = c
			if hasPort {
				p, err := strconv.Atoi(portStr)
				if err != nil || p < 1024 || p > 65535 {
					fmt.Fprintln(os.Stderr, "--cdp 的起始端口需在 1024-65535 之间")
					os.Exit(2)
				}
				cdpBase = p
			}
			continue
		}
		prefix = a
	}
	if cdpCount > n {
		cdpCount = n
	}

	base, err := os.UserConfigDir()
	must(err, "定位用户配置目录")
	root := filepath.Join(base, "better-web")

	db, err := store.Open(filepath.Join(root, "profiles.db"))
	must(err, "打开数据库")
	defer func() { _ = db.Close() }()

	all, err := db.List()
	must(err, "读取 profile")
	var pool []*model.Profile
	for _, p := range all {
		if prefix == "" || strings.HasPrefix(p.Name, prefix) {
			pool = append(pool, p)
		}
	}
	if len(pool) == 0 {
		fmt.Fprintln(os.Stderr, "没有匹配的 profile")
		os.Exit(1)
	}

	kernels := kernel.NewStore(filepath.Join(root, "kernels"))
	if _, err := kernels.Resolve(""); err != nil {
		fmt.Fprintf(os.Stderr, "定位内核失败: %v\n", err)
		os.Exit(1)
	}

	// 临时目录存放克隆 profile 的浏览数据，跑完即删，不污染真实 profile。
	//
	// 注意 defer 在进程被强杀（timeout、任务管理器结束）时不会执行，
	// 所以不能只靠它——main 开头的 sweepStaleDirs 负责兜住那种情况。
	tmpRoot, err := os.MkdirTemp("", "bw-multi-*")
	must(err, "创建临时目录")
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	// ports[i] 非零表示第 i 个实例开该调试端口。
	ports := make([]int, n)
	for i := 0; i < cdpCount; i++ {
		ports[i] = cdpBase + i
	}

	targets := make([]*model.Profile, 0, n)
	for i := 0; i < n; i++ {
		src := pool[i%len(pool)]
		if i < len(pool) {
			// 库里的 profile 不能直接改（会写回它的 ExtraArgs），
			// 需要调试端口时按值复制一份再改。
			if ports[i] != 0 {
				withCDP := *src
				withCDP.ExtraArgs = append(append([]string{}, src.ExtraArgs...),
					fmt.Sprintf("--remote-debugging-port=%d", ports[i]),
					// Chrome 111 起会拒绝 Origin 不在白名单的 WebSocket 升级，
					// 而 Node 的客户端默认带 Origin，不放行会直接握手失败。
					"--remote-allow-origins=*")
				targets = append(targets, &withCDP)
				continue
			}
			targets = append(targets, src)
			continue
		}
		// 克隆：换独立的 ID、种子与目录，但代理沿用源 profile 的。
		//
		// 种子必须换——相同种子等于同一台设备，多开就失去意义。
		//
		// 代理不换是本工具的局限，也是必须让使用者知道的事实：克隆出的实例
		// 与源 profile 走同一个出口 IP。这对测量启动能力无妨，但在真实的
		// 多账号场景下是致命的——多台"不同设备"从同一 IP 出网，平台侧
		// 一眼就能关联，指纹做得再好也无用。启动后会显式警告。
		seed, err := fingerprint.NewSeed()
		must(err, "生成种子")
		clone := *src
		clone.ID = fmt.Sprintf("multilaunch-%d", i)
		clone.Name = fmt.Sprintf("%s-clone-%d", src.Name, i)
		clone.Seed = seed
		clone.ProfileDir = filepath.Join(tmpRoot, clone.ID)
		clone.ExtraArgs = append([]string{}, src.ExtraArgs...)
		if ports[i] != 0 {
			clone.ExtraArgs = append(clone.ExtraArgs,
				fmt.Sprintf("--remote-debugging-port=%d", ports[i]),
				"--remote-allow-origins=*")
		}
		targets = append(targets, &clone)
	}

	fmt.Printf("并发启动 %d 个实例（%d 个来自库，%d 个为临时克隆）\n",
		n, min(n, len(pool)), max(0, n-len(pool)))
	if cdpCount > 0 {
		fmt.Printf("其中前 %d 个开放 CDP 调试端口 %d-%d，其余不开\n",
			cdpCount, cdpBase, cdpBase+cdpCount-1)
	}
	fmt.Println()

	mgr := session.NewManager(kernels)
	// 出口探测失败不阻断——本工具测的是启动能力，不是代理质量。
	mgr.StrictGeo = false
	defer mgr.StopAll()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	type result struct {
		name string
		pid  int
		ms   int64
		// port 非零表示该实例开放了 CDP 调试端口。
		port int
		err  error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	overall := time.Now()
	for i, p := range targets {
		wg.Add(1)
		go func(i int, p *model.Profile) {
			defer wg.Done()
			t0 := time.Now()
			st, err := mgr.Start(ctx, p)
			results[i] = result{name: p.Name, pid: st.PID,
				ms: time.Since(t0).Milliseconds(), port: ports[i], err: err}
		}(i, p)
	}
	wg.Wait()
	totalMs := time.Since(overall).Milliseconds()

	var okCount int
	var slowest int64
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("  失败  %-28s %s\n", trunc(r.name, 28), r.err)
			continue
		}
		okCount++
		if r.ms > slowest {
			slowest = r.ms
		}
		fmt.Printf("  OK    %-28s PID %-7d %5d ms", trunc(r.name, 28), r.pid, r.ms)
		if r.port != 0 {
			fmt.Printf("   CDP :%d", r.port)
		}
		fmt.Println()
	}

	fmt.Printf("\n成功 %d/%d，总耗时 %d ms（最慢单个 %d ms）\n",
		okCount, n, totalMs, slowest)
	if okCount > 1 {
		fmt.Printf("并发效率: 总耗时是最慢单个的 %.2f 倍"+
			"（接近 1 说明真并发，接近 %d 说明被串行化）\n",
			float64(totalMs)/float64(slowest), okCount)
	}

	// 内存占用是"能开几个"的真正限制，且它取决于 Chromium 而非本项目。
	pids := make([]int, 0, len(results))
	for _, r := range results {
		if r.err == nil && r.pid > 0 {
			pids = append(pids, r.pid)
		}
	}
	reportMemory(pids)

	// 出口 IP 是多账号场景真正的成败所在：指纹再自洽，多个实例共用一个
	// 出口也会被直接关联。这里逐个实测并汇总，不让共用 IP 静默发生。
	reportExits(ctx, targets)

	if cdpCount > 0 {
		fmt.Println("\n开放了调试端口的实例可接自动化工具:")
		for _, r := range results {
			if r.err == nil && r.port != 0 {
				fmt.Printf("  %-28s http://127.0.0.1:%d\n",
					trunc(r.name, 28), r.port)
			}
		}
		fmt.Println("  接之前建议先跑 cmd/identifykernel <端口> 确认连的是指纹内核")
	}

	fmt.Println("\n实例保持运行中。按 Ctrl+C 全部停止并清理。")
	<-ctx.Done()
	fmt.Println("\n正在停止全部实例…")
}

// sweepStaleDirs 删除上次运行残留的临时 profile 目录，返回清理个数。
//
// 只删本工具自己创建的 bw-multi-* 目录，不碰系统临时目录里的其他内容。
// 正在被运行中的实例占用的目录删不掉，跳过即可——下次运行会再尝试。
func sweepStaleDirs() int {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return 0
	}
	var n int
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "bw-multi-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(os.TempDir(), e.Name())); err == nil {
			n++
		}
	}
	return n
}

// reportExits 逐个查各实例的实际出口 IP 并汇总重复情况。
//
// 为什么必须查：多账号场景的成败取决于出口是否分散。指纹再自洽，多个实例
// 共用一个出口 IP 也会被平台直接关联——而这件事不会有任何报错提示，
// 必须主动测出来摆在眼前。
//
// 走各 profile 自己的代理配置发探测请求，而非读会话状态：会话里记的是启动
// 那一刻的探测结果，轮换型代理此刻的出口可能已经变了。
func reportExits(ctx context.Context, targets []*model.Profile) {
	fmt.Println("\n各实例的出口 IP:")
	byIP := map[string][]string{}
	var direct []string

	for _, p := range targets {
		if p.Proxy == nil {
			direct = append(direct, p.Name)
			fmt.Printf("  %-28s 直连（用本机真实 IP）\n", trunc(p.Name, 28))
			continue
		}
		f, err := proxy.New(p.Proxy)
		if err != nil {
			fmt.Printf("  %-28s 代理配置无效: %v\n", trunc(p.Name, 28), err)
			continue
		}
		client, err := f.HTTPClient()
		if err != nil {
			fmt.Printf("  %-28s 构造客户端失败: %v\n", trunc(p.Name, 28), err)
			continue
		}
		info, err := geo.NewResolver(client).LookupExit(ctx)
		if err != nil {
			fmt.Printf("  %-28s 探测失败: %v\n", trunc(p.Name, 28), err)
			continue
		}
		byIP[info.IP] = append(byIP[info.IP], p.Name)
		fmt.Printf("  %-28s %-16s %s (%s)\n",
			trunc(p.Name, 28), info.IP, trunc(info.Org, 24), info.Kind)
	}

	// 重复出口是关键结论，单独醒目列出。
	var shared int
	for ip, names := range byIP {
		if len(names) > 1 {
			shared++
			fmt.Printf("\n⚠ %d 个实例共用出口 %s:\n", len(names), ip)
			for _, n := range names {
				fmt.Printf("    %s\n", n)
			}
		}
	}
	if shared > 0 {
		fmt.Println("\n  多个\"不同设备\"从同一 IP 出网，平台侧可直接关联这些账号。")
		fmt.Println("  真实多账号场景下每个 profile 必须配独立代理——")
		fmt.Println("  本工具的克隆实例沿用源 profile 的代理，仅适合测量启动能力。")
	}
	if len(direct) > 0 {
		fmt.Printf("\n⚠ %d 个实例未配代理，直接用本机真实 IP 出网。\n", len(direct))
	}
	if shared == 0 && len(direct) == 0 && len(byIP) > 1 {
		fmt.Printf("\n  %d 个实例的出口两两不同，符合多账号场景的要求。\n", len(byIP))
	}
}

// reportMemory 汇总给定 PID 的内存占用。
//
// 只查传入的 PID，不枚举系统里的全部 chrome 进程——那样会把用户自己的浏览器
// 也算进来，得出误导性的数字。
func reportMemory(pids []int) {
	if len(pids) == 0 {
		return
	}
	fmt.Println("\n各实例主进程内存（不含渲染进程，实际总占用更高）:")
	for _, pid := range pids {
		out, err := exec.Command("tasklist",
			"/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
		if err != nil {
			continue
		}
		fields := strings.Split(strings.TrimSpace(string(out)), ",")
		if len(fields) >= 5 {
			fmt.Printf("  PID %-8d %s\n", pid, strings.Trim(fields[4], "\"\r\n"))
		}
	}
	fmt.Println("  提示: Chromium 每个实例还会派生多个渲染/GPU 进程，")
	fmt.Println("        按进程树统计才是真实总占用。")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

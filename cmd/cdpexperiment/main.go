// Command cdpexperiment 测量启用 CDP 对指纹跑分的影响。
//
// 回答一个具体问题：给 profile 加 --remote-debugging-port 换来自动化能力后，
// 检测站的评分会掉多少。项目一直因为"CDP 有痕迹"而不开它，但那是推断，
// 没有实测数据；本实验补上这个数据。
//
// 做法：同一个 profile、同一个种子、同一条代理链路，只切换有无调试端口，
// 各跑一次 CreepJS 与 BrowserScan，对比 lies / headless / stealth。
//
// 用法：
//
//	go run ./cmd/cdpexperiment <profile 名称> [调试端口]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/kernel"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/probe"
	"better-web/internal/proxy"
	"better-web/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: go run ./cmd/cdpexperiment <profile 名称> [调试端口]")
		os.Exit(2)
	}
	name := os.Args[1]
	port := "9333" // 默认不用 9222，避免和已在运行的调试实例抢端口
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

	ctx := context.Background()

	// 转发器与出口探测只做一次，两组实验共用同一条链路，
	// 否则代理换了出口 IP，对比就不成立。
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
		if info, err := geo.NewResolver(client).LookupExit(ctx); err == nil {
			fmt.Printf("出口: %s / %s (%s)\n", info.IP, info.Geo.CountryCode, info.Kind)
			g := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
			resolvedGeo = &g
		}
	}
	if resolvedGeo == nil {
		resolvedGeo = p.GeoOverride
	}

	fp := fingerprint.DeriveForKernel(p.Seed, resolvedGeo, k.Version)
	baseArgs, err := launcher.BuildArgs(p, &fp, proxyAddr, nil)
	must(err, "组装命令行")

	fmt.Printf("profile: %s（种子 %d）\n内核: %s\n机型: %s\n\n",
		p.Name, p.Seed, k.Version, fp.Device.Label)

	type run struct {
		label string
		extra []string
		// attach 为 true 时在跑分期间用 CDP 客户端持续发命令，
		// 模拟自动化的实际使用状态。
		attach  bool
		metrics map[string]any
		scan    map[string]any
	}
	// 三组对照，关键是区分"端口开着"和"客户端真在用"。
	//
	// 只开端口不产生任何痕迹：检测站查的是 CDP 的使用副作用（Runtime.enable
	// 改变异常栈行为、console 被劫持等），端口无人连接时浏览器行为不变。
	// 因此第三组必须真的连上去发命令，才测到自动化的实际代价。
	runs := []run{
		{label: "不开 CDP（当前默认）", extra: nil},
		{label: "只开端口，无客户端连接",
			extra: []string{"--remote-debugging-port=" + port}},
		{label: "开端口 + 客户端持续发命令",
			extra:  []string{"--remote-debugging-port=" + port},
			attach: true},
	}

	for i := range runs {
		r := &runs[i]
		fmt.Printf("── %s ──\n", r.label)

		// 每次用独立临时目录：调试端口会写 DevToolsActivePort 之类的文件，
		// 复用目录会让两组实验互相影响。
		tmp, err := os.MkdirTemp("", "bw-cdp-exp-*")
		must(err, "创建临时目录")
		defer func(d string) { _ = os.RemoveAll(d) }(tmp)

		args := make([]string, 0, len(baseArgs)+len(r.extra))
		for _, a := range baseArgs {
			if strings.HasPrefix(a, "--user-data-dir=") {
				args = append(args, "--user-data-dir="+tmp)
				continue
			}
			args = append(args, a)
		}
		args = append(args, r.extra...)

		// 跑分期间持续用 CDP 发命令，制造真实的使用痕迹。
		// 只开端口不足以复现自动化场景——痕迹来自命令的副作用，不是端口本身。
		var stopAttach func()
		if r.attach {
			stopAttach = startCDPActivity(ctx, port)
		}

		results, err := (&probe.Scorer{ExecPath: k.ExecPath}).Run(ctx, args)
		if stopAttach != nil {
			stopAttach()
		}
		must(err, "跑分")
		for _, res := range results {
			if res.Err != "" {
				fmt.Printf("  %s 采集失败: %s\n", res.Site, res.Err)
				continue
			}
			fmt.Printf("  %s\n", res.Summary())
			switch res.Site {
			case "creepjs":
				r.metrics = res.Metrics
			case "browserscan":
				r.scan = res.Metrics
			}
		}
		fmt.Println()
	}

	// 对比：只看直接反映伪造质量的四项。
	fmt.Println(strings.Repeat("═", 64))
	fmt.Println("对比（同一 profile、同一出口，仅切换有无调试端口）")
	fmt.Println()
	// 列宽固定，组数由 runs 决定——写死列数会在增减对照组时静默漏掉数据。
	fmt.Printf("%-20s", "指标")
	for i := range runs {
		fmt.Printf("%-16s", fmt.Sprintf("组%d", i+1))
	}
	fmt.Println()
	for _, key := range []string{"liesCount", "headlessPct", "stealthPct", "likeHeadlessPct"} {
		fmt.Printf("%-20s", key)
		for i := range runs {
			fmt.Printf("%-16v", runs[i].metrics[key])
		}
		fmt.Println()
	}
	fmt.Printf("%-20s", "BrowserScan")
	for i := range runs {
		fmt.Printf("%-16v", runs[i].scan["verdict"])
	}
	fmt.Println()
	fmt.Println()
	for i, r := range runs {
		fmt.Printf("  组%d = %s\n", i+1, r.label)
	}

	// lies 的具体条目最有价值：CDP 若被检出，会以具体某项"撒谎"的形式出现。
	for i, r := range runs {
		if lies, ok := r.metrics["lies"].([]any); ok && len(lies) > 0 {
			fmt.Printf("\n%s 的 lies 条目:\n", runs[i].label)
			for _, l := range lies {
				fmt.Printf("  · %v\n", l)
			}
		}
	}

	out := filepath.Join("internal", "probe", "testdata",
		fmt.Sprintf("cdp-experiment-%s.json", k.Version))
	groups := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		groups = append(groups, map[string]any{
			"label":       r.label,
			"extraArgs":   r.extra,
			"cdpAttached": r.attach,
			"creepjs":     r.metrics,
			"browserscan": r.scan,
		})
	}
	payload := map[string]any{
		"profile":   p.Name,
		"seed":      p.Seed,
		"kernel":    k.Version,
		"debugPort": port,
		"groups":    groups,
	}
	if b, err := json.MarshalIndent(payload, "", "  "); err == nil {
		if err := os.WriteFile(out, append(b, '\n'), 0o600); err == nil {
			fmt.Printf("\n结果已归档: %s\n", out)
		}
	}
}

// startCDPActivity 连上调试端口并持续发命令，返回停止函数。
//
// 目的是复现自动化的真实状态：检测站查的是 CDP 命令的副作用，而非端口本身。
// 因此这里反复调 Runtime.enable 与 Runtime.evaluate——前者会改变异常栈与
// console 的行为，是 CDP 痕迹的主要来源。
//
// 采用 HTTP 端点而非 WebSocket：只需制造痕迹，不需要读取结果，
// /json/list 与 /json/new 已足够触发 target 附着。
func startCDPActivity(ctx context.Context, port string) func() {
	ctx, cancel := context.WithCancel(ctx)
	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 5 * time.Second}

	go func() {
		// 等浏览器把调试端口准备好。启动瞬间连接会被拒。
		time.Sleep(3 * time.Second)
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()

		var wsURL string
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			// 找到跑分页面所在的 target。
			if wsURL == "" {
				resp, err := client.Get(base + "/json/list")
				if err != nil {
					continue
				}
				var targets []struct {
					Type  string `json:"type"`
					URL   string `json:"url"`
					WSURL string `json:"webSocketDebuggerUrl"`
					Title string `json:"title"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&targets)
				_ = resp.Body.Close()
				for _, t := range targets {
					if t.Type == "page" && t.WSURL != "" &&
						!strings.HasPrefix(t.URL, "about:") {
						wsURL = t.WSURL
						break
					}
				}
				continue
			}

			// 通过 WebSocket 发真正会留痕的命令。
			if err := pokeCDP(wsURL); err != nil {
				// target 可能已关闭（跑分换站点了），重新找。
				wsURL = ""
			}
		}
	}()
	return cancel
}

// pokeCDP 在一条短连接上发几个会留痕的 CDP 命令。
//
// Runtime.enable 是关键：它让 V8 向调试器上报异常与 console 调用，
// 副作用是改变 Error.stack 的行为——检测脚本正是据此判断有调试器附着。
func pokeCDP(wsURL string) error {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
		return err
	}

	cmds := []map[string]any{
		{"id": 1, "method": "Runtime.enable"},
		{"id": 2, "method": "Page.enable"},
		{"id": 3, "method": "Network.enable"},
		{"id": 4, "method": "Runtime.evaluate",
			"params": map[string]any{"expression": "1+1", "returnByValue": true}},
	}
	for _, c := range cmds {
		if err := conn.WriteJSON(c); err != nil {
			return err
		}
	}
	// 读几条回复，确认命令确实被处理而不是排在缓冲区里。
	// 事件通知也会混在其中，所以只按次数读，不校验 id。
	for i := 0; i < len(cmds); i++ {
		if _, _, err := conn.ReadMessage(); err != nil {
			return err
		}
	}
	return nil
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

// Command scoreprofile 用指定 profile 的真实配置在检测站点上跑分。
//
// 与 probe 包里的测试的区别：那些用固定种子和直连，测的是内核能力上限；
// 这里用库里真实的 profile 配置（含其代理），测的是该 profile 实际
// 呈现给网站的样子——包含代理链路带来的影响。
//
// 用法：
//
//	go run ./cmd/scoreprofile <profile 名称>            # 自洽性评分
//	go run ./cmd/scoreprofile <profile 名称> --antibot   # 商业风控实测
//
// 两个站点集的判据不同，因此分开跑：默认集（CreepJS / BrowserScan）测环境
// 自洽性，判定逻辑公开且稳定；--antibot 集（Cloudflare / DataDome）测放行或
// 拦截，判定逻辑不公开且随出口 IP 信誉浮动，结论必须连同出口 ASN 一起解读。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		fmt.Fprintln(os.Stderr,
			"用法: go run ./cmd/scoreprofile <profile 名称> [--antibot]")
		os.Exit(2)
	}
	target := os.Args[1]
	antibot := false
	for _, a := range os.Args[2:] {
		if a == "--antibot" {
			antibot = true
		}
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
		if item.Name == target {
			p = item
			break
		}
	}
	if p == nil {
		fmt.Fprintf(os.Stderr, "找不到名为 %q 的 profile\n", target)
		os.Exit(1)
	}
	if p.Kind != model.KindFingerprint {
		fmt.Fprintf(os.Stderr, "profile %q 是日常模式，不做指纹伪造，跑分无意义\n", target)
		os.Exit(1)
	}

	k, err := kernel.NewStore(filepath.Join(root, "kernels")).Resolve(p.KernelVersion)
	must(err, "定位内核")
	fmt.Printf("profile : %s（种子 %d）\n内核    : %s\n", p.Name, p.Seed, k.Version)

	ctx := context.Background()

	// 起转发器：内核的 --proxy-server 不支持密码认证，带认证的代理必须经转发接入。
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
			fmt.Printf("代理    : %s://%s:%d（出口探测失败: %v）\n",
				p.Proxy.Scheme, p.Proxy.Host, p.Proxy.Port, err)
		} else {
			fmt.Printf("代理    : %s://%s:%d\n出口    : %s / %s / %s (%s)\n",
				p.Proxy.Scheme, p.Proxy.Host, p.Proxy.Port,
				info.IP, info.Geo.CountryCode, info.Org, info.Kind)
			g := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
			resolvedGeo = &g
		}
	} else {
		fmt.Println("代理    : 直连（该 profile 未配置代理）")
	}
	if resolvedGeo == nil {
		resolvedGeo = p.GeoOverride
	}

	// 用与实际启动相同的方式推导指纹并组装参数。
	fp := fingerprint.DeriveForKernel(p.Seed, resolvedGeo, k.Version)
	fmt.Printf("机型    : %s\n声称    : %s / %s / %s\n\n",
		fp.Device.Label, fp.Device.Platform, fp.Timezone, fp.Locale)

	args, err := launcher.BuildArgs(p, &fp, proxyAddr, nil)
	must(err, "组装命令行")

	// 用临时目录跑分，不污染该 profile 的真实浏览数据。
	tmp, err := os.MkdirTemp("", "bw-score-*")
	must(err, "创建临时目录")
	defer func() { _ = os.RemoveAll(tmp) }()
	args = replaceUserDataDir(args, tmp)

	scorer := &probe.Scorer{ExecPath: k.ExecPath}
	label := "自洽性评分"
	if antibot {
		scorer.Sites = probe.AntibotSites
		label = "商业风控实测"
	}

	fmt.Printf("开始%s（每个站点单独启动一次浏览器，请勿关闭窗口）…\n", label)
	results, err := scorer.Run(ctx, args)
	must(err, label)

	for _, r := range results {
		fmt.Println("\n" + strings.Repeat("─", 56))
		fmt.Printf("%s（耗时 %dms）\n", r.Site, r.ElapsedMs)
		if r.Err != "" {
			fmt.Printf("  采集失败: %s\n", r.Err)
			continue
		}
		if antibot {
			printAntibot(r)
			continue
		}
		fmt.Println("  " + r.Summary())
	}
	if antibot {
		fmt.Println("\n注意：Cloudflare 与 DataDome 的判定随出口 IP 信誉浮动。")
		fmt.Println("通过不代表指纹过关（可能只是该 IP 当时干净），")
		fmt.Println("拦截也不代表指纹有问题（可能只是该 IP 被标记过）。")
	}

	prefix := "scores-profile"
	if antibot {
		prefix = "antibot-profile"
	}
	out := filepath.Join("internal", "probe", "testdata",
		fmt.Sprintf("%s-%s-%s.json", prefix, sanitize(p.Name), k.Version))
	if err := probe.SaveScores(out, results); err != nil {
		fmt.Fprintf(os.Stderr, "\n归档失败: %v\n", err)
		return
	}
	fmt.Printf("\n结果已归档: %s\n", out)
	if b, err := json.MarshalIndent(results, "", "  "); err == nil {
		fmt.Printf("\n完整数据:\n%s\n", b)
	}
}

// printAntibot 打印商业风控站的判定。
//
// 单独成函数而不复用 ScoreResult.Summary()：那个方法按固定键序拼一行，
// 适合趋势比对；这里的结论是二元的（过/被拦），且被拦时需要看清依据是哪一项，
// 平铺成一行反而看不出来。
func printAntibot(r probe.ScoreResult) {
	passed, _ := r.Metrics["passed"].(bool)
	if passed {
		fmt.Println("  结论: 通过（页面正常加载，未出现挑战或拦截）")
	} else {
		fmt.Println("  结论: 未通过")
	}
	for _, key := range []string{
		"finalHost", "finalURL", "title", "onCaptchaDomain",
		"challengeSelectors", "challengeTexts", "blockedTexts", "bodyLen",
	} {
		if v, ok := r.Metrics[key]; ok && v != nil && !isEmpty(v) {
			fmt.Printf("    %s = %v\n", key, v)
		}
	}
	// 该标记说明先到的上报判"通过"，但随后被跳转页的"未通过"推翻。
	// 必须显式提示：它意味着原页面短暂可读，容易被误当成过关。
	if v, ok := r.Metrics["__supersededPass"].(bool); ok && v {
		fmt.Println("    ⚠ 先到的「通过」已被跳转后的页面推翻，采信后者")
	}
	if v, ok := r.Metrics["excerpt"].(string); ok && v != "" {
		fmt.Printf("    excerpt = %s\n", v)
	}
}

// isEmpty 判断提取出的值是否为空，用于跳过无信息的字段。
// 空切片在 JSON 里是 []，直接打印会输出一堆无意义的 []。
func isEmpty(v any) bool {
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case string:
		return t == ""
	case bool:
		return !t
	}
	return false
}

// replaceUserDataDir 把参数里的 user-data-dir 换成给定目录。
// 跑分不应写入真实 profile 的浏览数据。
func replaceUserDataDir(args []string, dir string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "--user-data-dir=") {
			out = append(out, "--user-data-dir="+dir)
			continue
		}
		out = append(out, a)
	}
	return out
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func must(err error, what string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
		os.Exit(1)
	}
}

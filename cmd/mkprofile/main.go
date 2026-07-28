// Command mkprofile 从命令行创建一个 profile，供无 GUI 环境使用。
//
// 存在的理由：面板是唯一的 profile 创建入口，而自动化流程（注册机、爬虫）
// 需要在命令行里备好环境。本工具复用 store/fingerprint/model 三个包，
// 因此校验规则、DPAPI 加密、种子派生与面板完全一致，不是绕过它们直写 SQLite。
//
// 用法：
//
//	go run ./cmd/mkprofile -name <名称> [-seed N] [-match-gpu] [-proxy <行>] [-kernel <版本>]
//
//	-match-gpu  反复试种子直到派生出与宿主机同厂商的 GPU（过 Cloudflare 必需，
//	            见 memory/gpu-family-cloudflare.md）。与 -seed 互斥。
//	-proxy      支持 host:port、host:port:user:pass、scheme://user:pass@host:port
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"better-web/internal/app"
	"better-web/internal/fingerprint"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/probe"
	"better-web/internal/store"
)

// prober 把 probe 的探测能力适配成 fingerprint.GPUProber，
// 与 cmd/matchseed 里的同名适配器一致。
type prober struct{ execPath string }

func (p prober) SeedGPUFamily(ctx context.Context, seed int32) (model.GPUFamily, string, error) {
	return probe.SeedGPUFamily(ctx, p.execPath, seed)
}

func main() {
	var (
		name     = flag.String("name", "", "profile 名称（必填）")
		seedFlag = flag.Int64("seed", 0, "指定种子；0 表示随机生成")
		matchGPU = flag.Bool("match-gpu", false, "筛选与宿主机 GPU 同厂商的种子")
		proxyStr = flag.String("proxy", "", "代理，留空表示不用代理")
		kernelV  = flag.String("kernel", "", "锁定内核版本，留空跟随最新")
		daily    = flag.Bool("daily", false, "创建日常模式 profile（不做指纹伪造）")
		tries    = flag.Int("tries", 24, "-match-gpu 的尝试上限")
		device   = flag.String("device", "", "锁定机型档案标签，留空按种子抽取")
	)
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "必须给 -name")
		flag.Usage()
		os.Exit(2)
	}
	if *matchGPU && *seedFlag != 0 {
		fmt.Fprintln(os.Stderr, "-match-gpu 与 -seed 互斥：前者要自己搜种子")
		os.Exit(2)
	}
	if *daily && (*matchGPU || *seedFlag != 0) {
		fmt.Fprintln(os.Stderr, "日常模式不用种子，-daily 不能与 -seed/-match-gpu 同用")
		os.Exit(2)
	}

	paths, err := app.DefaultPaths()
	must(err, "定位数据目录")
	must(paths.EnsureDirs(), "创建数据目录")

	db, err := store.Open(paths.DB)
	must(err, "打开数据库")
	defer func() { _ = db.Close() }()

	// 重名直接拒绝：launchdebug 按名字查 profile，重名会让它取到第一个匹配，
	// 结果用错身份而没有任何提示。
	list, err := db.List()
	must(err, "读取现有 profile")
	for _, p := range list {
		if p.Name == *name {
			fmt.Fprintf(os.Stderr, "已存在名为 %q 的 profile（ID %s）\n", *name, p.ID)
			os.Exit(1)
		}
	}

	kind := model.KindFingerprint
	if *daily {
		kind = model.KindDaily
	}

	// 提前校验机型档案：存了不存在的标签会在启动时静默回退成按种子抽取，
	// 用户以为锁定了机型其实没有（与 app.CreateProfile 同一条理由）。
	if *device != "" {
		if _, ok := fingerprint.FindDevice(*device); !ok {
			fmt.Fprintf(os.Stderr, "机型档案 %q 不存在。可用标签见 internal/fingerprint/catalog.go\n", *device)
			os.Exit(2)
		}
	}

	id := uuid.NewString()
	p := &model.Profile{
		ID:            id,
		Name:          *name,
		Kind:          kind,
		ProfileDir:    paths.ProfileDir(id),
		KernelVersion: *kernelV,
		DeviceLabel:   *device,
		Notes:         "由 cmd/mkprofile 创建",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if *proxyStr != "" {
		pr, err := model.ParseProxy(*proxyStr)
		must(err, "解析代理")
		p.Proxy = pr
		// 只打 host:port，不打账密
		fmt.Printf("代理: %s://%s:%d%s\n", pr.Scheme, pr.Host, pr.Port,
			map[bool]string{true: "（含认证）", false: ""}[pr.NeedsAuth()])
	}

	if kind == model.KindFingerprint {
		switch {
		case *matchGPU:
			k, err := kernel.NewStore(paths.Kernels).Resolve(*kernelV)
			must(err, "定位内核")
			ctx := context.Background()
			host, renderer, err := probe.HostGPU(ctx, k.ExecPath)
			must(err, "探测宿主机 GPU")
			fmt.Printf("宿主机 GPU: %s\n  %s\n", host, renderer)
			fmt.Printf("筛选同族种子（上限 %d 次，每次约 2 秒）…\n", *tries)
			match, err := fingerprint.FindSeedForGPUFamily(
				ctx, prober{execPath: k.ExecPath}, host, *tries)
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
			// 筛选失败一律报错，不静默退回随机种子 —— 用这个选项就是为了过
			// Cloudflare，给一个跨厂商的种子会让人以为已经能过
			must(err, "筛选同族种子")
			p.Seed = match.Seed
			fmt.Printf("同族种子: %d（%s）\n", match.Seed, host)
		case *seedFlag != 0:
			if *seedFlag < 1 || *seedFlag > 2147483647 {
				fmt.Fprintln(os.Stderr, "-seed 必须在 1..2147483647（负值在内核里行为不确定）")
				os.Exit(2)
			}
			p.Seed = int32(*seedFlag)
		default:
			s, err := fingerprint.NewSeed()
			must(err, "生成种子")
			p.Seed = s
		}
	}

	must(p.ValidateBrowserChoice(), "校验浏览器选择")
	must(db.Save(p), "保存 profile")

	fmt.Printf("\n已创建 profile %q\n", p.Name)
	fmt.Printf("  ID    : %s\n", p.ID)
	fmt.Printf("  类型  : %s\n", p.Kind)
	if p.Kind == model.KindFingerprint {
		fmt.Printf("  种子  : %d\n", p.Seed)
	}
	if p.DeviceLabel != "" {
		fmt.Printf("  机型  : %s（已锁定）\n", p.DeviceLabel)
	}
	fmt.Printf("  目录  : %s\n", p.ProfileDir)
	fmt.Printf("\n带调试端口启动:\n  go run ./cmd/launchdebug %s [端口]\n", p.Name)
}

func must(err error, what string) {
	if err == nil {
		return
	}
	if errors.Is(err, model.ErrSystemBrowserWithFingerprint) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "%s失败: %v\n", what, err)
	os.Exit(1)
}

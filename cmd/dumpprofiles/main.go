// Command dumpprofiles 打印本机已配置的 profile 及其推导出的环境。
//
// 用途：在不启动界面的情况下核对配置是否符合预期，或排查"为什么这个
// profile 报出这样的指纹"。只读，不修改任何数据。
//
// 刻意不输出代理密码：该值以明文存于库中，回显到终端会进入命令历史与日志。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
	"better-web/internal/store"
)

func main() {
	base, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位用户配置目录失败:", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(base, "better-web", "profiles.db")

	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败:", err)
		os.Exit(1)
	}
	defer func() { _ = s.Close() }()

	list, err := s.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 profile 失败:", err)
		os.Exit(1)
	}

	fmt.Printf("数据库: %s\n共 %d 个 profile\n", dbPath, len(list))
	for _, p := range list {
		fmt.Println("\n" + line())
		fmt.Printf("名称   : %s\n", p.Name)
		fmt.Printf("类型   : %s\n", p.Kind)
		if p.Kind == model.KindFingerprint {
			fmt.Printf("种子   : %d\n", p.Seed)
		}
		printProxy(p.Proxy)
		if p.KernelVersion != "" {
			fmt.Printf("锁定内核: %s\n", p.KernelVersion)
		} else {
			fmt.Println("锁定内核: 未锁定（跟随最新）")
		}
		if len(p.DisableSpoofing) > 0 {
			fmt.Printf("已关闭伪造: %v  ← 排障用，正常应为空\n", p.DisableSpoofing)
		}
		if p.GeoOverride != nil {
			fmt.Printf("手动指定地理: %s / %s / %s  ← 跳过出口反查\n",
				p.GeoOverride.CountryCode, p.GeoOverride.Timezone, p.GeoOverride.Locale)
		}
		if p.Kind == model.KindFingerprint {
			printFingerprint(p)
		}
	}
	fmt.Println("\n" + line())
	fmt.Println("提示：出口 IP 与实际生效的时区只有启动后才能确定，")
	fmt.Println("上面的时区来自 GeoOverride 或推导默认值，界面卡片显示的才是运行时事实。")
}

func printProxy(pr *model.Proxy) {
	if pr == nil {
		fmt.Println("代理   : 直连（未配置）  ← 指纹伪装但 IP 是真实的")
		return
	}
	auth := "无认证"
	if pr.NeedsAuth() {
		auth = "已配置认证"
	}
	fmt.Printf("代理   : %s://%s:%d（%s）\n", pr.Scheme, pr.Host, pr.Port, auth)
}

func printFingerprint(p *model.Profile) {
	fp := fingerprint.Derive(p.Seed, p.GeoOverride)
	fmt.Println("推导出的环境:")
	fmt.Printf("  机型      : %s\n", fp.Device.Label)
	fmt.Printf("  声称系统  : %s %s\n", fp.Device.Platform, fp.Device.PlatformVersion)
	fmt.Printf("  浏览器品牌: %s %s\n", fp.Brand, fp.BrandVersion)
	fmt.Printf("  CPU 核数  : %d\n", fp.Device.HardwareConcurrency)
	fmt.Printf("  时区/语言 : %s / %s\n", fp.Timezone, fp.Locale)
	fmt.Printf("  Accept-Lang: %s\n", fp.AcceptLanguages)
	if !fp.Device.Safe() {
		fmt.Printf("  ⚠ 该档案有已知缺陷: %s\n", fp.Device.KnownIssue)
	}
	// 这几项在档案里有值但内核 148 不接受对应参数，避免用户误以为它们生效。
	fmt.Printf("  （档案中的 GPU「%s」、屏幕 %dx%d、内存 %.0fGB 均不生效，\n",
		fp.Device.GPURenderer, fp.Device.ScreenWidth, fp.Device.ScreenHeight,
		fp.Device.DeviceMemory)
	fmt.Println("    这些值由内核自行从种子派生，实际上报值需启动后实测）")
}

func line() string {
	return "────────────────────────────────────────────────────────"
}

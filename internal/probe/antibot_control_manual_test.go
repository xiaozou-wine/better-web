package probe

import (
	"context"
	"os"
	"testing"
)

// 对照实验：区分商业风控的拦截原因是指纹还是出口 IP。
//
// 启用方式（不带代理，直连本机出口）：
//
//	BW_RUN_ANTIBOT_CONTROL=1 \
//	  go test -run TestAntibotControl -timeout 600s -v ./internal/probe/
//
// 为什么必须做这个对照：TestAntibotRealSites 只能得出"这条链路没过"，
// 得不出"为什么没过"。Cloudflare 与 DataDome 都把 IP 信誉作为首要信号，
// 机房 IP 常被直接判定，此时指纹再自洽也过不去。反过来，若同一 IP 上
// 普通 Chrome 能过而指纹内核过不去，才说明问题出在指纹。
//
// 分组是累加式的：某组开始失败，责任就在该组新加的那个参数上。A 组（裸跑）
// 是基线，它同时充当 IP 信誉的对照——A 也失败时说明问题在 IP 不在指纹。
//
// # 实测结论（内核 148.0.7778.215，2026-07-27，直连住宅出口，宿主机 RTX 2070）
//
// Cloudflare 的判据是**伪造 GPU 与宿主机 GPU 跨厂商**，不是"存在 GPU 伪造"。
//
//	A 裸跑                                  → 通过
//	B 只加 --fingerprint（派生 Intel 集显）    → 被拦（Just a moment...）
//	F 种子，--disable-spoofing=canvas         → 被拦
//	G 种子，--disable-spoofing=canvas,gpu     → 通过
//	I 种子，--disable-spoofing=gpu            → 通过
//	J 种子，关除 gpu 外全部                    → 被拦
//	K 种子，--disable-features=WebGPU         → 被拦
//	L 种子，关 WebGPU + 屏蔽 debug_renderer_info → 被拦
//	M 种子 470000000（派生 RTX 3060，同族）    → 通过
//	N 种子 544000000（派生 RTX 4060，同族）    → 通过
//	O 种子 174000000（派生 AMD Radeon，跨厂商） → 被拦
//
// 三层验证：
//   - I 与 J 双向确认责任在 gpu 这一项（只关它就过、只留它就挂）
//   - M/N 与 B/O 确认判据是跨厂商而非存在伪造：同族伪造照样开着 GPU 伪造却能过，
//     而跨厂商在 Intel 与 AMD 两个方向上都被拦
//   - K/L 排除 WebGPU 与 debug_renderer_info 这两条通路
//
// B、M、N 各复跑两轮，同批次内结论一致。
//
// 矛盾的具体来源见 TestGPUSpoofContradiction：伪造只改 WebGL 的
// vendor/renderer 字符串，而 pixelHash（实际渲染结果）、扩展列表、着色器精度、
// 15/16 项能力上限、WebGPU 的 adapterInfo 全都是真实 NVIDIA 的值。
// 同族之所以能过，是因为同厂商不同型号之间这些值本就接近。
//
// DataDome：本次未能得出指纹层结论。裸跑组前三轮通过、之后转为持续被拦，
// 期间参数未变，仅该 IP 的累计访问次数增加——这是 IP 维度的频次限流，
// 不是指纹判定。要测 DataDome 需换未使用过的出口并隔开时间。
func TestAntibotControl(t *testing.T) {
	if os.Getenv("BW_RUN_ANTIBOT_CONTROL") != "1" {
		t.Skip("未设置 BW_RUN_ANTIBOT_CONTROL=1，跳过对照实验")
	}
	k := realKernel(t)

	// 直连：本机出口通常是住宅 IP，与代理的机房 IP 形成对照。
	// 不查出口地也不设时区，保持"什么都不伪造"这个基线的纯粹性。
	// 逐个参数加上去，定位是哪一项引入了可检测特征。
	// 顺序是累加式的：某组开始失败，责任就在该组新加的那个参数上。
	groups := []struct {
		name string
		args []string
	}{
		{"A 内核裸跑", nil},
		{"B 仅 --fingerprint 种子", []string{
			"--fingerprint=770828460",
		}},
		{"C 种子 + 平台", []string{
			"--fingerprint=770828460",
			"--fingerprint-platform=windows",
		}},
		{"D 种子 + 平台 + 时区", []string{
			"--fingerprint=770828460",
			"--fingerprint-platform=windows",
			"--timezone=Europe/Berlin",
		}},
		{"E 种子 + 平台 + 时区 + 语言", []string{
			"--fingerprint=770828460",
			"--fingerprint-platform=windows",
			"--timezone=Europe/Berlin",
			"--lang=de-DE",
		}},
		// 实测 B 组（只加种子）就足以让 Cloudflare 拦下，因此下面用
		// --disable-spoofing 逐维度排除，定位是哪一项渲染类伪造触发的检测。
		// 内核 144+ 支持的可关闭维度：font, audio, canvas, clientrects, gpu。
		{"F 种子，关 canvas", []string{
			"--fingerprint=770828460",
			"--disable-spoofing=canvas",
		}},
		{"G 种子，关 canvas+gpu", []string{
			"--fingerprint=770828460",
			"--disable-spoofing=canvas,gpu",
		}},
		{"H 种子，全关", []string{
			"--fingerprint=770828460",
			"--disable-spoofing=font,audio,canvas,clientrects,gpu",
		}},
		// F（关 canvas）失败而 G（关 canvas+gpu）通过，差别只有 gpu，
		// 因此单独验证：只关 gpu 是否足以通过。
		{"I 种子，只关 gpu", []string{
			"--fingerprint=770828460",
			"--disable-spoofing=gpu",
		}},
		// 复核另一个方向：只关 gpu 之外的四项，应当仍然失败。
		{"J 种子，关除 gpu 外全部", []string{
			"--fingerprint=770828460",
			"--disable-spoofing=font,audio,canvas,clientrects",
		}},
		// TestGPUSpoofContradiction 查明 GPU 伪造只改了 WebGL 的
		// vendor/renderer 字符串，而 WebGPU 的 adapterInfo 照实上报
		// （nvidia / turing），与声称的 Intel 直接矛盾。
		// K 组关掉 WebGPU，验证它是否就是检测所用的通路。
		{"K 种子，关 WebGPU", []string{
			"--fingerprint=770828460",
			"--disable-features=WebGPU",
		}},
		// L 组同时关掉 WebGPU 与 WebGL 能力查询扩展。若 K 仍失败，
		// 说明检测用的是能力参数或渲染结果，而非 WebGPU。
		{"L 种子，关 WebGPU + 屏蔽 debug_renderer_info", []string{
			"--fingerprint=770828460",
			"--disable-features=WebGPU",
			"--disable-webgl-debug-renderer-info",
		}},
		// 决定性实验：伪造成与宿主机同族的 GPU。
		//
		// 默认种子把 NVIDIA RTX 4060 伪造成 Intel 集显，跨厂商跨架构，
		// 渲染行为、能力上限、扩展列表全都对不上。下面两个种子经
		// TestScanSeedGPU 扫描确认派生出 NVIDIA 型号（与宿主机同族），
		// 若它们能过，说明判据是"跨厂商矛盾"而非"存在伪造"——
		// 那么按宿主机 GPU 族筛选种子就是可行的规避手段。
		{"M 同族伪造 RTX 3060", []string{"--fingerprint=470000000"}},
		{"N 同族伪造 RTX 4060", []string{"--fingerprint=544000000"}},
		// 反向对照：换一个跨厂商的方向（AMD 而非 Intel），
		// 排除"只有 Intel 这一族会被拦"的可能。
		{"O 跨厂商伪造 AMD Radeon", []string{"--fingerprint=174000000"}},
	}

	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			results, err := (&Scorer{ExecPath: k.ExecPath, Sites: AntibotSites}).
				Run(context.Background(), g.args)
			if err != nil {
				t.Fatalf("实测失败: %v", err)
			}
			for _, r := range results {
				if r.Err != "" {
					t.Logf("[%s] 采集失败: %s", r.Site, r.Err)
					continue
				}
				passed, _ := r.Metrics["passed"].(bool)
				verdict := "未通过"
				if passed {
					verdict = "通过"
				}
				t.Logf("[%s] %s | title=%v selectors=%v bodyLen=%v",
					r.Site, verdict, r.Metrics["title"],
					r.Metrics["challengeSelectors"], r.Metrics["bodyLen"])
			}
		})
	}
}

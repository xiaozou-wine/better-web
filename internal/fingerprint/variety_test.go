package fingerprint

import (
	"fmt"
	"testing"

	"better-web/internal/model"
)

// 统计档案库实际能产出多少个可区分的机型组合。
//
// 必须区分两个数字，它们差得很远：
//   - 档案条目数：库里写了几条
//   - 可区分组合数：网站实际能看出几种不同环境
//
// 只有 Platform、PlatformVersion、HardwareConcurrency 三个字段会传给内核。
// GPU、屏幕、内存不生效（内核 144 起由种子自行派生），所以两条只有 GPU
// 不同的档案，在网站看来是同一种机型——它们没有贡献多样性。
func TestReportDistinguishableDeviceVariety(t *testing.T) {
	type effective struct {
		platform model.Platform
		version  string
		cores    int
	}

	all := map[effective][]string{}
	safe := map[effective][]string{}
	for _, d := range deviceCatalog {
		k := effective{d.Platform, d.PlatformVersion, d.HardwareConcurrency}
		all[k] = append(all[k], d.Label)
		if d.Safe() {
			safe[k] = append(safe[k], d.Label)
		}
	}

	t.Logf("档案条目: 全部 %d 条，参与随机抽取 %d 条",
		len(deviceCatalog), len(safeCatalog))
	t.Logf("可区分组合（platform + platformVersion + cores）: 全部 %d 种，可抽取 %d 种",
		len(all), len(safe))

	t.Log("\n各组合及其对应的档案:")
	for k, labels := range all {
		os := map[model.Platform]string{
			model.PlatformWindows: "Windows",
			model.PlatformMacOS:   "macOS",
			model.PlatformLinux:   "Linux",
		}[k.platform]
		// platformVersion 的语义按平台不同：Windows 上 10=Win10、13+=Win11；
		// macOS 上直接是系统版本号。
		desc := fmt.Sprintf("%s %s / %d 核", os, k.version, k.cores)
		if len(labels) > 1 {
			t.Logf("  %-32s ← %d 条档案共用此组合（GPU 不同但不生效）:",
				desc, len(labels))
			for _, l := range labels {
				t.Logf("      %s", l)
			}
			continue
		}
		t.Logf("  %-32s ← %s", desc, labels[0])
	}

	// 重合的组合说明档案库里有"看起来不同、实际相同"的条目。
	var dup int
	for _, labels := range all {
		if len(labels) > 1 {
			dup++
		}
	}
	if dup > 0 {
		t.Logf("\n有 %d 组档案在生效字段上完全相同。它们不增加可区分的机型数——"+
			"网站看到的环境一致，只有内核自行派生的 GPU 会不同。", dup)
	}

	// canvas/audio 由种子驱动，与档案无关：即使两个 profile 抽到同一档案，
	// 它们的 canvas 仍然不同，因此仍可区分。这点要说清，避免把
	// "可区分机型数"误当成"可用 profile 上限"。
	t.Log("\n注意: 抽到相同档案的 profile 仍可被区分——canvas 由种子驱动，" +
		"实测 24 个种子 100% 唯一。可区分组合数衡量的是\"声称的硬件环境\"" +
		"有几种，不是能开多少个互不关联的 profile。")
}

package fingerprint

import (
	"fmt"
	"sort"
	"testing"

	"better-web/internal/model"
)

// 分析档案库在"生效字段"维度上的覆盖空缺，用于判断扩充的余地。
//
// 生效字段只有三个：platform、platformVersion、hardwareConcurrency。
// 因此可区分的机型组合数上界 = 平台数 × 该平台的版本数 × 核数档位数。
//
// 但上界不是目标。档案库的价值在于每一条都对应真实世界常见的配置——
// 填满所有组合会引入"macOS 4 核""Windows 10 20 核"这类罕见搭配，
// 反而缩小匿名集，比缺少档案更糟。
func TestReportCatalogCoverageGaps(t *testing.T) {
	// 真实人群中各平台的常见核数档位。
	//
	// 依据：Windows 桌面覆盖从办公本到游戏本，4~20 核都常见；
	// macOS 只有 Apple Silicon 的固定几档（M 系列基础 8 核、Pro 10~12 核、
	// Max 14~16 核），不存在 4 核或 20 核的机型。
	plausible := map[model.Platform][]int{
		model.PlatformWindows: {4, 6, 8, 12, 16, 20, 24},
		model.PlatformMacOS:   {8, 10, 12, 14, 16},
		model.PlatformLinux:   {4, 8, 12, 16},
	}

	// 各平台在档案库中已用的 platformVersion。
	versions := map[model.Platform]map[string]bool{}
	// 已覆盖的 (platform, version, cores) 组合。
	covered := map[string]bool{}
	for _, d := range deviceCatalog {
		if versions[d.Platform] == nil {
			versions[d.Platform] = map[string]bool{}
		}
		versions[d.Platform][d.PlatformVersion] = true
		covered[key(d.Platform, d.PlatformVersion, d.HardwareConcurrency)] = true
	}

	t.Logf("当前档案 %d 条，可区分组合 %d 种\n", len(deviceCatalog), len(covered))

	var totalGap int
	for _, p := range []model.Platform{
		model.PlatformWindows, model.PlatformMacOS, model.PlatformLinux,
	} {
		vers := sortedKeys(versions[p])
		if len(vers) == 0 {
			continue
		}
		cores := plausible[p]
		t.Logf("%s（已用版本 %v，合理核数档位 %v）:", p, vers, cores)

		var have, gap []string
		for _, v := range vers {
			for _, c := range cores {
				label := fmt.Sprintf("%s/%d核", v, c)
				if covered[key(p, v, c)] {
					have = append(have, label)
				} else {
					gap = append(gap, label)
				}
			}
		}
		t.Logf("  已覆盖 %d 种: %v", len(have), have)
		t.Logf("  未覆盖 %d 种: %v", len(gap), gap)
		totalGap += len(gap)
	}

	t.Logf("\n合计可补 %d 种组合（当前 %d 种 → 上限约 %d 种）",
		totalGap, len(covered), len(covered)+totalGap)
	t.Log("\n但上限不是目标:")
	t.Log("  · 每条档案都要对应真实常见配置，否则罕见组合会缩小匿名集")
	t.Log("  · 加条目会让约 90% 的已有 profile 漂移（见 TestQuantifyCatalogGrowthImpact），")
	t.Log("    需要重新采集 probe 基线，且已在使用的账号会看到设备变更")
	t.Log("  · 机型多样性不是关键指标——canvas 已经 100% 唯一，")
	t.Log("    profile 之间本就可区分；档案只决定\"声称的硬件环境\"有几种")
}

func key(p model.Platform, version string, cores int) string {
	return fmt.Sprintf("%s|%s|%d", p, version, cores)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

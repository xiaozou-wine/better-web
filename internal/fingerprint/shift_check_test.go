package fingerprint

import (
	"testing"

	"better-web/internal/model"
)

// 量化两种排除方案对已有 profile 的影响面，作为设计决策的依据。
//
// 方案 A（在筛后的短表上取模）：候选集大小从 10 变 9，索引整体错位。
// 方案 B（保持基数，仅替换命中的缺陷档案）：只有原本抽到该档案的种子受影响。
//
// 结论：A 会让绝大多数 profile 漂移，为修一个缺陷档案付出的代价远超收益，
// 因此实现采用 B。本测试记录该差距，防止将来有人"顺手简化"回 A。
func TestCatalogExclusionStrategyImpact(t *testing.T) {
	const n = 2000
	var shortTableChanged, replaceChanged int
	for i := 0; i < n; i++ {
		seed := int32(1 + i*7919)
		d := derived{seed: seed}
		before := deviceCatalog[d.pick("device", len(deviceCatalog))]

		// 方案 A：直接在安全档案表上取模。
		if before.Label != safeCatalog[d.pick("device", len(safeCatalog))].Label {
			shortTableChanged++
		}
		// 方案 B：实际实现。
		if before.Label != pickDevice(d).Label {
			replaceChanged++
		}
	}

	shortPct := float64(shortTableChanged) / float64(n) * 100
	replacePct := float64(replaceChanged) / float64(n) * 100
	t.Logf("档案数: 全部 %d，参与随机抽取 %d", len(deviceCatalog), len(safeCatalog))
	t.Logf("方案 A（短表取模）: %d/%d 个种子换机型（%.1f%%）", shortTableChanged, n, shortPct)
	t.Logf("方案 B（仅替换缺陷档案，已采用）: %d/%d 个种子换机型（%.1f%%）",
		replaceChanged, n, replacePct)

	// 方案 B 的影响面应约等于缺陷档案在库中的占比（1/10），
	// 显著小于方案 A。这个差距是选择 B 的全部理由，必须守住。
	if replacePct >= shortPct {
		t.Errorf("当前实现的影响面（%.1f%%）未小于短表取模方案（%.1f%%），"+
			"说明 pickDevice 的替换策略失效", replacePct, shortPct)
	}
	if replacePct > 25 {
		t.Errorf("影响面 %.1f%% 过大，预期约等于缺陷档案占比 %.1f%%",
			replacePct, 100/float64(len(deviceCatalog)))
	}
}

// 随机抽取绝不能产出有已知缺陷的档案。
func TestDeriveNeverPicksFlaggedProfile(t *testing.T) {
	for i := 0; i < 5000; i++ {
		seed := int32(1 + i*7919)
		fp := Derive(seed, nil)
		if !fp.Device.Safe() {
			t.Fatalf("种子 %d 抽到了有已知缺陷的档案 %q: %s",
				seed, fp.Device.Label, fp.Device.KnownIssue)
		}
	}
}

// 同一种子必须始终抽到同一机型，包括走了替换分支的那些种子。
// 替换若不确定，这部分 profile 每次启动机型都会变。
func TestPickDeviceIsDeterministic(t *testing.T) {
	for i := 0; i < 500; i++ {
		seed := int32(1 + i*7919)
		d := derived{seed: seed}
		first := pickDevice(d).Label
		for j := 0; j < 3; j++ {
			if got := pickDevice(d).Label; got != first {
				t.Fatalf("种子 %d 多次抽取结果不一致: %q vs %q", seed, first, got)
			}
		}
	}
}

// 用户显式指定有缺陷的档案时必须照办：告知风险后不应剥夺其选择权。
func TestDeriveWithDeviceHonorsFlaggedProfile(t *testing.T) {
	var flagged model.DeviceProfile
	for _, d := range deviceCatalog {
		if !d.Safe() {
			flagged = d
			break
		}
	}
	if flagged.Label == "" {
		t.Skip("档案库中没有标记缺陷的档案")
	}
	fp := DeriveWithDevice(12345, nil, flagged)
	if fp.Device.Label != flagged.Label {
		t.Errorf("显式指定的档案被替换: 期望 %q, 实际 %q", flagged.Label, fp.Device.Label)
	}
}

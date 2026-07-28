package fingerprint

import (
	"context"
	"errors"
	"strings"
	"testing"

	"better-web/internal/model"
)

// fakeProber 按预设序列返回厂商族，用于在不启动内核的情况下测筛选逻辑。
type fakeProber struct {
	// families 是每次调用依次返回的族。用完后重复最后一个。
	families []model.GPUFamily
	// failAt 中的下标（从 0 计）返回错误，模拟内核启动失败。
	failAt map[int]bool
	calls  int
	// seeds 记录被试过的种子，用于验证每次都用了新种子。
	seeds []int32
}

func (f *fakeProber) SeedGPUFamily(_ context.Context, seed int32) (model.GPUFamily, string, error) {
	i := f.calls
	f.calls++
	f.seeds = append(f.seeds, seed)
	if f.failAt[i] {
		return model.GPUFamilyUnknown, "", errors.New("模拟内核启动失败")
	}
	fam := f.families[len(f.families)-1]
	if i < len(f.families) {
		fam = f.families[i]
	}
	return fam, "ANGLE (" + string(fam) + ", fake renderer)", nil
}

// 找到同族种子后应立即返回，不再多试。
// 每次尝试都是一次内核冷启动，多试一次就是白等两秒。
func TestFindSeedStopsAtFirstMatch(t *testing.T) {
	p := &fakeProber{families: []model.GPUFamily{
		model.GPUFamilyIntel, model.GPUFamilyAMD, model.GPUFamilyNVIDIA,
		model.GPUFamilyIntel,
	}}
	got, err := FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, 10)
	if err != nil {
		t.Fatalf("应当找到，实际报错: %v", err)
	}
	if !got.Found {
		t.Fatal("Found 应为 true")
	}
	if p.calls != 3 {
		t.Errorf("命中后应立即停止，实际调用 %d 次", p.calls)
	}
	if len(got.Tried) != 3 {
		t.Errorf("Tried 应记录 3 个候选，实际 %d", len(got.Tried))
	}
	if got.Seed == 0 {
		t.Error("选中的种子不应为 0")
	}
	if !strings.Contains(got.Renderer, "NVIDIA") {
		t.Errorf("Renderer 应为命中族的字符串，实际 %q", got.Renderer)
	}
}

// 每次尝试必须用新生成的种子，不能重复试同一个。
func TestFindSeedUsesFreshSeedEachAttempt(t *testing.T) {
	p := &fakeProber{families: []model.GPUFamily{model.GPUFamilyIntel}}
	_, _ = FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, 5)
	if len(p.seeds) != 5 {
		t.Fatalf("应试 5 次，实际 %d", len(p.seeds))
	}
	seen := map[int32]bool{}
	for _, s := range p.seeds {
		if seen[s] {
			t.Errorf("种子 %d 被重复尝试", s)
		}
		seen[s] = true
	}
}

// 试满上限没找到时，应返回 ErrNoMatchingSeed 并带上实际分布。
// 分布信息让用户能判断是运气不好还是这台机器根本抽不到该族。
func TestFindSeedReportsDistributionWhenExhausted(t *testing.T) {
	p := &fakeProber{families: []model.GPUFamily{
		model.GPUFamilyIntel, model.GPUFamilyIntel, model.GPUFamilyAMD,
	}}
	got, err := FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, 3)
	if got.Found {
		t.Error("不应报告找到")
	}
	var noMatch *ErrNoMatchingSeed
	if !errors.As(err, &noMatch) {
		t.Fatalf("应返回 ErrNoMatchingSeed，实际 %T: %v", err, err)
	}
	if noMatch.Tried != 3 {
		t.Errorf("Tried 应为 3，实际 %d", noMatch.Tried)
	}
	if noMatch.Sampled[model.GPUFamilyIntel] != 2 {
		t.Errorf("分布统计有误: %v", noMatch.Sampled)
	}
	if !strings.Contains(err.Error(), "NVIDIA") {
		t.Errorf("错误信息应含目标族，实际 %q", err.Error())
	}
}

// 单个候选探测失败应跳过并继续，不中断整轮。
// 偶发的内核启动失败不该让已经花掉的时间白费。
func TestFindSeedSkipsFailedProbes(t *testing.T) {
	p := &fakeProber{
		families: []model.GPUFamily{
			model.GPUFamilyIntel, model.GPUFamilyIntel, model.GPUFamilyNVIDIA,
		},
		failAt: map[int]bool{0: true, 1: true},
	}
	got, err := FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, 5)
	if err != nil {
		t.Fatalf("应跳过失败候选并继续，实际报错: %v", err)
	}
	if !got.Found {
		t.Fatal("第三次应命中")
	}
	if len(got.Tried) != 3 {
		t.Fatalf("Tried 应含 3 条（含 2 条失败），实际 %d", len(got.Tried))
	}
	if got.Tried[0].Err == "" || got.Tried[1].Err == "" {
		t.Error("失败候选应记录 Err，否则排障时看不出发生过失败")
	}
}

// 全部候选都探测失败时必须报错，不能当成"没找到"。
// 前者是环境问题，后者是运气问题，处置方式不同。
func TestFindSeedFailsWhenAllProbesFail(t *testing.T) {
	p := &fakeProber{
		families: []model.GPUFamily{model.GPUFamilyIntel},
		failAt:   map[int]bool{0: true, 1: true, 2: true},
	}
	_, err := FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, 3)
	if err == nil {
		t.Fatal("全部探测失败时应报错")
	}
	var noMatch *ErrNoMatchingSeed
	if errors.As(err, &noMatch) {
		t.Error("不应报告为「没找到」，那会掩盖探测链路的问题")
	}
	if !strings.Contains(err.Error(), "探测链路") {
		t.Errorf("错误信息应指向探测链路，实际 %q", err.Error())
	}
}

// 目标族为 unknown 时应直接拒绝，不白跑满上限。
func TestFindSeedRejectsUnknownTarget(t *testing.T) {
	p := &fakeProber{families: []model.GPUFamily{model.GPUFamilyIntel}}
	for _, want := range []model.GPUFamily{model.GPUFamilyUnknown, ""} {
		if _, err := FindSeedForGPUFamily(
			context.Background(), p, want, 5); err == nil {
			t.Errorf("目标族 %q 应被拒绝", want)
		}
	}
	if p.calls != 0 {
		t.Errorf("无效目标不应发起探测，实际调用 %d 次", p.calls)
	}
}

// 未提供探测器时应报错而非 panic。
func TestFindSeedRequiresProber(t *testing.T) {
	if _, err := FindSeedForGPUFamily(
		context.Background(), nil, model.GPUFamilyNVIDIA, 3); err == nil {
		t.Error("prober 为 nil 时应报错")
	}
}

// ctx 取消后应立即停止，不再启动新的内核。
func TestFindSeedHonorsContextCancel(t *testing.T) {
	p := &fakeProber{families: []model.GPUFamily{model.GPUFamilyIntel}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindSeedForGPUFamily(ctx, p, model.GPUFamilyNVIDIA, 20); err == nil {
		t.Error("ctx 已取消应报错")
	}
	if p.calls != 0 {
		t.Errorf("ctx 已取消不应发起探测，实际调用 %d 次", p.calls)
	}
}

// attempts 传 0 或负数应退化为默认上限。
func TestFindSeedDefaultsAttempts(t *testing.T) {
	for _, n := range []int{0, -1} {
		p := &fakeProber{families: []model.GPUFamily{model.GPUFamilyIntel}}
		_, _ = FindSeedForGPUFamily(context.Background(), p, model.GPUFamilyNVIDIA, n)
		if p.calls != DefaultSeedAttempts {
			t.Errorf("attempts=%d 应用默认 %d，实际调用 %d 次",
				n, DefaultSeedAttempts, p.calls)
		}
	}
}

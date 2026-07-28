package fingerprint

import (
	"context"
	"errors"
	"fmt"

	"better-web/internal/model"
)

// GPUProber 探测某个种子会派生出哪个 GPU 厂商族。
//
// 用接口而非直接依赖 probe 包：probe 依赖 fingerprint（跑分要用 Derive），
// 反向依赖会成环。同时也让筛选逻辑可以在不启动真实内核的情况下测试。
type GPUProber interface {
	// SeedGPUFamily 报告 seed 派生出的厂商族与完整 renderer 字符串。
	SeedGPUFamily(ctx context.Context, seed int32) (model.GPUFamily, string, error)
}

// SeedCandidate 是一次筛选中试过的种子及其结果。
type SeedCandidate struct {
	Seed     int32           `json:"seed"`
	Family   model.GPUFamily `json:"family"`
	Renderer string          `json:"renderer"`
	// Err 非空表示该候选探测失败，已跳过。
	Err string `json:"err,omitempty"`
}

// SeedMatch 是筛选结果。
type SeedMatch struct {
	// Seed 是选中的种子。Found 为 false 时无意义。
	Seed  int32 `json:"seed"`
	Found bool  `json:"found"`
	// Want 是目标厂商族。
	Want model.GPUFamily `json:"want"`
	// Renderer 是选中种子派生出的完整 renderer 字符串。
	Renderer string `json:"renderer,omitempty"`
	// Tried 是全部试过的候选，按顺序记录，供界面展示与排障。
	Tried []SeedCandidate `json:"tried"`
}

// ErrNoMatchingSeed 表示在给定尝试上限内没找到同族种子。
//
// 单独定义错误类型而非返回 nil：调用方需要区分"确实没找到"和"探测出错"，
// 前者可以让用户加大上限或改用关闭 GPU 伪造的方案，后者是环境问题。
type ErrNoMatchingSeed struct {
	Want    model.GPUFamily
	Tried   int
	Sampled map[model.GPUFamily]int
}

func (e *ErrNoMatchingSeed) Error() string {
	return fmt.Sprintf("试了 %d 个种子都没找到 %s 族（实际分布 %v）",
		e.Tried, e.Want, e.Sampled)
}

// DefaultSeedAttempts 是筛选种子的默认尝试上限。
//
// 取 24 的依据：实测宿主机为 NVIDIA 时，14 个种子里有 2 个派生 NVIDIA
// （约 1/7）。按该比例，24 次尝试找不到的概率约 (6/7)^24 ≈ 2.4%。
// 上限不能太大——每个候选都要冷启动一次内核，实测单次约 2 秒，
// 24 次即约 50 秒，已是交互式操作能接受的上界。
const DefaultSeedAttempts = 24

// FindSeedForGPUFamily 反复生成随机种子，直到找到派生出 want 族的那个。
//
// 为什么必须实际启动内核试：GPU 由内核从种子派生，算法在 C++ 代码里，
// Go 侧算不出来，也没有命令行参数可以指定。这是不得不付的代价。
//
// attempts 为 0 时用 DefaultSeedAttempts。种子用 NewSeed 生成，
// 因此结果仍是密码学随机的——筛选只是拒绝不合族的候选，不降低随机性。
//
// 探测单个候选失败时跳过并继续（记录在 Tried 里），不中断整轮：
// 偶发的内核启动失败不该让整次筛选白费。但全部候选都失败时返回错误，
// 那说明探测链路本身有问题。
func FindSeedForGPUFamily(ctx context.Context, prober GPUProber,
	want model.GPUFamily, attempts int) (SeedMatch, error) {
	if prober == nil {
		return SeedMatch{}, errors.New("未提供 GPU 探测器")
	}
	if !want.Known() {
		// unknown 做目标会让每个候选都判为不匹配，白跑满上限。
		return SeedMatch{}, fmt.Errorf("目标 GPU 厂商族 %q 无效", want)
	}
	if attempts <= 0 {
		attempts = DefaultSeedAttempts
	}

	out := SeedMatch{Want: want, Tried: make([]SeedCandidate, 0, attempts)}
	sampled := map[model.GPUFamily]int{}
	probeErrs := 0

	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("筛选种子被中断: %w", err)
		}
		seed, err := NewSeed()
		if err != nil {
			return out, err
		}
		fam, renderer, err := prober.SeedGPUFamily(ctx, seed)
		if err != nil {
			probeErrs++
			out.Tried = append(out.Tried, SeedCandidate{
				Seed: seed, Err: err.Error(),
			})
			continue
		}
		out.Tried = append(out.Tried, SeedCandidate{
			Seed: seed, Family: fam, Renderer: renderer,
		})
		sampled[fam]++
		if fam == want {
			out.Seed = seed
			out.Found = true
			out.Renderer = renderer
			return out, nil
		}
	}

	if probeErrs == len(out.Tried) && probeErrs > 0 {
		return out, fmt.Errorf("全部 %d 次探测均失败，探测链路本身有问题", probeErrs)
	}
	return out, &ErrNoMatchingSeed{
		Want: want, Tried: len(out.Tried), Sampled: sampled,
	}
}

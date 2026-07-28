package probe

import (
	"context"
	"testing"

	"better-web/internal/model"
)

// 量化各指纹维度在多个种子间的碰撞率。
//
// 默认跳过：需要真实内核且要启动 N 次浏览器，耗时较长。
// 用 BW_COLLISION_SEEDS 指定种子数量来启用，例如：
//
//	BW_COLLISION_SEEDS=12 go test -run TestSeedCollisionRate -v ./internal/probe/
//
// 存在意义：如果某个维度在不同种子间大量重复，该维度就无法用于区分
// profile，平台侧可以据此把多个账号关联起来。这是选型和调参的依据，
// 不是通过/失败的断言，因此只报告数据。
func TestSeedCollisionRate(t *testing.T) {
	n := envInt(t, "BW_COLLISION_SEEDS")
	if n <= 0 {
		t.Skip("未设置 BW_COLLISION_SEEDS，跳过碰撞率统计")
	}
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/New_York", Locale: "en-US"}

	canvas := map[string][]int32{}
	audio := map[string][]int32{}
	ua := map[string][]int32{}

	for i := 0; i < n; i++ {
		seed := int32(1000 + i*7919) // 用质数步长避免种子本身有规律
		_, args := probeProfile(t, seed, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
		if err != nil {
			t.Fatalf("种子 %d 采集失败: %v", seed, err)
		}
		canvas[res.CanvasHash] = append(canvas[res.CanvasHash], seed)
		audio[res.AudioHash] = append(audio[res.AudioHash], seed)
		ua[res.UserAgent] = append(ua[res.UserAgent], seed)
		t.Logf("种子 %-10d canvas=%s audio=%s cores=%-3d mem=%-4v",
			seed, res.CanvasHash, res.AudioHash, res.HardwareConcurrency, res.DeviceMemory)
	}

	report := func(name string, m map[string][]int32) {
		t.Logf("%s: %d 个种子产出 %d 个不同取值（唯一率 %.0f%%）",
			name, n, len(m), float64(len(m))/float64(n)*100)
		for v, seeds := range m {
			if len(seeds) > 1 {
				t.Logf("  碰撞 %q ← 种子 %v", v, seeds)
			}
		}
	}
	report("Canvas", canvas)
	report("Audio", audio)
	report("UserAgent", ua)
}

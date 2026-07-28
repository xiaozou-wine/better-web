package app

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/model"
)

// 日常模式开 MatchHostGPU 必须报错，不能静默忽略。
//
// 日常模式不做任何伪造，没有种子可筛。静默忽略会让用户以为筛选生效了，
// 而实际上该 profile 报的是宿主机真实环境——他会基于错误的认知去用它。
func TestCreateProfileRejectsMatchHostGPUForDailyKind(t *testing.T) {
	s, _ := newTestService(t)
	_, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常", Kind: model.KindDaily, MatchHostGPU: true,
	})
	if err == nil {
		t.Fatal("日常模式开 MatchHostGPU 应报错")
	}
	if !strings.Contains(err.Error(), "日常模式") {
		t.Errorf("错误信息应说明原因，实际 %q", err.Error())
	}
}

// 未装内核时开 MatchHostGPU 必须报错而非退回随机种子。
//
// 这是本功能最关键的一条约束：用户开这个选项就是为了过 Cloudflare，
// 静默给一个未经筛选的种子会让他以为已经能过，直到账号出问题才发现。
// 失败必须显式。
func TestCreateProfileFailsLoudlyWhenSeedMatchUnavailable(t *testing.T) {
	s, _ := newTestService(t)
	// newTestService 不装内核，因此筛选必然失败。
	_, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "指纹", Kind: model.KindFingerprint, MatchHostGPU: true,
	})
	if err == nil {
		t.Fatal("筛选不可用时应报错，不能静默退回随机种子")
	}
	if !strings.Contains(err.Error(), "筛选种子") {
		t.Errorf("错误信息应指向筛选环节，实际 %q", err.Error())
	}

	// 失败后不得留下半成品 profile：用户看到报错却发现列表里多了一个
	// 未筛选的 profile，会更困惑。
	list, err := s.ListProfiles()
	if err != nil {
		t.Fatalf("列出 profile 失败: %v", err)
	}
	for _, p := range list {
		if p.Name == "指纹" {
			t.Error("筛选失败后不应留下 profile")
		}
	}
}

// 不开 MatchHostGPU 时行为不变：正常生成随机种子，不触发任何探测。
// 这条锁住新功能不影响既有路径——探测要冷启动内核，误触发会让新建变慢几十秒。
func TestCreateProfileUnaffectedWhenMatchHostGPUOff(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "普通", Kind: model.KindFingerprint,
	})
	if err != nil {
		t.Fatalf("未开筛选时应正常创建: %v", err)
	}
	if v.Seed == 0 {
		t.Error("应生成非零种子")
	}
	if v.Notes != "" {
		t.Errorf("未开筛选不应写入筛选说明，实际 %q", v.Notes)
	}
}

// 批量导入不得因为新增 ctx 参数而改变行为。
func TestImportProxiesStillWorksWithContext(t *testing.T) {
	s, _ := newTestService(t)
	res, err := s.ImportProxies(context.Background(), ImportRequest{
		Text: "1.2.3.4:1080\n5.6.7.8:1081",
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(res.Created) != 2 {
		t.Fatalf("应创建 2 个 profile，实际 %d", len(res.Created))
	}
	// 每个 profile 必须有独立种子——共用种子等于同一台设备，
	// canvas 哈希一致会让这批账号被一眼关联。
	if res.Created[0].Seed == res.Created[1].Seed {
		t.Error("导入的 profile 不应共用种子")
	}
}

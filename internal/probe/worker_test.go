package probe

import (
	"context"
	"testing"

	"better-web/internal/model"
)

// 主线程与 Worker 的指纹必须一致，且关键 API 不能有被改写的痕迹。
//
// 这是 CreepJS 一类检测器的主力手段：真实浏览器的这些值在两个作用域里
// 必然相同，只作用于主线程的伪造会在此暴露。原型改写痕迹同理——
// 源码层伪造不该留痕，留痕说明用了 JS 注入。
func TestRealKernelScopeConsistency(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"}

	for platform, seeds := range seedsByPlatform(t) {
		seed := seeds[0]
		_, args := probeProfile(t, seed, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).CollectScopes(context.Background(), args)
		if err != nil {
			t.Fatalf("种子 %d 采集失败: %v", seed, err)
		}
		if !res.WorkerAvailable {
			t.Errorf("种子 %d: Worker 不可用，无法验证跨作用域一致性", seed)
			continue
		}

		t.Logf("种子 %d（声称 %s）主线程: UA=%q tz=%q cores=%d mem=%v",
			seed, platform, res.Main.UserAgent, res.Main.Timezone,
			res.Main.HardwareConcurrency, res.Main.DeviceMemory)
		t.Logf("  Worker: UA=%q tz=%q cores=%d mem=%v",
			res.Worker.UserAgent, res.Worker.Timezone,
			res.Worker.HardwareConcurrency, res.Worker.DeviceMemory)

		issues := CheckScopeConsistency(res)
		for _, i := range issues {
			t.Logf("  %s", i)
		}
		if len(issues) > 0 {
			t.Errorf("种子 %d（声称 %s）存在 %d 项跨作用域矛盾或原型改写痕迹",
				seed, platform, len(issues))
		}
	}
}

// 原型链不应留下被改写的痕迹。
//
// 单独成测试是因为它与种子无关：一旦内核改用 JS 注入实现伪造，
// 所有 profile 都会同时暴露。
func TestRealKernelHasNoPrototypeLies(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	_, args := probeProfile(t, 20260727, geo)

	res, err := (&Probe{ExecPath: k.ExecPath}).CollectScopes(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}
	if len(res.PrototypeLies) > 0 {
		t.Errorf("检出 %d 项原型改写痕迹（源码层伪造不应留痕）: %v",
			len(res.PrototypeLies), res.PrototypeLies)
	}
}

// CheckScopeConsistency 必须能抓出不一致，否则它只是个空壳。
func TestCheckScopeConsistencyDetectsMismatch(t *testing.T) {
	base := ScopeValues{
		UserAgent: "UA/1", Platform: "Win32", HardwareConcurrency: 8,
		DeviceMemory: 16, Language: "en-US",
		Timezone: "America/Los_Angeles", TimezoneOffset: 420,
		UADataPlatform: "Windows",
	}

	t.Run("一致时无问题", func(t *testing.T) {
		w := base
		r := ScopeResult{Main: base, Worker: &w, WorkerAvailable: true}
		if got := CheckScopeConsistency(r); len(got) != 0 {
			t.Errorf("期望无问题，实际 %v", got)
		}
	})

	t.Run("Worker 不可用时不判为不一致", func(t *testing.T) {
		r := ScopeResult{Main: base, WorkerAvailable: false}
		if got := CheckScopeConsistency(r); len(got) != 0 {
			t.Errorf("Worker 不可用应返回空，实际 %v", got)
		}
	})

	cases := []struct {
		name   string
		mutate func(*ScopeValues)
	}{
		{"UA 不一致", func(v *ScopeValues) { v.UserAgent = "UA/2" }},
		{"platform 不一致", func(v *ScopeValues) { v.Platform = "MacIntel" }},
		{"核数不一致", func(v *ScopeValues) { v.HardwareConcurrency = 4 }},
		{"内存不一致", func(v *ScopeValues) { v.DeviceMemory = 8 }},
		{"时区不一致", func(v *ScopeValues) { v.Timezone = "Asia/Tokyo" }},
		{"时区偏移不一致", func(v *ScopeValues) { v.TimezoneOffset = -540 }},
		{"uaData 不一致", func(v *ScopeValues) { v.UADataPlatform = "macOS" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := base
			c.mutate(&w)
			r := ScopeResult{Main: base, Worker: &w, WorkerAvailable: true}
			got := CheckScopeConsistency(r)
			if len(got) == 0 {
				t.Error("期望检出不一致，实际未检出")
			}
			if len(got) > 0 && !got[0].Severe {
				t.Error("跨作用域不一致应判为可直接检出")
			}
		})
	}

	t.Run("原型改写痕迹被计入", func(t *testing.T) {
		w := base
		r := ScopeResult{
			Main: base, Worker: &w, WorkerAvailable: true,
			PrototypeLies: []string{"navigator.platform: toString 非原生"},
		}
		got := CheckScopeConsistency(r)
		if len(got) != 1 || got[0].Check != "原型被改写" {
			t.Errorf("期望检出原型改写，实际 %v", got)
		}
	})
}

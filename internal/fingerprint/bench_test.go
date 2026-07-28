package fingerprint

import (
	"testing"

	"better-web/internal/model"
)

// 指纹推导在每次启动和每次列表刷新时都会执行。
//
// 列表刷新是 2 秒一次的轮询，每次给全部 profile 各推导一遍，所以这里的
// 单次成本会被 profile 数量放大。测它是为了确认不需要缓存——如果单次是
// 微秒级，100 个 profile 也只有毫秒级，加缓存反而引入失效问题。
func BenchmarkDerive(b *testing.B) {
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Derive(int32(i%100000+1), geo)
	}
}

// 走替换分支的种子（抽中有已知缺陷的档案）会多做一次 pick，
// 这里确认额外成本可忽略。
func BenchmarkDeriveWithFallback(b *testing.B) {
	// 先找一个会命中缺陷档案的种子。
	var target int32
	for i := int32(1); i < 100000; i++ {
		d := derived{seed: i}
		if !deviceCatalog[d.pick("device", len(deviceCatalog))].Safe() {
			target = i
			break
		}
	}
	if target == 0 {
		b.Skip("未找到会命中缺陷档案的种子")
	}
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Derive(target, geo)
	}
}

func BenchmarkNewSeed(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewSeed(); err != nil {
			b.Fatalf("NewSeed 失败: %v", err)
		}
	}
}

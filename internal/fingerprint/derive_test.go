package fingerprint

import (
	"testing"

	"better-web/internal/model"
)

// 同一种子必须永远推导出完全一致的指纹，否则 profile 每次启动都会指纹漂移。
func TestDeriveIsDeterministic(t *testing.T) {
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	for _, seed := range []int32{1, 42, 1000, 2147483647} {
		first := Derive(seed, geo)
		for i := 0; i < 20; i++ {
			if got := Derive(seed, geo); got != first {
				t.Fatalf("种子 %d 第 %d 次推导结果不一致:\n第一次 %+v\n本次   %+v", seed, i, first, got)
			}
		}
	}
}

// 不同种子应当落到不同机型上，否则档案库形同虚设。
//
// 基准是 safeCatalog 而非 deviceCatalog：有已知缺陷的档案被刻意排除在
// 随机抽取之外，覆盖不到它们是正确行为，见 pickDevice。
func TestDeriveSpreadsAcrossCatalog(t *testing.T) {
	seen := map[string]bool{}
	for seed := int32(1); seed <= 500; seed++ {
		seen[Derive(seed, nil).Device.Label] = true
	}
	if len(seen) < len(safeCatalog) {
		t.Errorf("500 个种子只覆盖 %d/%d 个可用机型，分布不均", len(seen), len(safeCatalog))
	}
}

// 机型档案库中每条都必须自洽：声明的 OS 要与 GPU 厂商、分辨率缩放比匹配。
func TestCatalogEntriesAreCoherent(t *testing.T) {
	if len(deviceCatalog) == 0 {
		t.Fatal("机型档案库为空")
	}
	labels := map[string]bool{}
	for _, d := range deviceCatalog {
		if d.Label == "" {
			t.Error("存在缺少 Label 的机型")
			continue
		}
		if labels[d.Label] {
			t.Errorf("机型 Label 重复: %s", d.Label)
		}
		labels[d.Label] = true

		if !isKnownPlatform(d.Platform) {
			t.Errorf("%s: platform %q 未知", d.Label, d.Platform)
		}
		if d.PlatformVersion == "" {
			t.Errorf("%s: 缺少 platformVersion", d.Label)
		}
		if d.HardwareConcurrency <= 0 {
			t.Errorf("%s: hardwareConcurrency 必须为正数", d.Label)
		}
		if NormalizeDeviceMemory(d.DeviceMemory) != d.DeviceMemory {
			t.Errorf("%s: deviceMemory %v 不在规范允许的档位内", d.Label, d.DeviceMemory)
		}
		if d.ScreenWidth <= 0 || d.ScreenHeight <= 0 {
			t.Errorf("%s: 屏幕尺寸无效 %dx%d", d.Label, d.ScreenWidth, d.ScreenHeight)
		}
		if d.DevicePixelRatio < 1 {
			t.Errorf("%s: devicePixelRatio %v 小于 1", d.Label, d.DevicePixelRatio)
		}
		// Apple 的 GPU 只可能出现在 macOS 上，反之 macOS 也只该用 Apple GPU。
		// 这类交叉矛盾是最容易被检测到的破绽。
		isApple := d.GPUVendor == vendorApple
		if isApple != (d.Platform == model.PlatformMacOS) {
			t.Errorf("%s: platform %q 与 GPU 厂商 %q 矛盾", d.Label, d.Platform, d.GPUVendor)
		}
		if d.GPURenderer == "" {
			t.Errorf("%s: 缺少 GPU renderer", d.Label)
		}
	}
}

// 地理信息必须原样传导到指纹，这是 IP/时区/语言对齐的基础。
func TestDeriveHonorsGeo(t *testing.T) {
	geo := &model.Geo{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"}
	fp := Derive(777, geo)
	if fp.Timezone != "Asia/Tokyo" {
		t.Errorf("时区 = %q, 期望 Asia/Tokyo", fp.Timezone)
	}
	if fp.Locale != "ja-JP" {
		t.Errorf("语言 = %q, 期望 ja-JP", fp.Locale)
	}
	if fp.AcceptLanguages != "ja-JP,ja;q=0.9" {
		t.Errorf("AcceptLanguages = %q, 期望 ja-JP,ja;q=0.9", fp.AcceptLanguages)
	}
}

func TestAcceptLanguage(t *testing.T) {
	cases := map[string]string{
		"en-US": "en-US,en;q=0.9",
		"zh-CN": "zh-CN,zh;q=0.9",
		"de":    "de",
	}
	for in, want := range cases {
		if got := AcceptLanguage(in); got != want {
			t.Errorf("AcceptLanguage(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestNewSeedIsPositiveAndNonZero(t *testing.T) {
	for i := 0; i < 200; i++ {
		s, err := NewSeed()
		if err != nil {
			t.Fatalf("NewSeed 失败: %v", err)
		}
		if s <= 0 {
			t.Fatalf("NewSeed 返回非正值 %d", s)
		}
	}
}

// Chrome 的算法是向下取整到最近的 2 的幂再夹到 [0.25, 8]，
// 因此 16GiB 真机报 8，6GiB 真机报 4。
func TestNormalizeDeviceMemory(t *testing.T) {
	cases := map[float64]float64{
		16: 8, 12: 8, 8: 8, // 上界夹取
		6: 4, 4: 4,
		3: 2, 2: 2,
		1.4: 1, 1: 1,
		0.6: 0.5,
		0.3: 0.25, 0.1: 0.25, // 下界夹取
	}
	for in, want := range cases {
		if got := NormalizeDeviceMemory(in); got != want {
			t.Errorf("NormalizeDeviceMemory(%v) = %v, 期望 %v", in, got, want)
		}
	}
	// 归一化后的值必须落在允许档位内，且再次归一化是幂等的。
	for _, b := range deviceMemoryBuckets {
		if got := NormalizeDeviceMemory(b); got != b {
			t.Errorf("NormalizeDeviceMemory(%v) = %v, 归一化不幂等", b, got)
		}
	}
}

func isKnownPlatform(p model.Platform) bool {
	return p == model.PlatformWindows || p == model.PlatformMacOS || p == model.PlatformLinux
}

package fingerprint

import (
	"testing"

	"better-web/internal/model"
)

func TestBrandVersionFollowsKernel(t *testing.T) {
	cases := map[string]string{
		"148.0.7778.215": "148",
		"152.0.1234.56":  "152",
		"139.0.7258.154": "139",
		// 空值与畸形输入退回兜底值，不阻止启动。
		"":        fallbackBrandVersion,
		"unknown": fallbackBrandVersion,
		"   ":     fallbackBrandVersion,
	}
	for in, want := range cases {
		if got := brandVersion(in); got != want {
			t.Errorf("brandVersion(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// 品牌版本必须跟随内核实际版本：内核报 152 而 UA 声称 148
// 是可直接检出的矛盾。
func TestDeriveForKernelAlignsBrandVersion(t *testing.T) {
	g := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	fp := DeriveForKernel(12345, g, "152.0.1234.56")
	if fp.BrandVersion != "152" {
		t.Errorf("BrandVersion = %q, 期望 152", fp.BrandVersion)
	}
}

// 不传内核版本时用兜底值，保持 Derive 的原有行为。
func TestDeriveUsesFallbackBrandVersion(t *testing.T) {
	g := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	fp := Derive(12345, g)
	if fp.BrandVersion != fallbackBrandVersion {
		t.Errorf("BrandVersion = %q, 期望兜底值 %q", fp.BrandVersion, fallbackBrandVersion)
	}
}

// 内核版本不同不应改变种子派生的其他维度：
// 否则打个安全补丁就会让整个 profile 的指纹漂移。
func TestKernelVersionOnlyAffectsBrandVersion(t *testing.T) {
	g := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	a := DeriveForKernel(999, g, "148.0.7778.215")
	b := DeriveForKernel(999, g, "152.0.1234.56")

	if a.Device.Label != b.Device.Label {
		t.Errorf("换内核版本后机型变了: %q -> %q", a.Device.Label, b.Device.Label)
	}
	if a.Brand != b.Brand {
		t.Errorf("换内核版本后品牌变了: %q -> %q", a.Brand, b.Brand)
	}
	if a.Timezone != b.Timezone || a.Locale != b.Locale {
		t.Error("换内核版本后时区或语言变了")
	}
	if a.BrandVersion == b.BrandVersion {
		t.Error("品牌版本应随内核版本变化")
	}
}

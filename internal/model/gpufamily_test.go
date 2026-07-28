package model

import "testing"

// ParseGPUFamily 必须能识别实测采到的真实 renderer 字符串。
//
// 这个判据决定了按族筛种子是否有效：识别错一个厂商，筛出来的种子就仍然
// 跨厂商，Cloudflare 照样拦。用例全部取自 probe 包实测采集的原始字符串。
func TestParseGPUFamilyOnRealRenderers(t *testing.T) {
	cases := []struct {
		renderer string
		want     GPUFamily
	}{
		{
			"ANGLE (NVIDIA, NVIDIA GeForce RTX 4060 Laptop GPU (0x00002860) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyNVIDIA,
		},
		{
			"ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Laptop GPU (0x00002560) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyNVIDIA,
		},
		{
			"ANGLE (NVIDIA, NVIDIA GeForce RTX 4060 Laptop GPU (0x000028E0) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyNVIDIA,
		},
		{
			"ANGLE (Intel, Intel(R) Iris(R) Xe Graphics (0x00009A49) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyIntel,
		},
		{
			"ANGLE (Intel, Intel(R) UHD Graphics 620 (0x00003EA0) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyIntel,
		},
		{
			"ANGLE (Intel, Intel(R) Arc(TM) Graphics (0x00007D55) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyIntel,
		},
		{
			"ANGLE (AMD, AMD Radeon(TM) Graphics (0x00001638) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			GPUFamilyAMD,
		},
		{
			"ANGLE (Apple, ANGLE Metal Renderer: Apple M2, Unspecified Version)",
			GPUFamilyApple,
		},
		// 非 ANGLE 的原生写法。
		{"GeForce GTX 1650/PCIe/SSE2", GPUFamilyNVIDIA},
		{"AMD Radeon Pro 5500M OpenGL Engine", GPUFamilyAMD},
		{"", GPUFamilyUnknown},
		{"SwiftShader Device (LLVM 10.0.0)", GPUFamilyUnknown},
	}

	for _, c := range cases {
		if got := ParseGPUFamily(c.renderer); got != c.want {
			t.Errorf("ParseGPUFamily(%q) = %q，期望 %q", c.renderer, got, c.want)
		}
	}
}

// 识别必须大小写无关：不同平台与驱动的大小写写法不统一。
func TestParseGPUFamilyIsCaseInsensitive(t *testing.T) {
	if got := ParseGPUFamily("angle (nvidia, nvidia geforce rtx 4060"); got != GPUFamilyNVIDIA {
		t.Errorf("小写输入未识别，得到 %q", got)
	}
	if got := ParseGPUFamily("ANGLE (INTEL, INTEL(R) UHD GRAPHICS"); got != GPUFamilyIntel {
		t.Errorf("大写输入未识别，得到 %q", got)
	}
}

// unknown 不得被当成可用于筛选的族。
// 拿它做判据会把每个种子都判成"不同族"，筛选永远筛不出结果。
func TestGPUFamilyKnown(t *testing.T) {
	for _, f := range []GPUFamily{
		GPUFamilyNVIDIA, GPUFamilyIntel, GPUFamilyAMD, GPUFamilyApple,
	} {
		if !f.Known() {
			t.Errorf("%q 应为已知族", f)
		}
	}
	for _, f := range []GPUFamily{GPUFamilyUnknown, ""} {
		if f.Known() {
			t.Errorf("%q 不应为已知族", f)
		}
	}
}

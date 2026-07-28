package probe

import "testing"

// gpuFamily 必须能从真实的 ANGLE renderer 字符串里认出厂商族。
//
// 这个判据是 Cloudflare 检测的核心：实测表明伪造 GPU 与宿主机 GPU 跨厂商
// 就会被拦，同族则通过（见 TestAntibotControl）。因此厂商族识别一旦出错，
// 按族筛选种子的做法就会失效。用例全部取自实测采到的真实字符串。
func TestGPUFamilyRecognition(t *testing.T) {
	cases := []struct {
		renderer string
		want     string
	}{
		{
			"ANGLE (NVIDIA, NVIDIA GeForce RTX 4060 Laptop GPU (0x00002860) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"NVIDIA",
		},
		{
			"ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Laptop GPU (0x00002560) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"NVIDIA",
		},
		{
			"ANGLE (Intel, Intel(R) Iris(R) Xe Graphics (0x00009A49) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"Intel",
		},
		{
			"ANGLE (Intel, Intel(R) UHD Graphics 620 (0x00003EA0) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"Intel",
		},
		{
			"ANGLE (Intel, Intel(R) Arc(TM) Graphics (0x00007D55) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"Intel",
		},
		{
			"ANGLE (AMD, AMD Radeon(TM) Graphics (0x00001638) Direct3D11 vs_5_0 ps_5_0, D3D11)",
			"AMD",
		},
		{
			"ANGLE (Apple, ANGLE Metal Renderer: Apple M2, Unspecified Version)",
			"Apple",
		},
		// 只出现 GeForce 而没有 NVIDIA 字样的写法也应识别为 NVIDIA。
		{"GeForce GTX 1650/PCIe/SSE2", "NVIDIA"},
		// 只出现 Radeon 的写法同理。
		{"AMD Radeon Pro 5500M OpenGL Engine", "AMD"},
		{"", "空"},
		{"SwiftShader Device (LLVM 10.0.0)", "其他"},
	}

	for _, c := range cases {
		if got := gpuFamily(c.renderer); got != c.want {
			t.Errorf("gpuFamily(%q) = %q，期望 %q", c.renderer, got, c.want)
		}
	}
}

// 厂商族识别必须大小写无关：不同平台的 renderer 大小写写法不统一。
func TestGPUFamilyIsCaseInsensitive(t *testing.T) {
	if got := gpuFamily("angle (nvidia, nvidia geforce rtx 4060"); got != "NVIDIA" {
		t.Errorf("小写输入未识别，得到 %q", got)
	}
	if got := gpuFamily("ANGLE (INTEL, INTEL(R) UHD GRAPHICS"); got != "Intel" {
		t.Errorf("大写输入未识别，得到 %q", got)
	}
}

package model

import "strings"

// GPUFamily 是 GPU 的厂商族。
//
// 为什么需要这个概念：实测（见 probe.TestAntibotControl）表明 Cloudflare 的
// 判据是"伪造 GPU 与宿主机 GPU 跨厂商"，而不是"存在 GPU 伪造"。内核只改了
// WebGL 的 vendor/renderer 字符串，实际渲染结果、扩展列表、着色器精度、
// WebGPU 的 adapterInfo 全部仍是真实驱动的值——声称 Intel 集显却渲染出
// NVIDIA 的像素是单一信号即可判定的矛盾。
//
// 同厂商不同型号之间这些值本就接近，所以同族伪造能通过。厂商族因此成了
// 筛选种子的判据。
type GPUFamily string

const (
	GPUFamilyNVIDIA  GPUFamily = "NVIDIA"
	GPUFamilyIntel   GPUFamily = "Intel"
	GPUFamilyAMD     GPUFamily = "AMD"
	GPUFamilyApple   GPUFamily = "Apple"
	GPUFamilyUnknown GPUFamily = "unknown"
)

// ParseGPUFamily 从 WebGL 的 renderer 字符串识别厂商族。
//
// 输入形如 "ANGLE (NVIDIA, NVIDIA GeForce RTX 4060 Laptop GPU (0x00002860)
// Direct3D11 vs_5_0 ps_5_0, D3D11)"，也接受非 ANGLE 的原生格式。
//
// 判断顺序有讲究：Apple 的 renderer 形如
// "ANGLE (Apple, ANGLE Metal Renderer: Apple M2, ...)"，不含其他厂商名，
// 所以先判 Apple 还是先判 Intel 都不会错；但 NVIDIA 必须在检查 "geforce"
// 之外也检查 "nvidia"，因为两种写法都存在。
func ParseGPUFamily(renderer string) GPUFamily {
	s := strings.ToLower(renderer)
	switch {
	case strings.Contains(s, "nvidia"), strings.Contains(s, "geforce"):
		return GPUFamilyNVIDIA
	case strings.Contains(s, "apple"):
		return GPUFamilyApple
	case strings.Contains(s, "intel"):
		return GPUFamilyIntel
	case strings.Contains(s, "amd"), strings.Contains(s, "radeon"):
		return GPUFamilyAMD
	}
	return GPUFamilyUnknown
}

// Known 报告该厂商族是否被识别出来。
// unknown 不能用于筛选种子——拿它做判据会把所有种子都当成"不同族"。
func (f GPUFamily) Known() bool {
	return f != GPUFamilyUnknown && f != ""
}

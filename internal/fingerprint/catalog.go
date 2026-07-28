// Package fingerprint 负责把一个 int32 种子确定性地推导成一套完整且自洽的
// 浏览器环境。
//
// 设计前提：指纹的目标是"看起来像一台普通真机"，不是"看起来独一无二"。
// 逐字段独立随机会产出真实世界不存在的组合（例如声称 Windows 却报出 Apple
// 的 GPU），这本身就是最强的自动化信号。因此机型只能从下面这份自洽档案库中
// 整体抽取。
package fingerprint

import "better-web/internal/model"

// WebGL vendor 字符串在 Chromium/ANGLE 下的格式为 "Google Inc. (<厂商>)"，
// renderer 在 Windows 为 "ANGLE (<厂商>, <型号> Direct3D11 vs_5_0 ps_5_0, D3D11)"，
// 在 macOS 为 "ANGLE (Apple, ANGLE Metal Renderer: <芯片>, Unspecified Version)"。
const (
	vendorNVIDIA = "Google Inc. (NVIDIA)"
	vendorIntel  = "Google Inc. (Intel)"
	vendorAMD    = "Google Inc. (AMD)"
	vendorApple  = "Google Inc. (Apple)"
)

// deviceCatalog 是预置机型档案库。每条都取自真实机型的常见配置，字段之间
// 已保证自洽（OS 版本、GPU 厂商与型号、核数、内存档位、分辨率与缩放比）。
//
// 注意"自洽"指档案本身描述了一台合理存在的机器，不等于这些值都会生效：
// GPU、内存、屏幕三组字段在内核 148 上没有对应命令行参数，实际由种子派生。
// 详见 model.DeviceProfile 的字段说明。保留完整画像便于人工核对组合是否
// 合理，以及内核重新开放参数时直接接上。
//
// 新增机型时必须整条添加，不要只改其中一两个字段。
//
// 更重要的约束：**不要随意增删档案条目**。抽取是对档案库长度取模，长度一变
// 索引整体错位，实测追加一条会让约 90% 的已有 profile 抽到不同机型
// （见 TestQuantifyCatalogGrowthImpact）——那等于所有账号同时换了设备，
// 是本项目竭力避免的身份漂移。
//
// 因此调整分布（如让 Win10 占比更贴近真实人群）不能靠加条目解决。可选做法：
//   - 修改现有条目的字段值，保持条目数不变
//   - 接受漂移，但必须是明确的一次性决定，并重新采集 probe 基线
//   - 改用与长度无关的映射（按种子哈希落到固定桶区间），代价是实现复杂度
//
// 只有新建的 profile 不受影响——它们的种子本来就没用过。
var deviceCatalog = []model.DeviceProfile{
	{
		Label:               "Windows 11 / RTX 4060 Laptop",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce RTX 4060 Laptop GPU Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 16,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
	},
	{
		Label:               "Windows 11 / RTX 4070",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce RTX 4070 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 20,
		DeviceMemory:        8,
		ScreenWidth:         2560,
		ScreenHeight:        1440,
		DevicePixelRatio:    1,
	},
	{
		Label:               "Windows 11 / RTX 3060",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 12,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
	},
	{
		Label:               "Windows 10 / GTX 1650",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "10.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce GTX 1650 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 8,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
	},
	{
		Label:               "Windows 11 / Intel Iris Xe 轻薄本",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorIntel,
		GPURenderer:         "ANGLE (Intel, Intel(R) Iris(R) Xe Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 8,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1200,
		DevicePixelRatio:    1.25,
	},
	{
		// 改为 Win10 而非新增条目：档案条目数一变会让约 90% 的已有 profile
		// 漂移（见 catalog 顶部说明）。这台配置（4 核、UHD 620、1366x768）
		// 本就是老办公本，留在 Win10 比声称 Win11 更符合真实情况——
		// Win10 在 2026-07 仍占 Windows 桌面人群约四分之一。
		Label:               "Windows 10 / Intel UHD 620 办公本",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "10.0.0",
		GPUVendor:           vendorIntel,
		GPURenderer:         "ANGLE (Intel, Intel(R) UHD Graphics 620 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 4,
		DeviceMemory:        8,
		ScreenWidth:         1366,
		ScreenHeight:        768,
		DevicePixelRatio:    1,
	},
	{
		Label:               "Windows 11 / Radeon RX 6700 XT",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorAMD,
		GPURenderer:         "ANGLE (AMD, AMD Radeon RX 6700 XT Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 12,
		DeviceMemory:        8,
		ScreenWidth:         2560,
		ScreenHeight:        1440,
		DevicePixelRatio:    1,
	},
	{
		// 6 核是入门级桌面与主流轻薄本的常见档位，原档案库缺这一档，
		// 导致 4 核之上直接跳到 8 核。
		Label:               "Windows 11 / GTX 1660 入门桌面",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "15.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce GTX 1660 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 6,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
	},
	{
		// Win10 侧原本只有 4 核与 8 核两档。12 核对应上一代 i7 桌面机，
		// 这类机器留在 Win10 的比例不低。
		Label:               "Windows 10 / RTX 2060 桌面",
		Platform:            model.PlatformWindows,
		PlatformVersion:     "10.0.0",
		GPUVendor:           vendorNVIDIA,
		GPURenderer:         "ANGLE (NVIDIA, NVIDIA GeForce RTX 2060 Direct3D11 vs_5_0 ps_5_0, D3D11)",
		HardwareConcurrency: 12,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
	},
	// macOS 的 platformVersion 直接是系统版本号（与 Windows 不同，见文件末尾
	// 关于 platformVersion 语义的说明）。Apple 从 Sequoia 之后把编号改为按年份，
	// macOS 15 的下一版是 26（Tahoe）。据 TelemetryDeck 统计，26.3 在 2026-03
	// 已占 macOS 装机量约 57%，因此档案库以 26.x 为主。
	{
		Label:               "macOS 26 / MacBook Air M2",
		Platform:            model.PlatformMacOS,
		PlatformVersion:     "26.3.0",
		GPUVendor:           vendorApple,
		GPURenderer:         "ANGLE (Apple, ANGLE Metal Renderer: Apple M2, Unspecified Version)",
		HardwareConcurrency: 8,
		DeviceMemory:        8,
		ScreenWidth:         1470,
		ScreenHeight:        956,
		DevicePixelRatio:    2,
	},
	{
		// M3 Pro 的 CPU 有 11 核与 12 核两种配置（Apple 规格页），取 12 核。
		Label:               "macOS 26 / MacBook Pro M3 Pro",
		Platform:            model.PlatformMacOS,
		PlatformVersion:     "26.3.0",
		GPUVendor:           vendorApple,
		GPURenderer:         "ANGLE (Apple, ANGLE Metal Renderer: Apple M3 Pro, Unspecified Version)",
		HardwareConcurrency: 12,
		DeviceMemory:        8,
		ScreenWidth:         1728,
		ScreenHeight:        1117,
		DevicePixelRatio:    2,
	},
	{
		// M4 基础款是 10 核 CPU（Apple 规格页），是当前 MacBook Pro/Air
		// 入门配置中最常见的一档。
		Label:               "macOS 26 / MacBook Pro M4",
		Platform:            model.PlatformMacOS,
		PlatformVersion:     "26.3.0",
		GPUVendor:           vendorApple,
		GPURenderer:         "ANGLE (Apple, ANGLE Metal Renderer: Apple M4, Unspecified Version)",
		HardwareConcurrency: 10,
		DeviceMemory:        8,
		ScreenWidth:         1512,
		ScreenHeight:        982,
		DevicePixelRatio:    2,
	},
	{
		// M4 Pro 是 14 核 CPU（Apple 规格页），MacBook Pro 的中配主力机型，
		// 在真实人群中占比不低。补上它使 macOS 侧覆盖 8/10/12/14/16 全部
		// 常见档位。
		Label:               "macOS 26 / MacBook Pro M4 Pro",
		Platform:            model.PlatformMacOS,
		PlatformVersion:     "26.3.0",
		GPUVendor:           vendorApple,
		GPURenderer:         "ANGLE (Apple, ANGLE Metal Renderer: Apple M4 Pro, Unspecified Version)",
		HardwareConcurrency: 14,
		DeviceMemory:        8,
		ScreenWidth:         1512,
		ScreenHeight:        982,
		DevicePixelRatio:    2,
	},
	{
		// M4 Max 有 14 核与 16 核两种 CPU 配置（Apple 规格页），取 16 核。
		// 16 英寸机型的逻辑分辨率为 1728x1117。
		Label:               "macOS 26 / MacBook Pro M4 Max",
		Platform:            model.PlatformMacOS,
		PlatformVersion:     "26.3.0",
		GPUVendor:           vendorApple,
		GPURenderer:         "ANGLE (Apple, ANGLE Metal Renderer: Apple M4 Max, Unspecified Version)",
		HardwareConcurrency: 16,
		DeviceMemory:        8,
		ScreenWidth:         1728,
		ScreenHeight:        1117,
		DevicePixelRatio:    2,
	},
	{
		Label:               "Linux / Intel UHD 桌面",
		Platform:            model.PlatformLinux,
		PlatformVersion:     "6.8.0",
		GPUVendor:           vendorIntel,
		GPURenderer:         "ANGLE (Intel, Mesa Intel(R) UHD Graphics 770 (ADL-S GT1), OpenGL 4.6)",
		HardwareConcurrency: 16,
		DeviceMemory:        8,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		DevicePixelRatio:    1,
		// 实测内核 148 在 Windows 宿主机上声称 Linux 时，WebGL 仍报 Direct3D
		// 后端，而 Linux 上不存在 Direct3D，构成单一信号即可检出的矛盾。
		// 声称 macOS 时内核处理正确（报 Apple/Metal），只有 Linux 有此问题。
		// 复现见 internal/probe 的 TestKnownIssueLinuxWebGLBackendMismatch。
		KnownIssue: "在 Windows 宿主机上，WebGL 会报出 Linux 不存在的 Direct3D 后端，" +
			"构成可被直接检出的矛盾。仅在明确知晓风险时使用。",
	},
}

// Catalog 返回预置机型档案库的副本，供 UI 展示与手动指定机型。
// 含有 KnownIssue 的档案也会返回，由界面据此提示风险。
func Catalog() []model.DeviceProfile {
	out := make([]model.DeviceProfile, len(deviceCatalog))
	copy(out, deviceCatalog)
	return out
}

// safeCatalog 是可参与随机抽取的档案，在包初始化时筛出。
//
// 预先筛好而非每次抽取时过滤：抽取要靠索引对种子取模，若候选集大小随
// 调用变化，同一 seed 就会推导出不同机型，破坏 profile 指纹的稳定性。
var safeCatalog = func() []model.DeviceProfile {
	out := make([]model.DeviceProfile, 0, len(deviceCatalog))
	for _, d := range deviceCatalog {
		if d.Safe() {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		// 全部档案都有已知缺陷时退回完整档案库：没有档案可用会让
		// 所有 profile 无法启动，比使用有瑕疵的档案更糟。
		return deviceCatalog
	}
	return out
}()

// SafeCatalog 返回参与随机抽取的档案副本，供界面区分展示。
func SafeCatalog() []model.DeviceProfile {
	out := make([]model.DeviceProfile, len(safeCatalog))
	copy(out, safeCatalog)
	return out
}

// pickDevice 按种子抽取机型，抽中有已知缺陷的档案时改用替代档案。
//
// 关键约束：取模的基数必须是完整档案库的长度，不能是筛后的长度。
// 若在短表上取模，候选集大小一变几乎所有种子都会抽到不同机型
// （实测 2000 个种子中 90.8% 漂移），为修一个缺陷档案而让绝大多数
// profile 身份漂移，代价远超收益。
//
// 因此这里保持基数不变，只在命中缺陷档案时做一次确定性替换：
// 受影响的仅是原本会抽到该档案的那部分种子。
func pickDevice(d derived) model.DeviceProfile {
	device := deviceCatalog[d.pick("device", len(deviceCatalog))]
	if device.Safe() {
		return device
	}
	// 用独立 salt 在安全档案中另选一个，避免总是替换成同一款机型
	// 而让这部分 profile 聚成可识别的一簇。
	return safeCatalog[d.pick("device-fallback", len(safeCatalog))]
}

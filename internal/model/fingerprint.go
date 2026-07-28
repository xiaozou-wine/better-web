package model

// Platform 是声明给页面的操作系统。取值需与 fingerprint-chromium 的
// --fingerprint-platform 参数一致。
type Platform string

const (
	PlatformWindows Platform = "windows"
	PlatformMacOS   Platform = "macos"
	PlatformLinux   Platform = "linux"
)

// Brand 是声明给页面的浏览器品牌，对应 --fingerprint-brand。
type Brand string

const (
	BrandChrome   Brand = "Chrome"
	BrandEdge     Brand = "Edge"
	BrandOpera    Brand = "Opera"
	BrandVivaldi  Brand = "Vivaldi"
	BrandChromium Brand = "Chromium"
)

// DeviceProfile 是一台"真实机器"的完整画像。字段间存在强耦合约束：
// 声明 Windows 就不能配 Apple 的 GPU，声明 macOS 就不能配 Windows 字体。
// 因此机型只能从预置的自洽档案库中整体抽取，不允许逐字段独立随机。
// 详见 internal/fingerprint/catalog.go。
//
// 字段分两类，改动前务必分清：
//
// 生效字段（有对应命令行参数，实际传给内核）：
// Platform、PlatformVersion、HardwareConcurrency。
//
// 参考字段（内核 148 无对应参数，仅供 UI 展示与档案自洽性校对）：
// GPUVendor、GPURenderer、DeviceMemory、ScreenWidth、ScreenHeight、
// DevicePixelRatio。这些维度由内核自行从 --fingerprint 种子派生，
// 外部无法指定。保留它们是为了让档案库描述完整的机型画像，
// 便于人工核对组合是否合理，以及内核将来重新开放参数时直接接上。
//
// 不要因为"字段存在"就假设它已生效——参考字段填任何值都不会改变
// 浏览器的实际上报内容。
type DeviceProfile struct {
	// Label 是人类可读的机型名，如 "Windows 11 / RTX 4060 Laptop"。
	Label string `json:"label"`

	// Platform 生效，对应 --fingerprint-platform。
	Platform Platform `json:"platform"`
	// PlatformVersion 生效，对应 --fingerprint-platform-version。
	PlatformVersion string `json:"platformVersion"`

	// GPUVendor/GPURenderer 是参考字段，对应 WebGL 的 UNMASKED_VENDOR_WEBGL
	// 与 UNMASKED_RENDERER_WEBGL。
	//
	// 内核 144 版本移除了 --fingerprint-gpu-vendor 与
	// --fingerprint-gpu-renderer，GPU 指纹改为完全由种子派生（148 起取自
	// 真实硬件参数集）。因此这两个值当前无法指定，页面上报的 GPU 由内核决定，
	// 未必与本档案声明的一致。
	//
	// 另需注意 WebGL metadata 伪造仅在 Linux 生效，见 README 的
	// "WebGL Metadata: currently only supports Linux"。
	GPUVendor   string `json:"gpuVendor"`
	GPURenderer string `json:"gpuRenderer"`

	// HardwareConcurrency 生效，对应 --fingerprint-hardware-concurrency，
	// 须是该机型合理的逻辑核数。不传时内核从种子派生。
	HardwareConcurrency int `json:"hardwareConcurrency"`

	// DeviceMemory 是参考字段，单位 GiB，对应 navigator.deviceMemory。
	//
	// 无命令行参数可传。内核 148 起该值由种子从 8/16/32 中选取，
	// 注意这与 W3C device-memory 规范允许的 0.25/0.5/1/2/4/8 档位并不重合，
	// 上报 16 或 32 属于内核行为而非本档案控制。
	DeviceMemory float64 `json:"deviceMemory"`

	// ScreenWidth/ScreenHeight/DevicePixelRatio 是参考字段，无对应命令行参数。
	// 窗口尺寸可用 --window-size 影响，但 screen.width/height 由内核与宿主机
	// 决定，本档案的取值不会生效。高分屏机型的 DevicePixelRatio 须配 >1，
	// 仅用于人工核对档案是否自洽。
	ScreenWidth      int     `json:"screenWidth"`
	ScreenHeight     int     `json:"screenHeight"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`

	// KnownIssue 非空表示该档案存在已知的可检出缺陷，内容为面向用户的说明。
	// 有此标记的档案不参与随机抽取，只能由用户显式选择——默认路径不该
	// 把用户放到已知会暴露的档案上，但也不应剥夺其知情后自行选择的权利。
	KnownIssue string `json:"knownIssue,omitempty"`
}

// Safe 报告该档案是否可用于随机抽取。
func (d DeviceProfile) Safe() bool { return d.KnownIssue == "" }

// SpoofTarget 是可单独关闭的伪造子系统，对应内核 144 起提供的
// --disable-spoofing 参数（逗号分隔）。
//
// 用途是排障：某站点识别出自动化时，逐项关闭做二分定位，比盲猜快得多。
// 生产环境不应关闭任何一项——每关一项就少一层伪装。
type SpoofTarget string

const (
	SpoofFont        SpoofTarget = "font"
	SpoofAudio       SpoofTarget = "audio"
	SpoofCanvas      SpoofTarget = "canvas"
	SpoofClientRects SpoofTarget = "clientrects"
	SpoofGPU         SpoofTarget = "gpu"
)

// Valid 报告是否为内核已知的伪造子系统名。
// 传入未知名称时内核的行为未定义，因此必须在组装参数前拦掉。
func (s SpoofTarget) Valid() bool {
	switch s {
	case SpoofFont, SpoofAudio, SpoofCanvas, SpoofClientRects, SpoofGPU:
		return true
	}
	return false
}

// SpoofTargets 返回全部可关闭的子系统，供 UI 展示排障选项。
func SpoofTargets() []SpoofTarget {
	return []SpoofTarget{SpoofFont, SpoofAudio, SpoofCanvas, SpoofClientRects, SpoofGPU}
}

// Fingerprint 是一个 profile 最终生效的完整环境，由 seed 与地理信息推导。
// 它是启动参数组装的唯一输入，见 internal/launcher。
type Fingerprint struct {
	// Seed 传给 --fingerprint，内核据此推导 canvas/audio/字体等噪声。
	Seed int32 `json:"seed"`

	Device DeviceProfile `json:"device"`

	Brand        Brand  `json:"brand"`
	BrandVersion string `json:"brandVersion"`

	// Timezone 与 Locale 必须与代理出口地一致，否则构成明显矛盾。
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	// AcceptLanguages 是 Accept-Language 头的值，如 "en-US,en;q=0.9"。
	AcceptLanguages string `json:"acceptLanguages"`
}

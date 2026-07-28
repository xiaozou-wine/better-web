package kernel

// 系统已装 Chrome 的探测。
//
// 为什么需要它：日常模式（KindDaily）不做任何指纹伪造，只要目录隔离，
// 因此没有理由用指纹内核。而指纹内核基于 ungoogled-chromium，代价是
// 登不了 Google 账号、没有同步、可能缺 DRM，且更新滞后于官方 Chrome
// （实测本机指纹内核 148，而官方稳定版已到 150）。
//
// 日常浏览用官方 Chrome 就能拿回这些能力，代理与目录隔离照旧生效——
// 那两件事在 Go 侧完成，与用哪个内核无关。
//
// 反过来，指纹模式绝不能用官方 Chrome：--fingerprint 是打进 C++ 源码的
// 补丁，官方二进制不认识它，会当成未知参数忽略，于是伪造静默失效而
// 页面照常打开。这类故障没有任何可见痕迹，因此由 SystemChrome 标记
// 内核来源，供 launcher 做 fail-closed 判断。

// Source 标识一个内核的来源。
//
// 用显式来源标记而非靠路径猜：路径判断会被自定义安装位置绕过，
// 而这个标记决定的是"能不能用于指纹模式"，判错的后果是伪造静默失效。
type Source string

const (
	// SourceFingerprint 是 fingerprint-chromium 内核，支持指纹伪造。
	SourceFingerprint Source = "fingerprint"
	// SourceSystemChrome 是系统已装的官方 Chrome，不支持指纹伪造。
	SourceSystemChrome Source = "system-chrome"
)

// SupportsFingerprint 报告该来源的内核能否用于指纹模式。
//
// 只有 fingerprint-chromium 打了对应补丁。官方 Chrome 会静默忽略
// --fingerprint，导致伪造全部失效且不报错。
func (s Source) SupportsFingerprint() bool {
	return s == SourceFingerprint || s == ""
}

//go:build !windows

package urlhandler

// 非 Windows 平台尚未实现。
//
// macOS 要往 .app 的 Info.plist 里写 CFBundleURLTypes 并调 LSSetDefaultHandler，
// 而本项目在 macOS 上还没有 .app 打包（见 internal/shortcut 的同类说明）；
// Linux 要写 .desktop 的 MimeType 并调 xdg-settings，各桌面环境行为不一。
//
// 都返回 ErrUnsupported 而非静默成功：静默成功会让界面显示"已注册"，
// 而用户点链接时毫无反应，没有任何线索指向原因。

// Register 在当前平台不受支持。
func Register(string) error { return ErrUnsupported }

// Unregister 在当前平台不受支持。
func Unregister() error { return ErrUnsupported }

// Query 在当前平台恒返回未注册。
//
// 不返回错误：界面每次打开设置页都会调它，返回错误会让整页显示失败，
// 而"这个平台没有这项能力"本身是正常状态，由 Registered 为 false 表达即可。
func Query() (Status, error) { return Status{}, nil }

// Supported 报告当前平台是否实现了默认浏览器注册。
func Supported() bool { return false }

// OpenSettings 在当前平台不受支持。
func OpenSettings() error { return ErrUnsupported }

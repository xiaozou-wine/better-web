package model

import "strings"

// StartupMode 决定 profile 启动时打开什么。
type StartupMode string

const (
	// StartupNewTab 打开新标签页，即 Chromium 的默认行为。
	StartupNewTab StartupMode = "newtab"
	// StartupURLs 启动时打开指定的一组 URL。
	StartupURLs StartupMode = "urls"
)

// Valid 报告 mode 是否为已知取值。空串视为有效，表示沿用默认。
func (m StartupMode) Valid() bool {
	switch m {
	case "", StartupNewTab, StartupURLs:
		return true
	}
	return false
}

// Startup 是一个 profile 的启动页与新标签页配置。
//
// 两项都通过命令行传给内核，不改 profile 的 Preferences 文件。
// 实测（internal/probe 的 TestRealKernelStartupOptions）：
//   - 命令行末尾传 URL —— 生效
//   - --custom-ntp=<URL>（ungoogled-chromium 提供的开关）—— 生效
//   - Preferences 的 session.startup_urls —— **不生效**。内核会读入并保留
//     该键，但启动时仍打开新标签页，日志显示 "Requested load of
//     chrome://newtab/"。因此不要改回 Preferences 方案。
type Startup struct {
	Mode StartupMode `json:"mode,omitempty"`

	// URLs 仅在 Mode 为 StartupURLs 时生效，启动时逐个在标签页中打开。
	URLs []string `json:"urls,omitempty"`

	// NewTabURL 覆盖新标签页地址，对应 --custom-ntp。
	//
	// 与 URLs 的分工：URLs 只在启动那一次打开，NewTabURL 影响之后每次
	// 打开新标签页。留空表示用内核默认的新标签页。
	//
	// ungoogled-chromium 移除了 Google 的新标签页组件，默认页是空白的，
	// 因此这一项在实际使用中比启动页更常用。
	NewTabURL string `json:"newTabUrl,omitempty"`
}

// Effective 返回补齐默认值后的配置，nil 接收者也安全。
func (s *Startup) Effective() Startup {
	if s == nil {
		return Startup{Mode: StartupNewTab}
	}
	out := *s
	if out.Mode == "" {
		out.Mode = StartupNewTab
	}
	out.URLs = NormalizeURLs(out.URLs)
	out.NewTabURL = strings.TrimSpace(out.NewTabURL)
	return out
}

// StartupURLList 返回启动时应当打开的 URL。
// 非 StartupURLs 模式返回 nil——由内核决定打开什么。
func (s *Startup) StartupURLList() []string {
	cfg := s.Effective()
	if cfg.Mode != StartupURLs {
		return nil
	}
	return cfg.URLs
}

// NormalizeURLs 清洗 URL 列表：去空白、去空串、去重，保持原顺序。
//
// 不做协议补全或格式校验：Chromium 自己会把 "example.com" 当成 URL 处理，
// 而在这里判断"什么算合法 URL"只会误拒 file:// 和 chrome:// 这类合理输入。
func NormalizeURLs(urls []string) []string {
	if len(urls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(urls))
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// StartupModes 返回全部可选的启动模式，供界面构建选项。
func StartupModes() []StartupMode {
	return []StartupMode{StartupNewTab, StartupURLs}
}

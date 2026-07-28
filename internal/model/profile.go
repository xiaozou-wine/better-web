// Package model 定义 better-web 的核心领域模型。
package model

import (
	"errors"
	"time"
)

// ProfileKind 区分两种 profile 类型。日常模式不做任何指纹伪造，
// 只使用原生环境；指纹模式为每个 profile 生成独立且自洽的环境。
type ProfileKind string

const (
	// KindDaily 日常浏览模式：使用真实环境，不注入任何 fingerprint 参数。
	KindDaily ProfileKind = "daily"
	// KindFingerprint 指纹模式：按 seed 推导完整环境并隔离。
	KindFingerprint ProfileKind = "fingerprint"
)

// Valid 报告 kind 是否为已知取值。
func (k ProfileKind) Valid() bool {
	return k == KindDaily || k == KindFingerprint
}

// ProxyScheme 是转发代理支持的上游协议。
type ProxyScheme string

const (
	ProxyHTTP   ProxyScheme = "http"
	ProxyHTTPS  ProxyScheme = "https"
	ProxySOCKS5 ProxyScheme = "socks5"
)

// Proxy 描述一个上游代理。Chromium 的 --proxy-server 不支持带密码的
// 认证，因此带凭据的代理必须经由本地转发代理接入，见 internal/proxy。
type Proxy struct {
	Scheme   ProxyScheme `json:"scheme"`
	Host     string      `json:"host"`
	Port     int         `json:"port"`
	Username string      `json:"username,omitempty"`

	// Password 在本结构体中始终是明文——转发代理需要用它向上游认证。
	// 落库时由 internal/store 调用 internal/secret 加密，读出时自动解密，
	// 因此本字段的使用方无需关心加密。
	//
	// 加密的作用范围要认清：Windows 上用 DPAPI，密钥绑定当前用户账户，
	// 所以它能防"数据库文件被单独拷到别处或别的账户下"，但防不了已能在
	// 当前用户下执行代码的攻击者——那种情况下程序自己能解密，攻击者也能。
	// 非 Windows 平台尚无系统级密钥库接入，仍是明文存储。
	//
	// 由此产生的约束，调用方必须遵守：
	//   - 数据目录（含 profiles.db）不得放入网盘同步、共享目录或代码仓库
	//   - 不得把该字段写入日志、错误信息或发往前端的视图
	//     （见 app.ProxyView，它只回传"是否已设置密码"）
	Password string `json:"password,omitempty"`
}

// ErrSystemBrowserWithFingerprint 表示指纹模式的 profile 被配置成用系统
// 浏览器启动。这是必须拒绝的组合，不是可以警告后放行的问题。
//
// 单独定义错误类型是为了让调用方能识别并给出针对性提示——这个组合的后果
// （伪造静默失效）与其他配置错误不同，用户需要明确知道为什么被拒绝。
var ErrSystemBrowserWithFingerprint = errors.New(
	"指纹模式不能使用系统 Chrome：官方构建不认识 --fingerprint 参数，" +
		"会静默忽略全部伪造，页面照常打开但报出的是本机真实指纹")

// ValidateBrowserChoice 校验浏览器选择与 profile 类型是否相容。
//
// fail-closed：宁可拒绝启动，也不能让指纹 profile 在不支持伪造的内核上跑起来。
// 后者没有任何可见痕迹——页面正常打开、脚本正常执行，只是指纹全是真的，
// 等发现时账号已经出问题了。这与代理的 fail-closed 是同一条原则。
func (p *Profile) ValidateBrowserChoice() error {
	if p == nil {
		return errors.New("profile 为空")
	}
	if p.UseSystemBrowser && p.Kind == KindFingerprint {
		return ErrSystemBrowserWithFingerprint
	}
	return nil
}

// NeedsAuth 报告该代理是否需要经本地转发代理补认证。
func (p *Proxy) NeedsAuth() bool {
	return p != nil && p.Username != ""
}

// Geo 是代理出口的地理信息，用于自动对齐时区与语言。
type Geo struct {
	CountryCode string  `json:"countryCode"` // ISO 3166-1 alpha-2
	Timezone    string  `json:"timezone"`    // IANA 时区名，如 America/Los_Angeles
	Locale      string  `json:"locale"`      // 如 en-US
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// Profile 是一个独立的浏览器身份，对应一份 user-data-dir。
type Profile struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Kind ProfileKind `json:"kind"`

	// Seed 是指纹推导的唯一输入。同一 seed 永远推导出同一套环境，
	// 保证 profile 每次启动指纹稳定。仅 KindFingerprint 使用。
	Seed int32 `json:"seed"`

	// ProfileDir 是该 profile 的 user-data-dir 绝对路径。
	ProfileDir string `json:"profileDir"`

	Proxy *Proxy `json:"proxy,omitempty"`

	// GeoOverride 非空时跳过出口 IP 反查，直接采用此地理信息。
	GeoOverride *Geo `json:"geoOverride,omitempty"`

	// KernelVersion 锁定该 profile 使用的内核版本。留空表示用当前默认内核。
	// 锁定版本可避免内核升级导致指纹漂移。
	KernelVersion string `json:"kernelVersion,omitempty"`

	// DisableSpoofing 列出要关闭的伪造子系统，对应 --disable-spoofing。
	// 仅用于排障定位，正常使用应留空——每关一项就少一层伪装。
	DisableSpoofing []SpoofTarget `json:"disableSpoofing,omitempty"`

	// ExtraArgs 是追加到命令行末尾的原始参数，供高级用户使用。
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// UseSystemBrowser 为 true 时用系统已装的官方 Chrome 启动，而非指纹内核。
	//
	// 仅 KindDaily 允许开启。日常模式本就不做任何伪造、只要目录隔离，
	// 而指纹内核基于 ungoogled-chromium：登不了 Google 账号、没有同步、
	// 可能缺 DRM，且更新滞后于官方 Chrome。用官方 Chrome 能拿回这些能力，
	// 代理与目录隔离照旧生效——那两件事在 Go 侧完成，与用哪个内核无关。
	//
	// KindFingerprint 绝不允许开启：--fingerprint 是打进 C++ 源码的补丁，
	// 官方 Chrome 不认识它，会当成未知参数忽略，于是伪造静默失效而页面
	// 照常打开。这类故障没有任何可见痕迹，因此在启动前 fail-closed 拒绝，
	// 见 session.Manager 的启动流程与 Profile.ValidateBrowserChoice。
	UseSystemBrowser bool `json:"useSystemBrowser,omitempty"`

	// Startup 是启动页配置，nil 表示打开新标签页。
	//
	// 由 launcher 写进 profile 的 Preferences 文件而非命令行：
	// 命令行传 URL 只影响单次启动且与"恢复上次会话"互斥。
	Startup *Startup `json:"startup,omitempty"`

	// DeviceLabel 锁定该 profile 使用的机型档案，空串表示由种子随机抽取。
	//
	// 用 Label 而非索引：档案库会增删条目，索引会随之错位，
	// 导致已有 profile 悄悄换成另一台设备。Label 稳定且可读。
	//
	// 找不到对应 Label 时回退到按种子抽取，并在启动时给出警告——
	// 静默换设备比报错更危险。
	DeviceLabel string `json:"deviceLabel,omitempty"`

	// Group 是 profile 所属的分组名，空串表示未分组。
	//
	// 只做单层分组而非树形层级：多账号场景的实际用法是按平台或项目归类
	// （"电商-美国站"、"社媒-东南亚"），层级带来的复杂度换不回收益。
	Group string `json:"group,omitempty"`

	// Tags 是标签集合，用于跨分组的横向筛选（如 "已验证"、"待养号"）。
	//
	// 与 Group 的分工：一个 profile 只属于一个分组，但可以有多个标签。
	// 存储时去重并保持写入顺序，见 NormalizeTags。
	Tags []string `json:"tags,omitempty"`

	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	LastUseAt time.Time `json:"lastUseAt,omitzero"`
}

// Package launcher 负责把一份 Profile 与其指纹组装成 Chromium 命令行，
// 并管理浏览器进程的生命周期。
//
// 参数名全部对应 fingerprint-chromium 的实现，改动前请核对其 README：
// https://github.com/adryfish/fingerprint-chromium
package launcher

import (
	"fmt"
	"strconv"
	"strings"

	"better-web/internal/model"
)

// 内核支持的指纹相关命令行参数名。集中定义避免散落在各处的字符串字面量。
const (
	flagUserDataDir          = "--user-data-dir"
	flagFingerprint          = "--fingerprint"
	flagPlatform             = "--fingerprint-platform"
	flagPlatformVersion      = "--fingerprint-platform-version"
	flagBrand                = "--fingerprint-brand"
	flagBrandVersion         = "--fingerprint-brand-version"
	flagHardwareConcurrency  = "--fingerprint-hardware-concurrency"
	flagTimezone             = "--timezone"
	flagLang                 = "--lang"
	flagAcceptLang           = "--accept-lang"
	flagProxyServer          = "--proxy-server"
	flagDisableNonProxiedUDP = "--disable-non-proxied-udp"
	// flagCustomNTP 是 ungoogled-chromium 提供的新标签页覆盖开关，
	// 见其 patches/extra/ungoogled-chromium/add-flag-for-custom-ntp.patch。
	// 已确认存在于内核 148 的二进制中。
	flagCustomNTP       = "--custom-ntp"
	flagDisableSpoofing = "--disable-spoofing"
	// flagIncognito 开无痕窗口。Chromium 与官方 Chrome 都支持，非补丁项。
	flagIncognito = "--incognito"
	// flagNewWindow 强制新开窗口而非在现有窗口里加标签页。
	//
	// 只在把 URL 递给已运行实例时用得上，见 launcher.HandoffArgs。
	flagNewWindow = "--new-window"
)

// Options 是启动时的一次性附加项，不属于 Profile 的持久配置。
//
// 与 Profile.Startup 的分工：那个是"这个 profile 每次启动都打开什么"，
// 这个是"本次启动因为外部请求而要打开什么"。链接接管每次的 URL 都不同，
// 存进 profile 配置没有意义。
type Options struct {
	// URLs 是本次启动额外要打开的地址，追加在 Startup.URLs 之后。
	URLs []string
	// Incognito 为 true 时开无痕窗口。
	//
	// 只对日常模式有意义，由调用方保证——见 model.Profile 的说明与
	// app.Service.SetURLHandler 的校验。
	Incognito bool
}

// BuildArgs 组装启动 Chromium 所需的完整参数列表。
//
// proxyAddr 是浏览器实际要连的代理地址（形如 socks5://127.0.0.1:41234）。
// 需要认证的上游代理必须先由本地转发代理接管，此处只接受无认证地址，
// 因为内核的 --proxy-server 不支持带密码的认证。proxyAddr 为空表示直连。
//
// 日常模式（KindDaily）不注入任何指纹参数，只做 profile 目录隔离，
// 保证日常浏览使用的是真实环境。
//
// opt 为 nil 表示无附加项，等价于 BuildArgs 原先的行为。
func BuildArgs(p *model.Profile, fp *model.Fingerprint, proxyAddr string, opt *Options) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("profile 为空")
	}
	if p.ProfileDir == "" {
		return nil, fmt.Errorf("profile %q 缺少 user-data-dir", p.Name)
	}
	if !p.Kind.Valid() {
		return nil, fmt.Errorf("profile %q 的类型 %q 无效", p.Name, p.Kind)
	}

	startupCfg := p.Startup.Effective()
	if !startupCfg.Mode.Valid() {
		return nil, fmt.Errorf("profile %q 的启动模式 %q 无效", p.Name, startupCfg.Mode)
	}
	// 非 URLs 模式不传启动 URL，避免残留配置意外打开页面。
	if startupCfg.Mode != model.StartupURLs {
		startupCfg.URLs = nil
	}

	args := []string{flagUserDataDir + "=" + p.ProfileDir}

	if p.Kind == model.KindFingerprint {
		if fp == nil {
			return nil, fmt.Errorf("指纹模式的 profile %q 缺少指纹配置", p.Name)
		}
		fpArgs, err := fingerprintArgs(fp)
		if err != nil {
			return nil, fmt.Errorf("组装 profile %q 的指纹参数失败: %w", p.Name, err)
		}
		args = append(args, fpArgs...)

		// 排障开关只在指纹模式下有意义：日常模式本就不做任何伪造。
		if len(p.DisableSpoofing) > 0 {
			spoofArg, err := disableSpoofingArg(p.DisableSpoofing)
			if err != nil {
				return nil, fmt.Errorf("profile %q 的伪造开关无效: %w", p.Name, err)
			}
			args = append(args, spoofArg)
		}
	}

	if proxyAddr != "" {
		args = append(args, flagProxyServer+"="+proxyAddr)
		// 关闭非代理的 UDP，否则 WebRTC 会通过 STUN 直接泄露真实 IP，
		// 使前面所有伪造失效。
		args = append(args, flagDisableNonProxiedUDP)
	}

	// 新标签页覆盖。ungoogled-chromium 提供的开关，Google 的新标签页组件
	// 被移除后默认页是空白的，因此这一项在实际使用中很常用。
	//
	// 只在支持它的内核上传：官方 Chrome 没有这个补丁，传过去会被当成未知
	// 参数。虽然不致命，但 Chrome 会弹出"使用了不受支持的标记"提示条，
	// 而那个提示本身就是异常信号。
	if ntp := startupCfg.NewTabURL; ntp != "" && !p.UseSystemBrowser {
		args = append(args, flagCustomNTP+"="+ntp)
	}

	// 无痕窗口。只在日常模式放行：指纹模式开无痕会误导用户——无痕不落盘，
	// 但指纹伪造与代理照旧生效，出口 IP 和指纹一个字没变。用户以为"无痕
	// 更干净"，实际上只是丢掉了 Cookie 持久化，而那正是养号要留的东西。
	if opt != nil && opt.Incognito && p.Kind == model.KindDaily {
		args = append(args, flagIncognito)
	}

	args = append(args, p.ExtraArgs...)

	// 启动 URL 必须放在最末：Chromium 把不以 - 开头的尾部参数当作要打开的
	// URL，夹在开关之间会被解析成开关的值。
	//
	// 用命令行而非 Preferences 的 session.startup_urls：后者实测无效，
	// 内核会读入并保留该键但仍打开新标签页，见 model.Startup 的说明。
	args = append(args, startupCfg.URLs...)
	if opt != nil {
		args = append(args, opt.URLs...)
	}
	return args, nil
}

// HandoffArgs 组装"把 URL 递给已运行实例"所需的参数。
//
// 机制：用同一个 --user-data-dir 再启动一次内核，Chromium 自己的
// process singleton 会发现已有实例，把命令行转交过去后本进程立即退出
// （实测输出 "Opening in existing browser session."，约 1 秒返回）。
//
// 这条路径**不能带代理与指纹参数**。递送时那些开关会被已运行实例忽略——
// 它的代理和指纹在自己启动时就定好了，无法中途改。传过去只会让人误以为
// 生效了，因此这里干脆不传，避免留下"传了但没用"的错误线索。
//
// 同理不加跨进程锁：递送方根本不打开那个目录，不存在 user-data-dir 被
// 两个进程同时写的风险。
func HandoffArgs(p *model.Profile, opt *Options) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("profile 为空")
	}
	if p.ProfileDir == "" {
		return nil, fmt.Errorf("profile %q 缺少 user-data-dir", p.Name)
	}
	if opt == nil || len(opt.URLs) == 0 {
		return nil, fmt.Errorf("递送给已运行实例时必须给出至少一个 URL")
	}

	args := []string{flagUserDataDir + "=" + p.ProfileDir}
	// 无痕与新窗口二选一：--incognito 本身就会开一个新的无痕窗口，
	// 两个都传时 Chromium 的行为是开一个普通新窗口再开一个无痕窗口。
	if opt.Incognito && p.Kind == model.KindDaily {
		args = append(args, flagIncognito)
	} else {
		args = append(args, flagNewWindow)
	}
	return append(args, opt.URLs...), nil
}

// disableSpoofingArg 把要关闭的子系统组装成 --disable-spoofing=a,b 形式。
//
// 未知名称直接报错而非静默丢弃：用户以为关掉了某项而实际没关，会把排障
// 引向错误结论。重复项会去掉，避免传出 gpu,gpu 这类内核未定义的输入。
func disableSpoofingArg(targets []model.SpoofTarget) (string, error) {
	seen := make(map[model.SpoofTarget]bool, len(targets))
	vals := make([]string, 0, len(targets))
	for _, t := range targets {
		if !t.Valid() {
			return "", fmt.Errorf("未知的伪造子系统 %q，可选值: font/audio/canvas/clientrects/gpu", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		vals = append(vals, string(t))
	}
	if len(vals) == 0 {
		return "", fmt.Errorf("伪造开关列表为空")
	}
	return flagDisableSpoofing + "=" + strings.Join(vals, ","), nil
}

func fingerprintArgs(fp *model.Fingerprint) ([]string, error) {
	if fp.Seed == 0 {
		return nil, fmt.Errorf("指纹种子不能为 0")
	}
	d := fp.Device
	if d.Platform == "" {
		return nil, fmt.Errorf("机型档案缺少 platform")
	}
	if fp.Timezone == "" {
		return nil, fmt.Errorf("缺少时区，无法与代理出口地对齐")
	}
	if fp.Locale == "" {
		return nil, fmt.Errorf("缺少语言，无法与代理出口地对齐")
	}

	args := []string{
		flagFingerprint + "=" + strconv.FormatInt(int64(fp.Seed), 10),
		flagPlatform + "=" + string(d.Platform),
		flagTimezone + "=" + fp.Timezone,
		flagLang + "=" + fp.Locale,
		flagAcceptLang + "=" + fp.AcceptLanguages,
	}
	if d.PlatformVersion != "" {
		args = append(args, flagPlatformVersion+"="+d.PlatformVersion)
	}
	if fp.Brand != "" {
		args = append(args, flagBrand+"="+string(fp.Brand))
	}
	if fp.BrandVersion != "" {
		args = append(args, flagBrandVersion+"="+fp.BrandVersion)
	}
	if d.HardwareConcurrency > 0 {
		args = append(args, flagHardwareConcurrency+"="+strconv.Itoa(d.HardwareConcurrency))
	}
	return args, nil
}

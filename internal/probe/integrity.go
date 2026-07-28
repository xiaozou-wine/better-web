package probe

import (
	"fmt"
	"regexp"
	"strings"
)

// Issue 是一项伪装缺陷。
type Issue struct {
	// Check 是检查项名称。
	Check string
	// Detail 说明具体矛盾。
	Detail string
	// Severe 为 true 表示单一信号即可判定为伪造浏览器；
	// false 表示可疑但需结合其他信号。
	Severe bool
}

func (i Issue) String() string {
	tag := "可疑"
	if i.Severe {
		tag = "可直接检出"
	}
	return fmt.Sprintf("[%s] %s: %s", tag, i.Check, i.Detail)
}

// uaPlatformPattern 从 UA 中提取操作系统片段。
var uaPlatformPattern = regexp.MustCompile(`\(([^)]*)\)`)

// CheckIntegrity 检查一份采集结果内部是否自洽。
//
// 检测原理：反检测系统很少单看某个值是否"正常"，而是查多个维度之间有没有
// 真实世界不可能出现的组合。声称 macOS 却报 Win32、UA 说 Windows 而 WebGL
// 报 Apple GPU，这类矛盾比任何单项数值都更容易暴露伪造。
//
// 返回空切片表示未发现内部矛盾，不代表能通过所有真实检测站——
// 本函数只覆盖可离线判定的一致性问题。
func CheckIntegrity(r Result) []Issue {
	var out []Issue
	add := func(check, detail string, severe bool) {
		out = append(out, Issue{Check: check, Detail: detail, Severe: severe})
	}

	ua := r.UserAgent
	uaOS := ""
	if m := uaPlatformPattern.FindStringSubmatch(ua); m != nil {
		uaOS = m[1]
	}

	// navigator.platform 必须与 UA 声称的系统一致。
	// 这是最基础的一致性检查，几乎所有检测站都做。
	switch {
	case strings.Contains(uaOS, "Windows"):
		if r.Platform != "Win32" && r.Platform != "Win64" {
			add("platform 与 UA 矛盾",
				fmt.Sprintf("UA 声称 Windows 而 navigator.platform=%q", r.Platform), true)
		}
	case strings.Contains(uaOS, "Macintosh"):
		if r.Platform != "MacIntel" {
			add("platform 与 UA 矛盾",
				fmt.Sprintf("UA 声称 macOS 而 navigator.platform=%q", r.Platform), true)
		}
	case strings.Contains(uaOS, "Linux") || strings.Contains(uaOS, "X11"):
		if !strings.HasPrefix(r.Platform, "Linux") {
			add("platform 与 UA 矛盾",
				fmt.Sprintf("UA 声称 Linux 而 navigator.platform=%q", r.Platform), true)
		}
	}

	// userAgentData.platform 必须与 UA 一致。
	if uaData := uaDataPlatform(r); uaData != "" {
		want := map[string]string{"Windows": "Windows", "Macintosh": "macOS", "X11": "Linux", "Linux": "Linux"}
		for key, expect := range want {
			if strings.Contains(uaOS, key) && uaData != expect {
				add("userAgentData 与 UA 矛盾",
					fmt.Sprintf("UA 声称 %s 而 userAgentData.platform=%q", key, uaData), true)
				break
			}
		}
	}

	// WebGL 厂商必须与声称的系统相容：
	// Apple GPU 只存在于 Mac，声称 Windows 却报 Apple 是不可能的组合。
	vendor := strings.ToLower(r.WebGLVendor + " " + r.WebGLRenderer)
	if strings.Contains(vendor, "apple") && !strings.Contains(uaOS, "Macintosh") {
		add("WebGL 与系统矛盾",
			fmt.Sprintf("非 macOS 系统报出 Apple GPU: %q", r.WebGLRenderer), true)
	}
	if strings.Contains(vendor, "direct3d") && !strings.Contains(uaOS, "Windows") {
		add("WebGL 与系统矛盾",
			fmt.Sprintf("非 Windows 系统报出 Direct3D 渲染后端: %q", r.WebGLRenderer), true)
	}
	if strings.Contains(vendor, "metal") && !strings.Contains(uaOS, "Macintosh") {
		add("WebGL 与系统矛盾",
			fmt.Sprintf("非 macOS 系统报出 Metal 渲染后端: %q", r.WebGLRenderer), true)
	}

	// 软件渲染是强自动化信号：正常用户机器不会用 SwiftShader。
	if strings.Contains(vendor, "swiftshader") || strings.Contains(vendor, "llvmpipe") {
		add("软件渲染", fmt.Sprintf("WebGL 使用软件渲染器 %q", r.WebGLRenderer), true)
	}

	// navigator.webdriver 为 true 直接暴露自动化。
	if r.Webdriver {
		add("自动化特征", "navigator.webdriver 为 true", true)
	}

	// languages 的首项应与 language 一致，不一致是伪造时漏改的典型痕迹。
	if len(r.Languages) > 0 && r.Language != "" && r.Languages[0] != r.Language {
		add("语言不一致",
			fmt.Sprintf("navigator.language=%q 而 languages[0]=%q", r.Language, r.Languages[0]), false)
	}

	// 插件列表为空是无头浏览器的典型特征。
	if len(r.Plugins) == 0 {
		add("插件缺失", "navigator.plugins 为空，是无头浏览器的常见特征", false)
	}

	// deviceMemory 只能是 2 的幂，出现 6、12 这类值必为伪造。
	if r.DeviceMemory > 0 && !isPow2(r.DeviceMemory) {
		add("deviceMemory 非法",
			fmt.Sprintf("deviceMemory=%v 不是 2 的幂", r.DeviceMemory), true)
	}

	// hardwareConcurrency 为 0 或过大都不合理。
	if r.HardwareConcurrency <= 0 {
		add("核数非法", fmt.Sprintf("hardwareConcurrency=%d", r.HardwareConcurrency), true)
	} else if r.HardwareConcurrency > 128 {
		add("核数异常", fmt.Sprintf("hardwareConcurrency=%d 超出消费级硬件范围",
			r.HardwareConcurrency), false)
	}

	// 时区与偏移必须相容：报 Asia/Tokyo 却给出美国的偏移是明显矛盾。
	if r.Timezone != "" && !offsetMatchesZone(r.Timezone, r.TimezoneOffset) {
		add("时区与偏移矛盾",
			fmt.Sprintf("timezone=%q 而 getTimezoneOffset()=%d", r.Timezone, r.TimezoneOffset), true)
	}

	// UA 中不应残留自动化标记。
	if strings.Contains(ua, "HeadlessChrome") {
		add("UA 残留", "UA 中含 HeadlessChrome", true)
	}

	return out
}

// SevereIssues 筛出可直接检出的缺陷。
func SevereIssues(issues []Issue) []Issue {
	var out []Issue
	for _, i := range issues {
		if i.Severe {
			out = append(out, i)
		}
	}
	return out
}

func uaDataPlatform(r Result) string {
	if r.UAData == nil {
		return ""
	}
	p, _ := r.UAData["platform"].(string)
	return p
}

func isPow2(v float64) bool {
	if v <= 0 {
		return false
	}
	for x := v; x < 1; x *= 2 {
		// 处理 0.25、0.5 这类负指数档位。
		if x == 0.5 {
			return true
		}
	}
	n := int64(v)
	return float64(n) == v && n&(n-1) == 0
}

// zoneOffsets 是各时区在标准时与夏令时下 getTimezoneOffset() 的合法取值。
// JS 的偏移符号与 UTC 偏移相反：UTC-8 对应 480。
var zoneOffsets = map[string][]int{
	"America/Los_Angeles": {480, 420},
	"America/New_York":    {300, 240},
	"America/Chicago":     {360, 300},
	"America/Denver":      {420, 360},
	"Europe/London":       {0, -60},
	"Europe/Berlin":       {-60, -120},
	"Europe/Paris":        {-60, -120},
	"Europe/Moscow":       {-180},
	"Asia/Tokyo":          {-540},
	"Asia/Shanghai":       {-480},
	"Asia/Singapore":      {-480},
	"Asia/Kolkata":        {-330},
	"Australia/Sydney":    {-600, -660},
	"UTC":                 {0},
}

// offsetMatchesZone 报告偏移量是否与时区相容。
// 未收录的时区一律放行：宁可漏检，也不要对合法配置误报。
func offsetMatchesZone(zone string, offset int) bool {
	allowed, ok := zoneOffsets[zone]
	if !ok {
		return true
	}
	for _, a := range allowed {
		if offset == a {
			return true
		}
	}
	return false
}

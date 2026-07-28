package probe

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
)

// seedsByPlatform 为每种声称的系统各挑若干种子。
//
// 必须按 platform 分组覆盖，不能随手列几个种子：跨系统矛盾只在声称非宿主机
// 系统时才出现，随机选种子极可能全落在 Windows 上而漏掉问题。
func seedsByPlatform(t *testing.T) map[model.Platform][]int32 {
	t.Helper()
	out := map[model.Platform][]int32{}
	// 扫一批种子，按推导出的 platform 归类，每类取前两个。
	for i := 0; i < 400 && len(out) < 3; i++ {
		seed := int32(1000 + i*7919)
		fp := fingerprint.Derive(seed, nil)
		p := fp.Device.Platform
		if len(out[p]) < 2 {
			out[p] = append(out[p], seed)
		}
	}
	if len(out) == 0 {
		t.Fatal("未能按 platform 归类任何种子")
	}
	return out
}

// 真实内核采集的指纹必须内部自洽。
//
// 这比"参数是否生效"更接近真实检测：检测站很少单看某个值是否正常，
// 而是查多维度之间有没有真实世界不可能出现的组合。
//
// 已知问题：声称 Linux 时 WebGL 仍报 Direct3D，见
// TestKnownIssueLinuxWebGLBackendMismatch 的说明。
func TestRealKernelFingerprintIsInternallyConsistent(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}

	for platform, seeds := range seedsByPlatform(t) {
		for _, seed := range seeds {
			fp, args := probeProfile(t, seed, geo)
			res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
			if err != nil {
				t.Fatalf("种子 %d 采集失败: %v", seed, err)
			}

			issues := CheckIntegrity(res)
			for _, i := range issues {
				t.Logf("种子 %d（声称 %s）%s", seed, fp.Device.Platform, i)
			}
			severe := SevereIssues(issues)
			// Linux 的 WebGL 后端不匹配是内核已知限制，另有专门测试跟踪，
			// 此处豁免以免掩盖其他新出现的问题。
			severe = dropKnownLinuxWebGLIssue(platform, severe)
			if len(severe) > 0 {
				t.Errorf("种子 %d（声称 %s）存在 %d 项可直接检出的矛盾",
					seed, fp.Device.Platform, len(severe))
			}
		}
	}
}

// dropKnownLinuxWebGLIssue 过滤掉声称 Linux 时的 WebGL 后端矛盾。
func dropKnownLinuxWebGLIssue(platform model.Platform, issues []Issue) []Issue {
	if platform != model.PlatformLinux {
		return issues
	}
	var out []Issue
	for _, i := range issues {
		if i.Check == "WebGL 与系统矛盾" && strings.Contains(i.Detail, "Direct3D") {
			continue
		}
		out = append(out, i)
	}
	return out
}

// 已知缺陷：声称 Linux 时 WebGL 仍报 Windows 的 Direct3D 后端。
//
// 实测内核 148 在 Windows 宿主机上：
//   - 声称 macOS 时 WebGL 正确报 Apple/Metal Renderer，完全自洽
//   - 声称 Linux 时 WebGL 仍报 "... Direct3D11 vs_5_0 ps_5_0, D3D11"
//
// Linux 上不存在 Direct3D，这是真实世界不可能的组合，单一信号即可判定伪造。
// 属内核层面缺陷，无法通过参数规避，唯一可行的规避是不使用 Linux 机型档案。
//
// 本测试记录该缺陷的当前状态：一旦内核修好，它会失败，提示可以解除对
// Linux 机型的限制。
func TestKnownIssueLinuxWebGLBackendMismatch(t *testing.T) {
	k := realKernel(t)
	seeds := seedsByPlatform(t)[model.PlatformLinux]
	if len(seeds) == 0 {
		t.Skip("当前机型档案库未包含 Linux 机型")
	}

	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	_, args := probeProfile(t, seeds[0], geo)
	res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}

	if !strings.Contains(res.UserAgent, "Linux") {
		t.Fatalf("种子 %d 未声称 Linux，UA=%q", seeds[0], res.UserAgent)
	}
	if !strings.Contains(res.WebGLRenderer, "Direct3D") {
		t.Errorf("内核似已修复 Linux 的 WebGL 后端（renderer=%q）；"+
			"可以解除对 Linux 机型档案的限制并删除本测试", res.WebGLRenderer)
	}
	t.Logf("已知缺陷仍存在: UA 声称 Linux 而 WebGL renderer=%q", res.WebGLRenderer)
}

// 不同时区配置下时区与偏移必须始终相容。
func TestRealKernelTimezoneConsistency(t *testing.T) {
	k := realKernel(t)
	zones := []*model.Geo{
		{CountryCode: "US", Timezone: "America/New_York", Locale: "en-US"},
		{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"},
		{CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE"},
		{CountryCode: "SG", Timezone: "Asia/Singapore", Locale: "en-SG"},
	}
	for _, geo := range zones {
		_, args := probeProfile(t, 777, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
		if err != nil {
			t.Fatalf("时区 %s 采集失败: %v", geo.Timezone, err)
		}
		if res.Timezone != geo.Timezone {
			t.Errorf("时区 = %q, 期望 %q", res.Timezone, geo.Timezone)
		}
		if !offsetMatchesZone(res.Timezone, res.TimezoneOffset) {
			t.Errorf("时区 %q 与偏移 %d 矛盾", res.Timezone, res.TimezoneOffset)
		}
		t.Logf("时区 %-20s offset=%-5d lang=%q", res.Timezone, res.TimezoneOffset, res.Language)
	}
}

// CheckIntegrity 必须能抓出各类矛盾，否则它只是个空壳。
func TestCheckIntegrityDetectsContradictions(t *testing.T) {
	winUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
	macUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"

	// 一份自洽的基准结果。
	good := Result{
		UserAgent: winUA, Platform: "Win32",
		HardwareConcurrency: 8, DeviceMemory: 16,
		Language: "en-US", Languages: []string{"en-US", "en;q=0.9"},
		Timezone: "America/Los_Angeles", TimezoneOffset: 420,
		WebGLVendor:   "Google Inc. (Intel)",
		WebGLRenderer: "ANGLE (Intel, Intel(R) UHD Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)",
		Plugins:       []string{"PDF Viewer"},
		UAData:        map[string]any{"platform": "Windows"},
	}
	if issues := SevereIssues(CheckIntegrity(good)); len(issues) > 0 {
		t.Fatalf("自洽结果被误报: %v", issues)
	}

	cases := []struct {
		name   string
		mutate func(*Result)
		want   string
	}{
		{"UA 说 Windows 但 platform 是 Mac", func(r *Result) {
			r.Platform = "MacIntel"
		}, "platform 与 UA 矛盾"},
		{"UA 说 Mac 但 platform 是 Win32", func(r *Result) {
			r.UserAgent = macUA
			r.UAData = map[string]any{"platform": "macOS"}
			r.WebGLRenderer = "ANGLE (Apple, ANGLE Metal Renderer: Apple M2, Unspecified Version)"
			r.WebGLVendor = "Google Inc. (Apple)"
		}, "platform 与 UA 矛盾"},
		{"Windows 报 Apple GPU", func(r *Result) {
			r.WebGLVendor = "Google Inc. (Apple)"
			r.WebGLRenderer = "ANGLE (Apple, ANGLE Metal Renderer: Apple M2, Unspecified Version)"
		}, "WebGL 与系统矛盾"},
		{"userAgentData 与 UA 矛盾", func(r *Result) {
			r.UAData = map[string]any{"platform": "macOS"}
		}, "userAgentData 与 UA 矛盾"},
		{"软件渲染", func(r *Result) {
			r.WebGLRenderer = "ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device), SwiftShader driver)"
		}, "软件渲染"},
		{"webdriver 暴露", func(r *Result) {
			r.Webdriver = true
		}, "自动化特征"},
		{"deviceMemory 非 2 的幂", func(r *Result) {
			r.DeviceMemory = 6
		}, "deviceMemory 非法"},
		{"核数为 0", func(r *Result) {
			r.HardwareConcurrency = 0
		}, "核数非法"},
		{"时区与偏移矛盾", func(r *Result) {
			r.Timezone = "Asia/Tokyo" // 应为 -540
		}, "时区与偏移矛盾"},
		{"UA 残留 HeadlessChrome", func(r *Result) {
			r.UserAgent = strings.Replace(winUA, "Chrome/148", "HeadlessChrome/148", 1)
		}, "UA 残留"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := good
			// 切片与 map 是引用类型，需要复制以免污染基准。
			r.Languages = append([]string(nil), good.Languages...)
			r.Plugins = append([]string(nil), good.Plugins...)
			r.UAData = map[string]any{"platform": "Windows"}
			c.mutate(&r)

			issues := CheckIntegrity(r)
			var found bool
			for _, i := range issues {
				if i.Check == c.want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("期望检出 %q，实际检出 %v", c.want, issues)
			}
		})
	}
}

// 语言不一致与插件缺失只算可疑，不该判为可直接检出——
// 把弱信号提到最高级别会让严重问题被噪音掩盖。
func TestCheckIntegrityGradesSeverity(t *testing.T) {
	r := Result{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
		Platform:  "Win32", HardwareConcurrency: 8, DeviceMemory: 8,
		Language: "en-US", Languages: []string{"ja-JP"}, // 不一致
		Timezone: "America/Los_Angeles", TimezoneOffset: 420,
		Plugins: nil, // 缺失
		UAData:  map[string]any{"platform": "Windows"},
	}
	issues := CheckIntegrity(r)
	if len(SevereIssues(issues)) > 0 {
		t.Errorf("弱信号不应判为可直接检出: %v", SevereIssues(issues))
	}
	if len(issues) < 2 {
		t.Errorf("期望检出语言不一致与插件缺失，实际 %v", issues)
	}
}

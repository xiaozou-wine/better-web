package launcher

import (
	"slices"
	"strings"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
)

func testFingerprint() *model.Fingerprint {
	fp := fingerprint.Derive(1234, &model.Geo{
		CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US",
	})
	return &fp
}

func hasFlag(args []string, flag string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == flag || strings.HasPrefix(a, flag+"=")
	})
}

func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	t.Fatalf("参数列表中找不到 %s: %v", flag, args)
	return ""
}

// 日常模式绝不能注入指纹参数，否则日常浏览会跑在伪造环境上。
func TestDailyProfileHasNoFingerprintFlags(t *testing.T) {
	p := &model.Profile{Name: "日常", Kind: model.KindDaily, ProfileDir: `C:\p\daily`}
	args, err := BuildArgs(p, testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	for _, f := range []string{
		flagFingerprint, flagPlatform, flagPlatformVersion, flagBrand,
		flagBrandVersion, flagHardwareConcurrency, flagTimezone, flagLang, flagAcceptLang,
	} {
		if hasFlag(args, f) {
			t.Errorf("日常模式不应包含 %s，实际参数: %v", f, args)
		}
	}
	if got := flagValue(t, args, flagUserDataDir); got != `C:\p\daily` {
		t.Errorf("user-data-dir = %q", got)
	}
}

func TestFingerprintProfileEmitsAllFlags(t *testing.T) {
	fp := testFingerprint()
	p := &model.Profile{Name: "fp1", Kind: model.KindFingerprint, ProfileDir: `C:\p\fp1`, Seed: fp.Seed}
	args, err := BuildArgs(p, fp, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if got := flagValue(t, args, flagFingerprint); got != "1234" {
		t.Errorf("fingerprint = %q, 期望 1234", got)
	}
	if got := flagValue(t, args, flagPlatform); got != string(fp.Device.Platform) {
		t.Errorf("platform = %q, 期望 %q", got, fp.Device.Platform)
	}
	if got := flagValue(t, args, flagTimezone); got != "America/Los_Angeles" {
		t.Errorf("timezone = %q", got)
	}
	if got := flagValue(t, args, flagLang); got != "en-US" {
		t.Errorf("lang = %q", got)
	}
	if got := flagValue(t, args, flagAcceptLang); got != "en-US,en;q=0.9" {
		t.Errorf("accept-lang = %q", got)
	}
}

// 配了代理就必须同时关掉非代理 UDP，否则 WebRTC 会泄露真实 IP。
func TestProxyImpliesWebRTCLeakProtection(t *testing.T) {
	fp := testFingerprint()
	p := &model.Profile{Name: "fp2", Kind: model.KindFingerprint, ProfileDir: `C:\p\fp2`, Seed: fp.Seed}
	args, err := BuildArgs(p, fp, "socks5://127.0.0.1:41234", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if got := flagValue(t, args, flagProxyServer); got != "socks5://127.0.0.1:41234" {
		t.Errorf("proxy-server = %q", got)
	}
	if !hasFlag(args, flagDisableNonProxiedUDP) {
		t.Errorf("配了代理却没有 %s，WebRTC 会泄露真实 IP: %v", flagDisableNonProxiedUDP, args)
	}
}

func TestNoProxyOmitsProxyFlags(t *testing.T) {
	p := &model.Profile{Name: "d", Kind: model.KindDaily, ProfileDir: `C:\p\d`}
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if hasFlag(args, flagProxyServer) || hasFlag(args, flagDisableNonProxiedUDP) {
		t.Errorf("未配代理却出现代理参数: %v", args)
	}
}

func TestBuildArgsRejectsInvalidInput(t *testing.T) {
	fp := testFingerprint()
	cases := []struct {
		name string
		p    *model.Profile
		fp   *model.Fingerprint
	}{
		{"profile 为 nil", nil, fp},
		{"缺少 profileDir", &model.Profile{Name: "x", Kind: model.KindDaily}, fp},
		{"类型无效", &model.Profile{Name: "x", Kind: "bogus", ProfileDir: `C:\p\x`}, fp},
		{"指纹模式缺指纹", &model.Profile{Name: "x", Kind: model.KindFingerprint, ProfileDir: `C:\p\x`}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BuildArgs(c.p, c.fp, "", nil); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

// 缺时区或语言时必须报错而不是静默放行，静默放行会产出与出口 IP 矛盾的环境。
func TestFingerprintArgsRejectIncoherentFingerprint(t *testing.T) {
	base := testFingerprint()
	cases := map[string]func(*model.Fingerprint){
		"种子为 0":      func(f *model.Fingerprint) { f.Seed = 0 },
		"缺 platform": func(f *model.Fingerprint) { f.Device.Platform = "" },
		"缺时区":        func(f *model.Fingerprint) { f.Timezone = "" },
		"缺语言":        func(f *model.Fingerprint) { f.Locale = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fp := *base
			mutate(&fp)
			p := &model.Profile{Name: "x", Kind: model.KindFingerprint, ProfileDir: `C:\p\x`}
			if _, err := BuildArgs(p, &fp, "", nil); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

func TestExtraArgsAppendedLast(t *testing.T) {
	p := &model.Profile{
		Name: "d", Kind: model.KindDaily, ProfileDir: `C:\p\d`,
		ExtraArgs: []string{"--start-maximized"},
	}
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if args[len(args)-1] != "--start-maximized" {
		t.Errorf("ExtraArgs 未追加到末尾: %v", args)
	}
}

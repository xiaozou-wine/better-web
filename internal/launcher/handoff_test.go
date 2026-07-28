package launcher

import (
	"slices"
	"testing"

	"better-web/internal/model"
)

func dailyProfile() *model.Profile {
	return &model.Profile{Name: "日常", Kind: model.KindDaily, ProfileDir: `C:\p\daily`}
}

// TestBuildArgsAppendsExtraURLsLast 钉住附加 URL 在参数末尾。
//
// Chromium 把不以 - 开头的尾部参数当作要打开的 URL，夹在开关之间会被
// 解析成开关的值——那时表现是页面不打开而某个开关拿到了奇怪的值。
func TestBuildArgsAppendsExtraURLsLast(t *testing.T) {
	p := dailyProfile()
	p.ExtraArgs = []string{"--some-flag"}

	args, err := BuildArgs(p, nil, "", &Options{URLs: []string{"https://example.com/x"}})
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if got := args[len(args)-1]; got != "https://example.com/x" {
		t.Errorf("最后一个参数 = %q, 期望是附加的 URL", got)
	}
}

// TestBuildArgsExtraURLsAfterStartupURLs 钉住两类 URL 的先后顺序。
//
// profile 自己配的启动页在前、外部传入的在后：这样最后聚焦的标签页是
// 用户刚点开的那个链接，而不是启动页。
func TestBuildArgsExtraURLsAfterStartupURLs(t *testing.T) {
	p := dailyProfile()
	p.Startup = &model.Startup{
		Mode: model.StartupURLs,
		URLs: []string{"https://home.example"},
	}

	args, err := BuildArgs(p, nil, "", &Options{URLs: []string{"https://clicked.example"}})
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	home := slices.Index(args, "https://home.example")
	clicked := slices.Index(args, "https://clicked.example")
	if home < 0 || clicked < 0 {
		t.Fatalf("两个 URL 都应出现: %v", args)
	}
	if home > clicked {
		t.Errorf("启动页应在附加 URL 之前: %v", args)
	}
}

// TestBuildArgsNilOptionsUnchanged 钉住 opt 为 nil 时行为不变。
//
// 现有调用点全部传 nil，这条保证本次改动没有改变它们的行为。
func TestBuildArgsNilOptionsUnchanged(t *testing.T) {
	args, err := BuildArgs(dailyProfile(), nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if hasFlag(args, flagIncognito) {
		t.Errorf("opt 为 nil 时不应出现 %s: %v", flagIncognito, args)
	}
	if len(args) != 1 || !hasFlag(args, flagUserDataDir) {
		t.Errorf("日常模式无附加项时只该有 user-data-dir: %v", args)
	}
}

func TestBuildArgsIncognitoOnDaily(t *testing.T) {
	args, err := BuildArgs(dailyProfile(), nil, "", &Options{
		URLs: []string{"https://example.com"}, Incognito: true,
	})
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if !hasFlag(args, flagIncognito) {
		t.Errorf("日常模式开无痕时应有 %s: %v", flagIncognito, args)
	}
}

// TestBuildArgsIncognitoIgnoredOnFingerprint 钉住指纹模式不放行无痕。
//
// 无痕不落盘，但指纹伪造与代理照旧生效——用户以为"无痕更干净"，
// 实际上出口 IP 与指纹一个字没变，只是丢掉了养号要留的 Cookie。
func TestBuildArgsIncognitoIgnoredOnFingerprint(t *testing.T) {
	p := &model.Profile{
		Name: "指纹", Kind: model.KindFingerprint, ProfileDir: `C:\p\fp`, Seed: 1234,
	}
	args, err := BuildArgs(p, testFingerprint(), "", &Options{
		URLs: []string{"https://example.com"}, Incognito: true,
	})
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if hasFlag(args, flagIncognito) {
		t.Errorf("指纹模式不该出现 %s: %v", flagIncognito, args)
	}
}

func TestHandoffArgsUsesNewWindow(t *testing.T) {
	args, err := HandoffArgs(dailyProfile(), &Options{URLs: []string{"https://example.com/a"}})
	if err != nil {
		t.Fatalf("HandoffArgs 失败: %v", err)
	}
	if !hasFlag(args, flagNewWindow) {
		t.Errorf("递送时应有 %s: %v", flagNewWindow, args)
	}
	if flagValue(t, args, flagUserDataDir) != `C:\p\daily` {
		t.Errorf("user-data-dir 不对: %v", args)
	}
	if got := args[len(args)-1]; got != "https://example.com/a" {
		t.Errorf("URL 应在末尾, 得到 %q", got)
	}
}

// TestHandoffArgsNoProxyOrFingerprint 钉住递送路径不带代理与指纹参数。
//
// 递送时那些开关会被已运行实例忽略——它的代理和指纹在自己启动时就定好了。
// 传过去只会让人误以为生效，留下错误的排障线索。
func TestHandoffArgsNoProxyOrFingerprint(t *testing.T) {
	p := dailyProfile()
	p.Proxy = &model.Proxy{Scheme: model.ProxySOCKS5, Host: "1.2.3.4", Port: 1080}
	p.ExtraArgs = []string{"--should-not-appear"}

	args, err := HandoffArgs(p, &Options{URLs: []string{"https://example.com"}})
	if err != nil {
		t.Fatalf("HandoffArgs 失败: %v", err)
	}
	for _, unwanted := range []string{
		flagProxyServer, flagFingerprint, flagTimezone, flagLang,
		flagDisableNonProxiedUDP, "--should-not-appear",
	} {
		if hasFlag(args, unwanted) {
			t.Errorf("递送参数不该含 %s: %v", unwanted, args)
		}
	}
}

// TestHandoffArgsIncognitoReplacesNewWindow 钉住两个开关不同时出现。
//
// --incognito 本身就开一个新的无痕窗口。两个都传时 Chromium 会开一个
// 普通新窗口再开一个无痕窗口，用户看到两个窗口。
func TestHandoffArgsIncognitoReplacesNewWindow(t *testing.T) {
	args, err := HandoffArgs(dailyProfile(), &Options{
		URLs: []string{"https://example.com"}, Incognito: true,
	})
	if err != nil {
		t.Fatalf("HandoffArgs 失败: %v", err)
	}
	if !hasFlag(args, flagIncognito) {
		t.Errorf("应有 %s: %v", flagIncognito, args)
	}
	if hasFlag(args, flagNewWindow) {
		t.Errorf("有 %s 时不该再有 %s: %v", flagIncognito, flagNewWindow, args)
	}
}

func TestHandoffArgsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		p    *model.Profile
		opt  *Options
	}{
		{"profile 为空", nil, &Options{URLs: []string{"https://x.example"}}},
		{"缺少 user-data-dir", &model.Profile{Name: "x", Kind: model.KindDaily}, &Options{URLs: []string{"https://x.example"}}},
		{"opt 为 nil", dailyProfile(), nil},
		{"没有 URL", dailyProfile(), &Options{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := HandoffArgs(c.p, c.opt); err == nil {
				t.Error("应当报错")
			}
		})
	}
}

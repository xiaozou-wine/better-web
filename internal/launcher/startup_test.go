package launcher

import (
	"slices"
	"strings"
	"testing"

	"better-web/internal/model"
)

func startupProfile(st *model.Startup) *model.Profile {
	return &model.Profile{
		Name: "启动页", Kind: model.KindDaily, ProfileDir: `C:\p\s`, Startup: st,
	}
}

// 启动 URL 必须在参数列表最末。
//
// Chromium 把不以 - 开头的尾部参数当作要打开的 URL；夹在开关中间时，
// 它会被解析成前一个开关的值，表现为"启动页没生效且某个开关行为异常"。
func TestStartupURLsComeLast(t *testing.T) {
	p := startupProfile(&model.Startup{
		Mode: model.StartupURLs,
		URLs: []string{"https://a.example", "https://b.example"},
	})
	p.ExtraArgs = []string{"--start-maximized"}

	args, err := BuildArgs(p, nil, "socks5://127.0.0.1:1080", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if len(args) < 2 {
		t.Fatalf("参数过少: %v", args)
	}
	tail := args[len(args)-2:]
	if tail[0] != "https://a.example" || tail[1] != "https://b.example" {
		t.Errorf("末尾两项 = %v, 期望两个启动 URL", tail)
	}
	// 顺序也要保留：用户填的先后决定标签页顺序。
	if idx := slices.Index(args, "--start-maximized"); idx < 0 ||
		idx > len(args)-3 {
		t.Errorf("ExtraArgs 应在启动 URL 之前: %v", args)
	}
}

// 非 URLs 模式不该传任何 URL，否则残留配置会意外打开页面。
func TestNonURLModePassesNoURLs(t *testing.T) {
	// 刻意留着 URLs，模拟用户切换模式但没清空输入框的情况。
	p := startupProfile(&model.Startup{
		Mode: model.StartupNewTab,
		URLs: []string{"https://should-not-open.example"},
	})
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	for _, a := range args {
		if strings.Contains(a, "should-not-open") {
			t.Errorf("新标签页模式下仍传了启动 URL: %v", args)
		}
	}
}

func TestCustomNewTabURL(t *testing.T) {
	p := startupProfile(&model.Startup{
		Mode: model.StartupNewTab, NewTabURL: "https://ntp.example",
	})
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if !slices.Contains(args, "--custom-ntp=https://ntp.example") {
		t.Errorf("缺少 --custom-ntp: %v", args)
	}
}

// 新标签页与启动页是两套独立设置，可同时生效。
func TestNewTabAndStartupURLsCoexist(t *testing.T) {
	p := startupProfile(&model.Startup{
		Mode:      model.StartupURLs,
		URLs:      []string{"https://start.example"},
		NewTabURL: "https://ntp.example",
	})
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if !slices.Contains(args, "--custom-ntp=https://ntp.example") {
		t.Errorf("缺少 --custom-ntp: %v", args)
	}
	if args[len(args)-1] != "https://start.example" {
		t.Errorf("启动 URL 不在末尾: %v", args)
	}
}

func TestNoStartupConfigAddsNothing(t *testing.T) {
	p := startupProfile(nil)
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("nil 启动配置应按默认处理: %v", err)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--custom-ntp") || !strings.HasPrefix(a, "-") {
			t.Errorf("未配置启动页时不该出现 %q: %v", a, args)
		}
	}
}

func TestInvalidStartupModeRejected(t *testing.T) {
	p := startupProfile(&model.Startup{Mode: "bogus"})
	if _, err := BuildArgs(p, nil, "", nil); err == nil {
		t.Error("无效启动模式应报错")
	}
}

// URL 列表要去空白去重，避免重复标签页。
func TestStartupURLsNormalized(t *testing.T) {
	p := startupProfile(&model.Startup{
		Mode: model.StartupURLs,
		URLs: []string{"  https://a.example  ", "", "https://a.example", "https://b.example"},
	})
	args, err := BuildArgs(p, nil, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	urls := args[len(args)-2:]
	if urls[0] != "https://a.example" || urls[1] != "https://b.example" {
		t.Errorf("规范化后的 URL = %v", urls)
	}
}

func TestStartupURLListRespectsMode(t *testing.T) {
	urls := []string{"https://a.example"}
	if got := (&model.Startup{Mode: model.StartupURLs, URLs: urls}).StartupURLList(); len(got) != 1 {
		t.Errorf("URLs 模式应返回 URL, 实际 %v", got)
	}
	if got := (&model.Startup{Mode: model.StartupNewTab, URLs: urls}).StartupURLList(); got != nil {
		t.Errorf("新标签页模式应返回 nil, 实际 %v", got)
	}
	var nilStartup *model.Startup
	if got := nilStartup.StartupURLList(); got != nil {
		t.Errorf("nil 配置应返回 nil, 实际 %v", got)
	}
}

func TestStartupModeValidation(t *testing.T) {
	for _, m := range append(model.StartupModes(), "") {
		if !m.Valid() {
			t.Errorf("%q 应为有效模式", m)
		}
	}
	if model.StartupMode("bogus").Valid() {
		t.Error("未知模式不应通过校验")
	}
}

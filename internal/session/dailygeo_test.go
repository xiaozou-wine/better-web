package session

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"
)

// 日常模式下出口地探测失败不得阻断启动。
//
// 与 TestStrictGeoAbortsWhenLookupFails 的区别：那里是指纹模式，探测结果会
// 变成 --timezone / --lang，拿不到就无法与出口对齐，继续启动等于制造矛盾。
// 而日常模式不注入任何地理参数（见 launcher.BuildArgs），时区来自本机，
// 探测只为附加一条"出口是否机房 IP"的提示——为拿不到提示而拒绝启动不合理。
func TestDailyProfileStartsWhenExitProbeFails(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)
	// 必然连不通的端口，迫使出口地探测失败。
	p.Proxy = &model.Proxy{
		Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1,
		Username: "u", Password: "p",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	st, err := m.Start(ctx, p)
	if err != nil {
		t.Fatalf("日常模式因出口探测失败而无法启动: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	if st.State != StateRunning {
		t.Errorf("状态 = %q, 期望 running", st.State)
	}
	// 探测失败，出口画像自然为空，但必须留下一条警告说明为何没有出口信息。
	if st.Exit != nil {
		t.Errorf("探测失败却带回了出口画像: %+v", st.Exit)
	}
	if len(st.Warnings) == 0 {
		t.Fatal("探测失败时未产生警告，用户无从知晓出口位置未确认")
	}
	if !strings.Contains(strings.Join(st.Warnings, "\n"), "未能确认代理出口位置") {
		t.Errorf("警告内容未说明出口位置未确认: %v", st.Warnings)
	}
}

// 探测失败后启动的日常实例仍不得带地理参数——这是上面那条降级逻辑成立的前提。
// 若哪天日常模式开始注入时区，降级就变成"用本机时区配外国出口"的矛盾，
// 这条断言会失败以提醒同步修改 resolveGeo。
// 与 TestStartDailyProfile 的区别：那里没有代理，走的是另一条分支。
func TestDailyProfileInjectsNoGeoFlagsAfterProbeFailure(t *testing.T) {
	m, argsFile := newTestManager(t)
	p := newProfile(t, model.KindDaily)
	p.Proxy = &model.Proxy{
		Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1,
		Username: "u", Password: "p",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := m.Start(ctx, p); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	args := waitForFile(t, argsFile, 10*time.Second)
	for _, forbidden := range []string{"--timezone=", "--lang=", "--accept-lang=", "--fingerprint="} {
		if slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, forbidden) }) {
			t.Errorf("日常模式注入了地理参数 %s: %v", forbidden, args)
		}
	}
}

package session

import (
	"context"
	"testing"
	"time"

	"better-web/internal/model"
)

// 指定了地理信息时，出口探测失败不得阻断启动。
//
// 与 TestStrictGeoAbortsWhenLookupFails 的区别：那里没有 GeoOverride，
// 探测失败意味着时区无从确定，继续启动会造成时区与出口矛盾，所以必须中止。
// 而这里地理信息已由用户给定，启动前提已经满足，探测只是为了附加的风险提示，
// 失败了降级即可——为拿不到一条提示而拒绝启动是不合理的。
func TestGeoOverrideStartsEvenWhenExitProbeFails(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindFingerprint)
	p.GeoOverride = &model.Geo{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"}
	// 必然连不通的端口，迫使出口探测失败。
	p.Proxy = &model.Proxy{
		Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1,
		Username: "u", Password: "p",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	st, err := m.Start(ctx, p)
	if err != nil {
		t.Fatalf("指定地理信息后仍因探测失败而无法启动: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	if st.State != StateRunning {
		t.Errorf("状态 = %q, 期望 running", st.State)
	}
	// 用户指定的地理信息必须生效，不能被探测流程覆盖。
	if st.Geo == nil || st.Geo.Timezone != "Asia/Tokyo" {
		t.Errorf("会话地理信息 = %+v, 期望时区 Asia/Tokyo", st.Geo)
	}
	// 探测失败，所以没有出口画像可报——但这不影响启动。
	if st.Exit != nil {
		t.Errorf("探测失败却带回了出口画像: %+v", st.Exit)
	}
}

// StrictGeo 只约束"地理信息无从确定"这一种情况。
// 指定了 GeoOverride 后，即使 StrictGeo 为 true 也应当能启动。
func TestGeoOverrideBypassesStrictGeo(t *testing.T) {
	m, _ := newTestManager(t)
	if !m.StrictGeo {
		t.Fatal("测试前提是 StrictGeo 默认开启")
	}
	p := newProfile(t, model.KindFingerprint)
	p.GeoOverride = &model.Geo{CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE"}
	p.Proxy = &model.Proxy{Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := m.Start(ctx, p); err != nil {
		t.Fatalf("StrictGeo 下指定地理信息仍无法启动: %v", err)
	}
	_ = m.Stop(p.ID)
}

// 无代理且指定地理信息时不应尝试探测，直接采用指定值。
func TestGeoOverrideWithoutProxySkipsProbe(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindFingerprint)
	p.GeoOverride = &model.Geo{CountryCode: "SG", Timezone: "Asia/Singapore", Locale: "en-SG"}

	st, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	if st.Geo == nil || st.Geo.Timezone != "Asia/Singapore" {
		t.Errorf("会话地理信息 = %+v", st.Geo)
	}
	if st.Exit != nil {
		t.Errorf("无代理时不应有出口画像: %+v", st.Exit)
	}
	if len(st.Warnings) != 0 {
		t.Errorf("无代理时不应有出口警告: %v", st.Warnings)
	}
}

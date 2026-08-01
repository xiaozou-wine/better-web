package app

import (
	"testing"

	"better-web/internal/model"
)

// 运行中的会话指纹必须盖过按 GeoOverride 的推导结果。
//
// 这是本函数存在的理由：没有 GeoOverride 的 profile 推导出的是 en-US 兜底值，
// 而实际启动时时区语言按代理出口 IP 对齐。德国出口的 profile 界面显示 en-US、
// 浏览器里是德语，用户会误判成指纹没生效。
func TestDisplayFingerprintPrefersLiveSession(t *testing.T) {
	p := &model.Profile{Kind: model.KindFingerprint, Seed: 12345}
	live := &model.Fingerprint{
		Seed: 12345, Timezone: "Europe/Berlin", Locale: "de-DE",
	}

	got, src := displayFingerprint(p, live)
	if got == nil {
		t.Fatal("指纹模式应返回指纹")
	}
	if got.Locale != "de-DE" {
		t.Errorf("语言 = %q, 期望 de-DE（会话实际值）", got.Locale)
	}
	if got.Timezone != "Europe/Berlin" {
		t.Errorf("时区 = %q, 期望 Europe/Berlin（会话实际值）", got.Timezone)
	}
	if src != GeoSourceLive {
		t.Errorf("来源 = %q, 期望 live", src)
	}
}

// 无运行会话时退回推导，且不得残留上一次会话的值。
func TestDisplayFingerprintFallsBackWhenStopped(t *testing.T) {
	p := &model.Profile{Kind: model.KindFingerprint, Seed: 12345}

	got, src := displayFingerprint(p, nil)
	if got == nil {
		t.Fatal("指纹模式应返回指纹预览")
	}
	// 从未启动过且未设 GeoOverride，推导只能走兜底。
	if got.Locale != "en-US" {
		t.Errorf("语言 = %q, 期望兜底值 en-US", got.Locale)
	}
	if got.Seed != 12345 {
		t.Errorf("种子 = %d, 期望 12345", got.Seed)
	}
	// 来源必须标成 default，界面才能说明"真实出口未知"而非当作事实。
	if src != GeoSourceDefault {
		t.Errorf("来源 = %q, 期望 default", src)
	}
}

// 停止态必须用上次实测到的出口地理，而非内核兜底值。
//
// 这是用户报的原始现象：德国代理的 profile 停止后详情显示 en-US /
// America/New_York，而浏览器里是德语，看起来像指纹没生效。
func TestDisplayFingerprintUsesLastGeoWhenStopped(t *testing.T) {
	p := &model.Profile{
		Kind: model.KindFingerprint, Seed: 770828460,
		LastGeo: &model.Geo{
			CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE",
		},
	}

	got, src := displayFingerprint(p, nil)
	if got == nil {
		t.Fatal("指纹模式应返回指纹预览")
	}
	if got.Locale != "de-DE" {
		t.Errorf("语言 = %q, 期望 de-DE（上次实测值）", got.Locale)
	}
	if got.Timezone != "Europe/Berlin" {
		t.Errorf("时区 = %q, 期望 Europe/Berlin（上次实测值）", got.Timezone)
	}
	// 必须标成 lastRun 而非 live：代理出口可能已经变了，
	// 界面要能说明这是上次的实测值而非当前事实。
	if src != GeoSourceLastRun {
		t.Errorf("来源 = %q, 期望 lastRun", src)
	}
}

// GeoOverride 优先于 LastGeo：用户手填的是明确意图，实测值只是缓存。
func TestDisplayFingerprintGeoOverrideBeatsLastGeo(t *testing.T) {
	p := &model.Profile{
		Kind: model.KindFingerprint, Seed: 12345,
		GeoOverride: &model.Geo{
			CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP",
		},
		LastGeo: &model.Geo{
			CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE",
		},
	}

	got, src := displayFingerprint(p, nil)
	if got.Locale != "ja-JP" {
		t.Errorf("语言 = %q, 期望 ja-JP（GeoOverride 优先）", got.Locale)
	}
	if src != GeoSourceOverride {
		t.Errorf("来源 = %q, 期望 override", src)
	}
}

// 设了 GeoOverride 且未运行时，推导必须采用覆盖值而非兜底值。
func TestDisplayFingerprintUsesGeoOverrideWhenStopped(t *testing.T) {
	p := &model.Profile{
		Kind: model.KindFingerprint, Seed: 12345,
		GeoOverride: &model.Geo{
			CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP",
		},
	}

	got, _ := displayFingerprint(p, nil)
	if got == nil {
		t.Fatal("指纹模式应返回指纹预览")
	}
	if got.Locale != "ja-JP" {
		t.Errorf("语言 = %q, 期望 ja-JP（GeoOverride 值）", got.Locale)
	}
	if got.Timezone != "Asia/Tokyo" {
		t.Errorf("时区 = %q, 期望 Asia/Tokyo（GeoOverride 值）", got.Timezone)
	}
}

// 日常模式不伪造指纹，两条路径都不得返回指纹。
//
// 返回一份指纹会让界面显示"时区 America/New_York / 语言 en-US"，
// 而日常模式用的是本机真实环境，那两行是凭空捏造的。
func TestDisplayFingerprintNilForDaily(t *testing.T) {
	p := &model.Profile{Kind: model.KindDaily, Seed: 12345}

	if got, _ := displayFingerprint(p, nil); got != nil {
		t.Errorf("日常模式无会话时返回了指纹: %+v", got)
	}
	// 日常模式的会话本不该带指纹，但即便带了也不能透出去。
	live := &model.Fingerprint{Seed: 12345, Locale: "de-DE"}
	if got, _ := displayFingerprint(p, live); got != nil {
		t.Errorf("日常模式带会话时返回了指纹: %+v", got)
	}
}

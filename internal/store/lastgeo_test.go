package store

import (
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

func openTempStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func saveProfile(t *testing.T, s *Store, p *model.Profile) {
	t.Helper()
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
}

// LastGeo 必须能存进库并原样读回。
//
// 新增列时 selectColumns 与 scanProfile 的顺序必须同步改，错位不会报错——
// SQLite 是动态类型，字符串会被塞进任何列。本测试守住这条。
func TestLastGeoRoundTrip(t *testing.T) {
	s := openTempStore(t)
	p := &model.Profile{
		ID: "p1", Name: "德国出口", Kind: model.KindFingerprint,
		Seed: 770828460, ProfileDir: t.TempDir(),
		LastGeo: &model.Geo{
			CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE",
		},
	}
	saveProfile(t, s, p)

	got, err := s.Get("p1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.LastGeo == nil {
		t.Fatal("LastGeo 丢失")
	}
	if got.LastGeo.Locale != "de-DE" {
		t.Errorf("语言 = %q, 期望 de-DE", got.LastGeo.Locale)
	}
	if got.LastGeo.Timezone != "Europe/Berlin" {
		t.Errorf("时区 = %q, 期望 Europe/Berlin", got.LastGeo.Timezone)
	}
	if got.LastGeo.CountryCode != "DE" {
		t.Errorf("国家码 = %q, 期望 DE", got.LastGeo.CountryCode)
	}

	// 顺带确认相邻字段没被错位写坏。
	if got.Name != "德国出口" || got.Seed != 770828460 {
		t.Errorf("相邻字段错位: name=%q seed=%d", got.Name, got.Seed)
	}
	if got.DeviceLabel != "" || got.UseSystemBrowser {
		t.Errorf("相邻字段错位: deviceLabel=%q useSystemBrowser=%v",
			got.DeviceLabel, got.UseSystemBrowser)
	}
}

// 没启动过的 profile 其 LastGeo 应为 nil，而非零值 Geo。
//
// 零值 Geo 会让界面显示空白时区与空白语言，比显示兜底值更糟——
// 而 displayFingerprint 判的是 nil，零值指针会绕过那个判断。
func TestLastGeoNilWhenNeverStarted(t *testing.T) {
	s := openTempStore(t)
	saveProfile(t, s, &model.Profile{
		ID: "p2", Name: "没启动过", Kind: model.KindFingerprint,
		Seed: 123, ProfileDir: t.TempDir(),
	})

	got, err := s.Get("p2")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.LastGeo != nil {
		t.Errorf("没启动过的 profile 带了 LastGeo: %+v", got.LastGeo)
	}
}

// TouchLastGeo 只改这一列，不得碰其他字段。
func TestTouchLastGeoLeavesOtherFields(t *testing.T) {
	s := openTempStore(t)
	saveProfile(t, s, &model.Profile{
		ID: "p3", Name: "原名", Kind: model.KindFingerprint,
		Seed: 999, ProfileDir: t.TempDir(),
		Notes: "原备注", Group: "原分组", DeviceLabel: "Windows 10 / RTX 2060 桌面",
	})

	g := &model.Geo{CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE"}
	if err := s.TouchLastGeo("p3", g); err != nil {
		t.Fatalf("TouchLastGeo 失败: %v", err)
	}

	got, err := s.Get("p3")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.LastGeo == nil || got.LastGeo.Locale != "de-DE" {
		t.Errorf("LastGeo 未写入: %+v", got.LastGeo)
	}
	if got.Name != "原名" || got.Notes != "原备注" || got.Group != "原分组" {
		t.Errorf("TouchLastGeo 改动了其他字段: name=%q notes=%q grp=%q",
			got.Name, got.Notes, got.Group)
	}
	if got.DeviceLabel != "Windows 10 / RTX 2060 桌面" {
		t.Errorf("机型被改动: %q", got.DeviceLabel)
	}
	if got.Seed != 999 {
		t.Errorf("种子被改动: %d", got.Seed)
	}
}

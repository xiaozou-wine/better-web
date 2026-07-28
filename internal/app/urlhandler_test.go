package app

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/model"
)

func TestURLHandlerDefaultsEmpty(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.URLHandler()
	if err != nil {
		t.Fatalf("URLHandler 失败: %v", err)
	}
	if v.ProfileID != "" || v.Incognito {
		t.Errorf("未配置时应为空: %+v", v)
	}
}

func TestSetURLHandlerRoundTrip(t *testing.T) {
	s, _ := newTestService(t)
	p, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常-01", Kind: model.KindDaily,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	if err := s.SetURLHandler(p.ID, true); err != nil {
		t.Fatalf("SetURLHandler 失败: %v", err)
	}
	v, err := s.URLHandler()
	if err != nil {
		t.Fatalf("URLHandler 失败: %v", err)
	}
	if v.ProfileID != p.ID {
		t.Errorf("ProfileID = %q, 期望 %q", v.ProfileID, p.ID)
	}
	if v.ProfileName != "日常-01" {
		t.Errorf("ProfileName = %q, 期望 日常-01", v.ProfileName)
	}
	if !v.Incognito {
		t.Error("Incognito 应为 true")
	}
}

// TestSetURLHandlerRejectsIncognitoOnFingerprint 钉住指纹模式拒绝无痕。
//
// 无痕不落盘，但指纹伪造与代理照旧生效——出口 IP 和指纹一个字没变。
// 用户以为"无痕更干净"，实际只是丢掉了养号要留的登录态。这种"以为得到了
// 实际没得到"的误解，与 fail-closed 拦的是同一类问题，因此拒绝而非警告。
func TestSetURLHandlerRejectsIncognitoOnFingerprint(t *testing.T) {
	s, _ := newTestService(t)
	p, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "指纹-01", Kind: model.KindFingerprint,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	err = s.SetURLHandler(p.ID, true)
	if err == nil {
		t.Fatal("指纹模式 + 无痕应被拒绝")
	}
	if !strings.Contains(err.Error(), "无痕") {
		t.Errorf("错误信息应说明原因, 实际: %v", err)
	}

	// 不开无痕时同一个 profile 应当放行——拒绝的是组合，不是指纹模式本身。
	if err := s.SetURLHandler(p.ID, false); err != nil {
		t.Errorf("指纹模式不开无痕应放行, 却报错: %v", err)
	}
}

func TestSetURLHandlerRejectsUnknownProfile(t *testing.T) {
	s, _ := newTestService(t)
	if err := s.SetURLHandler("no-such-id", false); err == nil {
		t.Error("指向不存在的 profile 应报错")
	}
}

// TestSetURLHandlerEmptyClears 钉住传空串等于清除配置。
func TestSetURLHandlerEmptyClears(t *testing.T) {
	s, _ := newTestService(t)
	p, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常-01", Kind: model.KindDaily,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if err := s.SetURLHandler(p.ID, false); err != nil {
		t.Fatalf("SetURLHandler 失败: %v", err)
	}
	if err := s.SetURLHandler("", false); err != nil {
		t.Fatalf("清除失败: %v", err)
	}
	v, _ := s.URLHandler()
	if v.ProfileID != "" {
		t.Errorf("清除后 ProfileID = %q, 期望空", v.ProfileID)
	}
}

// TestURLHandlerDeletedProfileLeavesNameEmpty 钉住目标被删后的表现。
//
// 配置指向一个不存在的 profile 是可恢复的状态，因此留空 ProfileName
// 让界面提示重新选，而不是让整个设置页报错打不开。
func TestURLHandlerDeletedProfileLeavesNameEmpty(t *testing.T) {
	s, _ := newTestService(t)
	p, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常-01", Kind: model.KindDaily,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if err := s.SetURLHandler(p.ID, false); err != nil {
		t.Fatalf("SetURLHandler 失败: %v", err)
	}
	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatalf("DeleteProfile 失败: %v", err)
	}

	v, err := s.URLHandler()
	if err != nil {
		t.Fatalf("目标被删后 URLHandler 不应报错: %v", err)
	}
	if v.ProfileID != p.ID {
		t.Errorf("ProfileID 应保留原值以便界面提示, 得到 %q", v.ProfileID)
	}
	if v.ProfileName != "" {
		t.Errorf("目标已删除, ProfileName 应为空, 得到 %q", v.ProfileName)
	}
}

// TestOpenURLRejectsBadScheme 钉住非 http/https 一律拒绝。
//
// 这是安全边界：注册成默认浏览器后，机器上任何应用都能往这个入口传字符串。
// 校验必须在读取配置之前发生，未配置 profile 也应先拒绝坏 URL。
func TestOpenURLRejectsBadScheme(t *testing.T) {
	s, _ := newTestService(t)
	for _, raw := range []string{
		"file:///C:/Windows/win.ini",
		"javascript:alert(1)",
		"chrome://settings/",
		"",
	} {
		if _, _, err := s.OpenURL(context.Background(), raw); err == nil {
			t.Errorf("OpenURL(%q) 应被拒绝", raw)
		}
	}
}

func TestOpenURLRequiresConfiguredProfile(t *testing.T) {
	s, _ := newTestService(t)
	_, _, err := s.OpenURL(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("未配置目标 profile 时应报错")
	}
	if !strings.Contains(err.Error(), "设置") {
		t.Errorf("错误信息应指引用户去设置, 实际: %v", err)
	}
}

func TestOpenURLReportsDeletedProfile(t *testing.T) {
	s, _ := newTestService(t)
	p, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常-01", Kind: model.KindDaily,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if err := s.SetURLHandler(p.ID, false); err != nil {
		t.Fatalf("SetURLHandler 失败: %v", err)
	}
	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatalf("DeleteProfile 失败: %v", err)
	}

	_, _, err = s.OpenURL(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("目标 profile 已删除时应报错")
	}
	if !strings.Contains(err.Error(), "重新指定") {
		t.Errorf("错误信息应提示重新指定, 实际: %v", err)
	}
}

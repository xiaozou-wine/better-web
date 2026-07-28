package app

import (
	"context"
	"errors"
	"testing"

	"better-web/internal/model"
)

func TestCrossesMajor(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		// 跨大版本：指纹算法会变，须拦。
		{"148.0.7778.215", "152.0.1234.56", true},
		{"142.0.7444.175", "148.0.7778.215", true},
		// 同大版本内的小版本更新是安全补丁，拦下来只会阻止用户打补丁。
		{"148.0.7778.215", "148.0.7999.100", false},
		{"148.0.7778.215", "148.0.7778.215", false},
		// 空值表示跟随默认内核，本身没有锁定，不构成显式切换。
		{"", "148.0.7778.215", false},
		{"148.0.7778.215", "", false},
		{"", "", false},
		// 解析失败时保守放行：宁可漏一次提醒，也不要用解析 bug 把用户锁死。
		{"unknown", "148.0.7778.215", false},
		{"148.0.7778.215", "bogus", false},
	}
	for _, c := range cases {
		if got := crossesMajor(c.from, c.to); got != c.want {
			t.Errorf("crossesMajor(%q, %q) = %v, 期望 %v", c.from, c.to, got, c.want)
		}
	}
}

// 用过的 profile 换内核大版本必须被拦下：同一 seed 在不同大版本上
// 推导出的指纹不同，等同于账号换了设备。
func TestUpdateRejectsKernelDriftOnUsedProfile(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "已用过", Kind: model.KindFingerprint, KernelVersion: "148.0.7778.215",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	// 标记为已使用过。
	markUsed(t, s, v.ID)

	_, err = s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, KernelVersion: "152.0.1234.56",
	})
	var drift *ErrKernelDrift
	if !errors.As(err, &drift) {
		t.Fatalf("期望 ErrKernelDrift，实际: %v", err)
	}
	if drift.From != "148.0.7778.215" || drift.To != "152.0.1234.56" {
		t.Errorf("错误中的版本信息不对: %+v", drift)
	}

	// 内核版本不应被改动。
	got, err := s.GetProfile(v.ID)
	if err != nil {
		t.Fatalf("GetProfile 失败: %v", err)
	}
	if got.KernelVersion != "148.0.7778.215" {
		t.Errorf("被拒绝后内核版本仍被改成了 %q", got.KernelVersion)
	}
}

// 显式确认后应当允许切换：用户可能明知风险仍要升级。
func TestUpdateAllowsKernelChangeWithConfirmation(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "确认切换", Kind: model.KindFingerprint, KernelVersion: "148.0.7778.215",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	markUsed(t, s, v.ID)

	got, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, KernelVersion: "152.0.1234.56",
		ConfirmKernelChange: true,
	})
	if err != nil {
		t.Fatalf("确认后仍失败: %v", err)
	}
	if got.KernelVersion != "152.0.1234.56" {
		t.Errorf("KernelVersion = %q, 期望已切换", got.KernelVersion)
	}
}

// 从未启动过的 profile 还没在任何平台留下痕迹，换内核无害，不该拦。
func TestUpdateAllowsKernelChangeOnUnusedProfile(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "没用过", Kind: model.KindFingerprint, KernelVersion: "148.0.7778.215",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	got, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, KernelVersion: "152.0.1234.56",
	})
	if err != nil {
		t.Fatalf("未使用过的 profile 换内核被拒: %v", err)
	}
	if got.KernelVersion != "152.0.1234.56" {
		t.Errorf("KernelVersion = %q", got.KernelVersion)
	}
}

// 小版本更新不该被拦，否则用户无法打安全补丁。
func TestUpdateAllowsMinorKernelUpdate(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "打补丁", Kind: model.KindFingerprint, KernelVersion: "148.0.7778.215",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	markUsed(t, s, v.ID)

	if _, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, KernelVersion: "148.0.7999.100",
	}); err != nil {
		t.Fatalf("同大版本内更新被拒: %v", err)
	}
}

// 日常模式不做指纹伪造，也就没有漂移一说，不该拦。
func TestUpdateAllowsKernelChangeForDailyProfile(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "日常", Kind: model.KindDaily, KernelVersion: "148.0.7778.215",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	markUsed(t, s, v.ID)

	if _, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, KernelVersion: "152.0.1234.56",
	}); err != nil {
		t.Fatalf("日常模式换内核被拒: %v", err)
	}
}

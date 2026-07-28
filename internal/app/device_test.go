package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
)

// firstCatalogLabel 返回档案库中第一个机型的标签，用于测试锁定。
func firstCatalogLabel(t *testing.T) string {
	t.Helper()
	catalog := fingerprint.Catalog()
	if len(catalog) == 0 {
		t.Fatal("机型档案库为空")
	}
	return catalog[0].Label
}

// 锁定机型后，推导出的指纹必须用该机型而非种子抽取的那个。
func TestCreateProfileWithLockedDevice(t *testing.T) {
	s, _ := newTestService(t)
	label := firstCatalogLabel(t)

	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "锁定机型", Kind: model.KindFingerprint, DeviceLabel: label,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.DeviceLabel != label {
		t.Errorf("DeviceLabel = %q, 期望 %q", v.DeviceLabel, label)
	}
	if v.Fingerprint == nil {
		t.Fatal("缺少指纹预览")
	}
	if v.Fingerprint.Device.Label != label {
		t.Errorf("指纹机型 = %q, 期望锁定的 %q", v.Fingerprint.Device.Label, label)
	}
}

// 不存在的机型标签必须报错。
// 静默接受会让用户以为锁定成功，实际启动时回退成按种子抽取。
func TestCreateProfileRejectsUnknownDevice(t *testing.T) {
	s, _ := newTestService(t)
	_, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "坏机型", Kind: model.KindFingerprint, DeviceLabel: "不存在的机型",
	})
	if err == nil {
		t.Error("不存在的机型标签应报错")
	}
}

// 未锁定时机型由种子抽取，预览里应有机型但 DeviceLabel 为空。
func TestCreateProfileWithoutLockUsesSeedDevice(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "自动机型", Kind: model.KindFingerprint,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.DeviceLabel != "" {
		t.Errorf("未锁定时 DeviceLabel 应为空, 实际 %q", v.DeviceLabel)
	}
	if v.Fingerprint == nil || v.Fingerprint.Device.Label == "" {
		t.Error("仍应推导出一个机型")
	}
}

// 给用过的 profile 换机型需要显式确认——等同于账号换设备。
func TestUpdateRejectsDeviceChangeOnUsedProfile(t *testing.T) {
	s, _ := newTestService(t)
	catalog := fingerprint.Catalog()
	if len(catalog) < 2 {
		t.Skip("档案库不足两个机型，无法测试切换")
	}

	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "已用过", Kind: model.KindFingerprint, DeviceLabel: catalog[0].Label,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	// 标记为已使用。
	if err := s.store.TouchLastUse(v.ID, time.Now()); err != nil {
		t.Fatalf("TouchLastUse 失败: %v", err)
	}

	_, err = s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, DeviceLabel: catalog[1].Label,
	})
	var drift *ErrDeviceDrift
	if !errors.As(err, &drift) {
		t.Fatalf("期望 ErrDeviceDrift, 实际 %v", err)
	}
	// 错误信息要说清从哪个换到哪个，用户才能判断要不要确认。
	if drift.From != catalog[0].Label || drift.To != catalog[1].Label {
		t.Errorf("漂移信息 = %q -> %q", drift.From, drift.To)
	}

	// 带上确认后应当放行。
	updated, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, DeviceLabel: catalog[1].Label,
		ConfirmDeviceChange: true,
	})
	if err != nil {
		t.Fatalf("确认后仍失败: %v", err)
	}
	if updated.DeviceLabel != catalog[1].Label {
		t.Errorf("DeviceLabel = %q, 期望 %q", updated.DeviceLabel, catalog[1].Label)
	}
}

// 从未启动过的 profile 换机型无需确认：还没在任何平台留下痕迹。
func TestUpdateAllowsDeviceChangeOnUnusedProfile(t *testing.T) {
	s, _ := newTestService(t)
	catalog := fingerprint.Catalog()
	if len(catalog) < 2 {
		t.Skip("档案库不足两个机型")
	}

	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "没用过", Kind: model.KindFingerprint, DeviceLabel: catalog[0].Label,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	got, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: v.Name, DeviceLabel: catalog[1].Label,
	})
	if err != nil {
		t.Fatalf("未用过的 profile 换机型不应报错: %v", err)
	}
	if got.DeviceLabel != catalog[1].Label {
		t.Errorf("DeviceLabel = %q", got.DeviceLabel)
	}
}

// 锁定的机型在库里持久化，重新读出后仍生效。
func TestDeviceLabelPersists(t *testing.T) {
	s, _ := newTestService(t)
	label := firstCatalogLabel(t)

	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "持久化", Kind: model.KindFingerprint, DeviceLabel: label,
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	got, err := s.GetProfile(v.ID)
	if err != nil {
		t.Fatalf("GetProfile 失败: %v", err)
	}
	if got.DeviceLabel != label {
		t.Errorf("读回的 DeviceLabel = %q, 期望 %q", got.DeviceLabel, label)
	}
	if got.Fingerprint == nil || got.Fingerprint.Device.Label != label {
		t.Error("读回后指纹机型不是锁定的那个")
	}
}

// 锁定同一机型时，不同种子的 canvas 等噪声仍应不同——
// 机型只决定 OS/核数/GPU 这类描述性字段，不该让指纹整体撞车。
func TestLockedDeviceStillVariesBySeed(t *testing.T) {
	s, _ := newTestService(t)
	label := firstCatalogLabel(t)

	seeds := map[int32]bool{}
	for i := 0; i < 3; i++ {
		v, err := s.CreateProfile(context.Background(), CreateRequest{
			Name: "同机型-" + string(rune('A'+i)),
			Kind: model.KindFingerprint, DeviceLabel: label,
		})
		if err != nil {
			t.Fatalf("CreateProfile 失败: %v", err)
		}
		if v.Fingerprint.Device.Label != label {
			t.Errorf("机型 = %q, 期望 %q", v.Fingerprint.Device.Label, label)
		}
		if seeds[v.Seed] {
			t.Errorf("种子 %d 重复", v.Seed)
		}
		seeds[v.Seed] = true
	}
}

func TestFindDevice(t *testing.T) {
	label := firstCatalogLabel(t)
	if _, ok := fingerprint.FindDevice(label); !ok {
		t.Errorf("FindDevice(%q) 未找到", label)
	}
	if _, ok := fingerprint.FindDevice("绝不存在的机型"); ok {
		t.Error("不存在的标签不应被找到")
	}
}

// 标签为空时应回退为按种子抽取，且与不锁定的结果一致。
func TestDeriveWithEmptyLabelMatchesSeedDerivation(t *testing.T) {
	geo := &model.Geo{CountryCode: "US", Timezone: "America/New_York", Locale: "en-US"}
	a := fingerprint.DeriveWithDeviceLabel(12345, geo, "", "")
	b := fingerprint.Derive(12345, geo)
	if a.Device.Label != b.Device.Label {
		t.Errorf("空标签的机型 = %q, 期望与按种子抽取一致的 %q",
			a.Device.Label, b.Device.Label)
	}
}

// 标签指向已删除的档案时回退为按种子抽取，而非返回空机型。
func TestDeriveWithUnknownLabelFallsBack(t *testing.T) {
	geo := &model.Geo{CountryCode: "US", Timezone: "America/New_York", Locale: "en-US"}
	fp := fingerprint.DeriveWithDeviceLabel(12345, geo, "已删除的机型", "")
	if fp.Device.Label == "" {
		t.Error("回退后应仍有一个有效机型，不能是空档案")
	}
	if fp.Device.Platform == "" || fp.Device.HardwareConcurrency <= 0 {
		t.Errorf("回退的机型不完整: %+v", fp.Device)
	}
}

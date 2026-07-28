package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"better-web/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := &model.Profile{
		ID:         "id-1",
		Name:       "美国-洛杉矶-01",
		Kind:       model.KindFingerprint,
		Seed:       20260727,
		ProfileDir: `C:\bw\profiles\id-1`,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
			Username: "user1", Password: "secret",
		},
		GeoOverride:   &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"},
		KernelVersion: "148.0.7778.215",
		ExtraArgs:     []string{"--start-maximized"},
		Notes:         "测试用",
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	got, err := s.Get("id-1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Name != want.Name || got.Kind != want.Kind || got.Seed != want.Seed {
		t.Errorf("基础字段不符: %+v", got)
	}
	if got.ProfileDir != want.ProfileDir || got.KernelVersion != want.KernelVersion {
		t.Errorf("路径或内核版本不符: %+v", got)
	}
	if got.Proxy == nil || got.Proxy.Host != "gate.example.com" || got.Proxy.Port != 7000 {
		t.Errorf("代理配置不符: %+v", got.Proxy)
	}
	// 凭据必须能完整往返，否则重启后代理会认证失败。
	if got.Proxy.Username != "user1" || got.Proxy.Password != "secret" {
		t.Error("代理凭据未完整保存")
	}
	if got.GeoOverride == nil || got.GeoOverride.Timezone != "America/Los_Angeles" {
		t.Errorf("地理配置不符: %+v", got.GeoOverride)
	}
	if len(got.ExtraArgs) != 1 || got.ExtraArgs[0] != "--start-maximized" {
		t.Errorf("附加参数不符: %v", got.ExtraArgs)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("时间戳未写入")
	}
}

// 种子是 profile 身份的根，更新其他字段时绝不能被改动。
func TestSaveUpdatePreservesSeedAndCreatedAt(t *testing.T) {
	s := openTemp(t)
	p := &model.Profile{ID: "id-1", Name: "原名", Kind: model.KindFingerprint, Seed: 12345, ProfileDir: "d"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	created := p.CreatedAt

	// 等一毫秒确保 updated_at 能区分出来。
	time.Sleep(2 * time.Millisecond)
	p.Name = "改名后"
	if err := s.Save(p); err != nil {
		t.Fatalf("二次 Save 失败: %v", err)
	}

	got, err := s.Get("id-1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Name != "改名后" {
		t.Errorf("名称 = %q, 期望 改名后", got.Name)
	}
	if got.Seed != 12345 {
		t.Errorf("种子被改动: %d", got.Seed)
	}
	if !got.CreatedAt.Equal(created.Truncate(time.Millisecond)) &&
		got.CreatedAt.UnixMilli() != created.UnixMilli() {
		t.Errorf("创建时间被改动: %v vs %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.After(got.CreatedAt) && got.UpdatedAt.UnixMilli() < got.CreatedAt.UnixMilli() {
		t.Errorf("更新时间未推进: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestNilFieldsRoundTripAsNil(t *testing.T) {
	s := openTemp(t)
	p := &model.Profile{ID: "id-2", Name: "日常", Kind: model.KindDaily, ProfileDir: "d"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := s.Get("id-2")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Proxy != nil || got.GeoOverride != nil || got.ExtraArgs != nil {
		t.Errorf("空字段读回后非 nil: %+v", got)
	}
	if !got.LastUseAt.IsZero() {
		t.Errorf("从未使用过的 profile 的 LastUseAt 应为零值: %v", got.LastUseAt)
	}
}

// 名称唯一：同名 profile 在界面上无法区分，容易误操作到错误的账号环境。
func TestDuplicateNameRejected(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{ID: "a", Name: "同名", Kind: model.KindDaily, ProfileDir: "d1"}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	err := s.Save(&model.Profile{ID: "b", Name: "同名", Kind: model.KindDaily, ProfileDir: "d2"})
	if err == nil {
		t.Error("同名 profile 期望报错，实际通过")
	}
}

func TestListOrdersByLastUse(t *testing.T) {
	s := openTemp(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Save(&model.Profile{ID: id, Name: "p-" + id, Kind: model.KindDaily, ProfileDir: id}); err != nil {
			t.Fatalf("Save %s 失败: %v", id, err)
		}
	}
	now := time.Now()
	if err := s.TouchLastUse("b", now); err != nil {
		t.Fatalf("TouchLastUse 失败: %v", err)
	}
	if err := s.TouchLastUse("a", now.Add(-time.Hour)); err != nil {
		t.Fatalf("TouchLastUse 失败: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("数量 = %d, 期望 3", len(list))
	}
	if list[0].ID != "b" || list[1].ID != "a" {
		t.Errorf("排序错误: %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestDelete(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{ID: "x", Name: "待删", Kind: model.KindDaily, ProfileDir: "d"}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := s.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 Get 期望 ErrNotFound, 实际 %v", err)
	}
	if err := s.Delete("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复 Delete 期望 ErrNotFound, 实际 %v", err)
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Get("不存在"); !errors.Is(err, ErrNotFound) {
		t.Errorf("期望 ErrNotFound, 实际 %v", err)
	}
}

func TestSaveRejectsInvalidProfile(t *testing.T) {
	s := openTemp(t)
	cases := map[string]*model.Profile{
		"nil":  nil,
		"缺 ID": {Name: "n", Kind: model.KindDaily, ProfileDir: "d"},
		"缺名称":  {ID: "i", Kind: model.KindDaily, ProfileDir: "d"},
		"类型无效": {ID: "i", Name: "n", Kind: "bogus", ProfileDir: "d"},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if err := s.Save(p); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

func TestListEmptyDatabase(t *testing.T) {
	list, err := openTemp(t).List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("期望空列表, 实际 %d 项", len(list))
	}
}

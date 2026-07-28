package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"better-web/internal/model"
)

func newTestService(t *testing.T) (*Service, Paths) {
	t.Helper()
	paths := NewPaths(t.TempDir())
	s, err := New(paths)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, paths
}

// 指纹模式必须自动生成非零种子，否则启动时参数组装会被拒绝。
func TestCreateFingerprintProfileGeneratesSeed(t *testing.T) {
	s, paths := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{Name: "指纹-01", Kind: model.KindFingerprint})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.Seed <= 0 {
		t.Errorf("种子 = %d, 期望正数", v.Seed)
	}
	if v.Fingerprint == nil {
		t.Error("指纹模式应返回指纹预览")
	}
	// profile 目录必须落在受管目录内。
	if !strings.HasPrefix(v.ProfileDir, paths.Profiles) {
		t.Errorf("profile 目录 %q 不在受管目录 %q 内", v.ProfileDir, paths.Profiles)
	}

	// 两个 profile 的种子不应相同。
	v2, err := s.CreateProfile(context.Background(), CreateRequest{Name: "指纹-02", Kind: model.KindFingerprint})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v2.Seed == v.Seed {
		t.Error("两个 profile 生成了相同的种子")
	}
}

func TestCreateDailyProfileHasNoFingerprint(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{Name: "日常", Kind: model.KindDaily})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.Fingerprint != nil {
		t.Errorf("日常模式不该有指纹: %+v", v.Fingerprint)
	}
	if v.Seed != 0 {
		t.Errorf("日常模式种子 = %d, 期望 0", v.Seed)
	}
}

// 代理密码绝不能出现在发给前端的视图里：前端不需要它，
// 而送进渲染层等于让它出现在 DevTools、日志和崩溃报告中。
func TestProfileViewNeverExposesProxyPassword(t *testing.T) {
	s, _ := newTestService(t)
	const secret = "sup3r-s3cret-pw"
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "带代理", Kind: model.KindFingerprint,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
			Username: "user1", Password: secret,
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.Proxy == nil {
		t.Fatal("代理配置丢失")
	}
	if !v.Proxy.HasPassword {
		t.Error("HasPassword 应为 true")
	}
	if v.Proxy.Username != "user1" {
		t.Errorf("用户名 = %q", v.Proxy.Username)
	}

	// 逐个视图接口都不得泄漏密码。
	got, err := s.GetProfile(v.ID)
	if err != nil {
		t.Fatalf("GetProfile 失败: %v", err)
	}
	list, err := s.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles 失败: %v", err)
	}
	for name, view := range map[string]ProfileView{"Create": v, "Get": got, "List": list[0]} {
		if strings.Contains(dumpJSON(t, view), secret) {
			t.Errorf("%s 返回的视图中包含代理密码原文", name)
		}
	}
}

// 前端不持有密码原文，提交空密码时必须保留原有凭据，
// 否则每次编辑 profile 都会把代理密码清掉。
func TestUpdatePreservesPasswordWhenOmitted(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "p", Kind: model.KindDaily,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "h", Port: 1080,
			Username: "u", Password: "keepme",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	// 模拟前端回填：同主机同用户名，密码为空。
	if _, err := s.UpdateProfile(UpdateRequest{
		ID: v.ID, Name: "改名",
		Proxy: &model.Proxy{Scheme: model.ProxySOCKS5, Host: "h", Port: 1080, Username: "u"},
	}); err != nil {
		t.Fatalf("UpdateProfile 失败: %v", err)
	}

	p, err := s.store.Get(v.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if p.Proxy == nil || p.Proxy.Password != "keepme" {
		t.Error("编辑后代理密码被清空")
	}
	if p.Name != "改名" {
		t.Errorf("名称 = %q", p.Name)
	}
}

// 种子不可变：改种子等于换设备，会让该 profile 已建立的身份整体漂移。
func TestUpdateCannotChangeSeed(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{Name: "p", Kind: model.KindFingerprint})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if _, err := s.UpdateProfile(UpdateRequest{ID: v.ID, Name: "改名"}); err != nil {
		t.Fatalf("UpdateProfile 失败: %v", err)
	}
	after, err := s.GetProfile(v.ID)
	if err != nil {
		t.Fatalf("GetProfile 失败: %v", err)
	}
	if after.Seed != v.Seed {
		t.Errorf("种子从 %d 变成了 %d", v.Seed, after.Seed)
	}
}

func TestClearProxy(t *testing.T) {
	s, _ := newTestService(t)
	v, _ := s.CreateProfile(context.Background(), CreateRequest{
		Name: "p", Kind: model.KindDaily,
		Proxy: &model.Proxy{Scheme: model.ProxySOCKS5, Host: "h", Port: 1080},
	})
	if _, err := s.UpdateProfile(UpdateRequest{ID: v.ID, ClearProxy: true}); err != nil {
		t.Fatalf("UpdateProfile 失败: %v", err)
	}
	after, _ := s.GetProfile(v.ID)
	if after.Proxy != nil {
		t.Errorf("代理未被清除: %+v", after.Proxy)
	}
}

// 删除配置不能连带删掉浏览数据：里面有 Cookie 和登录态，误删不可恢复。
func TestDeleteProfileKeepsBrowsingData(t *testing.T) {
	s, _ := newTestService(t)
	v, _ := s.CreateProfile(context.Background(), CreateRequest{Name: "p", Kind: model.KindDaily})
	if err := os.MkdirAll(v.ProfileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(v.ProfileDir, "Cookies")
	if err := os.WriteFile(marker, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProfile(v.ID); err != nil {
		t.Fatalf("DeleteProfile 失败: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("删除配置时误删了浏览数据: %v", err)
	}
}

// 清除数据只允许作用于受管目录内，防止配置被篡改后误删无关路径。
func TestDeleteProfileDataRefusesUnmanagedPath(t *testing.T) {
	s, _ := newTestService(t)
	v, _ := s.CreateProfile(context.Background(), CreateRequest{Name: "p", Kind: model.KindDaily})

	outside := t.TempDir()
	marker := filepath.Join(outside, "important.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 直接改库，模拟配置被篡改。
	p, _ := s.store.Get(v.ID)
	p.ProfileDir = outside
	if err := s.store.Save(p); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProfileData(v.ID); err == nil {
		t.Error("对受管目录外的路径期望拒绝，实际执行了删除")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("受管目录外的文件被删除了: %v", err)
	}
}

func TestDeleteProfileDataRemovesManagedDir(t *testing.T) {
	s, _ := newTestService(t)
	v, _ := s.CreateProfile(context.Background(), CreateRequest{Name: "p", Kind: model.KindDaily})
	if err := os.MkdirAll(v.ProfileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfileData(v.ID); err != nil {
		t.Fatalf("DeleteProfileData 失败: %v", err)
	}
	if _, err := os.Stat(v.ProfileDir); !os.IsNotExist(err) {
		t.Errorf("受管目录未被清除: %v", err)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	s, _ := newTestService(t)
	cases := map[string]CreateRequest{
		"名称为空": {Name: "  ", Kind: model.KindDaily},
		"类型无效": {Name: "n", Kind: "bogus"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.CreateProfile(context.Background(), req); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

// owns 是删除操作的安全边界，必须挡住 ..、绝对路径和 profiles 根目录本身。
func TestPathsOwns(t *testing.T) {
	p := NewPaths(filepath.Join(t.TempDir(), "root"))
	if !p.owns(p.ProfileDir("abc")) {
		t.Error("受管 profile 目录应被认定为 owns")
	}
	for name, dir := range map[string]string{
		"profiles 根目录本身": p.Profiles,
		"上级目录":           filepath.Join(p.Profiles, ".."),
		"越界路径":           filepath.Join(p.Profiles, "..", "..", "other"),
		"无关绝对路径":         filepath.Join(t.TempDir(), "elsewhere"),
	} {
		if p.owns(dir) {
			t.Errorf("%s (%q) 不应被认定为 owns", name, dir)
		}
	}
}

func TestListProfilesEmpty(t *testing.T) {
	s, _ := newTestService(t)
	list, err := s.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles 失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("期望空列表, 实际 %d 项", len(list))
	}
}

func TestListDeviceProfilesReturnsCatalog(t *testing.T) {
	s, _ := newTestService(t)
	if len(s.ListDeviceProfiles()) == 0 {
		t.Error("机型档案库为空")
	}
}

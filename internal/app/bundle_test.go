package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"better-web/internal/model"
)

// 导出再导入应还原全部配置，且新 profile 拿到本机的 ID 与目录。
func TestBundleRoundTrip(t *testing.T) {
	src, _ := newTestService(t)
	created, err := src.CreateProfile(context.Background(), CreateRequest{
		Name: "美国-01", Kind: model.KindFingerprint,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
			Username: "u1", Password: "pw1",
		},
		Notes: "备注",
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	path := filepath.Join(t.TempDir(), "bundle.json")
	n, err := src.ExportBundle(path, false)
	if err != nil {
		t.Fatalf("ExportBundle 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("导出 %d 条, 期望 1", n)
	}

	// 导入到另一个空库，模拟迁移到新机器。
	dst, _ := newTestService(t)
	res, err := dst.ImportBundle(path, BundleImportOptions{NewSeeds: false})
	if err != nil {
		t.Fatalf("ImportBundle 失败: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("导入 %d 条, 期望 1: %+v", res.Imported, res.Failures)
	}

	list, err := dst.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles 失败: %v", err)
	}
	got := list[0]
	if got.Name != "美国-01" || got.Notes != "备注" {
		t.Errorf("配置未还原: %+v", got)
	}
	// 备份恢复语义：种子必须保留，否则还原出来的是另一台设备。
	if got.Seed != created.Seed {
		t.Errorf("种子 = %d, 期望保留原值 %d", got.Seed, created.Seed)
	}
	// ID 与目录必须是本机新生成的。
	if got.ID == created.ID {
		t.Error("ID 未重新生成，跨机器导入会与既有记录冲突")
	}
	// 代理配置保留，但密码未导出，所以 hasPassword 为 false。
	if got.Proxy == nil || got.Proxy.Host != "gate.example.com" {
		t.Errorf("代理未还原: %+v", got.Proxy)
	}
	if got.Proxy.HasPassword {
		t.Error("未导出凭据却报告已设置密码")
	}
	if !hasWarningContaining(res.Warnings, "补填") {
		t.Errorf("未提示补填密码: %v", res.Warnings)
	}
}

// 导出文件默认不得含密码明文——它会被复制、发送、提交到仓库。
func TestExportBundleOmitsPassword(t *testing.T) {
	s, _ := newTestService(t)
	const secret = "bundle-secret-3f9a"
	if _, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "带密码", Kind: model.KindDaily,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "h", Port: 1080,
			Username: "u", Password: secret,
		},
	}); err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	path := filepath.Join(t.TempDir(), "b.json")
	if _, err := s.ExportBundle(path, false); err != nil {
		t.Fatalf("ExportBundle 失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取导出文件失败: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Error("默认导出泄漏了代理密码")
	}
}

// 导出文件权限必须是 0600：即使不含密码，里面也有代理地址与账号名。
// Windows 上的权限模型不同，仅在类 Unix 平台校验。
func TestExportBundleFilePermission(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows 不适用 Unix 权限位")
	}
	s, _ := newTestService(t)
	if _, err := s.CreateProfile(context.Background(), CreateRequest{Name: "x", Kind: model.KindDaily}); err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "b.json")
	if _, err := s.ExportBundle(path, false); err != nil {
		t.Fatalf("ExportBundle 失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("导出文件权限 = %o, 期望 600", perm)
	}
}

// 批量建号语义：生成新种子，否则多个 profile 共用一套指纹。
func TestImportBundleWithNewSeeds(t *testing.T) {
	src, _ := newTestService(t)
	orig, err := src.CreateProfile(context.Background(), CreateRequest{Name: "模板", Kind: model.KindFingerprint})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "b.json")
	if _, err := src.ExportBundle(path, false); err != nil {
		t.Fatalf("ExportBundle 失败: %v", err)
	}

	dst, _ := newTestService(t)
	res, err := dst.ImportBundle(path, BundleImportOptions{
		NewSeeds: true, NamePrefix: "批次A-", Group: "电商",
	})
	if err != nil {
		t.Fatalf("ImportBundle 失败: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("导入 %d 条: %+v", res.Imported, res.Failures)
	}

	list, _ := dst.ListProfiles()
	got := list[0]
	if got.Seed == orig.Seed {
		t.Error("要求新种子却沿用了原值，多个 profile 会共用同一套指纹")
	}
	if got.Name != "批次A-模板" {
		t.Errorf("名称 = %q, 期望带前缀", got.Name)
	}
	if got.Group != "电商" {
		t.Errorf("分组 = %q, 期望被覆盖", got.Group)
	}
}

// 与既有 profile 重名时跳过而非报错，且不影响其余条目。
func TestImportBundleSkipsDuplicateNames(t *testing.T) {
	src, _ := newTestService(t)
	for _, name := range []string{"重复的", "唯一的"} {
		if _, err := src.CreateProfile(context.Background(), CreateRequest{
			Name: name, Kind: model.KindDaily}); err != nil {
			t.Fatalf("CreateProfile 失败: %v", err)
		}
	}
	path := filepath.Join(t.TempDir(), "b.json")
	if _, err := src.ExportBundle(path, false); err != nil {
		t.Fatalf("ExportBundle 失败: %v", err)
	}

	// 目标库里已有同名的一条。
	dst, _ := newTestService(t)
	if _, err := dst.CreateProfile(context.Background(), CreateRequest{
		Name: "重复的", Kind: model.KindDaily}); err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	res, err := dst.ImportBundle(path, BundleImportOptions{})
	if err != nil {
		t.Fatalf("ImportBundle 失败: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("导入 %d 条, 期望 1（另一条重名跳过）", res.Imported)
	}
	if len(res.SkippedNames) != 1 || res.SkippedNames[0] != "重复的" {
		t.Errorf("跳过列表 = %v", res.SkippedNames)
	}
	if len(res.Failures) != 0 {
		t.Errorf("重名应跳过而非失败: %+v", res.Failures)
	}
}

func TestExportBundleRejectsEmptyLibrary(t *testing.T) {
	s, _ := newTestService(t)
	if _, err := s.ExportBundle(filepath.Join(t.TempDir(), "b.json"), false); err == nil {
		t.Error("空库导出应报错")
	}
}

func TestImportBundleRejectsBadFile(t *testing.T) {
	s, _ := newTestService(t)
	dir := t.TempDir()

	t.Run("文件不存在", func(t *testing.T) {
		if _, err := s.ImportBundle(filepath.Join(dir, "nope.json"),
			BundleImportOptions{}); err == nil {
			t.Error("期望报错")
		}
	})
	t.Run("内容非法", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		if _, err := s.ImportBundle(bad, BundleImportOptions{}); err == nil {
			t.Error("期望报错")
		}
	})
}

func hasWarningContaining(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

package store

import (
	"os"
	"strings"
	"testing"

	"better-web/internal/model"
	"better-web/internal/secret"
)

// 代理密码在数据库中必须是密文。
//
// 本测试的前身是 TestPasswordIsStoredInPlaintextByDesign，它把"密码是明文"
// 固化为预期，并要求实现加密后同步更新文档再删除自己。加密已落地
// （internal/secret，Windows 走 DPAPI），文档也已更正，故反转断言。
//
// 保留在这里而非只依赖 encryption_test.go：那边覆盖主库与 WAL，
// 这边确保带特殊字符与非 ASCII 的密码也不会以明文形式落盘。
func TestPasswordIsStoredEncrypted(t *testing.T) {
	if !secret.Available() {
		t.Skip("当前平台无系统级加密，明文存储是已知且已在文档中标注的行为")
	}
	dir := t.TempDir()
	path := dir + "/plain.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	const marker = "unique-plaintext-marker-9c8f2a"
	p := &model.Profile{
		ID: "pw-1", Name: "带密码", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\pw1`, Seed: 1,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1080,
			Username: "u", Password: marker,
		},
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取数据库文件失败: %v", err)
	}
	if strings.Contains(string(raw), marker) {
		t.Error("数据库文件中含有密码明文，加密未生效")
	}
}

// 密码必须能正确往返，否则代理认证会失败。
func TestPasswordRoundTrip(t *testing.T) {
	s := openTemp(t)
	const pw = "p@ss w0rd/带中文&符号=+"
	p := &model.Profile{
		ID: "pw-2", Name: "特殊字符密码", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\pw2`, Seed: 2,
		Proxy: &model.Proxy{
			Scheme: model.ProxyHTTP, Host: "example.test", Port: 8080,
			Username: "user", Password: pw,
		},
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := s.Get("pw-2")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Proxy == nil {
		t.Fatal("代理配置丢失")
	}
	if got.Proxy.Password != pw {
		t.Error("密码往返后不一致")
	}
}

// 数据库出错时，错误信息里不能带上密码。
// 错误信息会进日志、进崩溃报告、被用户复制到聊天里。
func TestSaveErrorDoesNotLeakPassword(t *testing.T) {
	s := openTemp(t)
	const pw = "leaky-secret-4f7b1e"

	// 用同名 profile 触发唯一索引冲突，制造一次真实的写入失败。
	first := &model.Profile{
		ID: "pw-3a", Name: "同名", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\a`, Seed: 3,
	}
	if err := s.Save(first); err != nil {
		t.Fatalf("首次 Save 失败: %v", err)
	}
	dup := &model.Profile{
		ID: "pw-3b", Name: "同名", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\b`, Seed: 4,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1080,
			Username: "u", Password: pw,
		},
	}
	err := s.Save(dup)
	if err == nil {
		t.Skip("未触发唯一索引冲突，跳过")
	}
	if strings.Contains(err.Error(), pw) {
		t.Errorf("错误信息中泄漏了密码: %v", err)
	}
}

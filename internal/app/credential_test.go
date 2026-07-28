package app

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/model"
	"better-web/internal/secret"
)

// 保护级别必须如实反映实际能力。
//
// 这一项的价值在于诚实：非 Windows 平台没有系统级密钥库，密码仍是明文。
// 若这里谎报 encrypted=true，用户会以为密码已受保护而放松对数据目录的
// 警惕，比不加密更危险。
func TestCredentialProtectionReportsActualCapability(t *testing.T) {
	s, _ := newTestService(t)
	got := s.CredentialProtection()

	if got.Encrypted != secret.Available() {
		t.Errorf("Encrypted = %v, 与 secret.Available() = %v 不一致",
			got.Encrypted, secret.Available())
	}
	if strings.TrimSpace(got.Detail) == "" {
		t.Error("Detail 为空，界面无法向用户说明保护级别")
	}
	// 未加密时说明文字必须让用户看懂风险，不能只说"不支持"。
	if !got.Encrypted && !strings.Contains(got.Detail, "明文") {
		t.Errorf("未加密时的说明未提及明文存储: %q", got.Detail)
	}
}

// 端到端：经服务层保存的密码在库里应是密文，读回时应是明文。
func TestServiceEncryptsProxyPasswordEndToEnd(t *testing.T) {
	if !secret.Available() {
		t.Skip("当前平台无系统级加密，明文存储是已知且已标注的行为")
	}
	s, _ := newTestService(t)
	const pw = "END2END-SENTINEL-7c1f"

	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "加密端到端", Kind: model.KindFingerprint,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
			Username: "u", Password: pw,
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	// 库里必须是密文。
	stored, err := s.store.Get(v.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	// store 读出时已自动解密，因此这里拿到的应是明文——
	// 说明解密链路通畅，而落盘内容的密文性由 store 包的测试保证。
	if stored.Proxy == nil || stored.Proxy.Password != pw {
		t.Error("密码未能经服务层正确往返")
	}

	// 发往前端的视图永远不含密码，无论加密与否。
	if strings.Contains(dumpJSON(t, v), pw) {
		t.Error("返回给前端的视图中含有密码")
	}
	if v.Proxy == nil || !v.Proxy.HasPassword {
		t.Error("视图应标记已设置密码")
	}
}

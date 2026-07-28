package transfer

import (
	"bytes"
	"strings"
	"testing"

	"better-web/internal/model"
)

func sampleProfiles() []*model.Profile {
	return []*model.Profile{
		{
			ID: "id-1", Name: "美国-01", Kind: model.KindFingerprint, Seed: 111,
			ProfileDir: `C:\p\1`,
			Proxy: &model.Proxy{
				Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
				Username: "u1", Password: "secret-one",
			},
			Group: "电商", Tags: []string{"已验证"}, Notes: "备注一",
		},
		{
			ID: "id-2", Name: "日常", Kind: model.KindDaily, ProfileDir: `C:\p\2`,
		},
	}
}

// 默认导出不得含代理密码。
//
// 导出文件会被复制、通过聊天工具发送、甚至提交到仓库；凭据进入这类文件的
// 风险远高于导入后重填一次密码的成本。
func TestExportOmitsPasswordByDefault(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, sampleProfiles(), 15, false, ""); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "secret-one") {
		t.Error("默认导出泄漏了代理密码")
	}
	// 但要留下"原本设过密码"的标记，否则导入后代理会静默认证失败。
	if !strings.Contains(out, "hadPassword") {
		t.Error("未标记原 profile 设置过密码，导入后用户不会知道要补填")
	}
}

func TestExportIncludesPasswordWhenRequested(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, sampleProfiles(), 15, true, ""); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}
	if !strings.Contains(buf.String(), "secret-one") {
		t.Error("显式要求带凭据时未包含密码")
	}
}

// 导出不含本机特有字段：ID 与 ProfileDir 在目标机器上都会重新生成。
func TestExportOmitsMachineSpecificFields(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, sampleProfiles(), 15, false, ""); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}
	out := buf.String()
	for _, bad := range []string{"id-1", `C:\\p\\1`, "profileDir"} {
		if strings.Contains(out, bad) {
			t.Errorf("导出含本机特有字段 %q", bad)
		}
	}
}

// 往返：导出再解析，配置字段应完整保留。
func TestRoundTripPreservesConfig(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, sampleProfiles(), 15, false, ""); err != nil {
		t.Fatalf("Export 失败: %v", err)
	}
	b, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if b.FormatVersion != FormatVersion {
		t.Errorf("formatVersion = %d", b.FormatVersion)
	}
	if b.CatalogSize != 15 {
		t.Errorf("catalogSize = %d, 期望 15", b.CatalogSize)
	}
	if len(b.Profiles) != 2 {
		t.Fatalf("profile 数 = %d, 期望 2", len(b.Profiles))
	}
	e := b.Profiles[0]
	if e.Name != "美国-01" || e.Seed != 111 || e.Group != "电商" {
		t.Errorf("首条配置丢失: %+v", e)
	}
	if e.Proxy == nil || e.Proxy.Host != "gate.example.com" || e.Proxy.Port != 7000 {
		t.Errorf("代理配置丢失: %+v", e.Proxy)
	}
}

// 更高的格式版本必须拒绝而非忽略未知字段。
// 静默导入会让用户以为配置完整，实际丢了本版本不认识的项。
func TestParseRejectsNewerFormat(t *testing.T) {
	json := `{"formatVersion":999,"profiles":[{"name":"x","kind":"daily"}]}`
	_, err := Parse(strings.NewReader(json))
	if err == nil {
		t.Fatal("期望拒绝更高的格式版本")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("错误信息未指出版本号: %v", err)
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"非 JSON":          "not json at all",
		"缺 formatVersion": `{"profiles":[{"name":"x","kind":"daily"}]}`,
		"空列表":             `{"formatVersion":1,"profiles":[]}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(in)); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

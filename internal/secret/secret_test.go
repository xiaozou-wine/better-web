package secret

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []string{
		"simple",
		"p@ssw0rd!#$%^&*()",
		"含中文的密码",
		strings.Repeat("x", 4096), // 长密码不应触发缓冲区问题
		"with\nnewline\tand\ttabs",
	}
	for _, plain := range cases {
		enc, err := Encrypt(plain)
		if err != nil {
			t.Fatalf("加密 %q 失败: %v", truncate(plain), err)
		}
		got, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if got != plain {
			t.Errorf("往返后不一致: 期望 %q, 实际 %q", truncate(plain), truncate(got))
		}
	}
}

// 密文中不得出现明文片段，否则加密形同虚设。
func TestCiphertextDoesNotContainPlaintext(t *testing.T) {
	if !Available() {
		t.Skip("当前平台无系统级加密，明文存储是已知且已标注的行为")
	}
	const plain = "UNIQUE-SENTINEL-VALUE-12345"
	enc, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if strings.Contains(enc, plain) {
		t.Error("密文中包含明文")
	}
	if enc == plain {
		t.Error("加密后与明文相同，加密未生效")
	}
	if !IsEncrypted(enc) {
		t.Errorf("密文缺少标记前缀: %q", enc)
	}
}

// 空串必须原样返回：加密空串会让"未设置密码"与"密码为空"无法区分。
func TestEmptyStringPassesThrough(t *testing.T) {
	enc, err := Encrypt("")
	if err != nil {
		t.Fatalf("加密空串失败: %v", err)
	}
	if enc != "" {
		t.Errorf("空串加密后 = %q, 期望空串", enc)
	}
	dec, err := Decrypt("")
	if err != nil {
		t.Fatalf("解密空串失败: %v", err)
	}
	if dec != "" {
		t.Errorf("空串解密后 = %q", dec)
	}
}

// 升级兼容：旧记录里的明文没有前缀，读取时必须原样返回而非报解密失败。
func TestPlaintextLegacyValueReadsBack(t *testing.T) {
	const legacy = "old-plaintext-password"
	got, err := Decrypt(legacy)
	if err != nil {
		t.Fatalf("读取历史明文失败: %v", err)
	}
	if got != legacy {
		t.Errorf("历史明文 = %q, 期望 %q", got, legacy)
	}
}

// 重复加密不应叠加：UpdateProfile 会把读出的值再写回去。
func TestEncryptIsIdempotentOnCiphertext(t *testing.T) {
	if !Available() {
		t.Skip("当前平台无系统级加密")
	}
	enc, err := Encrypt("secret-value")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	again, err := Encrypt(enc)
	if err != nil {
		t.Fatalf("二次加密失败: %v", err)
	}
	if again != enc {
		t.Error("对密文再次加密产生了不同结果，会导致解密失败")
	}
	got, err := Decrypt(again)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != "secret-value" {
		t.Errorf("往返后 = %q", got)
	}
}

// 损坏的密文必须报错而非返回垃圾数据。
func TestCorruptedCiphertextReportsError(t *testing.T) {
	if !Available() {
		t.Skip("当前平台无系统级加密")
	}
	cases := map[string]string{
		"base64 非法": cipherPrefix + "!!!not-base64!!!",
		"内容被篡改":     cipherPrefix + "AAAAAAAAAAAAAAAAAAAAAA==",
		"仅有前缀":      cipherPrefix,
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decrypt(bad); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

// 每次加密应产生不同密文（DPAPI 会引入随机盐），
// 否则相同密码在库里呈现相同密文，等于泄露"这两个 profile 密码相同"。
func TestCiphertextIsSalted(t *testing.T) {
	if !Available() {
		t.Skip("当前平台无系统级加密")
	}
	a, err := Encrypt("same-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("same-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("相同明文产生了相同密文，可据此推断两个 profile 使用同一密码")
	}
}

func TestDescriptionIsNotEmpty(t *testing.T) {
	if Description() == "" {
		t.Error("Description 不应为空，界面需要它提示用户当前的保护级别")
	}
}

func truncate(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[:40] + "..."
}

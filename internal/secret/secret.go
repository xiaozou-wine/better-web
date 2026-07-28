// Package secret 加密存储代理凭据。
//
// 存在原因：代理密码此前以明文存在 SQLite 的 JSON 列里，而
// store 包的注释声称"目录权限 0700"提供保护——那个保护在 Windows 上
// 不存在。Go 的 os.MkdirAll(0o700) 不会设置 NTFS ACL，实测
// %APPDATA%\better-web 的 ACL 全部从父目录继承，同机器上以当前用户
// 身份运行的任何程序都能直接读到密码。
//
// 现在的做法是把密文存库：Windows 走 DPAPI（密钥绑定当前用户账户），
// 其他平台没有等价的系统级密钥库，退化为明文并明确标注，
// 不假装提供了保护。
package secret

import (
	"errors"
	"fmt"
	"strings"
)

// cipherPrefix 标记一个值是本包加密过的密文。
//
// 需要这个标记是为了兼容既有数据：升级前存的是明文，
// 读取时按有无前缀区分，避免把明文当密文去解密而报错。
const cipherPrefix = "bwenc:v1:"

// ErrUnavailable 表示当前平台没有可用的系统级加密。
var ErrUnavailable = errors.New("当前平台不支持系统级凭据加密")

// Encrypt 加密一个凭据，返回可直接入库的字符串。
//
// 空串原样返回：没有密码就没有要保护的东西，加密空串只会让
// "未设置密码"和"密码为空"变得无法区分。
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	// 已是密文时不重复加密：UpdateProfile 会把读出的值再写回去。
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}

	blob, err := protect([]byte(plaintext))
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			// 平台不支持时原样返回明文。上层通过 Available() 决定
			// 是否向用户提示该风险，此处不静默失败也不阻断功能。
			return plaintext, nil
		}
		return "", fmt.Errorf("加密凭据失败: %w", err)
	}
	return cipherPrefix + encodeBase64(blob), nil
}

// Decrypt 解密一个入库的凭据值。
//
// 不带密文前缀的值视为历史遗留的明文，原样返回。这样升级不需要
// 数据迁移，旧记录在下次保存时自然转成密文。
func Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !IsEncrypted(stored) {
		return stored, nil
	}

	blob, err := decodeBase64(strings.TrimPrefix(stored, cipherPrefix))
	if err != nil {
		return "", fmt.Errorf("凭据密文格式无效: %w", err)
	}
	plain, err := unprotect(blob)
	if err != nil {
		// 解密失败的常见原因是换了用户账户或换了机器——DPAPI 的密钥
		// 绑定当前用户。报清楚原因，让用户知道要重新填密码，
		// 而不是以为代理配置坏了。
		return "", fmt.Errorf("解密凭据失败（密文可能来自其他用户或其他机器，需重新填写密码）: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted 报告一个入库值是否为本包产生的密文。
func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, cipherPrefix)
}

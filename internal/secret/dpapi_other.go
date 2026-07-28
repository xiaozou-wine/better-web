//go:build !windows

package secret

import "encoding/base64"

// Available 报告当前平台是否提供系统级凭据加密。
//
// 非 Windows 平台返回 false：macOS 的 Keychain 与 Linux 的 Secret Service
// 都需要额外依赖且可能弹出交互，尚未接入。此时凭据以明文入库，
// 由上层向用户明确提示该风险，而不是假装已加密。
func Available() bool { return false }

// Description 返回加密方式的可读说明，用于界面提示。
func Description() string {
	return "无系统级凭据加密，代理密码以明文存储（请确保数据目录不被同步或共享）"
}

func protect(plain []byte) ([]byte, error) {
	return nil, ErrUnavailable
}

func unprotect(cipher []byte) ([]byte, error) {
	return nil, ErrUnavailable
}

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func decodeBase64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

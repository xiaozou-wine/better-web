//go:build windows

package secret

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Available 报告当前平台是否提供系统级凭据加密。
func Available() bool { return true }

// Description 返回加密方式的可读说明，用于界面提示。
func Description() string {
	return "Windows DPAPI（密钥绑定当前用户账户，密文无法在其他账户或其他机器上解开）"
}

// protect 用 DPAPI 加密。
//
// 默认作用域即"同一用户 + 同一机器"，不传 CRYPTPROTECT_LOCAL_MACHINE：
// 那个标志会让同机器上任何用户都能解密，正是我们要防的场景。
func protect(plain []byte) ([]byte, error) {
	in := windows.DataBlob{
		Size: uint32(len(plain)),
		Data: &plain[0],
	}
	var out windows.DataBlob

	// name 传 nil：描述字符串会明文存在密文里，没必要泄露额外信息。
	// promptStruct 传 nil：不弹 UI，加密要在后台静默完成。
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("CryptProtectData 调用失败: %w", err)
	}
	defer freeBlob(&out)

	return copyBlob(&out), nil
}

// unprotect 用 DPAPI 解密。
func unprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, fmt.Errorf("密文为空")
	}
	in := windows.DataBlob{
		Size: uint32(len(cipher)),
		Data: &cipher[0],
	}
	var out windows.DataBlob

	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData 调用失败: %w", err)
	}
	defer freeBlob(&out)

	return copyBlob(&out), nil
}

// copyBlob 把 DPAPI 分配的缓冲区拷进 Go 管理的内存。
// 必须拷贝：原缓冲区随后会被 LocalFree 释放。
func copyBlob(b *windows.DataBlob) []byte {
	if b.Data == nil || b.Size == 0 {
		return nil
	}
	src := unsafe.Slice(b.Data, b.Size)
	dst := make([]byte, b.Size)
	copy(dst, src)
	return dst
}

// freeBlob 释放 DPAPI 分配的缓冲区。
// 不释放会造成每次加解密都泄漏一小块内存。
func freeBlob(b *windows.DataBlob) {
	if b.Data != nil {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data)))
		b.Data = nil
	}
}

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func decodeBase64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

package kernel

import (
	"errors"
	"os"
	"testing"
)

// 指纹模式的支持性判断必须严格：只有 fingerprint-chromium 打了补丁。
//
// 空串按支持处理是为了兼容已有调用方——List 之外构造的 Kernel 零值
// Source 为空，那些都是指纹内核。但系统 Chrome 必须判为不支持。
func TestSourceSupportsFingerprint(t *testing.T) {
	if !SourceFingerprint.SupportsFingerprint() {
		t.Error("指纹内核应支持伪造")
	}
	if !Source("").SupportsFingerprint() {
		t.Error("空来源应按指纹内核处理，否则会拦住已有调用方")
	}
	if SourceSystemChrome.SupportsFingerprint() {
		t.Error("系统 Chrome 不支持伪造，必须判为 false——" +
			"判错会让指纹 profile 在官方 Chrome 上静默失去全部伪装")
	}
}

// List 出的内核必须标记为指纹来源，否则 session 的第二道闸会误拦。
func TestListMarksFingerprintSource(t *testing.T) {
	root := t.TempDir()
	dir := root + string(os.PathSeparator) + "148.0.7778.215"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	exe := dir + string(os.PathSeparator) + execName()
	if err := os.WriteFile(exe, []byte("stub"), 0o700); err != nil {
		t.Fatalf("写桩失败: %v", err)
	}

	list, err := NewStore(root).List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应列出 1 个内核，实际 %d", len(list))
	}
	if list[0].Source != SourceFingerprint {
		t.Errorf("Source 应为 %q，实际 %q", SourceFingerprint, list[0].Source)
	}
	if !list[0].Source.SupportsFingerprint() {
		t.Error("已安装内核应支持指纹伪造")
	}
}

// 探测系统 Chrome。本机未装时跳过，装了则校验返回值自洽。
//
// 不断言"必须找到"：CI 环境通常没有 Chrome，那不是代码错误。
func TestSystemChrome(t *testing.T) {
	k, err := SystemChrome()
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("未装 Chrome 时应返回 ErrNotFound，实际 %v", err)
		}
		t.Skipf("本机未安装 Chrome，跳过: %v", err)
	}

	t.Logf("找到系统 Chrome: %s（版本 %q）", k.ExecPath, k.Version)
	if k.Source != SourceSystemChrome {
		t.Errorf("Source 应为 %q，实际 %q", SourceSystemChrome, k.Source)
	}
	if k.Source.SupportsFingerprint() {
		t.Error("系统 Chrome 必须判为不支持伪造")
	}
	if k.Name == "" {
		t.Error("应有界面显示名")
	}
	if info, err := os.Stat(k.ExecPath); err != nil || info.IsDir() {
		t.Errorf("返回的路径不是可执行文件: %v", err)
	}
}

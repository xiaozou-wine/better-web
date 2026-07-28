package shortcut

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// profile 名称由用户自由输入，其中的路径分隔符必须被清洗。
//
// 不清洗的后果不只是创建失败：名称里带 ..\ 会让快捷方式写到目标目录之外。
func TestSanitizeFileNameStripsPathSeparators(t *testing.T) {
	cases := map[string]string{
		`美国-01`:         `美国-01`,
		`a/b`:           `a_b`,
		`a\b`:           `a_b`,
		`..\..\Windows`: `.._.._Windows`,
		`a:b`:           `a_b`,
		`a<b>c`:         `a_b_c`,
		`a|b?c*d`:       `a_b_c_d`,
		`a"b`:           `a_b`,
		"tab\there":     `tab_here`,
		"newline\nhere": `newline_here`,
		`  两端空格  `:      `两端空格`,
		`结尾点.`:          `结尾点`,
		`结尾多个点...`:      `结尾多个点`,
		`   `:           `profile`,
		`...`:           `profile`,
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// 清洗后的名称不得含任何路径分隔符，否则快捷方式会落到别处。
func TestSanitizeFileNameNeverContainsSeparator(t *testing.T) {
	for _, in := range []string{
		`../../etc/passwd`, `..\..\..\Windows\System32`,
		`C:\absolute\path`, `/usr/local/bin`,
	} {
		got := sanitizeFileName(in)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("sanitizeFileName(%q) = %q 仍含路径分隔符", in, got)
		}
		// 拼接后必须仍在目标目录内。
		joined := filepath.Join("base", got)
		if !strings.HasPrefix(filepath.Clean(joined), "base") {
			t.Errorf("sanitizeFileName(%q) 拼接后逃出了目标目录: %q", in, joined)
		}
	}
}

// 超长名称要截断，否则路径可能超出系统上限而创建失败。
func TestSanitizeFileNameTruncates(t *testing.T) {
	long := strings.Repeat("很长的名字", 40)
	got := sanitizeFileName(long)
	if len(got) > 80 {
		t.Errorf("清洗后长度 %d 超过 80", len(got))
	}
	if got == "" {
		t.Error("截断后不应为空")
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	for _, name := range []string{"", "   ", "\t"} {
		if _, err := Create(Options{ProfileName: name, Dir: t.TempDir()}); err == nil {
			t.Errorf("名称 %q 应被拒绝", name)
		}
	}
}

// 实际创建：验证文件确实生成，且内容里含 --profile 参数。
func TestCreateProducesShortcut(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS 不支持自动创建，见 shortcut_unix.go")
	}
	dir := t.TempDir()
	// 用一个真实存在的可执行文件作为目标，避免因路径无效而失败。
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("定位测试可执行文件失败: %v", err)
	}

	const name = "测试-01"
	path, err := Create(Options{ProfileName: name, Dir: dir, ExePath: exe})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("快捷方式文件不存在: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("快捷方式写到了 %q，期望在 %q 内", path, dir)
	}

	// Windows 的 .lnk 是二进制，只能校验存在与非空；
	// Linux 的 .desktop 是文本，可以检查参数是否正确写入。
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("快捷方式为空或不可读: %v", err)
	}
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		content := string(b)
		if !strings.Contains(content, "--profile=") {
			t.Error(".desktop 中缺少 --profile 参数")
		}
		if !strings.Contains(content, name) {
			t.Error(".desktop 中缺少 profile 名称")
		}
	}
}

// 含引号与空格的名称不得破坏生成的文件——那既是正确性问题也是注入面。
func TestCreateHandlesHostileNames(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS 不支持自动创建")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("定位测试可执行文件失败: %v", err)
	}
	hostile := []string{
		`带 空格`,
		`带"引号`,
		`带'单引号`,
		`$(whoami)`,
		"`whoami`",
		`带;分号`,
		`带&与号`,
	}
	for _, name := range hostile {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := Create(Options{ProfileName: name, Dir: dir, ExePath: exe})
			if err != nil {
				t.Fatalf("Create 失败: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("文件未生成: %v", err)
			}
			// 生成物必须留在指定目录内。
			if filepath.Dir(path) != dir {
				t.Errorf("写到了目录外: %q", path)
			}
		})
	}
}

func TestEncodeUTF16LEIsStable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 使用该编码")
	}
	// 已知值：ASCII "A" 的 UTF-16LE 是 0x41 0x00，base64 为 "QQA="。
	if got := encodeUTF16LE("A"); got != "QQA=" {
		t.Errorf("encodeUTF16LE(\"A\") = %q, 期望 QQA=", got)
	}
}

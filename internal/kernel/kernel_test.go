package kernel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// installFake 在 root 下造一份指定版本的假内核。
func installFake(t *testing.T, root, version string) string {
	t.Helper()
	dir := filepath.Join(root, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建假内核目录失败: %v", err)
	}
	exec := filepath.Join(dir, execName())
	if err := os.WriteFile(exec, []byte("fake"), 0o755); err != nil {
		t.Fatalf("写入假内核失败: %v", err)
	}
	return exec
}

// 内核目录不存在是首次运行的正常状态，不该报错。
func TestListReturnsEmptyWhenRootMissing(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "not-exist"))
	list, err := s.List()
	if err != nil {
		t.Fatalf("List 报错: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("期望空列表，实际 %d 项", len(list))
	}
}

// 版本必须按数值降序，不能按字符串排（否则 9 会排在 148 前面）。
func TestListSortsByVersionDescending(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"139.0.7258.154", "148.0.7778.215", "99.0.1.1", "144.0.7559.132"} {
		installFake(t, root, v)
	}
	list, err := NewStore(root).List()
	if err != nil {
		t.Fatalf("List 报错: %v", err)
	}
	want := []string{"148.0.7778.215", "144.0.7559.132", "139.0.7258.154", "99.0.1.1"}
	if len(list) != len(want) {
		t.Fatalf("内核数量 = %d, 期望 %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i].Version != w {
			t.Errorf("第 %d 项 = %q, 期望 %q", i, list[i].Version, w)
		}
	}
}

// 目录里没有可执行文件的视为残留安装，必须跳过而不是报出一个用不了的内核。
func TestListSkipsDirsWithoutExecutable(t *testing.T) {
	root := t.TempDir()
	installFake(t, root, "148.0.7778.215")
	if err := os.MkdirAll(filepath.Join(root, "147.0.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := NewStore(root).List()
	if err != nil {
		t.Fatalf("List 报错: %v", err)
	}
	if len(list) != 1 || list[0].Version != "148.0.7778.215" {
		t.Errorf("期望只识别出 148.0.7778.215, 实际 %+v", list)
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	installFake(t, root, "144.0.7559.132")
	want := installFake(t, root, "148.0.7778.215")
	s := NewStore(root)

	t.Run("留空取最高版本", func(t *testing.T) {
		k, err := s.Resolve("")
		if err != nil {
			t.Fatalf("Resolve 报错: %v", err)
		}
		if k.Version != "148.0.7778.215" || k.ExecPath != want {
			t.Errorf("Resolve(\"\") = %+v", k)
		}
	})

	t.Run("锁定指定版本", func(t *testing.T) {
		k, err := s.Resolve("144.0.7559.132")
		if err != nil {
			t.Fatalf("Resolve 报错: %v", err)
		}
		if k.Version != "144.0.7559.132" {
			t.Errorf("Resolve 返回版本 %q", k.Version)
		}
	})

	t.Run("版本未安装时报错", func(t *testing.T) {
		if _, err := s.Resolve("999.0.0.0"); !errors.Is(err, ErrNotFound) {
			t.Errorf("期望 ErrNotFound, 实际 %v", err)
		}
	})

	t.Run("目录为空时报错", func(t *testing.T) {
		if _, err := NewStore(t.TempDir()).Resolve(""); !errors.Is(err, ErrNotFound) {
			t.Errorf("期望 ErrNotFound, 实际 %v", err)
		}
	})
}

func TestMajor(t *testing.T) {
	cases := map[string]int{
		"148.0.7778.215": 148,
		"99.0.1.1":       99,
		"":               0,
		"abc":            0,
	}
	for v, want := range cases {
		if got := (Kernel{Version: v}).Major(); got != want {
			t.Errorf("Kernel{%q}.Major() = %d, 期望 %d", v, got, want)
		}
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"148.0.7778.215", "144.0.7559.132", 1},
		{"144.0.7559.132", "148.0.7778.215", -1},
		{"148.0.7778.215", "148.0.7778.215", 0},
		{"99.0.1.1", "148.0.0.0", -1},
		{"148.0", "148.0.0.1", -1},
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q, %q) = %d, 期望 %d", c.a, c.b, got, c.want)
		}
	}
}

package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip 生成一个内存 zip，entries 的 key 为包内路径。
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目 %s 失败: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// serveZip 起一个返回指定 zip 的测试服务器。
func serveZip(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallExtractsKernel(t *testing.T) {
	// 模拟真实包结构：带一层顶层目录。
	top := "ungoogled-chromium_148.0.7778.215"
	data := buildZip(t, map[string]string{
		top + "/" + execName():     "fake-kernel-binary",
		top + "/resources.pak":     "pak",
		top + "/locales/zh-CN.pak": "locale",
	})
	srv := serveZip(t, data)

	root := t.TempDir()
	store := NewStore(root)
	inst := NewInstaller(store)

	var lastDownloaded, lastTotal int64
	k, err := inst.Install(context.Background(), Release{
		Version:     "148.0.7778.215",
		DownloadURL: srv.URL,
		Size:        int64(len(data)),
		AssetName:   "ungoogled-chromium_148.0.7778.215-1.1_windows_x64.zip",
	}, func(d, tt int64) { lastDownloaded, lastTotal = d, tt })
	if err != nil {
		t.Fatalf("Install 失败: %v", err)
	}

	if k.Version != "148.0.7778.215" {
		t.Errorf("版本 = %q", k.Version)
	}
	// 可执行文件必须落在 <root>/<版本>/ 下，这是 Store.List 识别的布局。
	wantExec := filepath.Join(root, "148.0.7778.215", execName())
	if k.ExecPath != wantExec {
		t.Errorf("ExecPath = %q, 期望 %q", k.ExecPath, wantExec)
	}
	if b, err := os.ReadFile(wantExec); err != nil || string(b) != "fake-kernel-binary" {
		t.Errorf("内核文件内容不符: %v", err)
	}
	// 顶层目录应被剥掉，附属文件与可执行文件同级。
	if _, err := os.Stat(filepath.Join(root, "148.0.7778.215", "locales", "zh-CN.pak")); err != nil {
		t.Errorf("附属文件未正确解压: %v", err)
	}

	// 安装完必须能被 Store 识别。
	list, err := store.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 1 || list[0].Version != "148.0.7778.215" {
		t.Errorf("安装后 List = %+v", list)
	}

	if lastTotal != int64(len(data)) || lastDownloaded != lastTotal {
		t.Errorf("进度回调最终值 = %d/%d, 期望 %d/%d",
			lastDownloaded, lastTotal, len(data), len(data))
	}

	// 不留临时目录。
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Errorf("残留临时目录: %s", e.Name())
		}
	}
}

// 压缩包内的 "../" 条目可以覆写目录之外的任意文件（Zip Slip）。
// 内核包来自第三方，必须当不可信输入处理。
func TestExtractRejectsPathTraversal(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, evil := range []string{
		"../../../../../../evil.txt",
		"foo/../../evil.txt",
	} {
		t.Run(evil, func(t *testing.T) {
			data := buildZip(t, map[string]string{
				evil:       "pwned",
				execName(): "kernel",
			})
			srv := serveZip(t, data)
			root := t.TempDir()
			_, err := NewInstaller(NewStore(root)).Install(context.Background(), Release{
				Version: "148.0.0.1", DownloadURL: srv.URL, Size: int64(len(data)),
			}, nil)
			if err == nil {
				t.Fatal("含路径穿越的压缩包期望被拒绝，实际安装成功")
			}
			if !strings.Contains(err.Error(), "非法路径") &&
				!strings.Contains(err.Error(), "越界路径") {
				t.Errorf("错误信息未指出路径问题: %v", err)
			}
		})
	}

	if b, _ := os.ReadFile(victim); string(b) != "original" {
		t.Error("目录外的文件被压缩包覆写了")
	}
}

// 版本号会作为目录名，必须挡住路径穿越与盘符。
func TestInstallRejectsUnsafeVersion(t *testing.T) {
	data := buildZip(t, map[string]string{execName(): "k"})
	srv := serveZip(t, data)
	for _, v := range []string{
		"../escape", "..", "148/../../etc", `C:\windows`, "148;rm -rf", "",
	} {
		t.Run(v, func(t *testing.T) {
			_, err := NewInstaller(NewStore(t.TempDir())).Install(context.Background(),
				Release{Version: v, DownloadURL: srv.URL}, nil)
			if err == nil {
				t.Errorf("版本号 %q 期望被拒绝", v)
			}
		})
	}
}

// 包里没有可执行文件说明结构变了，必须报错而不是留下一个装不全的版本。
func TestInstallFailsWhenExecutableMissing(t *testing.T) {
	data := buildZip(t, map[string]string{"some/readme.txt": "no kernel here"})
	srv := serveZip(t, data)
	root := t.TempDir()

	_, err := NewInstaller(NewStore(root)).Install(context.Background(), Release{
		Version: "148.0.0.1", DownloadURL: srv.URL, Size: int64(len(data)),
	}, nil)
	if err == nil {
		t.Fatal("缺少可执行文件时期望报错")
	}

	// 失败后不能留下半装的版本：List 会把它当成一个用不了的内核。
	list, err := NewStore(root).List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("失败后残留了内核记录: %+v", list)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".staging-") {
			t.Errorf("失败后残留目录: %s", e.Name())
		}
	}
}

func TestInstallRejectsDuplicateVersion(t *testing.T) {
	root := t.TempDir()
	installFake(t, root, "148.0.7778.215")
	data := buildZip(t, map[string]string{execName(): "k"})
	srv := serveZip(t, data)

	_, err := NewInstaller(NewStore(root)).Install(context.Background(), Release{
		Version: "148.0.7778.215", DownloadURL: srv.URL,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("重复安装期望报错，实际 %v", err)
	}
}

// 服务端谎报长度或中途截断时，必须报错而非留下损坏的内核。
func TestInstallDetectsTruncatedDownload(t *testing.T) {
	data := buildZip(t, map[string]string{execName(): "kernel"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 声明完整长度但只写一半。
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data[:len(data)/2])
	}))
	defer srv.Close()

	_, err := NewInstaller(NewStore(t.TempDir())).Install(context.Background(), Release{
		Version: "148.0.0.1", DownloadURL: srv.URL, Size: int64(len(data)),
	}, nil)
	if err == nil {
		t.Error("下载被截断时期望报错")
	}
}

func TestInstallReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewInstaller(NewStore(t.TempDir())).Install(context.Background(), Release{
		Version: "148.0.0.1", DownloadURL: srv.URL,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("期望报出 HTTP 404, 实际 %v", err)
	}
}

func TestInstallCancelledByContext(t *testing.T) {
	data := buildZip(t, map[string]string{execName(): "k"})
	srv := serveZip(t, data)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewInstaller(NewStore(t.TempDir())).Install(ctx, Release{
		Version: "148.0.0.1", DownloadURL: srv.URL,
	}, nil)
	if err == nil {
		t.Error("context 已取消时期望报错")
	}
}

func TestSafeJoin(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ok := []string{"chrome.exe", "locales/zh-CN.pak", "a/b/c.dat", "./chrome.exe"}
	for _, name := range ok {
		if _, err := safeJoin(base, name); err != nil {
			t.Errorf("safeJoin(%q) 意外报错: %v", name, err)
		}
	}
	bad := []string{"../evil", "../../evil", "a/../../evil", `\\server\share\evil`}
	for _, name := range bad {
		if _, err := safeJoin(base, name); err == nil {
			t.Errorf("safeJoin(%q) 期望报错，实际通过", name)
		}
	}
}

func TestSafeVersion(t *testing.T) {
	for _, v := range []string{"148.0.7778.215", "99.0.1.1", "148"} {
		if !safeVersion(v) {
			t.Errorf("safeVersion(%q) = false, 期望 true", v)
		}
	}
	for _, v := range []string{"", "..", "../x", "148/x", `C:\x`, "148-beta", "148 0"} {
		if safeVersion(v) {
			t.Errorf("safeVersion(%q) = true, 期望 false", v)
		}
	}
}

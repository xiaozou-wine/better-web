package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 用真实的 fingerprint-chromium 发行包验证安装器。
//
// 默认跳过：需要一个约 190MB 的真实包，CI 环境不该依赖它。
// 通过 BW_REAL_KERNEL_ZIP 指向本地已下载的包来启用：
//
//	BW_REAL_KERNEL_ZIP=/tmp/bw-kernel/kernel.zip go test -run TestInstallRealPackage ./internal/kernel/
//
// 这条测试关心的是真实包的目录结构能否被正确识别——findExecRoot 依赖
// "带一层顶层目录"这个假设，包结构变更会让安装静默产出用不了的内核。
func TestInstallRealPackage(t *testing.T) {
	src := os.Getenv("BW_REAL_KERNEL_ZIP")
	if src == "" {
		t.Skip("未设置 BW_REAL_KERNEL_ZIP，跳过真实包安装测试")
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("读取真实包失败: %v", err)
	}

	// 用本地文件服务器供包，走与线上完全相同的下载代码路径。
	fileSrv := newFileServer(t, src, info.Size())

	root := t.TempDir()
	store := NewStore(root)

	const version = "148.0.7778.215"
	k, err := NewInstaller(store).Install(context.Background(), Release{
		Version:     version,
		DownloadURL: fileSrv,
		Size:        info.Size(),
		AssetName:   "ungoogled-chromium_148.0.7778.215-1.1_windows_x64.zip",
	}, nil)
	if err != nil {
		t.Fatalf("安装真实包失败: %v", err)
	}

	// 顶层目录必须被剥掉，可执行文件直接位于版本目录下。
	wantExec := filepath.Join(root, version, execName())
	if k.ExecPath != wantExec {
		t.Errorf("ExecPath = %q, 期望 %q", k.ExecPath, wantExec)
	}
	if fi, err := os.Stat(wantExec); err != nil {
		t.Fatalf("可执行文件缺失: %v", err)
	} else if fi.Size() == 0 {
		t.Error("可执行文件为空")
	}

	// 真实内核依赖这些同级文件，缺任何一个都起不来。
	for _, name := range []string{"chrome.dll", "icudtl.dat", "resources.pak"} {
		if _, err := os.Stat(filepath.Join(root, version, name)); err != nil {
			t.Errorf("依赖文件 %s 缺失: %v", name, err)
		}
	}
	// 子目录也应完整解出。
	if entries, err := os.ReadDir(filepath.Join(root, version, "locales")); err != nil {
		t.Errorf("locales 目录缺失: %v", err)
	} else if len(entries) == 0 {
		t.Error("locales 目录为空")
	}

	// 安装结果必须能被 Store 识别。
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].Version != version {
		t.Errorf("List = %+v (err=%v)", list, err)
	}
	fmt.Printf("真实包安装成功: %s\n", k.ExecPath)
}

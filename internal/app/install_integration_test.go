package app

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"better-web/internal/kernel"
	"better-web/internal/model"
)

// 走完整链路：安装内核 → 创建 profile → 启动 → 校验内核收到的参数。
//
// 这里用本地服务器提供一个结构与真实包一致的内核压缩包，内核可执行文件是
// 一个把命令行参数写入文件的桩程序。真实 Chromium 的行为不在本测试范围内，
// 但从"下载安装"到"参数正确送达内核"这段全部被覆盖。
func TestInstallThenLaunchEndToEnd(t *testing.T) {
	exec := kernelExecName()
	stub := buildArgsDumpingBinary(t)

	top := "ungoogled-chromium_148.0.7778.215"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.CreateHeader(&zip.FileHeader{
		Name:   top + "/" + exec,
		Method: zip.Deflate,
	})
	if err != nil {
		t.Fatalf("创建 zip 条目失败: %v", err)
	}
	if _, err := entry.Write(stub); err != nil {
		t.Fatalf("写入桩程序失败: %v", err)
	}
	// 附带一个资源文件，验证整包解压而非只取可执行文件。
	res, _ := zw.Create(top + "/resources.pak")
	if _, err := res.Write([]byte("pak")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	pkg := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(pkg)))
		_, _ = w.Write(pkg)
	}))
	defer srv.Close()

	svc, paths := newTestService(t)

	// 安装前应当没有可用内核。
	if list, err := svc.ListKernels(); err != nil || len(list) != 0 {
		t.Fatalf("初始内核列表 = %+v (err=%v)", list, err)
	}

	var lastProgress InstallProgress
	k, err := svc.InstallKernel(context.Background(), kernel.Release{
		Version:     "148.0.7778.215",
		DownloadURL: srv.URL,
		Size:        int64(len(pkg)),
		AssetName:   "ungoogled-chromium_148.0.7778.215-1.1_windows_x64.zip",
	}, func(p InstallProgress) { lastProgress = p })
	if err != nil {
		t.Fatalf("InstallKernel 失败: %v", err)
	}
	if !lastProgress.Done || lastProgress.Err != "" {
		t.Errorf("最终进度 = %+v, 期望 done 且无错误", lastProgress)
	}
	if k.Version != "148.0.7778.215" {
		t.Errorf("安装版本 = %q", k.Version)
	}
	if _, err := os.Stat(filepath.Join(paths.Kernels, k.Version, "resources.pak")); err != nil {
		t.Errorf("资源文件未随包解压: %v", err)
	}

	// 安装后内核应可被识别。
	list, err := svc.ListKernels()
	if err != nil || len(list) != 1 {
		t.Fatalf("安装后内核列表 = %+v (err=%v)", list, err)
	}

	// 用刚安装的内核启动一个指纹 profile。
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("BW_ARGS_FILE", argsFile)

	view, err := svc.CreateProfile(context.Background(), CreateRequest{
		Name: "端到端-01", Kind: model.KindFingerprint,
		GeoOverride: &model.Geo{
			CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	st, err := svc.Start(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	// 必须等进程真正退出：Stop 只投递关闭消息就返回，
	// 测试随即结束会留下残留进程，累积起来耗尽本机端口。
	t.Cleanup(func() { stopAndWait(t, svc, []string{view.ID}) })
	if st.PID <= 0 {
		t.Errorf("PID = %d", st.PID)
	}

	// 校验内核实际收到的参数，这是整条链路的最终事实。
	args := readArgsFile(t, argsFile)
	assertArg(t, args, "--fingerprint", fmt.Sprint(view.Seed))
	assertArg(t, args, "--timezone", "America/Los_Angeles")
	assertArg(t, args, "--lang", "en-US")
	assertArg(t, args, "--accept-lang", "en-US,en;q=0.9")
	assertArg(t, args, "--user-data-dir", paths.ProfileDir(view.ID))
	if view.Fingerprint == nil {
		t.Fatal("指纹配置缺失")
	}
	assertArg(t, args, "--fingerprint-platform", string(view.Fingerprint.Device.Platform))
}

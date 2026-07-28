package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/probe"
)

// 端到端：用真实内核建一个开启 GPU 匹配的 profile，核对种子确实筛过。
//
// 启用方式（需已安装内核）：
//
//	BW_RUN_GPU_MATCH=1 go test -run TestCreateProfileMatchesHostGPU -timeout 600s -v ./internal/app/
//
// 与 gpumatch_test.go 里的单元测试互补：那些用不装内核的服务验证失败路径，
// 这个验证成功路径——筛出的种子派生出的 GPU 是否真与宿主机同族。
func TestCreateProfileMatchesHostGPU(t *testing.T) {
	if os.Getenv("BW_RUN_GPU_MATCH") != "1" {
		t.Skip("未设置 BW_RUN_GPU_MATCH=1，跳过真实内核 GPU 匹配")
	}

	// 用真实的内核目录，但 profile 库放临时目录，不污染用户数据。
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("定位用户配置目录失败: %v", err)
	}
	realKernels := filepath.Join(base, "better-web", "kernels")
	k, err := kernel.NewStore(realKernels).Resolve("")
	if err != nil {
		t.Skipf("未安装内核，跳过: %v", err)
	}

	tmp := t.TempDir()
	s, err := New(Paths{
		Root:     tmp,
		DB:       filepath.Join(tmp, "profiles.db"),
		Profiles: filepath.Join(tmp, "profiles"),
		Kernels:  realKernels,
	})
	if err != nil {
		t.Fatalf("初始化服务失败: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx := context.Background()
	host, hostRenderer, err := probe.HostGPU(ctx, k.ExecPath)
	if err != nil {
		t.Fatalf("探测宿主机 GPU 失败: %v", err)
	}
	t.Logf("宿主机 GPU: %s（%s）", host, hostRenderer)

	v, err := s.CreateProfile(ctx, CreateRequest{
		Name: "gpu匹配", Kind: model.KindFingerprint, MatchHostGPU: true,
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	t.Logf("创建成功: 种子 %d", v.Seed)
	t.Logf("备注: %s", v.Notes)

	if v.Notes == "" {
		t.Error("应在备注里记录筛选结果，否则日后无从判断该 profile 是否筛过")
	}

	// 核对：用该 profile 的种子实测派生出的 GPU 族。
	fam, renderer, err := probe.SeedGPUFamily(ctx, k.ExecPath, v.Seed)
	if err != nil {
		t.Fatalf("核对种子 GPU 失败: %v", err)
	}
	t.Logf("该种子派生的 GPU: %s（%s）", fam, renderer)
	if fam != host {
		t.Errorf("筛选未生效：种子派生 %s，宿主机为 %s", fam, host)
	}
}

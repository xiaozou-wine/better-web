package probe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/proxy"
)

// 在真实检测站点上跑分。
//
// 默认跳过：需要真实内核、可用代理和外网访问。启用方式：
//
//	BW_TEST_PROXY=socks5://127.0.0.1:10808 BW_RUN_SCORE=1 \
//	  go test -run TestScoreRealSites -timeout 900s -v ./internal/probe/
//
// 这不是通过/失败的断言，而是采集当前配置在各检测站的表现，
// 用于跨内核版本比对趋势。绝对分数不宜与他人结果对照：评分受出口 IP、
// 系统字体、GPU 等宿主机因素影响。
func TestScoreRealSites(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过检测站跑分")
	}
	k := realKernel(t)
	up := parseTestProxy(t)

	fwd, err := proxy.New(up)
	if err != nil {
		t.Fatalf("创建转发器失败: %v", err)
	}
	addr, err := fwd.Start()
	if err != nil {
		t.Fatalf("启动转发器失败: %v", err)
	}
	defer func() { _ = fwd.Close() }()

	client, err := fwd.HTTPClient()
	if err != nil {
		t.Fatalf("构造代理客户端失败: %v", err)
	}
	resolved, err := geo.NewResolver(client).Lookup(context.Background())
	if err != nil {
		t.Fatalf("查询出口地失败: %v", err)
	}
	t.Logf("出口: %s / %s / %s", resolved.CountryCode, resolved.Timezone, resolved.Locale)

	fp := fingerprint.Derive(20260727, &resolved)
	p := &model.Profile{
		ID: "score", Name: "score", Kind: model.KindFingerprint,
		Seed: fp.Seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, addr, nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	t.Logf("机型: %s / 声明 %s %s / %d 核",
		fp.Device.Label, fp.Device.Platform, fp.Device.PlatformVersion,
		fp.Device.HardwareConcurrency)

	results, err := (&Scorer{ExecPath: k.ExecPath}).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("跑分失败: %v", err)
	}

	failed := 0
	for _, r := range results {
		t.Logf("[%s] %s（耗时 %dms）", r.Site, r.Summary(), r.ElapsedMs)
		for key, val := range r.Metrics {
			if key == "excerpt" {
				continue // 摘要太长，只在需要时单独看
			}
			t.Logf("    %s = %v", key, val)
		}
		if r.Err != "" {
			failed++
		}
	}
	if failed == len(results) {
		t.Fatal("所有检测站点均采集失败，跑分链路本身有问题")
	}

	out := filepath.Join("testdata", "scores-"+k.Version+".json")
	if err := SaveScores(out, results); err != nil {
		t.Fatalf("保存跑分结果失败: %v", err)
	}
	t.Logf("跑分结果已写入 %s", out)

	// 用门禁校验：内核升级后 patch 失效往往不报错，只让伪造静默失灵。
	if violations := DefaultGate.Check(results); len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("门禁未通过: %s", v)
		}
	}
}

// 只跑 CreepJS，便于快速迭代提取表达式。
func TestScoreCreepJSOnly(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过 CreepJS 跑分")
	}
	k := realKernel(t)
	up := parseTestProxy(t)

	fwd, err := proxy.New(up)
	if err != nil {
		t.Fatalf("创建转发器失败: %v", err)
	}
	addr, err := fwd.Start()
	if err != nil {
		t.Fatalf("启动转发器失败: %v", err)
	}
	defer func() { _ = fwd.Close() }()

	client, _ := fwd.HTTPClient()
	resolved, err := geo.NewResolver(client).Lookup(context.Background())
	if err != nil {
		t.Fatalf("查询出口地失败: %v", err)
	}
	fp := fingerprint.Derive(20260727, &resolved)
	p := &model.Profile{
		ID: "score", Name: "score", Kind: model.KindFingerprint,
		Seed: fp.Seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, addr, nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}

	results, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{CreepJS}}).
		Run(context.Background(), args)
	if err != nil {
		t.Fatalf("跑分失败: %v", err)
	}
	r := results[0]
	if r.Err != "" {
		t.Fatalf("CreepJS 采集失败: %s", r.Err)
	}
	for key, val := range r.Metrics {
		t.Logf("%s = %v", key, val)
	}
	if rendered, ok := r.Metrics["rendered"].(bool); ok && !rendered {
		t.Error("CreepJS 评分区块未渲染，SettleMs 可能不够或选择器已变更")
	}
}

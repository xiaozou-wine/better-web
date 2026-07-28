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

// 在真实商业风控站点上实测能否通过。
//
// 默认跳过：需要真实内核、可用代理和外网访问。启用方式：
//
//	BW_TEST_PROXY=http://user:pass@host:port BW_RUN_ANTIBOT=1 \
//	  go test -run TestAntibotRealSites -timeout 600s -v ./internal/probe/
//
// 与 TestScoreRealSites 的区别：那个测的是环境自洽性评分（自查工具，判定
// 逻辑公开）；这个测的是 Cloudflare 与 DataDome 的放行/拦截结果（判定逻辑
// 不公开，且随 IP 信誉浮动）。
//
// 因此本测试不设断言门槛，只记录结论。单次通过不证明指纹过关——可能只是
// 出口 IP 当时干净；单次拦截也不证明指纹有问题——可能只是该 IP 被标记过。
// 结论必须与出口 ASN 一起解读，这也是下面要把出口信息打出来的原因。
func TestAntibotRealSites(t *testing.T) {
	if os.Getenv("BW_RUN_ANTIBOT") != "1" {
		t.Skip("未设置 BW_RUN_ANTIBOT=1，跳过商业风控实测")
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
	// 用 LookupExit 而非 Lookup：需要 ASN 与网络类型来解读判定结果。
	info, err := geo.NewResolver(client).LookupExit(context.Background())
	if err != nil {
		t.Fatalf("查询出口失败: %v", err)
	}
	t.Logf("出口: %s / %s / %s (%s)",
		info.IP, info.Geo.CountryCode, info.Org, info.Kind)

	resolved := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
	fp := fingerprint.DeriveForKernel(20260727, &resolved, k.Version)
	p := &model.Profile{
		ID: "antibot", Name: "antibot", Kind: model.KindFingerprint,
		Seed: fp.Seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, addr, nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	t.Logf("机型: %s / 声明 %s / %s / %s",
		fp.Device.Label, fp.Device.Platform, fp.Timezone, fp.Locale)

	results, err := (&Scorer{ExecPath: k.ExecPath, Sites: AntibotSites}).
		Run(context.Background(), args)
	if err != nil {
		t.Fatalf("实测失败: %v", err)
	}

	collected := 0
	for _, r := range results {
		t.Logf("\n[%s] %s（耗时 %dms）", r.Site, r.Summary(), r.ElapsedMs)
		if r.Err != "" {
			t.Logf("    采集失败: %s", r.Err)
			continue
		}
		collected++
		for _, key := range []string{
			"passed", "finalURL", "finalHost", "title", "onCaptchaDomain",
			"challengeSelectors", "challengeTexts", "blockedTexts",
			"bodyLen", "__settled", "__settleMs", "__supersededPass",
		} {
			if v, ok := r.Metrics[key]; ok && v != nil {
				t.Logf("    %s = %v", key, v)
			}
		}
		if v, ok := r.Metrics["excerpt"]; ok {
			t.Logf("    excerpt = %v", v)
		}
	}
	if collected == 0 {
		t.Fatal("所有站点均采集失败，实测链路本身有问题")
	}

	out := filepath.Join("testdata", "antibot-"+k.Version+".json")
	if err := SaveScores(out, results); err != nil {
		t.Fatalf("保存结果失败: %v", err)
	}
	t.Logf("结果已写入 %s", out)
}

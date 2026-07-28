package probe

import (
	"context"
	"os"
	"strconv"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
)

// kernelProber 把本包的探测能力适配成 fingerprint.GPUProber。
// 与 app 包的同名类型重复，但让本测试不必依赖 app 包。
type kernelProber struct{ execPath string }

func (k kernelProber) SeedGPUFamily(ctx context.Context, seed int32) (model.GPUFamily, string, error) {
	return SeedGPUFamily(ctx, k.execPath, seed)
}

// 端到端验证：筛出的种子是否真能过 Cloudflare。
//
// 启用方式（直连，不需要代理）：
//
//	BW_RUN_SEED_MATCH=1 go test -run TestSeedMatchEndToEnd -timeout 600s -v ./internal/probe/
//
// 这是整条链路的最终验收：探测宿主机 GPU 族 → 筛选同族种子 → 用该种子实测
// Cloudflare。前面的单元测试只验证了筛选逻辑本身正确，唯有这一步能证明
// 筛出来的种子确实解决了问题。
func TestSeedMatchEndToEnd(t *testing.T) {
	if os.Getenv("BW_RUN_SEED_MATCH") != "1" {
		t.Skip("未设置 BW_RUN_SEED_MATCH=1，跳过端到端验收")
	}
	k := realKernel(t)
	ctx := context.Background()

	host, hostRenderer, err := HostGPU(ctx, k.ExecPath)
	if err != nil {
		t.Fatalf("探测宿主机 GPU 失败: %v", err)
	}
	t.Logf("宿主机 GPU: %s（%s）", host, hostRenderer)

	attempts := 24
	if v := os.Getenv("BW_SEED_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			attempts = n
		}
	}

	match, err := fingerprint.FindSeedForGPUFamily(
		ctx, kernelProber{execPath: k.ExecPath}, host, attempts)
	if err != nil {
		t.Fatalf("筛选种子失败: %v", err)
	}
	t.Logf("筛选结果: 种子 %d（试 %d 个）→ %s",
		match.Seed, len(match.Tried), match.Renderer)
	for _, c := range match.Tried {
		if c.Err != "" {
			t.Logf("  种子 %-12d 探测失败: %s", c.Seed, c.Err)
			continue
		}
		t.Logf("  种子 %-12d → %s", c.Seed, c.Family)
	}

	// 核对筛出的种子确实是同族——筛选逻辑若有 bug，这里会先暴露。
	if got := model.ParseGPUFamily(match.Renderer); got != host {
		t.Fatalf("筛出的种子族为 %s，与宿主机 %s 不符", got, host)
	}

	// 决定性一步：用筛出的种子实测 Cloudflare。
	args := []string{"--fingerprint=" + strconv.Itoa(int(match.Seed))}
	results, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{CloudflareChallenge}}).
		Run(ctx, args)
	if err != nil {
		t.Fatalf("实测失败: %v", err)
	}
	r := results[0]
	if r.Err != "" {
		t.Fatalf("采集失败: %s", r.Err)
	}
	passed, _ := r.Metrics["passed"].(bool)
	t.Logf("\nCloudflare 实测（种子 %d，GPU 伪造开启）: passed=%v title=%v",
		match.Seed, passed, r.Metrics["title"])

	if !passed {
		// 不是 Fatal 而是 Error 并继续：需要把对照组也跑出来，
		// 否则无法判断是筛选没用还是这次访问被 IP 信誉影响。
		t.Errorf("筛出的同族种子仍未通过 Cloudflare，判据可能不止 GPU 厂商族")
	}

	// 对照组：裸跑。它同时充当 IP 信誉的基线——若它也失败，
	// 上面的失败就不能归因到筛选。
	ctrl, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{CloudflareChallenge}}).
		Run(ctx, nil)
	if err == nil && ctrl[0].Err == "" {
		ctrlPassed, _ := ctrl[0].Metrics["passed"].(bool)
		t.Logf("对照组（裸跑，零伪造）: passed=%v", ctrlPassed)
		if !ctrlPassed {
			t.Log("⚠ 裸跑也未通过，本次结果受 IP 信誉影响，不能归因到筛选")
		}
	}
}

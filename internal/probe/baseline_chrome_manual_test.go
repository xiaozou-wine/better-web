package probe

import (
	"context"
	"os"
	"testing"
)

// 用宿主机上真实安装的 Chrome 做对照跑分。
//
// 存在意义：判断某项 "like headless" 命中究竟是本项目伪造得不好，
// 还是桌面版 Chrome 本来就长这样。没有这个对照，就只能靠猜，
// 很容易把上游行为误当成自己的缺陷去"修"，反而制造新的矛盾。
//
//	BW_REAL_CHROME="C:\Program Files\Google\Chrome\Application\chrome.exe" \
//	  BW_RUN_SCORE=1 go test -run TestRealChromeBaseline -timeout 600s -v ./internal/probe/
func TestRealChromeBaseline(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过真实 Chrome 对照")
	}
	exe := os.Getenv("BW_REAL_CHROME")
	if exe == "" {
		t.Skip("未设置 BW_REAL_CHROME，跳过真实 Chrome 对照")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("指定的 Chrome 不可用: %v", err)
	}
	// 已有实例在跑时，新进程会把命令行交给它处理，--user-data-dir 与
	// --load-extension 全部失效，采集脚本永远不会注入，表现为跑分超时。
	// 提前检查并给出明确原因，避免误判成选择器或网络问题。
	if name, running := chromiumAlreadyRunning(); running {
		t.Skipf("检测到 %s 正在运行，对照实验需先完全退出所有 Chromium 系浏览器进程", name)
	}

	// 只加代理，不加任何指纹参数：目的是看未经改造的 Chrome 本身得几分。
	// 代理是必需的，CreepJS 直连不通。
	var args []string
	if raw := os.Getenv("BW_TEST_PROXY"); raw != "" {
		args = append(args, "--proxy-server="+raw)
	}

	results, err := (&Scorer{ExecPath: exe, Sites: []Site{CreepJS}}).
		Run(context.Background(), args)
	if err != nil {
		t.Fatalf("对照跑分失败: %v", err)
	}
	r := results[0]
	if r.Err != "" {
		t.Fatalf("对照采集失败: %s", r.Err)
	}

	t.Logf("真实 Chrome: likeHeadless=%v%% headless=%v%% stealth=%v%% lies=%v",
		r.Metrics["likeHeadlessPct"], r.Metrics["headlessPct"],
		r.Metrics["stealthPct"], r.Metrics["liesCount"])

	flags, _ := r.Metrics["headlessFlags"].(map[string]any)
	if flags != nil {
		t.Logf("真实 Chrome 的 likeHeadless 命中项: %v", flags["likeHeadless"])
	}
	t.Logf("真实 Chrome 的 lowEntropy: %v", r.Metrics["lowEntropy"])
}

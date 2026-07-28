package probe

import (
	"context"
	"os"
	"slices"
	"testing"
)

// 验证 prefersLightColor 这一项能否通过命令行参数消除。
//
// 其余三项（noContentIndex / noContactsManager / noDownlinkMax）是
// ungoogled-chromium 移除 Google 服务组件时连带砍掉的 Web API，
// 无参数可控，只能靠内核补丁补回。
//
//	BW_TEST_PROXY=socks5://127.0.0.1:10808 BW_RUN_SCORE=1 \
//	  go test -run TestDarkModeReducesLikeHeadless -timeout 600s -v ./internal/probe/
func TestDarkModeReducesLikeHeadless(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过参数对照测试")
	}
	k := realKernel(t)
	base := proxiedArgs(t)

	cases := []struct {
		name  string
		extra []string
	}{
		{"默认", nil},
		{"强制深色", []string{"--force-dark-mode"}},
	}

	for _, c := range cases {
		args := slices.Concat(base, c.extra)
		results, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{CreepJS}}).
			Run(context.Background(), args)
		if err != nil {
			t.Fatalf("%s: 跑分失败: %v", c.name, err)
		}
		r := results[0]
		if r.Err != "" {
			t.Errorf("%s: 采集失败: %s", c.name, r.Err)
			continue
		}
		flags, _ := r.Metrics["headlessFlags"].(map[string]any)
		var hits []any
		if flags != nil {
			hits, _ = flags["likeHeadless"].([]any)
		}
		t.Logf("%s: likeHeadless=%v%% 命中项=%v",
			c.name, r.Metrics["likeHeadlessPct"], hits)
	}
}

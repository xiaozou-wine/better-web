package probe

import (
	"context"
	"os"
	"testing"
	"time"

	"better-web/internal/model"
)

// 验证连续采集不会累积残留进程。
//
// 这是对之前一处真实缺陷的回归：原实现只调 cmd.Process.Kill()，
// 而 Chromium 是多进程架构，渲染/GPU/网络子进程会成为孤儿并各自占着
// socket。实测约 40 次采集后残留 15 个进程、近 2000 个 TIME_WAIT，
// 导致后续连接报 "Only one usage of each socket address"。
//
//	BW_RUN_SCORE=1 go test -run TestRepeatedCollectLeavesNoOrphans -timeout 600s -v ./internal/probe/
func TestRepeatedCollectLeavesNoOrphans(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过进程回收验证")
	}
	k := realKernel(t)

	before := countKernelProcesses(t, k.ExecPath)
	t.Logf("采集前的内核进程数: %d", before)

	const rounds = 5
	for i := 0; i < rounds; i++ {
		fp := probeFingerprint(t, int32(1000+i))
		if _, err := (&Probe{ExecPath: k.ExecPath}).
			Collect(context.Background(), fp); err != nil {
			t.Fatalf("第 %d 轮采集失败: %v", i+1, err)
		}
	}

	// 进程终止是异步的，给系统一点回收时间。
	deadline := time.Now().Add(15 * time.Second)
	after := before
	for time.Now().Before(deadline) {
		after = countKernelProcesses(t, k.ExecPath)
		if after <= before {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("%d 轮采集后的内核进程数: %d", rounds, after)
	if after > before {
		t.Errorf("残留了 %d 个内核进程，进程树回收未生效", after-before)
	}
}

// probeFingerprint 构造一份用于采集的启动参数。
func probeFingerprint(t *testing.T, seed int32) []string {
	t.Helper()
	_, args := probeProfile(t, seed, &model.Geo{
		CountryCode: "US", Timezone: "America/New_York", Locale: "en-US",
	})
	return args
}

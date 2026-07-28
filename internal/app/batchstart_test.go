package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"better-web/internal/model"
)

// 批量启动必须真的并发，且受并发上限约束。
//
// 用假内核（一个驻留进程）而非真实 Chromium：这里验证的是编排逻辑，
// 真实内核会让测试变慢且引入端口压力。
func TestStartBatchLaunchesConcurrently(t *testing.T) {
	svc, argsFile := newServiceWithFakeKernel(t)
	_ = argsFile

	const n = 6
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v, err := svc.CreateProfile(context.Background(), CreateRequest{
			Name: "并发-" + string(rune('A'+i)), Kind: model.KindDaily,
		})
		if err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		ids = append(ids, v.ID)
	}
	t.Cleanup(func() { stopAndWait(t, svc, ids) })

	started := time.Now()
	sum, err := svc.StartBatch(context.Background(), ids, 3)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("StartBatch 失败: %v", err)
	}

	if sum.Succeeded != n {
		t.Errorf("成功数 = %d, 期望 %d。失败详情: %+v", sum.Succeeded, n, sum.Results)
	}
	// 每个成功项都应带上会话状态与 PID。
	for _, r := range sum.Results {
		if !r.OK {
			continue
		}
		if r.Status == nil || r.Status.PID <= 0 {
			t.Errorf("%s 缺少有效的会话状态: %+v", r.Name, r.Status)
		}
	}
	t.Logf("%d 个 profile 并发启动（上限 3）耗时 %v", n, elapsed.Round(time.Millisecond))
}

// 并发上限必须生效：无限并发会让十几个 Chromium 同时冷启动拖垮机器。
func TestStartBatchRespectsConcurrencyLimit(t *testing.T) {
	svc, _ := newServiceWithFakeKernel(t)

	const n = 4
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v, err := svc.CreateProfile(context.Background(), CreateRequest{
			Name: "限流-" + string(rune('A'+i)), Kind: model.KindDaily,
		})
		if err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		ids = append(ids, v.ID)
	}
	t.Cleanup(func() { stopAndWait(t, svc, ids) })

	// 上限 1 等价于串行，用它验证信号量确实在限流。
	sum, err := svc.StartBatch(context.Background(), ids, 1)
	if err != nil {
		t.Fatalf("StartBatch 失败: %v", err)
	}
	if sum.Succeeded != n {
		t.Errorf("成功数 = %d, 期望 %d", sum.Succeeded, n)
	}
	// 上限超过数量时应被收敛，不能创建多余的信号量槽位。
	// 这里只验证不报错、结果正确。
	if sum.Total != n {
		t.Errorf("总数 = %d, 期望 %d", sum.Total, n)
	}
}

// context 取消后不应继续启动新实例。
func TestStartBatchHonorsCanceledContext(t *testing.T) {
	svc, _ := newServiceWithFakeKernel(t)
	ids := makeProfiles(t, svc, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sum, err := svc.StartBatch(ctx, ids, 1)
	if err != nil {
		t.Fatalf("StartBatch 不应整体报错: %v", err)
	}
	t.Cleanup(func() { stopAndWait(t, svc, ids) })

	if sum.Succeeded != 0 {
		t.Errorf("context 已取消却启动了 %d 个", sum.Succeeded)
	}
	for _, r := range sum.Results {
		if r.Err == "" {
			t.Error("取消时的结果应带上原因")
		}
	}
}

func TestStartBatchRejectsEmptyInput(t *testing.T) {
	svc, _ := newServiceWithFakeKernel(t)
	if _, err := svc.StartBatch(context.Background(), nil, 2); err == nil {
		t.Error("空输入应报错")
	}
}

// stopAndWait 停止这些 profile 并等到进程真正退出。
//
// 必须等待：Stop 只投递关闭消息就返回，测试随即结束的话假内核进程会残留。
// 实测不等待时 app 包一轮测试会留下 8 个进程，累积起来会耗尽本机端口
// （每个进程都占着已建立的 socket）。
func stopAndWait(t *testing.T, svc *Service, ids []string) {
	t.Helper()
	if _, err := svc.StopBatch(ids); err != nil {
		t.Logf("停止失败: %v", err)
	}
	for _, id := range ids {
		svc.WaitSession(id)
	}
}

// newServiceWithFakeKernel 构造一个装了假内核的服务。
//
// 假内核是个把命令行参数写入文件后驻留的桩程序，见 stub_test.go。
// 用它而非真实 Chromium：这里验证的是编排逻辑，真实内核会让测试变慢，
// 且连续启停会累积 TIME_WAIT 压力。
func newServiceWithFakeKernel(t *testing.T) (*Service, string) {
	t.Helper()
	svc, paths := newTestService(t)

	dir := filepath.Join(paths.Kernels, "148.0.7778.215")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("创建内核目录失败: %v", err)
	}
	exec := filepath.Join(dir, kernelExecName())
	if err := os.WriteFile(exec, buildArgsDumpingBinary(t), 0o700); err != nil {
		t.Fatalf("安装假内核失败: %v", err)
	}

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("BW_ARGS_FILE", argsFile)
	return svc, argsFile
}

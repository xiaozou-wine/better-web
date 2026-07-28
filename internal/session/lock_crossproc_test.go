package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

// 跨进程场景：两个独立的 Manager（模拟界面与命令行工具各自的实例）
// 启动同一个 profile，第二个必须被拒绝。
//
// 这是本机制存在的原因。m.live 是 Manager 的私有状态，两个 Manager
// 各有一张表，都会放行——而两个 Chromium 打开同一个 user-data-dir 会损坏
// profile 数据，Chromium 自己的处理是让后来者静默退出，用户看不到任何原因。
//
// 用两个 Manager 而非两个进程：Manager 是进程内的会话表，两个实例就等价于
// 两个进程的视角，而锁文件是真实的进程间媒介，这样测已足够。
func TestTwoManagersCannotStartSameProfile(t *testing.T) {
	store := buildFakeKernel(t, "148.0.0.0")
	first := NewManager(store)
	second := NewManager(store)
	first.StrictGeo, second.StrictGeo = false, false

	p := newProfile(t, model.KindDaily)

	if _, err := first.Start(context.Background(), p); err != nil {
		t.Fatalf("第一个 Manager 启动失败: %v", err)
	}
	defer func() {
		_ = first.Stop(p.ID)
		first.Wait(p.ID)
	}()

	// 第二个 Manager 的会话表是空的，只有锁文件能拦住它。
	st, err := second.Start(context.Background(), p)
	if err == nil {
		_ = second.Stop(p.ID)
		t.Fatal("第二个 Manager 启动成功了 —— 两个进程会同时打开一个 " +
			"user-data-dir，profile 数据将被损坏")
	}
	if !errors.Is(err, ErrLockedByOtherProcess) {
		t.Errorf("期望 ErrLockedByOtherProcess，实际: %v", err)
	}
	if st.State != StateFailed {
		t.Errorf("状态 = %q, 期望 failed", st.State)
	}
	// 被拒绝的一方不得留下占位，否则它自己后续也启动不了。
	if got := second.Status(p.ID); got.State != StateStopped {
		t.Errorf("被拒后状态 = %q, 期望 stopped", got.State)
	}
	// 也不得删掉或改写持有者的锁。
	holder, stale := readLock(filepath.Join(p.ProfileDir, lockFileName))
	if holder == nil {
		t.Fatal("被拒后锁文件消失了")
	}
	if stale {
		t.Error("持有者仍在运行，锁却被判为陈旧")
	}
	if holder.PID != os.Getpid() {
		t.Errorf("锁文件的 PID = %d, 期望持有者 %d", holder.PID, os.Getpid())
	}
}

// 持有者停止后，另一个 Manager 应当能接手启动。
// 锁不能变成需要人工清理的障碍。
func TestSecondManagerSucceedsAfterFirstStops(t *testing.T) {
	store := buildFakeKernel(t, "148.0.0.0")
	first := NewManager(store)
	second := NewManager(store)
	first.StrictGeo, second.StrictGeo = false, false

	p := newProfile(t, model.KindDaily)
	if _, err := first.Start(context.Background(), p); err != nil {
		t.Fatalf("第一个 Manager 启动失败: %v", err)
	}
	if err := first.Stop(p.ID); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	first.Wait(p.ID)

	if _, err := second.Start(context.Background(), p); err != nil {
		t.Fatalf("持有者已停止，第二个 Manager 仍无法启动: %v", err)
	}
	_ = second.Stop(p.ID)
	second.Wait(p.ID)
}

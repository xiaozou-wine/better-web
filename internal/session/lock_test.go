package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"
)

func TestAcquireLockCreatesAndReleases(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireLock(dir, "p1")
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}
	path := filepath.Join(dir, lockFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("锁文件未创建: %v", err)
	}

	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("释放后锁文件仍存在")
	}
}

// 被活着的进程持有时必须拒绝加锁。
//
// 这是整个机制的目的：两个 Chromium 打开同一个 user-data-dir 会损坏
// profile 数据，而 Chromium 自己的处理是让后来者静默退出，用户看不到原因。
func TestAcquireLockRejectsLiveHolder(t *testing.T) {
	dir := t.TempDir()
	// 伪造一个由别的进程持有的锁。用 os.Getppid() 作为 PID：
	// 父进程（go test 或 shell）确实活着，且不是本进程。
	writeLockFile(t, dir, lockInfo{
		PID: os.Getppid(), ProfileID: "other", Since: time.Now(),
	})

	_, err := acquireLock(dir, "p1")
	if !errors.Is(err, ErrLockedByOtherProcess) {
		t.Fatalf("期望 ErrLockedByOtherProcess，实际: %v", err)
	}
	// 错误信息要带上持有者 PID，否则用户无从判断该关掉哪个进程。
	if !strings.Contains(err.Error(), "PID") {
		t.Errorf("错误信息未包含持有者 PID: %v", err)
	}
}

// 持有者已退出的陈旧锁必须能直接接管。
//
// 崩溃或被强杀时锁文件会留下。若只看文件存在就拒绝，该 profile 会永久
// 不可用，只能手工删文件——那是比不加锁更糟的故障模式。
func TestAcquireLockTakesOverStaleLock(t *testing.T) {
	dir := t.TempDir()
	// 用一个几乎不可能存在的 PID 模拟已退出的持有者。
	writeLockFile(t, dir, lockInfo{
		PID: 999999999, ProfileID: "crashed", Since: time.Now().Add(-time.Hour),
	})

	release, err := acquireLock(dir, "p1")
	if err != nil {
		t.Fatalf("陈旧锁未能接管: %v", err)
	}
	defer release()

	// 接管后锁文件应记为本进程。
	holder, _ := readLock(filepath.Join(dir, lockFileName))
	if holder == nil || holder.PID != os.Getpid() {
		t.Errorf("接管后锁文件的 PID = %v, 期望 %d", holder, os.Getpid())
	}
}

// 内容损坏的锁文件同样应允许接管——它没有任何保护价值，
// 留着只会让 profile 永久不可用。
func TestAcquireLockTakesOverCorruptLock(t *testing.T) {
	for name, content := range map[string]string{
		"非 JSON":  "this is not json",
		"空文件":     "",
		"PID 为 0": `{"pid":0}`,
		"PID 为负":  `{"pid":-5}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, lockFileName),
				[]byte(content), 0o600); err != nil {
				t.Fatalf("写入损坏锁文件失败: %v", err)
			}
			release, err := acquireLock(dir, "p1")
			if err != nil {
				t.Fatalf("损坏的锁未能接管: %v", err)
			}
			release()
		})
	}
}

// 释放时不得删掉别人的锁。
// 本进程长时间挂起后锁可能已被接管，此时删除会让对方失去保护。
func TestReleaseDoesNotDeleteForeignLock(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireLock(dir, "p1")
	if err != nil {
		t.Fatalf("加锁失败: %v", err)
	}

	// 模拟锁被别的进程接管。
	writeLockFile(t, dir, lockInfo{
		PID: os.Getppid(), ProfileID: "taken-over", Since: time.Now(),
	})

	release()

	holder, _ := readLock(filepath.Join(dir, lockFileName))
	if holder == nil {
		t.Fatal("释放删掉了已被接管的锁文件")
	}
	if holder.PID != os.Getppid() {
		t.Errorf("锁文件的 PID = %d, 期望仍是接管者 %d", holder.PID, os.Getppid())
	}
}

// Start 遇到其他进程的锁时必须失败，且不留下半启动状态。
func TestStartRejectsProfileLockedByOtherProcess(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)
	if err := os.MkdirAll(p.ProfileDir, 0o700); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	writeLockFile(t, p.ProfileDir, lockInfo{
		PID: os.Getppid(), ProfileID: "other", Since: time.Now(),
	})

	st, err := m.Start(context.Background(), p)
	if !errors.Is(err, ErrLockedByOtherProcess) {
		_ = m.Stop(p.ID)
		t.Fatalf("期望 ErrLockedByOtherProcess，实际: %v", err)
	}
	if st.State != StateFailed {
		t.Errorf("状态 = %q, 期望 failed", st.State)
	}
	// 失败后必须清掉占位，否则该 profile 再也启动不了。
	if got := m.Status(p.ID); got.State != StateStopped {
		t.Errorf("失败后状态 = %q, 期望 stopped", got.State)
	}
}

// 正常停止后锁必须释放，使该 profile 能再次启动。
func TestLockReleasedAfterStop(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)

	if _, err := m.Start(context.Background(), p); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	lockPath := filepath.Join(p.ProfileDir, lockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("运行中锁文件不存在: %v", err)
	}

	if err := m.Stop(p.ID); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	m.Wait(p.ID)

	// 锁的释放在进程回收 goroutine 里，等它完成。
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("停止后锁文件仍存在，该 profile 将无法再次启动")
	}

	// 能再次启动才算真正释放。
	if _, err := m.Start(context.Background(), p); err != nil {
		t.Errorf("停止后无法再次启动: %v", err)
	}
	_ = m.Stop(p.ID)
}

func writeLockFile(t *testing.T, dir string, li lockInfo) {
	t.Helper()
	b, err := json.Marshal(li)
	if err != nil {
		t.Fatalf("序列化锁信息失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, lockFileName), b, 0o600); err != nil {
		t.Fatalf("写入锁文件失败: %v", err)
	}
}

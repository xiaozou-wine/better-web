package session

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"better-web/internal/model"
)

// 多个不同 profile 并发启动必须全部成功，且各自独立。
//
// 这是多账号运营的核心场景。与 TestStartRejectsDuplicateProfile 的区别：
// 那里防的是同一 profile 被启动两次（会损坏 user-data-dir），
// 这里要保证的恰好相反——不同 profile 之间不得互相阻塞或串状态。
func TestConcurrentStartOfDistinctProfiles(t *testing.T) {
	m, _ := newTestManager(t)
	const n = 8

	profiles := make([]*model.Profile, n)
	for i := range profiles {
		p := newProfile(t, model.KindFingerprint)
		// newProfile 生成的 ID 与目录需要区分开，否则会撞上重复启动保护。
		p.ID = fmt.Sprintf("multi-%d", i)
		p.Name = fmt.Sprintf("并发-%d", i)
		p.ProfileDir = t.TempDir()
		p.Seed = int32(1000 + i*7919)
		profiles[i] = p
	}

	var wg sync.WaitGroup
	statuses := make([]Status, n)
	errs := make([]error, n)
	for i, p := range profiles {
		wg.Add(1)
		go func(i int, p *model.Profile) {
			defer wg.Done()
			statuses[i], errs[i] = m.Start(context.Background(), p)
		}(i, p)
	}
	wg.Wait()
	defer func() {
		for _, p := range profiles {
			_ = m.Stop(p.ID)
		}
	}()

	for i := range profiles {
		if errs[i] != nil {
			t.Errorf("profile %d 启动失败: %v", i, errs[i])
			continue
		}
		if statuses[i].State != StateRunning {
			t.Errorf("profile %d 状态 = %q, 期望 running", i, statuses[i].State)
		}
	}

	// PID 必须两两不同——相同说明记错了进程，Stop 会停错实例。
	seen := map[int]int{}
	for i, st := range statuses {
		if st.PID == 0 {
			continue
		}
		if prev, dup := seen[st.PID]; dup {
			t.Errorf("profile %d 与 %d 记录了相同的 PID %d", i, prev, st.PID)
		}
		seen[st.PID] = i
	}

	// 指纹必须各不相同，否则多开等于同一台设备开了多个窗口。
	fps := map[int32]int{}
	for i, st := range statuses {
		if st.Fingerprint == nil {
			t.Errorf("profile %d 缺少指纹", i)
			continue
		}
		if prev, dup := fps[st.Fingerprint.Seed]; dup {
			t.Errorf("profile %d 与 %d 的种子相同", i, prev)
		}
		fps[st.Fingerprint.Seed] = i
	}

	if got := len(m.Running()); got != n {
		t.Errorf("Running() = %d 个会话, 期望 %d", got, n)
	}
}

// 并发停止不得互相干扰，且必须全部收敛。
// 用户关闭应用时会一次性停掉所有实例，这条路径必须干净。
func TestConcurrentStopOfDistinctProfiles(t *testing.T) {
	m, _ := newTestManager(t)
	const n = 6

	profiles := make([]*model.Profile, n)
	for i := range profiles {
		p := newProfile(t, model.KindDaily) // 日常模式启动更快，本测试只关心生命周期
		p.ID = fmt.Sprintf("stop-%d", i)
		p.Name = fmt.Sprintf("停止-%d", i)
		p.ProfileDir = t.TempDir()
		profiles[i] = p
		if _, err := m.Start(context.Background(), p); err != nil {
			t.Fatalf("profile %d 启动失败: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for _, p := range profiles {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := m.Stop(id); err != nil {
				t.Errorf("停止 %s 失败: %v", id, err)
			}
		}(p.ID)
	}
	wg.Wait()

	// 进程退出是异步的，等状态收敛。
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Running()) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("停止后仍有 %d 个会话未收敛", len(m.Running()))
}

// StopAll 必须停掉全部实例，这是应用退出时的收尾路径。
func TestStopAllClearsEverything(t *testing.T) {
	m, _ := newTestManager(t)
	for i := 0; i < 5; i++ {
		p := newProfile(t, model.KindDaily)
		p.ID = fmt.Sprintf("all-%d", i)
		p.Name = fmt.Sprintf("全停-%d", i)
		p.ProfileDir = t.TempDir()
		if _, err := m.Start(context.Background(), p); err != nil {
			t.Fatalf("启动失败: %v", err)
		}
	}
	if got := len(m.Running()); got != 5 {
		t.Fatalf("启动后 Running() = %d, 期望 5", got)
	}

	m.StopAll()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Running()) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("StopAll 后仍有 %d 个会话未收敛", len(m.Running()))
}

// 同一 profile 并发启动只能有一个成功。
// 两个进程同时打开一个 user-data-dir 会损坏 profile 数据。
func TestConcurrentStartOfSameProfileOnlyOneWins(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)

	const attempts = 6
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = m.Start(context.Background(), p)
		}(i)
	}
	wg.Wait()
	defer func() { _ = m.Stop(p.ID) }()

	var ok int
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d 次并发启动有 %d 次成功，期望恰好 1 次", attempts, ok)
	}
	if got := len(m.Running()); got != 1 {
		t.Errorf("Running() = %d, 期望 1", got)
	}
}

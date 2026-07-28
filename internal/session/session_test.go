package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"
)

const fakeVersion = "148.0.7778.215"

// newTestManager 构造一个使用假内核的管理器，并把假内核收到的参数
// 导出到 argsFile。
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	store := buildFakeKernel(t, fakeVersion)
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("BW_ARGS_FILE", argsFile)
	m := NewManager(store)
	t.Cleanup(m.StopAll)
	return m, argsFile
}

func newProfile(t *testing.T, kind model.ProfileKind) *model.Profile {
	t.Helper()
	return &model.Profile{
		ID:         "p-" + string(kind),
		Name:       "测试-" + string(kind),
		Kind:       kind,
		Seed:       20260727,
		ProfileDir: filepath.Join(t.TempDir(), "profile"),
	}
}

func TestStartDailyProfile(t *testing.T) {
	m, argsFile := newTestManager(t)
	p := newProfile(t, model.KindDaily)

	st, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if st.State != StateRunning {
		t.Errorf("状态 = %q, 期望 running", st.State)
	}
	if st.PID <= 0 {
		t.Errorf("PID = %d, 期望正数", st.PID)
	}
	// 日常模式不该带指纹配置。
	if st.Fingerprint != nil {
		t.Errorf("日常模式不该有指纹配置: %+v", st.Fingerprint)
	}

	args := waitForFile(t, argsFile, 10*time.Second)
	if !slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, "--user-data-dir=")
	}) {
		t.Errorf("参数中缺少 --user-data-dir: %v", args)
	}
	for _, forbidden := range []string{"--fingerprint=", "--timezone=", "--lang="} {
		if slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, forbidden) }) {
			t.Errorf("日常模式不该出现 %s: %v", forbidden, args)
		}
	}

	// profile 目录必须被创建。
	if _, err := os.Stat(p.ProfileDir); err != nil {
		t.Errorf("profile 目录未创建: %v", err)
	}

	if err := m.Stop(p.ID); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	m.Wait(p.ID)
	if st := m.Status(p.ID); st.State != StateStopped {
		t.Errorf("停止后状态 = %q, 期望 stopped", st.State)
	}
}

func TestStartFingerprintProfileWithGeoOverride(t *testing.T) {
	m, argsFile := newTestManager(t)
	p := newProfile(t, model.KindFingerprint)
	p.GeoOverride = &model.Geo{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"}

	st, err := m.Start(context.Background(), p)
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	if st.Fingerprint == nil {
		t.Fatal("指纹模式缺少指纹配置")
	}
	if st.Fingerprint.Timezone != "Asia/Tokyo" {
		t.Errorf("指纹时区 = %q, 期望 Asia/Tokyo", st.Fingerprint.Timezone)
	}
	if st.Geo == nil || st.Geo.CountryCode != "JP" {
		t.Errorf("会话地理信息 = %+v, 期望 JP", st.Geo)
	}

	args := waitForFile(t, argsFile, 10*time.Second)
	want := map[string]string{
		"--fingerprint=": "20260727",
		"--timezone=":    "Asia/Tokyo",
		"--lang=":        "ja-JP",
		"--accept-lang=": "ja-JP,ja;q=0.9",
	}
	for prefix, wantVal := range want {
		idx := slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, prefix) })
		if idx < 0 {
			t.Errorf("参数中缺少 %s: %v", prefix, args)
			continue
		}
		if got := strings.TrimPrefix(args[idx], prefix); got != wantVal {
			t.Errorf("%s = %q, 期望 %q", prefix, got, wantVal)
		}
	}
}

// 同一 user-data-dir 被两个进程同时打开会损坏 profile 数据，必须拦住。
func TestStartRejectsDuplicateProfile(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)
	if _, err := m.Start(context.Background(), p); err != nil {
		t.Fatalf("首次 Start 失败: %v", err)
	}
	defer func() { _ = m.Stop(p.ID) }()

	if _, err := m.Start(context.Background(), p); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("期望 ErrAlreadyRunning, 实际 %v", err)
	}
}

// 出口地查询失败时，严格模式必须中止启动：跑起来但时区与出口矛盾更糟。
func TestStrictGeoAbortsWhenLookupFails(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindFingerprint)
	// 指向一个必然连不通的本地端口，迫使出口地查询失败。
	p.Proxy = &model.Proxy{
		Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1,
		Username: "u", Password: "p",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st, err := m.Start(ctx, p)
	if err == nil {
		_ = m.Stop(p.ID)
		t.Fatal("代理不可用时期望启动失败，实际成功")
	}
	if st.State != StateFailed {
		t.Errorf("状态 = %q, 期望 failed", st.State)
	}
	// 失败后必须清理占位，否则该 profile 再也启动不了。
	if got := m.Status(p.ID); got.State != StateStopped {
		t.Errorf("失败后状态 = %q, 期望 stopped", got.State)
	}
}

func TestStartFailsWhenKernelMissing(t *testing.T) {
	m, _ := newTestManager(t)
	p := newProfile(t, model.KindDaily)
	p.KernelVersion = "999.0.0.0"
	if _, err := m.Start(context.Background(), p); err == nil {
		t.Error("内核版本不存在时期望报错，实际成功")
	}
}

func TestStopUnknownProfileIsNoop(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Stop("不存在的-id"); err != nil {
		t.Errorf("停止未运行的 profile 应为无操作，实际报错: %v", err)
	}
}

func TestRunningListsLiveSessions(t *testing.T) {
	m, _ := newTestManager(t)
	if got := m.Running(); len(got) != 0 {
		t.Fatalf("初始运行列表非空: %+v", got)
	}
	p := newProfile(t, model.KindDaily)
	if _, err := m.Start(context.Background(), p); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if got := m.Running(); len(got) != 1 || got[0].ProfileID != p.ID {
		t.Errorf("运行列表 = %+v, 期望仅含 %s", got, p.ID)
	}
	m.StopAll()
	m.Wait(p.ID)
	if got := m.Running(); len(got) != 0 {
		t.Errorf("StopAll 后运行列表非空: %+v", got)
	}
}

func TestStartRejectsNilProfile(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Start(context.Background(), nil); err == nil {
		t.Error("profile 为 nil 时期望报错")
	}
}

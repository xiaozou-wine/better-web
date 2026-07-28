package store

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"better-web/internal/model"
)

// 伪造开关必须能持久化：配了排障项重启后丢失，会让用户以为已关闭而实际没关。
func TestDisableSpoofingRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := []model.SpoofTarget{model.SpoofGPU, model.SpoofCanvas}
	p := &model.Profile{
		ID: "spoof-1", Name: "排障", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\1`, Seed: 42, DisableSpoofing: want,
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	got, err := s.Get("spoof-1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if !slices.Equal(got.DisableSpoofing, want) {
		t.Errorf("DisableSpoofing = %v, 期望 %v", got.DisableSpoofing, want)
	}
}

// 未设置时读回必须是空，不能是 [""] 之类的伪值——那会让 BuildArgs 传出无效参数。
func TestDisableSpoofingEmptyStaysEmpty(t *testing.T) {
	s := openTemp(t)
	p := &model.Profile{
		ID: "spoof-2", Name: "普通", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\2`, Seed: 7,
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := s.Get("spoof-2")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(got.DisableSpoofing) != 0 {
		t.Errorf("DisableSpoofing = %v, 期望空", got.DisableSpoofing)
	}
}

// 清空开关必须真的写回 NULL：排障结束后忘记生效等于一直裸奔。
func TestDisableSpoofingCanBeCleared(t *testing.T) {
	s := openTemp(t)
	p := &model.Profile{
		ID: "spoof-3", Name: "先关后开", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\3`, Seed: 9,
		DisableSpoofing: []model.SpoofTarget{model.SpoofFont},
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("首次 Save 失败: %v", err)
	}
	p.DisableSpoofing = nil
	if err := s.Save(p); err != nil {
		t.Fatalf("清空后 Save 失败: %v", err)
	}
	got, err := s.Get("spoof-3")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(got.DisableSpoofing) != 0 {
		t.Errorf("清空后仍读到 %v", got.DisableSpoofing)
	}
}

// 旧库（无 disable_spoofing 列）打开后必须自动补列，且原有数据不丢。
// 用户升级版本不该丢 profile。
func TestMigrationAddsColumnToLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// 手工建一个不含新列的旧表，并塞一条记录。
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE profiles (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL,
			seed INTEGER NOT NULL, profile_dir TEXT NOT NULL, proxy TEXT,
			geo_override TEXT, kernel_version TEXT NOT NULL DEFAULT '',
			extra_args TEXT, notes TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			last_use_at INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO profiles (id, name, kind, seed, profile_dir, created_at, updated_at)
		VALUES ('old-1', '旧记录', 'fingerprint', 555, 'C:\p\old', 1000, 1000);`)
	if err != nil {
		t.Fatalf("构造旧库失败: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("关闭旧库失败: %v", err)
	}

	// 正常打开应触发迁移。
	s, err := Open(path)
	if err != nil {
		t.Fatalf("打开旧库并迁移失败: %v", err)
	}
	defer func() { _ = s.Close() }()

	got, err := s.Get("old-1")
	if err != nil {
		t.Fatalf("迁移后读取旧记录失败: %v", err)
	}
	if got.Name != "旧记录" || got.Seed != 555 {
		t.Errorf("迁移后旧数据被改动: name=%q seed=%d", got.Name, got.Seed)
	}
	if len(got.DisableSpoofing) != 0 {
		t.Errorf("旧记录的新字段应为空，实际 %v", got.DisableSpoofing)
	}

	// 迁移后新字段应可正常写入。
	got.DisableSpoofing = []model.SpoofTarget{model.SpoofAudio}
	if err := s.Save(got); err != nil {
		t.Fatalf("迁移后写入新字段失败: %v", err)
	}
	again, err := s.Get("old-1")
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	if !slices.Equal(again.DisableSpoofing, []model.SpoofTarget{model.SpoofAudio}) {
		t.Errorf("DisableSpoofing = %v, 期望 [audio]", again.DisableSpoofing)
	}
}

// 重复打开同一库不应因迁移重复执行而失败。
func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repeat.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("第 %d 次打开失败: %v", i+1, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("第 %d 次关闭失败: %v", i+1, err)
		}
	}
}

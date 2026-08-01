package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

// 分组索引必须在新库和迁移后的旧库上都存在。
//
// 这是对一处真实缺陷的回归：索引最初写在 schema 常量里，而 Open 先执行
// schema 再执行 migrate。旧库的 profiles 表已存在（CREATE TABLE
// IF NOT EXISTS 不生效）且尚无 grp 列，于是在 schema 阶段建索引直接失败，
// 整个 Open 报 "no such column: grp"——旧库再也打不开。
func TestGroupIndexExistsOnFreshAndMigratedDatabases(t *testing.T) {
	t.Run("新库", func(t *testing.T) {
		s := openTemp(t)
		assertIndexExists(t, s.db, "idx_profiles_group")
	})

	t.Run("迁移后的旧库", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "legacy.db")
		legacy, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("打开旧库失败: %v", err)
		}
		// 不含 grp/tags/disable_spoofing 的历史表结构。
		if _, err := legacy.Exec(`
			CREATE TABLE profiles (
				id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL,
				seed INTEGER NOT NULL, profile_dir TEXT NOT NULL, proxy TEXT,
				geo_override TEXT, kernel_version TEXT NOT NULL DEFAULT '',
				extra_args TEXT, notes TEXT NOT NULL DEFAULT '',
				created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
				last_use_at INTEGER NOT NULL DEFAULT 0
			)`); err != nil {
			t.Fatalf("构造旧库失败: %v", err)
		}
		if err := legacy.Close(); err != nil {
			t.Fatalf("关闭旧库失败: %v", err)
		}

		s, err := Open(path)
		if err != nil {
			t.Fatalf("打开旧库并迁移失败: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		assertIndexExists(t, s.db, "idx_profiles_group")
	})
}

// 带数据的旧库必须能迁移出 last_geo 列，且原有数据完好。
//
// last_geo 是后加的列，用户现有的库里没有它。迁移漏了这一列时 selectColumns
// 会因 "no such column: last_geo" 让每次读取都失败——整个面板打不开。
func TestLastGeoMigrationOnLegacyDatabaseWithData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-data.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开旧库失败: %v", err)
	}
	// 加 last_geo 之前的表结构，并塞一条真实数据。
	if _, err := legacy.Exec(`
		CREATE TABLE profiles (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL,
			seed INTEGER NOT NULL, profile_dir TEXT NOT NULL, proxy TEXT,
			geo_override TEXT, kernel_version TEXT NOT NULL DEFAULT '',
			extra_args TEXT, notes TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			last_use_at INTEGER NOT NULL DEFAULT 0,
			disable_spoofing TEXT, grp TEXT NOT NULL DEFAULT '', tags TEXT,
			device_label TEXT NOT NULL DEFAULT '', startup TEXT,
			use_system_browser INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO profiles (id, name, kind, seed, profile_dir, notes,
			created_at, updated_at, device_label)
		VALUES ('old1', 'test', 'fingerprint', 770828460, 'C:\dir', 'test',
			1, 1, 'Windows 10 / RTX 2060 桌面')`); err != nil {
		t.Fatalf("构造旧库失败: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("关闭旧库失败: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("打开旧库并迁移失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// 读取必须成功——这是迁移漏列时最先崩的地方。
	got, err := s.Get("old1")
	if err != nil {
		t.Fatalf("迁移后读取失败: %v", err)
	}
	if got.LastGeo != nil {
		t.Errorf("旧数据的 LastGeo 应为 nil, 实际 %+v", got.LastGeo)
	}
	// 原有字段不得在迁移中丢失或错位。
	if got.Name != "test" || got.Seed != 770828460 {
		t.Errorf("旧数据受损: name=%q seed=%d", got.Name, got.Seed)
	}
	if got.DeviceLabel != "Windows 10 / RTX 2060 桌面" {
		t.Errorf("机型丢失: %q", got.DeviceLabel)
	}

	// 迁移后写入新字段也要能生效。
	g := &model.Geo{CountryCode: "DE", Timezone: "Europe/Berlin", Locale: "de-DE"}
	if err := s.TouchLastGeo("old1", g); err != nil {
		t.Fatalf("迁移后写入 LastGeo 失败: %v", err)
	}
	again, err := s.Get("old1")
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if again.LastGeo == nil || again.LastGeo.Locale != "de-DE" {
		t.Errorf("迁移后写入的 LastGeo 未生效: %+v", again.LastGeo)
	}
}

func assertIndexExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var got string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&got)
	if err != nil {
		t.Errorf("索引 %s 不存在: %v", name, err)
	}
}

// 迁移必须可重复执行：每次启动都会跑一遍。
func TestMigrationIsIdempotentAcrossReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("第 %d 次打开失败: %v", i+1, err)
		}
		assertIndexExists(t, s.db, "idx_profiles_group")
		if err := s.Close(); err != nil {
			t.Fatalf("第 %d 次关闭失败: %v", i+1, err)
		}
	}
}

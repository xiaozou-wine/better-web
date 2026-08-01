// Package store 持久化 profile 配置。
//
// 使用 modernc.org/sqlite（纯 Go 实现，无需 cgo），以便交叉编译分发。
//
// 代理密码经 internal/secret 加密后存入 proxy 列：Windows 上用 DPAPI，
// 密钥绑定当前用户账户。其他平台暂无系统级密钥库，退化为明文。
//
// 数据库文件仍应按凭据文件对待，不要同步到网盘或共享位置：
// 密文只挡住"文件被拷走"，挡不住以当前用户身份运行的程序。
// 注意 Go 的 os.MkdirAll(0o700) 在 Windows 上不设置 NTFS ACL，
// 目录的实际保护仅来自 %APPDATA% 的位置本身。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"better-web/internal/model"
	"better-web/internal/secret"
	_ "modernc.org/sqlite"
)

// ErrNotFound 表示指定 profile 不存在。
var ErrNotFound = errors.New("profile 不存在")

// schema 是数据库结构。proxy 与 geo_override 以 JSON 存储：它们是随内核
// 能力演进的可选配置，拆成列会让每次扩展都要迁移。
const schema = `
CREATE TABLE IF NOT EXISTS profiles (
	id             TEXT PRIMARY KEY,
	name           TEXT NOT NULL,
	kind           TEXT NOT NULL,
	seed           INTEGER NOT NULL,
	profile_dir    TEXT NOT NULL,
	proxy          TEXT,
	geo_override   TEXT,
	kernel_version TEXT NOT NULL DEFAULT '',
	extra_args     TEXT,
	notes          TEXT NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL,
	last_use_at    INTEGER NOT NULL DEFAULT 0,
	disable_spoofing TEXT,
	grp            TEXT NOT NULL DEFAULT '',
	tags           TEXT,
	device_label   TEXT NOT NULL DEFAULT '',
	startup        TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_profiles_name ON profiles(name);

-- settings 存不属于任何 profile 的全局配置，如链接接管的目标 profile。
--
-- 建在 schema 里而非 migrations：这是新表，CREATE TABLE IF NOT EXISTS 对
-- 新旧库都正确，不像加列那样受"表已存在时 schema 不生效"的影响。
--
-- 键值表而非固定列：全局配置项会随功能增加，每加一项都迁移一次不值得。
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// migrations 是对已存在的库补加的列。
//
// SQLite 的 ALTER TABLE ADD COLUMN 无 IF NOT EXISTS，重复执行会报
// "duplicate column name"，因此这里对该错误单独放行——它表示迁移已应用过。
// 新增迁移只能追加到末尾，不能修改或删除已有条目。
var migrations = []string{
	`ALTER TABLE profiles ADD COLUMN disable_spoofing TEXT`,
	// 列名用 grp 而非 group：GROUP 是 SQL 保留字，直接用会导致语法错误。
	`ALTER TABLE profiles ADD COLUMN grp TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE profiles ADD COLUMN tags TEXT`,
	`ALTER TABLE profiles ADD COLUMN device_label TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE profiles ADD COLUMN startup TEXT`,
	// use_system_browser 让日常 profile 走系统已装的官方 Chrome。
	// 默认 0：已有 profile 一律沿用指纹内核，升级不改变任何现有行为。
	`ALTER TABLE profiles ADD COLUMN use_system_browser INTEGER NOT NULL DEFAULT 0`,
	// last_geo 缓存上次启动实测到的出口地理，仅供停止态的界面预览。
	// 默认 NULL：没启动过的 profile 无实测数据，界面退回按配置推导。
	`ALTER TABLE profiles ADD COLUMN last_geo TEXT`,
	// 索引必须建在这里而不是 schema 里：schema 先于 migrate 执行，
	// 而旧库的 profiles 表已存在（CREATE TABLE IF NOT EXISTS 不生效）
	// 且还没有 grp 列，在 schema 阶段建索引会因列不存在而失败。
	`CREATE INDEX IF NOT EXISTS idx_profiles_group ON profiles(grp)`,
}

// Store 是 profile 存储。
type Store struct {
	db *sql.DB
}

// Open 打开或创建数据库并建表。
func Open(path string) (*Store, error) {
	// 开启外键与 WAL：WAL 让读写不互相阻塞，界面刷新不会被写入卡住。
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写入者，限制连接数避免 SQLITE_BUSY。
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化数据库结构失败: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate 对旧库补加缺失的列。已应用过的迁移会被跳过。
func migrate(db *sql.DB) error {
	for i, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			// 列已存在说明该迁移先前已应用，属正常路径。
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("应用第 %d 项数据库迁移失败: %w", i+1, err)
		}
	}
	return nil
}

// Close 关闭数据库。
//
// 关闭前先做一次 WAL checkpoint，把日志合并回主库。否则应用被硬杀时，
// 最后一批写入只存在于 -wal 文件里，用户拷贝或备份 profiles.db 时
// 会漏掉这部分数据而不自知。checkpoint 失败不阻断关闭：数据仍在 WAL 中，
// 下次正常打开时会自动恢复。
func (s *Store) Close() error {
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		// 只影响 -wal 文件是否合并，不影响数据完整性，因此不上报。
		_ = err
	}
	return s.db.Close()
}

// Save 插入或更新一个 profile。
func (s *Store) Save(p *model.Profile) error {
	if p == nil {
		return errors.New("profile 为空")
	}
	if p.ID == "" {
		return errors.New("profile 缺少 ID")
	}
	if p.Name == "" {
		return errors.New("profile 缺少名称")
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("profile 类型 %q 无效", p.Kind)
	}

	// 密码加密后再序列化。在副本上操作，避免改动调用方持有的 Proxy——
	// 调用方随后可能还要用明文密码去连代理。
	proxyForDB := p.Proxy
	if proxyForDB != nil && proxyForDB.Password != "" {
		enc, err := secret.Encrypt(proxyForDB.Password)
		if err != nil {
			return fmt.Errorf("加密 profile %q 的代理密码失败: %w", p.Name, err)
		}
		copied := *proxyForDB
		copied.Password = enc
		proxyForDB = &copied
	}
	proxyJSON, err := marshalOptional(proxyForDB)
	if err != nil {
		return fmt.Errorf("序列化代理配置失败: %w", err)
	}
	geoJSON, err := marshalOptional(p.GeoOverride)
	if err != nil {
		return fmt.Errorf("序列化地理配置失败: %w", err)
	}
	argsJSON, err := marshalOptional(p.ExtraArgs)
	if err != nil {
		return fmt.Errorf("序列化附加参数失败: %w", err)
	}
	spoofJSON, err := marshalOptional(p.DisableSpoofing)
	if err != nil {
		return fmt.Errorf("序列化伪造开关失败: %w", err)
	}

	// 入库前清洗分组与标签，保证筛选时不会因空白或大小写差异而漏项。
	// 直接写回 p：调用方拿到规范化后的值才与库内一致。
	p.Group = model.NormalizeGroup(p.Group)
	p.Tags = model.NormalizeTags(p.Tags)
	tagsJSON, err := marshalOptional(p.Tags)
	if err != nil {
		return fmt.Errorf("序列化标签失败: %w", err)
	}
	startupJSON, err := marshalOptional(p.Startup)
	if err != nil {
		return fmt.Errorf("序列化启动页配置失败: %w", err)
	}
	lastGeoJSON, err := marshalOptional(p.LastGeo)
	if err != nil {
		return fmt.Errorf("序列化上次出口地理失败: %w", err)
	}

	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err = s.db.Exec(`
		INSERT INTO profiles (id, name, kind, seed, profile_dir, proxy, geo_override,
			kernel_version, extra_args, notes, created_at, updated_at, last_use_at,
			disable_spoofing, grp, tags, device_label, startup, use_system_browser,
			last_geo)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, kind=excluded.kind, seed=excluded.seed,
			profile_dir=excluded.profile_dir, proxy=excluded.proxy,
			geo_override=excluded.geo_override, kernel_version=excluded.kernel_version,
			extra_args=excluded.extra_args, notes=excluded.notes,
			updated_at=excluded.updated_at, last_use_at=excluded.last_use_at,
			disable_spoofing=excluded.disable_spoofing,
			grp=excluded.grp, tags=excluded.tags,
			device_label=excluded.device_label, startup=excluded.startup,
			use_system_browser=excluded.use_system_browser,
			last_geo=excluded.last_geo`,
		p.ID, p.Name, string(p.Kind), p.Seed, p.ProfileDir, proxyJSON, geoJSON,
		p.KernelVersion, argsJSON, p.Notes,
		p.CreatedAt.UnixMilli(), p.UpdatedAt.UnixMilli(), unixMilliOrZero(p.LastUseAt),
		spoofJSON, p.Group, tagsJSON, p.DeviceLabel, startupJSON, p.UseSystemBrowser,
		lastGeoJSON)
	if err != nil {
		return fmt.Errorf("保存 profile %q 失败: %w", p.Name, err)
	}
	return nil
}

// Get 按 ID 读取 profile。
func (s *Store) Get(id string) (*model.Profile, error) {
	row := s.db.QueryRow(selectColumns+` WHERE id = ?`, id)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return p, err
}

// List 返回全部 profile，最近使用的排在前面。
func (s *Store) List() ([]*model.Profile, error) {
	rows, err := s.db.Query(selectColumns + ` ORDER BY last_use_at DESC, created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("查询 profile 列表失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*model.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Delete 删除一个 profile 记录。
//
// 只删数据库记录，不动磁盘上的 user-data-dir：里面有 Cookie、登录态和
// 浏览历史，误删无法恢复。目录的清理必须由用户显式确认后另行处理。
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除 profile %s 失败: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}

// TouchLastUse 记录一次使用时间，用于列表排序。
func (s *Store) TouchLastUse(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE profiles SET last_use_at = ? WHERE id = ?`, at.UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("更新 profile %s 的使用时间失败: %w", id, err)
	}
	return nil
}

// TouchLastGeo 记录本次启动实测到的出口地理，供停止态的界面预览。
//
// 单独一条 UPDATE 而非走 Save：Save 会重写全部字段，而调用方手里的
// Profile 可能已被别处改过，整行回写会覆盖掉那些改动。
func (s *Store) TouchLastGeo(id string, g *model.Geo) error {
	val, err := marshalOptional(g)
	if err != nil {
		return fmt.Errorf("序列化 profile %s 的出口地理失败: %w", id, err)
	}
	_, err = s.db.Exec(`UPDATE profiles SET last_geo = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("更新 profile %s 的出口地理失败: %w", id, err)
	}
	return nil
}

func marshalOptional(v any) (any, error) {
	switch val := v.(type) {
	case nil:
		return nil, nil
	case *model.Proxy:
		if val == nil {
			return nil, nil
		}
	case *model.Geo:
		if val == nil {
			return nil, nil
		}
	case []string:
		if len(val) == 0 {
			return nil, nil
		}
	case []model.SpoofTarget:
		if len(val) == 0 {
			return nil, nil
		}
	case *model.Startup:
		if val == nil {
			return nil, nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

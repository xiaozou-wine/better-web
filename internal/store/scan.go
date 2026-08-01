package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"better-web/internal/model"
	"better-web/internal/secret"
)

// selectColumns 的列顺序必须与 scanProfile 的 Scan 参数顺序严格一致。
// 新增列时两处一起改，否则会静默错位——SQLite 的动态类型不会报错。
const selectColumns = `
	SELECT id, name, kind, seed, profile_dir, proxy, geo_override,
	       kernel_version, extra_args, notes, created_at, updated_at, last_use_at,
	       disable_spoofing, grp, tags, device_label, startup, use_system_browser,
	       last_geo
	FROM profiles`

// scanner 抽象 *sql.Row 与 *sql.Rows 的共同能力。
type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(sc scanner) (*model.Profile, error) {
	var (
		p                               model.Profile
		kind                            string
		proxyJSON, geoJSON, argsJSON    sql.NullString
		spoofJSON, tagsJSON             sql.NullString
		startupJSON, lastGeoJSON        sql.NullString
		createdMs, updatedMs, lastUseMs int64
	)
	err := sc.Scan(&p.ID, &p.Name, &kind, &p.Seed, &p.ProfileDir,
		&proxyJSON, &geoJSON, &p.KernelVersion, &argsJSON, &p.Notes,
		&createdMs, &updatedMs, &lastUseMs, &spoofJSON, &p.Group, &tagsJSON,
		&p.DeviceLabel, &startupJSON, &p.UseSystemBrowser, &lastGeoJSON)
	if err != nil {
		return nil, err
	}
	if tagsJSON.Valid {
		if err := json.Unmarshal([]byte(tagsJSON.String), &p.Tags); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的标签失败: %w", p.ID, err)
		}
	}
	if startupJSON.Valid {
		var st model.Startup
		if err := json.Unmarshal([]byte(startupJSON.String), &st); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的启动页配置失败: %w", p.ID, err)
		}
		p.Startup = &st
	}

	p.Kind = model.ProfileKind(kind)
	p.CreatedAt = time.UnixMilli(createdMs)
	p.UpdatedAt = time.UnixMilli(updatedMs)
	if lastUseMs > 0 {
		p.LastUseAt = time.UnixMilli(lastUseMs)
	}

	if proxyJSON.Valid {
		var pr model.Proxy
		if err := json.Unmarshal([]byte(proxyJSON.String), &pr); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的代理配置失败: %w", p.ID, err)
		}
		// 解密密码。库里可能是密文（本版本写入）也可能是明文
		// （升级前的历史记录），由 secret.Decrypt 按前缀区分。
		if pr.Password != "" {
			plain, err := secret.Decrypt(pr.Password)
			if err != nil {
				return nil, fmt.Errorf("解密 profile %s 的代理密码失败: %w", p.ID, err)
			}
			pr.Password = plain
		}
		p.Proxy = &pr
	}
	if geoJSON.Valid {
		var g model.Geo
		if err := json.Unmarshal([]byte(geoJSON.String), &g); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的地理配置失败: %w", p.ID, err)
		}
		p.GeoOverride = &g
	}
	if lastGeoJSON.Valid {
		var g model.Geo
		if err := json.Unmarshal([]byte(lastGeoJSON.String), &g); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的上次出口地理失败: %w", p.ID, err)
		}
		p.LastGeo = &g
	}
	if argsJSON.Valid {
		if err := json.Unmarshal([]byte(argsJSON.String), &p.ExtraArgs); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的附加参数失败: %w", p.ID, err)
		}
	}
	if spoofJSON.Valid {
		if err := json.Unmarshal([]byte(spoofJSON.String), &p.DisableSpoofing); err != nil {
			return nil, fmt.Errorf("解析 profile %s 的伪造开关失败: %w", p.ID, err)
		}
	}
	return &p, nil
}

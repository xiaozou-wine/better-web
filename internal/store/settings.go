package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// 全局配置的键名。集中定义避免字符串字面量散落各处——
// 写错一个字符的表现是"设置保存成功但读出来是空的"，很难查。
const (
	// KeyURLHandlerProfileID 是接管系统链接的目标 profile ID。
	KeyURLHandlerProfileID = "url_handler.profile_id"
	// KeyURLHandlerIncognito 为 "1" 时用无痕窗口打开接管的链接。
	KeyURLHandlerIncognito = "url_handler.incognito"
)

// Setting 读一个全局配置项。键不存在时返回空串且不报错。
//
// 缺键不算错误：调用方几乎总是"没配就用默认值"，返回 ErrNotFound
// 会让每个调用点都要写一遍相同的 errors.Is 判断。
func (s *Store) Setting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取配置项 %s 失败: %w", key, err)
	}
	return v, nil
}

// SetSetting 写一个全局配置项。
//
// 值为空串时删除该行而非存空值：这样 Setting 的"缺键"与"值为空"两种状态
// 归一，调用方不必区分。清空一项配置的语义就是恢复默认。
func (s *Store) SetSetting(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("配置项键名为空")
	}
	if value == "" {
		if _, err := s.db.Exec(`DELETE FROM settings WHERE key=?`, key); err != nil {
			return fmt.Errorf("清除配置项 %s 失败: %w", key, err)
		}
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("写入配置项 %s 失败: %w", key, err)
	}
	return nil
}

// SettingBool 读一个布尔配置项。"1" 为真，其余（含缺键）为假。
func (s *Store) SettingBool(key string) (bool, error) {
	v, err := s.Setting(key)
	if err != nil {
		return false, err
	}
	return v == "1", nil
}

// SetSettingBool 写一个布尔配置项。false 存为空串，即删除该行。
func (s *Store) SetSettingBool(key string, v bool) error {
	if v {
		return s.SetSetting(key, "1")
	}
	return s.SetSetting(key, "")
}

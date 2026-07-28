package store

import "testing"

func TestSettingRoundTrip(t *testing.T) {
	s := openTemp(t)

	if err := s.SetSetting(KeyURLHandlerProfileID, "id-42"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	got, err := s.Setting(KeyURLHandlerProfileID)
	if err != nil {
		t.Fatalf("Setting 失败: %v", err)
	}
	if got != "id-42" {
		t.Errorf("Setting = %q, 期望 id-42", got)
	}

	// 覆盖写：ON CONFLICT 必须更新而非报主键冲突。
	if err := s.SetSetting(KeyURLHandlerProfileID, "id-99"); err != nil {
		t.Fatalf("覆盖写失败: %v", err)
	}
	if got, _ := s.Setting(KeyURLHandlerProfileID); got != "id-99" {
		t.Errorf("覆盖后 Setting = %q, 期望 id-99", got)
	}
}

// TestSettingMissingKeyIsEmpty 钉住"缺键不报错"。
//
// 调用方几乎总是"没配就用默认值"，若返回错误则每个调用点都要写一遍
// errors.Is 判断，而漏写的后果是把"未配置"当成读取失败上报给用户。
func TestSettingMissingKeyIsEmpty(t *testing.T) {
	s := openTemp(t)
	got, err := s.Setting("never.set")
	if err != nil {
		t.Fatalf("缺键不应报错，却得到: %v", err)
	}
	if got != "" {
		t.Errorf("缺键应返回空串，却得到 %q", got)
	}
}

// TestSetSettingEmptyDeletesRow 钉住"写空串等于恢复默认"。
func TestSetSettingEmptyDeletesRow(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSetting(KeyURLHandlerProfileID, "id-1"); err != nil {
		t.Fatalf("SetSetting 失败: %v", err)
	}
	if err := s.SetSetting(KeyURLHandlerProfileID, ""); err != nil {
		t.Fatalf("清空失败: %v", err)
	}

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM settings WHERE key=?`, KeyURLHandlerProfileID,
	).Scan(&n); err != nil {
		t.Fatalf("查询行数失败: %v", err)
	}
	if n != 0 {
		t.Errorf("写空串后应删除该行，却仍有 %d 行", n)
	}
}

func TestSettingBool(t *testing.T) {
	s := openTemp(t)

	// 未设置时为假。
	if v, err := s.SettingBool(KeyURLHandlerIncognito); err != nil || v {
		t.Errorf("未设置时 SettingBool = %v (err=%v), 期望 false", v, err)
	}

	if err := s.SetSettingBool(KeyURLHandlerIncognito, true); err != nil {
		t.Fatalf("SetSettingBool(true) 失败: %v", err)
	}
	if v, _ := s.SettingBool(KeyURLHandlerIncognito); !v {
		t.Error("SetSettingBool(true) 后应为 true")
	}

	if err := s.SetSettingBool(KeyURLHandlerIncognito, false); err != nil {
		t.Fatalf("SetSettingBool(false) 失败: %v", err)
	}
	if v, _ := s.SettingBool(KeyURLHandlerIncognito); v {
		t.Error("SetSettingBool(false) 后应为 false")
	}
}

func TestSetSettingRejectsEmptyKey(t *testing.T) {
	s := openTemp(t)
	if err := s.SetSetting("  ", "x"); err == nil {
		t.Error("空键名应报错")
	}
}

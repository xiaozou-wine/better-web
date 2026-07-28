package store

import (
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

// use_system_browser 必须能存能读。
//
// 单独测这个字段是因为 scan.go 的注释警告过：新增列时 selectColumns 与
// Scan 的参数顺序必须一起改，否则会静默错位——SQLite 的动态类型不会报错，
// 错位表现为某个字段读到隔壁列的值，而不是查询失败。
func TestSaveAndLoadUseSystemBrowser(t *testing.T) {
	db := openTemp(t)

	p := &model.Profile{
		ID: "sys-1", Name: "日常-系统Chrome", Kind: model.KindDaily,
		ProfileDir:       filepath.Join(t.TempDir(), "sys-1"),
		UseSystemBrowser: true,
	}
	if err := db.Save(p); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got, err := db.Get("sys-1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !got.UseSystemBrowser {
		t.Error("UseSystemBrowser 未持久化")
	}
	// 顺带核对相邻字段没有错位。
	if got.Name != p.Name || got.Kind != model.KindDaily {
		t.Errorf("相邻字段错位: name=%q kind=%q", got.Name, got.Kind)
	}
}

// 默认值必须是 false：已有 profile 升级后一律沿用指纹内核，
// 不能因为加了这一列就悄悄换掉浏览器。
func TestUseSystemBrowserDefaultsFalse(t *testing.T) {
	db := openTemp(t)

	p := &model.Profile{
		ID: "def-1", Name: "默认", Kind: model.KindDaily,
		ProfileDir: filepath.Join(t.TempDir(), "def-1"),
	}
	if err := db.Save(p); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	got, err := db.Get("def-1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.UseSystemBrowser {
		t.Error("未设置时应为 false")
	}
}

// 关掉之后要能存回 false，不能只写不删。
func TestUseSystemBrowserCanBeTurnedOff(t *testing.T) {
	db := openTemp(t)

	dir := filepath.Join(t.TempDir(), "off-1")
	p := &model.Profile{
		ID: "off-1", Name: "切换", Kind: model.KindDaily,
		ProfileDir: dir, UseSystemBrowser: true,
	}
	if err := db.Save(p); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	p.UseSystemBrowser = false
	if err := db.Save(p); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got, err := db.Get("off-1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.UseSystemBrowser {
		t.Error("关闭后应存为 false")
	}
}

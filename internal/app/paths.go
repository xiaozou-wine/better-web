// Package app 是给前端绑定的服务层，把各内部包编排成界面可直接调用的接口。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths 是应用的数据目录布局。
//
// 整个 Root 目录都应按凭据目录对待。DB 里的代理密码在 Windows 上经 DPAPI
// 加密（密钥绑定当前用户账户），其他平台仍是明文；而 Profiles 下的 Cookie
// 与登录态在所有平台都是明文。因此无论哪个平台，都不得把该目录同步到网盘、
// 放入共享目录或提交到代码仓库。详见 model.Proxy.Password。
type Paths struct {
	// Root 是应用数据根目录。
	Root string
	// DB 是 profile 配置数据库文件。含代理凭据，Windows 上密码字段已加密。
	DB string
	// Kernels 是内核安装根目录，布局为 <Kernels>/<版本号>/chrome[.exe]。
	Kernels string
	// Profiles 是各 profile 的 user-data-dir 所在目录。
	Profiles string
}

// DefaultPaths 返回默认数据目录，位于用户配置目录下的 better-web。
// 不写到程序所在目录：便携版常被放在只读或受 UAC 保护的位置。
func DefaultPaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	return NewPaths(filepath.Join(base, "better-web")), nil
}

// NewPaths 基于给定根目录构造目录布局。
func NewPaths(root string) Paths {
	return Paths{
		Root:     root,
		DB:       filepath.Join(root, "profiles.db"),
		Kernels:  filepath.Join(root, "kernels"),
		Profiles: filepath.Join(root, "profiles"),
	}
}

// EnsureDirs 创建所需目录。权限设为 0700：profile 目录含 Cookie 与登录态。
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.Root, p.Kernels, p.Profiles} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}

// ProfileDir 返回指定 profile 的 user-data-dir 路径。
func (p Paths) ProfileDir(id string) string {
	return filepath.Join(p.Profiles, id)
}

// owns 报告 dir 是否位于受管的 profiles 目录之内。
// 删除操作前必须校验，避免配置被篡改后误删无关路径。
func (p Paths) owns(dir string) bool {
	base, err := filepath.Abs(p.Profiles)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	// rel 以 ".." 开头说明目标在 base 之外；等于 "." 说明就是 base 本身，
	// 删掉它会连带清空所有 profile。
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

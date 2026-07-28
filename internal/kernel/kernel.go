// Package kernel 负责定位与校验浏览器内核二进制。
//
// 内核是打过指纹 patch 的 Chromium 构建（fingerprint-chromium，基于
// ungoogled-chromium）。本包不自行编译内核：维护 Chromium patch 需要跟随
// 每四周一次的大版本、数小时的全量构建和持续的冲突解决，代价远超收益。
package kernel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ErrNotFound 表示在给定根目录下找不到任何可用内核。
var ErrNotFound = errors.New("未找到可用的浏览器内核")

// Kernel 是一份可用的内核。
type Kernel struct {
	// Version 是 Chromium 完整版本号，如 148.0.7778.215。
	Version string `json:"version"`
	// ExecPath 是可执行文件的绝对路径。
	ExecPath string `json:"execPath"`
	// Source 标识来源，决定该内核能否用于指纹模式。
	// 零值（空串）按 SourceFingerprint 处理，兼容已有调用方。
	Source Source `json:"source,omitempty"`
	// Name 是给界面显示的名称，如「系统 Chrome」。留空时界面用版本号。
	Name string `json:"name,omitempty"`
}

// Major 返回主版本号。解析失败时返回 0。
func (k Kernel) Major() int {
	major, _, _ := strings.Cut(k.Version, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// execName 返回当前平台的内核可执行文件名。
func execName() string {
	if runtime.GOOS == "windows" {
		return "chrome.exe"
	}
	return "chrome"
}

// Store 管理一个内核安装目录。目录布局为 <root>/<版本号>/<可执行文件>，
// 例如 kernels/148.0.7778.215/chrome.exe。按版本分目录存放使得多个 profile
// 可以锁定不同内核版本，避免升级导致已有 profile 指纹漂移。
type Store struct {
	root string
}

// NewStore 用指定根目录构造 Store。
func NewStore(root string) *Store { return &Store{root: root} }

// Root 返回内核安装根目录。
func (s *Store) Root() string { return s.root }

// List 枚举已安装的内核，按版本号降序返回。
// 根目录不存在时返回空列表而非错误：首次运行尚未安装内核是正常状态。
func (s *Store) List() ([]Kernel, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取内核目录 %s 失败: %w", s.root, err)
	}

	var out []Kernel
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		exec := filepath.Join(s.root, e.Name(), execName())
		info, err := os.Stat(exec)
		if err != nil || info.IsDir() {
			// 目录里没有可执行文件，视为残留或未完成的安装，跳过。
			continue
		}
		out = append(out, Kernel{
			Version: e.Name(), ExecPath: exec, Source: SourceFingerprint,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return compareVersion(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// Resolve 返回指定版本的内核。version 为空时返回版本号最高的一个。
func (s *Store) Resolve(version string) (Kernel, error) {
	list, err := s.List()
	if err != nil {
		return Kernel{}, err
	}
	if len(list) == 0 {
		return Kernel{}, fmt.Errorf("%w: 内核目录 %s 为空", ErrNotFound, s.root)
	}
	if version == "" {
		return list[0], nil
	}
	for _, k := range list {
		if k.Version == version {
			return k, nil
		}
	}
	return Kernel{}, fmt.Errorf("%w: 未安装版本 %s", ErrNotFound, version)
}

// compareVersion 比较两个点分数字版本号，返回 -1/0/1。
// 非数字段按字符串比较，保证含后缀的版本号也能得到稳定顺序。
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv string
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		an, aerr := strconv.Atoi(av)
		bn, berr := strconv.Atoi(bv)
		if aerr == nil && berr == nil {
			if an != bn {
				return sign(an - bn)
			}
			continue
		}
		if c := strings.Compare(av, bv); c != 0 {
			return c
		}
	}
	return 0
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

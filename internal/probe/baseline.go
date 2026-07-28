package probe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Drift 是某一维度上基线值与实测值的差异。
type Drift struct {
	// Field 是维度名，如 "canvasHash"。
	Field string
	// Want 是基线记录的值，Got 是本次实测值。
	Want string
	Got  string
	// Critical 为 true 表示该维度变化会让已有 profile 的身份漂移，
	// 相当于账号换了设备；false 表示变化通常无害或来自宿主环境差异。
	Critical bool
}

func (d Drift) String() string {
	tag := "变化"
	if d.Critical {
		tag = "身份漂移"
	}
	return fmt.Sprintf("[%s] %s: %q -> %q", tag, d.Field, d.Want, d.Got)
}

// LoadBaseline 读取先前保存的基线。
func LoadBaseline(path string) (Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("读取基线 %s 失败: %w", path, err)
	}
	var res Result
	if err := json.Unmarshal(b, &res); err != nil {
		return Result{}, fmt.Errorf("解析基线 %s 失败: %w", path, err)
	}
	return res, nil
}

// BaselinePath 返回指定内核版本的基线文件路径。
func BaselinePath(dir, version string) string {
	return filepath.Join(dir, "baseline-"+version+".json")
}

// CompareBaseline 比对基线与实测结果，返回全部差异。
//
// 区分 critical 与否的依据是「该维度变化会不会让平台认为账号换了设备」：
// canvas、audio、UA、核数由种子决定，同一 seed 下变化即为身份漂移；
// 而屏幕尺寸、WebGL 型号取自宿主机或内核内部策略，换机器跑就会不同，
// 报为普通变化以免掩盖真正的问题。
func CompareBaseline(want, got Result) []Drift {
	var out []Drift
	add := func(field, w, g string, critical bool) {
		if w != g {
			out = append(out, Drift{Field: field, Want: w, Got: g, Critical: critical})
		}
	}

	// 种子驱动的维度：同一 seed 下必须稳定。
	add("canvasHash", want.CanvasHash, got.CanvasHash, true)
	add("audioHash", want.AudioHash, got.AudioHash, true)
	add("userAgent", want.UserAgent, got.UserAgent, true)
	add("platform", want.Platform, got.Platform, true)
	add("hardwareConcurrency",
		fmt.Sprint(want.HardwareConcurrency), fmt.Sprint(got.HardwareConcurrency), true)
	add("deviceMemory", fmt.Sprint(want.DeviceMemory), fmt.Sprint(got.DeviceMemory), true)

	// 参数驱动的维度：由 profile 配置决定，变化说明参数未生效。
	add("timezone", want.Timezone, got.Timezone, true)
	add("language", want.Language, got.Language, true)
	add("languages", strings.Join(want.Languages, ","), strings.Join(got.Languages, ","), true)

	// 宿主环境相关：换机器就会不同，不代表伪造出了问题。
	add("screenWidth", fmt.Sprint(want.ScreenWidth), fmt.Sprint(got.ScreenWidth), false)
	add("screenHeight", fmt.Sprint(want.ScreenHeight), fmt.Sprint(got.ScreenHeight), false)
	add("devicePixelRatio",
		fmt.Sprint(want.DevicePixelRatio), fmt.Sprint(got.DevicePixelRatio), false)
	add("webglVendor", want.WebGLVendor, got.WebGLVendor, false)
	add("webglRenderer", want.WebGLRenderer, got.WebGLRenderer, false)

	// webdriver 必须始终为 false，变成 true 是严重问题。
	add("webdriver", fmt.Sprint(want.Webdriver), fmt.Sprint(got.Webdriver), true)

	// 插件列表变化通常伴随内核大版本更新，不影响身份一致性。
	add("plugins", strings.Join(want.Plugins, ","), strings.Join(got.Plugins, ","), false)

	sort.SliceStable(out, func(i, j int) bool {
		// critical 排在前面，便于人工优先处理。
		return out[i].Critical && !out[j].Critical
	})
	return out
}

// CriticalDrifts 从差异列表中筛出会导致身份漂移的项。
func CriticalDrifts(drifts []Drift) []Drift {
	var out []Drift
	for _, d := range drifts {
		if d.Critical {
			out = append(out, d)
		}
	}
	return out
}

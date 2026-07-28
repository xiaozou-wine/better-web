package probe

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"better-web/internal/model"
)

// 报告内核实际派生出的 GPU 型号分布。
//
// 存在原因：常见的担心是"档案库过时了，会不会给我分配十年前的显卡"。
// 但内核 144 起 GPU 完全由种子派生，档案里的 GPU 字段不生效——所以真正
// 该核对的是内核自己的硬件参数集是否跟得上时代。这只能实测。
//
// 这是报告而非断言：GPU 型号的"新旧"没有客观阈值，真实人群里也确实有
// 老显卡在用。输出结果供人工判断。
func TestReportGPUModelFreshness(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}

	seen := map[string]int{}
	for i := 0; i < 10; i++ {
		seed := int32(1000 + i*7919)
		_, args := probeProfile(t, seed, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
		if err != nil {
			t.Fatalf("种子 %d 采集失败: %v", seed, err)
		}
		seen[res.WebGLRenderer]++
		t.Logf("种子 %-8d %s", seed, res.WebGLRenderer)
	}

	t.Logf("\n%d 次采集得到 %d 种不同的 GPU:", 10, len(seen))
	var oldGen []string
	for renderer, n := range seen {
		t.Logf("  ×%d  %s", n, renderer)
		if gen := nvidiaGeneration(renderer); gen > 0 && gen < 30 {
			oldGen = append(oldGen, renderer)
		}
	}
	if len(oldGen) > 0 {
		t.Logf("\n注意: 检出 %d 款 NVIDIA 20 系及更早的型号。真实人群中仍有这些卡，"+
			"但若占比过高说明内核的硬件参数集偏旧:", len(oldGen))
		for _, r := range oldGen {
			t.Logf("  %s", r)
		}
	}
}

// nvidiaGeneration 从 renderer 字符串里取 NVIDIA 显卡的代际数字。
// 例如 "RTX 4060" 返回 40，"GTX 1650" 返回 16。非 NVIDIA 或无法解析时返回 0。
func nvidiaGeneration(renderer string) int {
	if !strings.Contains(renderer, "NVIDIA") {
		return 0
	}
	m := regexp.MustCompile(`(?:RTX|GTX)\s+(\d{3,4})`).FindStringSubmatch(renderer)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	// 四位数取前两位（4060 → 40），三位数取前两位（960 → 96 视为老卡）。
	if n >= 1000 {
		return n / 100
	}
	return n / 10
}

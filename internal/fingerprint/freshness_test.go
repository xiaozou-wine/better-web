package fingerprint

import (
	"strconv"
	"strings"
	"testing"

	"better-web/internal/model"
)

// PlatformVersion 不是系统版本号，别按系统版本去改。
//
// 它是 UA-CH 的 platformVersion，在 Windows 上取自
// Windows.Foundation.UniversalApiContract 的版本，与"Windows 11 25H2"这类
// 系统版本号没有直接对应关系。微软给出的映射（learn.microsoft.com 的
// how-to-detect-win11）：
//
//	Win7/8/8.1        → 0
//	Win10 2004~21H2   → 10
//	Win11（全部版本）  → 13 及以上
//
// 已实测印证：本机 Windows 10 上真实 Chrome 148 报 platformVersion "10.0.0"。
// 所以档案里的 10.0.0 与 15.0.0 分别正确表示 Win10 与 Win11，不是"过时的
// 系统版本号"。要调整的是各值在档案库中的占比，而非把数字往上加。
//
// macOS 的 platformVersion 则直接是系统版本号（如 15.3.0 = Sequoia 15.3），
// 两个平台的语义不同，改动前务必分清。

// catalogReviewedAt 是档案库最近一次人工核对真实机型分布的日期。
//
// 存在原因：档案库里真正生效的字段（PlatformVersion、HardwareConcurrency）
// 描述的是"当下普通用户的机器长什么样"，这个事实会随时间漂移。声称一个
// 三年前的系统版本不致命，但整个库停留在同一个旧版本，多 profile 的版本
// 分布就不像真实人群，反而成为可聚类的特征。
//
// GPU、屏幕、内存不在此列：内核 148 起这些值由种子自行派生，档案里的值
// 不生效，过时也无影响。
// 2026-07 核对内容：Windows 的 10.0.0 / 15.0.0 经微软映射表与本机实测确认
// 正确（platformVersion 取自 UniversalApiContract，非系统版本号）；macOS 从
// 15.3.0 更新到 26.3.0（Apple 改按年份编号）；补齐 Windows 6 核与 Win10 12 核
// 两个缺失档位，以及 macOS 的 M4（10 核）与 M4 Max（16 核）。
const catalogReviewedAt = "2026-07"

// 期望的版本下限，需随真实人群分布更新。
const (
	// Windows 11 的 platformVersion 从 13 起。低于 13 且不等于 10 的值
	// 意味着 Win8.1 或更早，那已不在合理的目标人群里。
	wantWindowsMajorMin = 13
	// macOS 的 platformVersion 是系统版本号。Apple 在 Sequoia（15）之后改为
	// 按年份编号，下一版直接是 26（Tahoe）——所以 16~25 是不存在的版本号，
	// 下限设 26 而非 16。据 TelemetryDeck 统计 26.3 在 2026-03 已占约 57%。
	wantMacOSMajorMin = 26
)

// win10Share 是 Windows 10 在 Windows 桌面人群中的实际占比（2026-07 前后约 26%）。
// 档案库的 Win10 占比应与之大致相当——全是 Win11 会让整库偏离真实分布。
const win10Share = 0.26

// 系统版本不得低于设定下限。
//
// 该测试会随时间自然变得需要维护——这是刻意的：档案库过时本身就该
// 触发一次人工复核，静默沿用旧数据比测试失败更糟。
func TestCatalogPlatformVersionsNotStale(t *testing.T) {
	for _, d := range deviceCatalog {
		if d.PlatformVersion == "" {
			continue
		}
		major, ok := platformMajor(t, d.PlatformVersion)
		if !ok {
			t.Errorf("档案 %q 的 PlatformVersion %q 无法解析主版本",
				d.Label, d.PlatformVersion)
			continue
		}
		switch d.Platform {
		case model.PlatformWindows:
			// 只有 10（Win10）与 13 以上（Win11）是合理取值。
			// 其余数字对应 Win8.1 及更早，已不在目标人群里。
			if major != 10 && major < wantWindowsMajorMin {
				t.Errorf("档案 %q 的 platformVersion %s 对应 Win8.1 或更早"+
					"（合理取值：10 表示 Win10，>=%d 表示 Win11）；"+
					"档案库上次核对于 %s",
					d.Label, d.PlatformVersion, wantWindowsMajorMin, catalogReviewedAt)
			}
		case model.PlatformMacOS:
			if major < wantMacOSMajorMin {
				t.Errorf("档案 %q 的 macOS 版本 %s 偏旧（期望 >= %d）；"+
					"档案库上次核对于 %s",
					d.Label, d.PlatformVersion, wantMacOSMajorMin, catalogReviewedAt)
			}
		}
	}
}

// Windows 档案里 Win10 与 Win11 的占比应贴近真实人群。
//
// 全是 Win11 或全是 Win10 都会让整库偏离实际分布——多 profile 的系统版本
// 分布本身就是可聚类的特征。2026-07 前后 StatCounter 的数据是
// Win11 约 73%、Win10 约 26%。
//
// 允许较宽的容差：档案库只有个位数条目，无法精确匹配百分比，
// 这里只拦住"一个都没有"这类明显偏离。
func TestCatalogWindowsVersionMix(t *testing.T) {
	var win10, win11 int
	for _, d := range deviceCatalog {
		if d.Platform != model.PlatformWindows {
			continue
		}
		switch major, _ := platformMajor(t, d.PlatformVersion); {
		case major == 10:
			win10++
		case major >= wantWindowsMajorMin:
			win11++
		}
	}
	total := win10 + win11
	if total == 0 {
		t.Fatal("没有 Windows 档案")
	}
	t.Logf("Windows 档案分布: Win10 %d 个、Win11 %d 个（真实人群约 %.0f%% / %.0f%%）",
		win10, win11, win10Share*100, (1-win10Share)*100)

	if win10 == 0 {
		t.Errorf("没有 Win10 档案，而真实人群中 Win10 仍占约 %.0f%%", win10Share*100)
	}
	if win11 == 0 {
		t.Errorf("没有 Win11 档案，而它是当前主流（约 %.0f%%）", (1-win10Share)*100)
	}
	// Win11 应当占多数，与真实分布一致。
	if win11 < win10 {
		t.Errorf("Win11 档案（%d）少于 Win10（%d），与真实人群分布相反", win11, win10)
	}
}

// platformMajor 解析 platformVersion 的首段。
func platformMajor(t *testing.T, v string) (int, bool) {
	t.Helper()
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.Split(v, ".")[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

// 核数分布应以主流档位为主。
//
// hardwareConcurrency 会实际生效，且高核数显眼：真实人群里 4/8 核占大头，
// 16 核以上是少数。若高核档案占比过半，多 profile 会整体偏离常见分布。
func TestCatalogCoreCountDistribution(t *testing.T) {
	var high, total int
	counts := map[int]int{}
	for _, d := range deviceCatalog {
		if d.HardwareConcurrency <= 0 {
			t.Errorf("档案 %q 的核数 %d 无效", d.Label, d.HardwareConcurrency)
			continue
		}
		total++
		counts[d.HardwareConcurrency]++
		if d.HardwareConcurrency >= 16 {
			high++
		}
	}
	if total == 0 {
		t.Fatal("档案库为空")
	}
	t.Logf("核数分布: %v（共 %d 个档案，>=16 核占 %d 个）", counts, total, high)

	// 阈值取 40%：高于此说明档案库整体偏高端，偏离主流人群。
	if pct := float64(high) / float64(total) * 100; pct > 40 {
		t.Errorf("高核数（>=16）档案占 %.0f%%，偏离真实人群分布；"+
			"应增补 4/8 核的主流机型", pct)
	}
	// 主流档位必须有覆盖，否则所有 profile 都落在少见的核数上。
	for _, want := range []int{4, 8} {
		if counts[want] == 0 {
			t.Errorf("缺少 %d 核档案，该档位在真实人群中占比很高", want)
		}
	}
}

// Label 只是给人看的标签，不构成技术断言。
//
// 档案名里的 GPU 型号在内核 148 上不会生效（GPU 由种子派生），
// 因此 Label 与实际上报值无关。本测试确保这一点在代码中有据可查，
// 避免有人依据 Label 推断浏览器会报什么 GPU。
func TestCatalogLabelIsNotAContract(t *testing.T) {
	for _, d := range deviceCatalog {
		if d.Label == "" {
			t.Error("存在无标签的档案")
		}
		// GPURenderer 是参考字段，与 Label 里的型号一致只是便于人工核对，
		// 两者都不会传给内核。
		if d.GPURenderer == "" {
			t.Errorf("档案 %q 缺少 GPURenderer（虽不生效，但需保持画像完整）", d.Label)
		}
	}
	t.Log("提醒: Label 与 GPURenderer 均不影响浏览器实际上报的 GPU，" +
		"实际值由 --fingerprint 种子派生，需用 internal/probe 实测确认")
}

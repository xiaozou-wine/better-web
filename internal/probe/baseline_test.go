package probe

import (
	"context"
	"os"
	"testing"

	"better-web/internal/model"
)

// baselineSeed 是采集基线所用的固定种子，与 TestRealKernelWriteBaseline 一致。
// 比对基线必须用同一种子，否则差异来自种子不同而非内核变化。
const baselineSeed int32 = 20260727

// baselineGeo 是采集基线所用的固定地理信息。
func baselineGeo() *model.Geo {
	return &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
}

// 当前内核的实测指纹必须与基线一致。
//
// 这是防漂移的自动化关口：内核升级后若 canvas/audio/UA 等种子驱动的维度
// 发生变化，说明已有 profile 的身份会漂移，等同于所有账号同时换了设备。
// 该测试失败不代表代码有 bug，而是提示必须评估升级影响并重新采集基线。
//
// 基线不存在时跳过（新内核版本尚未建立基线）。
func TestBaselineHasNotDrifted(t *testing.T) {
	k := realKernel(t)
	path := BaselinePath("testdata", k.Version)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("内核 %s 尚无基线，先运行 BW_WRITE_BASELINE=1 采集: %s", k.Version, path)
	}

	want, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("加载基线失败: %v", err)
	}

	_, args := probeProfile(t, baselineSeed, baselineGeo())
	got, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}

	drifts := CompareBaseline(want, got)
	for _, d := range drifts {
		t.Log(d)
	}
	if crit := CriticalDrifts(drifts); len(crit) > 0 {
		t.Errorf("检测到 %d 项身份漂移，已有 profile 的指纹已改变；"+
			"确认属于预期的内核变更后，重新采集基线", len(crit))
	}
}

// CompareBaseline 必须能区分身份漂移与环境差异：
// 把换台机器跑就会变的屏幕尺寸报成身份漂移，会掩盖真正的问题。
func TestCompareBaselineClassifiesDrift(t *testing.T) {
	base := Result{
		UserAgent: "UA/1", Platform: "Win32", CanvasHash: "aaa", AudioHash: "bbb",
		HardwareConcurrency: 8, DeviceMemory: 16,
		Timezone: "America/Los_Angeles", Language: "en-US",
		ScreenWidth: 1920, ScreenHeight: 1080, DevicePixelRatio: 1,
		WebGLRenderer: "ANGLE (Intel, A)",
	}

	t.Run("完全一致时无差异", func(t *testing.T) {
		if d := CompareBaseline(base, base); len(d) != 0 {
			t.Errorf("期望无差异，实际 %v", d)
		}
	})

	t.Run("canvas 变化是身份漂移", func(t *testing.T) {
		got := base
		got.CanvasHash = "zzz"
		crit := CriticalDrifts(CompareBaseline(base, got))
		if len(crit) != 1 || crit[0].Field != "canvasHash" {
			t.Errorf("期望 canvasHash 被判为身份漂移，实际 %v", crit)
		}
	})

	t.Run("屏幕尺寸变化不是身份漂移", func(t *testing.T) {
		got := base
		got.ScreenWidth = 2048
		got.ScreenHeight = 1152
		drifts := CompareBaseline(base, got)
		if len(drifts) != 2 {
			t.Fatalf("期望 2 项差异，实际 %v", drifts)
		}
		if len(CriticalDrifts(drifts)) != 0 {
			t.Error("屏幕尺寸取自宿主机，不应判为身份漂移")
		}
	})

	t.Run("WebGL 型号变化不是身份漂移", func(t *testing.T) {
		got := base
		got.WebGLRenderer = "ANGLE (NVIDIA, B)"
		if len(CriticalDrifts(CompareBaseline(base, got))) != 0 {
			t.Error("WebGL 型号由内核内部策略决定，不应判为身份漂移")
		}
	})

	t.Run("webdriver 变 true 是严重问题", func(t *testing.T) {
		got := base
		got.Webdriver = true
		crit := CriticalDrifts(CompareBaseline(base, got))
		if len(crit) != 1 || crit[0].Field != "webdriver" {
			t.Errorf("期望 webdriver 被判为身份漂移，实际 %v", crit)
		}
	})

	t.Run("身份漂移项排在前面", func(t *testing.T) {
		got := base
		got.ScreenWidth = 2048 // 非 critical
		got.CanvasHash = "zzz" // critical
		drifts := CompareBaseline(base, got)
		if len(drifts) < 2 {
			t.Fatalf("期望至少 2 项差异，实际 %v", drifts)
		}
		if !drifts[0].Critical {
			t.Errorf("身份漂移项应排在最前，实际首项为 %v", drifts[0])
		}
	})
}

// 基线文件必须能被正确读回，否则漂移检测形同虚设。
func TestBaselineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Result{
		UserAgent: "UA/1", CanvasHash: "abc", AudioHash: "def",
		HardwareConcurrency: 12, DeviceMemory: 32,
		Languages: []string{"en-US", "en;q=0.9"},
		Plugins:   []string{"PDF Viewer"},
	}
	path := BaselinePath(dir, "148.0.7778.215")
	if err := SaveBaseline(path, want); err != nil {
		t.Fatalf("SaveBaseline 失败: %v", err)
	}
	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline 失败: %v", err)
	}
	if d := CompareBaseline(want, got); len(d) != 0 {
		t.Errorf("往返后出现差异: %v", d)
	}
}

// 基线缺失时必须报错而非返回零值——零值会让比对全项通过，
// 制造出"没有漂移"的假象。
func TestLoadBaselineFailsWhenMissing(t *testing.T) {
	if _, err := LoadBaseline(BaselinePath(t.TempDir(), "nonexistent")); err == nil {
		t.Error("基线缺失时期望报错")
	}
}

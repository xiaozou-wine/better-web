package probe

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/kernel"
	"better-web/internal/launcher"
	"better-web/internal/model"
)

// realKernel 定位已安装的真实内核。未安装时跳过测试。
//
// 这组测试需要真实的 fingerprint-chromium 内核，默认在无内核环境下跳过，
// 因此不会阻塞常规测试。指定 BW_KERNEL_DIR 可覆盖内核目录。
func realKernel(t *testing.T) kernel.Kernel {
	t.Helper()
	root := os.Getenv("BW_KERNEL_DIR")
	if root == "" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			t.Skip("未设置 APPDATA 且未指定 BW_KERNEL_DIR，跳过真实内核测试")
		}
		root = filepath.Join(appData, "better-web", "kernels")
	}
	list, err := kernel.NewStore(root).List()
	if err != nil {
		t.Fatalf("枚举内核失败: %v", err)
	}
	if len(list) == 0 {
		t.Skipf("目录 %s 下没有已安装的内核，跳过真实内核测试", root)
	}
	return list[0]
}

// probeProfile 构造一个用于采集的指纹 profile 及其命令行参数。
// envInt 读取一个整数环境变量，未设置或非法时返回 0。
func envInt(t *testing.T, key string) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("环境变量 %s=%q 不是整数", key, v)
	}
	return n
}

// isPowerOfTwo 报告 v 是否为 2 的整数次幂（含 0.25、0.5 这类负指数）。
// deviceMemory 只会是 2 的幂，出现 6、12 这类值即为伪造痕迹。
func isPowerOfTwo(v float64) bool {
	if v <= 0 {
		return false
	}
	exp := math.Log2(v)
	return exp == math.Trunc(exp)
}

func probeProfile(t *testing.T, seed int32, geo *model.Geo) (model.Fingerprint, []string) {
	t.Helper()
	fp := fingerprint.Derive(seed, geo)
	p := &model.Profile{
		ID: "probe", Name: "probe", Kind: model.KindFingerprint,
		Seed: seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	return fp, args
}

// 指纹参数必须真正生效：这是整个项目的核心断言。
// 参数传对了但内核没照做，等于什么都没做。
func TestRealKernelAppliesFingerprint(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	fp, args := probeProfile(t, 20260727, geo)

	res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}

	// 时区必须与代理出口地一致，这是最容易被检测到的矛盾项。
	if res.Timezone != fp.Timezone {
		t.Errorf("时区 = %q, 期望 %q（--timezone 未生效）", res.Timezone, fp.Timezone)
	}
	// 洛杉矶为 UTC-8/-7，offset 应为正数 420 或 480（JS 的符号与 UTC 偏移相反）。
	if res.TimezoneOffset != 420 && res.TimezoneOffset != 480 {
		t.Errorf("时区偏移 = %d, 洛杉矶应为 420 或 480", res.TimezoneOffset)
	}
	if res.Language != fp.Locale {
		t.Errorf("语言 = %q, 期望 %q（--lang 未生效）", res.Language, fp.Locale)
	}
	if res.HardwareConcurrency != fp.Device.HardwareConcurrency {
		t.Errorf("CPU 核数 = %d, 期望 %d（--fingerprint-hardware-concurrency 未生效）",
			res.HardwareConcurrency, fp.Device.HardwareConcurrency)
	}
	// 声明的品牌应出现在 UA 中。
	if fp.Brand != "" && !strings.Contains(res.UserAgent, string(fp.Brand)) {
		t.Errorf("UA %q 中不含声明的品牌 %q", res.UserAgent, fp.Brand)
	}
	// 自动化特征必须不可见。
	if res.Webdriver {
		t.Error("navigator.webdriver 为 true，暴露了自动化特征")
	}
	// deviceMemory 没有对应命令行参数，由内核自行从种子选值上报，
	// 实测 148 会给出 8/16/32。这里只断言它是 2 的幂：非 2 的幂
	// （例如 6、12）一定是伪造痕迹。
	if res.DeviceMemory > 0 && !isPowerOfTwo(res.DeviceMemory) {
		t.Errorf("deviceMemory = %v 不是 2 的幂，暴露伪造痕迹", res.DeviceMemory)
	}
	// WebGL 不应退化成软件渲染：SwiftShader 本身就是强自动化信号。
	if strings.Contains(res.WebGLRenderer, "SwiftShader") {
		t.Errorf("WebGL renderer 为软件渲染 %q，采集环境不可信", res.WebGLRenderer)
	}

	t.Logf("采集结果: UA=%q platform=%q cores=%d mem=%v tz=%q lang=%q",
		res.UserAgent, res.Platform, res.HardwareConcurrency,
		res.DeviceMemory, res.Timezone, res.Language)
	t.Logf("WebGL: vendor=%q renderer=%q", res.WebGLVendor, res.WebGLRenderer)
	t.Logf("Canvas=%s Audio=%s", res.CanvasHash, res.AudioHash)
}

// 同一种子两次启动必须得到一致的指纹，否则平台侧会看到同一账号设备特征漂移。
func TestRealKernelFingerprintIsStableAcrossRuns(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "JP", Timezone: "Asia/Tokyo", Locale: "ja-JP"}

	var first Result
	for i := 0; i < 2; i++ {
		_, args := probeProfile(t, 987654321, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
		if err != nil {
			t.Fatalf("第 %d 次采集失败: %v", i+1, err)
		}
		if i == 0 {
			first = res
			continue
		}
		if res.CanvasHash != first.CanvasHash {
			t.Errorf("Canvas 指纹在两次运行间漂移: %s -> %s", first.CanvasHash, res.CanvasHash)
		}
		if res.AudioHash != first.AudioHash {
			t.Errorf("Audio 指纹在两次运行间漂移: %s -> %s", first.AudioHash, res.AudioHash)
		}
		if res.UserAgent != first.UserAgent {
			t.Errorf("UA 在两次运行间变化: %q -> %q", first.UserAgent, res.UserAgent)
		}
		if res.HardwareConcurrency != first.HardwareConcurrency {
			t.Errorf("CPU 核数在两次运行间变化: %d -> %d",
				first.HardwareConcurrency, res.HardwareConcurrency)
		}
	}
}

// 不同种子必须产出不同的 Canvas 指纹，否则多 profile 无法区分，
// 平台侧可以据此把多个账号关联成同一设备。
//
// 只断言 Canvas：实测内核 148 的 Audio 噪声离散度有限，24 个种子只产出
// 5 个不同取值（唯一率 21%，最大一组碰撞覆盖 7 个种子），因此 Audio 会在
// 种子间大量碰撞，不能作为 profile 的区分依据。
// 该取值空间似为内核固定行为，与种子数量无关；用 TestSeedCollisionRate 可复现。
func TestRealKernelDifferentSeedsDifferentFingerprints(t *testing.T) {
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/New_York", Locale: "en-US"}

	seen := map[string]int32{}
	for _, seed := range []int32{111, 222, 333, 444, 555} {
		_, args := probeProfile(t, seed, geo)
		res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
		if err != nil {
			t.Fatalf("种子 %d 采集失败: %v", seed, err)
		}
		if res.CanvasHash == "" {
			t.Fatalf("种子 %d 未采集到 Canvas 指纹", seed)
		}
		if prev, dup := seen[res.CanvasHash]; dup {
			t.Errorf("种子 %d 与 %d 产出相同的 Canvas 指纹 %s，profile 之间可被关联",
				seed, prev, res.CanvasHash)
		}
		seen[res.CanvasHash] = seed
		t.Logf("种子 %d: canvas=%s audio=%s cores=%d",
			seed, res.CanvasHash, res.AudioHash, res.HardwareConcurrency)
	}
}

// 采集基线并写入文件，供内核升级后比对指纹是否漂移。
// 设置 BW_WRITE_BASELINE=1 时才写文件。
func TestRealKernelWriteBaseline(t *testing.T) {
	if os.Getenv("BW_WRITE_BASELINE") != "1" {
		t.Skip("未设置 BW_WRITE_BASELINE=1，跳过基线写入")
	}
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	_, args := probeProfile(t, 20260727, geo)

	res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}
	path := filepath.Join("testdata", "baseline-"+k.Version+".json")
	if err := SaveBaseline(path, res); err != nil {
		t.Fatalf("写入基线失败: %v", err)
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	t.Logf("基线已写入 %s:\n%s", path, b)
}

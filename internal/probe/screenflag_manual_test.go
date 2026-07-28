package probe

import (
	"context"
	"os"
	"slices"
	"testing"
)

// 实测 --fingerprint-screen-width / -height / -device-scale-factor 是否生效。
//
// 背景：这三个开关在 fingerprint-chromium 144 的
// patches/extra/fingerprint/000-add-fingerprint-switches.patch 中已注册，
// 并在 render_process_host_impl.cc 里被转发给渲染进程，但仓库中没有任何
// patch 消费它们，README 的特性列表也未提及分辨率。因此需要实测判断
// 它们是真能用，还是只注册未实现的死开关。
//
//	BW_RUN_SCORE=1 go test -run TestScreenFingerprintFlags -timeout 600s -v ./internal/probe/
func TestScreenFingerprintFlags(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过屏幕参数实测")
	}
	k := realKernel(t)

	// 取一组明显不同于宿主机的值，便于分辨是否生效。
	// 宿主机实测为 2048x1152 / dpr 1.25。
	const wantW, wantH = 1366, 768
	const wantDPR = 1.0

	base := []string{
		"--fingerprint=20260727",
		"--fingerprint-platform=windows",
		"--timezone=America/New_York",
		"--lang=en-US",
		"--accept-lang=en-US,en;q=0.9",
	}

	cases := []struct {
		name  string
		extra []string
	}{
		{"不带屏幕参数", nil},
		{"带屏幕参数", []string{
			"--fingerprint-screen-width=1366",
			"--fingerprint-screen-height=768",
			"--fingerprint-device-scale-factor=1",
		}},
	}

	got := map[string]Result{}
	for _, c := range cases {
		res, err := (&Probe{ExecPath: k.ExecPath}).
			Collect(context.Background(), slices.Concat(base, c.extra))
		if err != nil {
			t.Fatalf("%s: 采集失败: %v", c.name, err)
		}
		got[c.name] = res
		t.Logf("%s: screen=%dx%d dpr=%v", c.name,
			res.ScreenWidth, res.ScreenHeight, res.DevicePixelRatio)
	}

	withFlags := got["带屏幕参数"]
	if withFlags.ScreenWidth == wantW && withFlags.ScreenHeight == wantH {
		t.Logf("结论: 屏幕参数生效，可用于伪造分辨率")
		if withFlags.DevicePixelRatio != wantDPR {
			t.Logf("注意: devicePixelRatio = %v，未跟随 --fingerprint-device-scale-factor",
				withFlags.DevicePixelRatio)
		}
		return
	}

	// 未生效时对照两次结果，确认不是随机波动。
	noFlags := got["不带屏幕参数"]
	if withFlags.ScreenWidth == noFlags.ScreenWidth &&
		withFlags.ScreenHeight == noFlags.ScreenHeight {
		t.Logf("结论: 屏幕参数未生效（两次均为宿主机真实值 %dx%d），"+
			"该开关只注册未实现，需自行打补丁",
			noFlags.ScreenWidth, noFlags.ScreenHeight)
		return
	}
	t.Errorf("结果不一致但也不等于指定值: 带参数 %dx%d, 不带参数 %dx%d",
		withFlags.ScreenWidth, withFlags.ScreenHeight,
		noFlags.ScreenWidth, noFlags.ScreenHeight)
}

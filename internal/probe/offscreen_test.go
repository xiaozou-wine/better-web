package probe

import (
	"os"
	"strings"
	"testing"
)

// 采集路径必须把窗口移出可视区域，跑分路径必须不移。
//
// 这两条要求方向相反，各有硬理由：
//   - 采集（probe.go / gpu.go）在用户操作时被动触发（导出 bundle、探测 GPU、
//     筛种子），一次筛种子最多启动 24 次浏览器。窗口可见会在用户面前连闪
//     二十几下。
//   - 跑分（score.go）反过来必须可见：窗口被遮挡或最小化时 Chromium 会节流
//     后台渲染与定时器，实测约 40% 的运行会卡住算不完评分。
//
// 用源码断言而非行为断言：这两处是命令行参数的取舍，跑一次真实浏览器
// 既慢又无法自动判断"窗口有没有闪"。源码检查能确切锁住意图。
func TestOffscreenFlagsAppliedToCollectionOnly(t *testing.T) {
	const offscreen = "--window-position=-32000,-32000"

	cases := []struct {
		file     string
		wantFlag bool
		reason   string
	}{
		{
			file:     "probe.go",
			wantFlag: true,
			reason:   "基线采集在用户操作时触发，窗口可见会闪",
		},
		{
			file:     "gpu.go",
			wantFlag: true,
			reason:   "GPU 探测与筛种子最多连启 24 次浏览器",
		},
		{
			file:     "score.go",
			wantFlag: false,
			reason: "跑分窗口必须可见：被遮挡时 Chromium 节流后台渲染，" +
				"实测约 40% 的运行会卡住算不完评分",
		},
	}

	for _, c := range cases {
		src, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", c.file, err)
		}
		got := strings.Contains(string(src), offscreen)
		if got != c.wantFlag {
			if c.wantFlag {
				t.Errorf("%s 缺少 %s——%s", c.file, offscreen, c.reason)
			} else {
				t.Errorf("%s 不应有 %s——%s", c.file, offscreen, c.reason)
			}
		}
	}
}

// 采集路径不得用 --headless 代替移出屏幕。
//
// headless 会让 WebGL 退化为 SwiftShader，采到的 renderer 与真实驱动无关，
// 而 GPU 探针存在的意义就是读真实驱动行为。实测已确认移出屏幕后
// honest 组仍报真实 RTX 2070、pixelHash 不变，说明这条路是对的。
func TestCollectionDoesNotUseHeadless(t *testing.T) {
	for _, f := range []string{"probe.go", "gpu.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		if strings.Contains(string(src), `"--headless`) {
			t.Errorf("%s 用了 --headless：那会让 WebGL 退化为 SwiftShader，"+
				"采到的 GPU 不是真实驱动的值", f)
		}
	}
}

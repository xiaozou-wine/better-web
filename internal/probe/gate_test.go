package probe

import (
	"strings"
	"testing"
)

// 门禁的价值在于"该拦的拦住"。这组测试全部离线，用构造的跑分结果验证判定，
// 因此可以进常规 CI。
func TestGatePassesOnKnownGoodBaseline(t *testing.T) {
	// 取自内核 148.0.7778.215 的实测值。
	results := []ScoreResult{
		{Site: "creepjs", Metrics: map[string]any{
			"rendered": true, "liesCount": 0.0, "headlessPct": 0.0,
			"stealthPct": 0.0, "likeHeadlessPct": 25.0,
		}},
		{Site: "browserscan", Metrics: map[string]any{
			"passed": true, "verdict": "Normal",
		}},
	}
	if v := DefaultGate.Check(results); len(v) != 0 {
		t.Errorf("已知良好的基线被拦下: %v", v)
	}
}

// lies 非零意味着伪造项之间互相矛盾，是必须拦的退化。
func TestGateBlocksNonZeroLies(t *testing.T) {
	results := []ScoreResult{{Site: "creepjs", Metrics: map[string]any{
		"rendered": true, "liesCount": 3.0, "headlessPct": 0.0, "stealthPct": 0.0,
		"lies": []any{"timezone", "canvas"},
	}}}
	v := DefaultGate.Check(results)
	if len(v) != 1 {
		t.Fatalf("期望 1 条违规, 实际 %d 条: %v", len(v), v)
	}
	if v[0].Metric != "liesCount" {
		t.Errorf("违规项 = %q", v[0].Metric)
	}
	// 违规信息要指出具体是哪些项，否则排查无从下手。
	if !strings.Contains(v[0].Reason, "timezone") {
		t.Errorf("违规原因未列出具体项: %q", v[0].Reason)
	}
}

func TestGateBlocksHeadlessAndStealth(t *testing.T) {
	cases := map[string]map[string]any{
		"headlessPct": {
			"rendered": true, "liesCount": 0.0, "headlessPct": 33.0, "stealthPct": 0.0,
		},
		"stealthPct": {
			"rendered": true, "liesCount": 0.0, "headlessPct": 0.0, "stealthPct": 20.0,
		},
	}
	for metric, m := range cases {
		t.Run(metric, func(t *testing.T) {
			v := DefaultGate.Check([]ScoreResult{{Site: "creepjs", Metrics: m}})
			if len(v) != 1 || v[0].Metric != metric {
				t.Errorf("期望拦下 %s, 实际 %v", metric, v)
			}
		})
	}
}

// 评分未渲染说明这次采集不可信，绝不能当成通过——那会让门禁形同虚设。
func TestGateBlocksUnrenderedScore(t *testing.T) {
	v := DefaultGate.Check([]ScoreResult{{Site: "creepjs", Metrics: map[string]any{
		"rendered": false, "liesCount": 0.0, "headlessPct": 0.0, "stealthPct": 0.0,
	}}})
	if len(v) != 1 || v[0].Metric != "rendered" {
		t.Errorf("未渲染的结果应被拦下, 实际 %v", v)
	}
}

// 采集失败同样要拦：拿不到数据就无法判断是否退化。
func TestGateBlocksCollectionFailure(t *testing.T) {
	v := DefaultGate.Check([]ScoreResult{
		{Site: "creepjs", Err: "跑分超时"},
	})
	if len(v) != 1 || v[0].Metric != "采集" {
		t.Errorf("采集失败应被拦下, 实际 %v", v)
	}
	if !strings.Contains(v[0].Reason, "超时") {
		t.Errorf("违规原因应包含失败详情: %q", v[0].Reason)
	}
}

func TestGateBlocksBrowserScanNotNormal(t *testing.T) {
	v := DefaultGate.Check([]ScoreResult{{Site: "browserscan", Metrics: map[string]any{
		"passed": false, "verdict": "Webdriver",
	}}})
	if len(v) != 1 || v[0].Metric != "verdict" {
		t.Errorf("非 Normal 判定应被拦下, 实际 %v", v)
	}
}

// 结论解析不出来时要拦：静默放过会让选择器失效变成"永远通过"。
func TestGateBlocksUnparsableVerdict(t *testing.T) {
	v := DefaultGate.Check([]ScoreResult{{Site: "browserscan", Metrics: map[string]any{
		"verdict": nil,
	}}})
	if len(v) != 1 {
		t.Fatalf("期望 1 条违规, 实际 %v", v)
	}
	if !strings.Contains(v[0].Reason, "选择器") {
		t.Errorf("违规原因应提示选择器问题: %q", v[0].Reason)
	}
}

// likeHeadless 受宿主机环境影响，不设硬门槛，只记录趋势。
func TestGateIgnoresLikeHeadless(t *testing.T) {
	v := DefaultGate.Check([]ScoreResult{{Site: "creepjs", Metrics: map[string]any{
		"rendered": true, "liesCount": 0.0, "headlessPct": 0.0, "stealthPct": 0.0,
		"likeHeadlessPct": 44.0,
	}}})
	if len(v) != 0 {
		t.Errorf("likeHeadless 不应触发门禁: %v", v)
	}
}

func TestGateAllowsRelaxedLimits(t *testing.T) {
	relaxed := Gate{MaxLies: 5, MaxHeadlessPct: 50, MaxStealthPct: 50}
	v := relaxed.Check([]ScoreResult{{Site: "creepjs", Metrics: map[string]any{
		"rendered": true, "liesCount": 4.0, "headlessPct": 20.0, "stealthPct": 10.0,
	}}})
	if len(v) != 0 {
		t.Errorf("放宽后的门槛不应拦下: %v", v)
	}
}

func TestViolationStringIsReadable(t *testing.T) {
	s := Violation{
		Site: "creepjs", Metric: "liesCount", Got: 2, Limit: 0, Reason: "有矛盾项",
	}.String()
	for _, want := range []string{"creepjs", "liesCount", "有矛盾项"} {
		if !strings.Contains(s, want) {
			t.Errorf("违规描述 %q 缺少 %q", s, want)
		}
	}
}
